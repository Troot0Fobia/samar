package controllers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"Troot0Fobia/samar/helpers"
	"Troot0Fobia/samar/initializers"
	"Troot0Fobia/samar/middleware"
	"Troot0Fobia/samar/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// identitySnapshotJSON mirrors snapshot.identitySnapshot's JSON shape — kept
// as a separate local type since IdentityEvent stores it as an opaque JSON
// string and this package doesn't import the snapshot package.
type identitySnapshotJSON struct {
	Model      string    `json:"model"`
	Serial     string    `json:"serial"`
	MAC        string    `json:"mac"`
	Firmware   string    `json:"firmware"`
	CapturedAt time.Time `json:"capturedAt"`
}

type identityCamDTO struct {
	ID         uint   `json:"ID"`
	Name       string `json:"Name"`
	IP         string `json:"IP"`
	Port       string `json:"Port"`
	Maintainer string `json:"Maintainer"`
	CityID     *uint  `json:"CityID"`
}

func toIdentityCamDTO(cam models.Camera) identityCamDTO {
	maintainer := ""
	if cam.MaintainerRef != nil {
		maintainer = cam.MaintainerRef.Name
	}
	return identityCamDTO{ID: cam.ID, Name: cam.Name, IP: cam.IP, Port: cam.Port, Maintainer: maintainer, CityID: cam.CityID}
}

// GET /identity/events?status=&trigger=&outcome=&minConfidence=
func GetIdentityEvents(c *gin.Context) {
	q := initializers.DB.
		Preload("OldCamera").Preload("OldCamera.MaintainerRef").
		Preload("NewCamera").Preload("NewCamera.MaintainerRef").
		Order("id DESC")

	if status := c.Query("status"); status != "" {
		q = q.Where("status = ?", status)
	}
	if trigger := c.Query("trigger"); trigger != "" {
		q = q.Where("trigger_type = ?", trigger)
	}
	if outcome := c.Query("outcome"); outcome != "" {
		q = q.Where("outcome = ?", outcome)
	}
	if minConfidence := c.Query("minConfidence"); minConfidence != "" {
		levels := map[string]int{"low": 0, "medium": 1, "high": 2}
		if lvl, ok := levels[minConfidence]; ok {
			allowed := make([]string, 0, 3)
			for name, l := range levels {
				if l >= lvl {
					allowed = append(allowed, name)
				}
			}
			q = q.Where("confidence IN ?", allowed)
		}
	}

	var events []models.IdentityEvent
	if err := q.Find(&events).Error; err != nil {
		helpers.LogError("GetIdentityEvents: db error", "", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}

	type listItem struct {
		ID             uint            `json:"ID"`
		TriggerType    string          `json:"TriggerType"`
		Confidence     string          `json:"Confidence"`
		ConfirmingRuns int             `json:"ConfirmingRuns"`
		Status         string          `json:"Status"`
		Outcome        string          `json:"Outcome"`
		OldCamera      identityCamDTO  `json:"OldCamera"`
		NewCamera      *identityCamDTO `json:"NewCamera"`
		SameCity       bool            `json:"SameCity"`
		CreatedAt      time.Time       `json:"CreatedAt"`
		ResolvedAt     *time.Time      `json:"ResolvedAt"`
	}

	result := make([]listItem, 0, len(events))
	for _, ev := range events {
		item := listItem{
			ID:             ev.ID,
			TriggerType:    ev.TriggerType,
			Confidence:     ev.Confidence,
			ConfirmingRuns: ev.ConfirmingRuns,
			Status:         ev.Status,
			Outcome:        ev.Outcome,
			OldCamera:      toIdentityCamDTO(ev.OldCamera),
			CreatedAt:      ev.CreatedAt,
			ResolvedAt:     ev.ResolvedAt,
		}
		if ev.NewCamera != nil {
			dto := toIdentityCamDTO(*ev.NewCamera)
			item.NewCamera = &dto
			item.SameCity = ev.OldCamera.CityID != nil && ev.NewCamera.CityID != nil && *ev.OldCamera.CityID == *ev.NewCamera.CityID
		}
		result = append(result, item)
	}

	c.JSON(http.StatusOK, result)
}

// GET /identity/events/:id
func GetIdentityEventDetail(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	var event models.IdentityEvent
	if err := initializers.DB.
		Preload("OldCamera").Preload("OldCamera.MaintainerRef").Preload("OldCamera.CityRef").Preload("OldCamera.Region").
		Preload("NewCamera").Preload("NewCamera.MaintainerRef").Preload("NewCamera.CityRef").Preload("NewCamera.Region").
		First(&event, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "event not found"})
		return
	}

	var oldSnap, newSnap identitySnapshotJSON
	json.Unmarshal([]byte(event.OldIdentitySnapshot), &oldSnap)
	json.Unmarshal([]byte(event.NewIdentitySnapshot), &newSnap)

	c.JSON(http.StatusOK, gin.H{
		"ID":             event.ID,
		"TriggerType":    event.TriggerType,
		"Confidence":     event.Confidence,
		"ConfirmingRuns": event.ConfirmingRuns,
		"Status":         event.Status,
		"Outcome":        event.Outcome,
		"OldCamera":      event.OldCamera,
		"NewCamera":      event.NewCamera,
		"OldIdentity":    oldSnap,
		"NewIdentity":    newSnap,
		"CreatedAt":      event.CreatedAt,
		"ResolvedAt":     event.ResolvedAt,
	})
}

// validResolveOutcomes is the fixed (TriggerType, Outcome) lookup table —
// the only source of truth for what a resolve action is allowed to do.
// Nothing in this handler infers intent from live camera state; the
// moderator's clicked outcome is the only input.
var validResolveOutcomes = map[string]map[string]bool{
	"A_moved":    {"moved": true, "new_camera": true},
	"B_replaced": {"new_camera": true},
	"C_offline":  {"offline": true},
}

// POST /identity/events/:id/resolve  body: {"outcome": "..."}
func ResolveIdentityEvent(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	var body struct {
		Outcome string `json:"outcome"`
	}
	_, _, username := middleware.CheckAuth(c)
	if err := c.ShouldBindBodyWithJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}

	db := initializers.DB

	var event models.IdentityEvent
	if err := db.First(&event, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "event not found"})
		return
	}
	if event.Status != "pending" {
		c.JSON(http.StatusConflict, gin.H{"error": "event already resolved"})
		return
	}
	if !validResolveOutcomes[event.TriggerType][body.Outcome] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid outcome for this trigger type"})
		return
	}

	switch {
	case event.TriggerType == "A_moved" && body.Outcome == "moved":
		if event.NewCameraID == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "event missing new camera"})
			return
		}
		var newCam models.Camera
		if err := db.Select("id, ip, port").First(&newCam, *event.NewCameraID).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "new camera not found"})
			return
		}
		if err := db.Model(&models.Camera{}).Where("id = ?", event.OldCameraID).
			Updates(map[string]any{"ip": newCam.IP, "port": newCam.Port}).Error; err != nil {
			helpers.LogError("ResolveIdentityEvent: failed to update old camera ip/port", username, err.Error())
			c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
			return
		}
		if err := db.Model(&models.Camera{}).Where("id = ?", *event.NewCameraID).
			Updates(map[string]any{"canonical_id": event.OldCameraID, "status": "duplicate"}).Error; err != nil {
			helpers.LogError("ResolveIdentityEvent: failed to mark new camera duplicate", username, err.Error())
			c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
			return
		}

	case event.TriggerType == "A_moved" && body.Outcome == "new_camera":
		var oldSnap, newSnap identitySnapshotJSON
		json.Unmarshal([]byte(event.OldIdentitySnapshot), &oldSnap)
		json.Unmarshal([]byte(event.NewIdentitySnapshot), &newSnap)
		if oldSnap.Serial != "" && oldSnap.Serial == newSnap.Serial {
			markUnreliableIdentifier(db, oldSnap.Serial, "serial", username, event.ID)
		}
		if oldSnap.MAC != "" && oldSnap.MAC == newSnap.MAC {
			markUnreliableIdentifier(db, oldSnap.MAC, "mac", username, event.ID)
		}

	case event.TriggerType == "B_replaced" && body.Outcome == "new_camera":
		// DeviceIdentity row was already created at detection time — no further write needed.

	case event.TriggerType == "C_offline" && body.Outcome == "offline":
		if err := db.Model(&models.Camera{}).Where("id = ?", event.OldCameraID).
			Update("status", "inactive").Error; err != nil {
			helpers.LogError("ResolveIdentityEvent: failed to mark camera inactive", username, err.Error())
			c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
			return
		}
	}

	var resolvedByID *uint
	var user models.User
	if err := db.Where("username = ?", username).First(&user).Error; err == nil {
		resolvedByID = &user.ID
	}
	now := time.Now()
	db.Model(&models.IdentityEvent{}).Where("id = ?", event.ID).Updates(map[string]any{
		"status":         "resolved",
		"outcome":        body.Outcome,
		"resolved_by_id": resolvedByID,
		"resolved_at":    now,
	})

	helpers.LogSuccess(fmt.Sprintf("Identity event %d resolved as %s", event.ID, body.Outcome), username)
	c.JSON(http.StatusOK, gin.H{"ok": true, "outcome": body.Outcome})
}

// DELETE /identity/events/:id — dismisses a pending event that turned out to
// be a detection bug/glitch, not a real discrepancy worth resolving. Unlike
// ResolveIdentityEvent this makes no camera-record writes at all; it just
// removes the queue entry (soft delete, per gorm.Model) so it stops
// cluttering the review queue.
func DeleteIdentityEvent(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	_, _, username := middleware.CheckAuth(c)
	db := initializers.DB

	var event models.IdentityEvent
	if err := db.First(&event, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "event not found"})
		return
	}
	if event.Status != "pending" {
		c.JSON(http.StatusConflict, gin.H{"error": "only pending events can be skipped"})
		return
	}

	if err := db.Delete(&event).Error; err != nil {
		helpers.LogError("DeleteIdentityEvent: db error", username, err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}

	helpers.LogSuccess(fmt.Sprintf("Identity event %d skipped/deleted", event.ID), username)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func markUnreliableIdentifier(db *gorm.DB, value, kind, username string, eventID uint) {
	var createdBy uint
	var user models.User
	if err := db.Where("username = ?", username).First(&user).Error; err == nil {
		createdBy = user.ID
	}
	db.Where("value = ? AND kind = ?", value, kind).FirstOrCreate(&models.UnreliableIdentifier{
		Value:       value,
		Kind:        kind,
		Reason:      fmt.Sprintf("false match on identity_event #%d", eventID),
		CreatedByID: createdBy,
	})
}
