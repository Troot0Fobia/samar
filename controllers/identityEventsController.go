package controllers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"Troot0Fobia/samar/helpers"
	"Troot0Fobia/samar/initializers"
	"Troot0Fobia/samar/middleware"
	"Troot0Fobia/samar/models"
	"Troot0Fobia/samar/snapshot"

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

const identityEventsPageSize = 50

// applyIdentityEventFilters narrows a query by the status/trigger/outcome/
// minConfidence query params shared by the list and count endpoints.
func applyIdentityEventFilters(q *gorm.DB, c *gin.Context) *gorm.DB {
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
	return q
}

// GET /identity/events?status=&trigger=&outcome=&minConfidence=&offset=
// Returns {items: [...], total: N, nextOffset: N|null} — paged
// identityEventsPageSize at a time. With ?count=1 returns only {total: N}
// (used by the toolbar badge).
func GetIdentityEvents(c *gin.Context) {
	base := applyIdentityEventFilters(initializers.DB.Model(&models.IdentityEvent{}), c)

	var total int64
	if err := base.Count(&total).Error; err != nil {
		helpers.LogError("GetIdentityEvents: count error", "", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}
	if c.Query("count") == "1" {
		c.JSON(http.StatusOK, gin.H{"total": total})
		return
	}

	offset := 0
	if v, err := strconv.Atoi(c.Query("offset")); err == nil && v > 0 {
		offset = v
	}

	var events []models.IdentityEvent
	if err := applyIdentityEventFilters(initializers.DB, c).
		Preload("OldCamera").Preload("OldCamera.MaintainerRef").
		Preload("NewCamera").Preload("NewCamera.MaintainerRef").
		Order("id DESC").Limit(identityEventsPageSize).Offset(offset).
		Find(&events).Error; err != nil {
		helpers.LogError("GetIdentityEvents: db error", "", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}

	type listItem struct {
		ID              uint            `json:"ID"`
		TriggerType     string          `json:"TriggerType"`
		Confidence      string          `json:"Confidence"`
		ConfirmingRuns  int             `json:"ConfirmingRuns"`
		Status          string          `json:"Status"`
		Outcome         string          `json:"Outcome"`
		OldCamera       identityCamDTO  `json:"OldCamera"`
		NewCamera       *identityCamDTO `json:"NewCamera"`
		OldCameraOnline bool            `json:"OldCameraOnline"`
		SameCity        bool            `json:"SameCity"`
		CreatedAt       time.Time       `json:"CreatedAt"`
		ResolvedAt      *time.Time      `json:"ResolvedAt"`
	}

	items := make([]listItem, 0, len(events))
	for _, ev := range events {
		item := listItem{
			ID:              ev.ID,
			TriggerType:     ev.TriggerType,
			Confidence:      ev.Confidence,
			ConfirmingRuns:  ev.ConfirmingRuns,
			Status:          ev.Status,
			Outcome:         ev.Outcome,
			OldCamera:       toIdentityCamDTO(ev.OldCamera),
			OldCameraOnline: ev.OldCamera.ID != 0 && !oldCameraOffline(ev.OldCamera),
			CreatedAt:       ev.CreatedAt,
			ResolvedAt:      ev.ResolvedAt,
		}
		if ev.NewCamera != nil {
			dto := toIdentityCamDTO(*ev.NewCamera)
			item.NewCamera = &dto
			item.SameCity = ev.OldCamera.CityID != nil && ev.NewCamera.CityID != nil && *ev.OldCamera.CityID == *ev.NewCamera.CityID
		}
		items = append(items, item)
	}

	var nextOffset *int
	if int64(offset+len(items)) < total {
		n := offset + len(items)
		nextOffset = &n
	}

	c.JSON(http.StatusOK, gin.H{"items": items, "total": total, "nextOffset": nextOffset})
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
		Preload("OldCamera").Preload("OldCamera.MaintainerRef").
		Preload("NewCamera").Preload("NewCamera.MaintainerRef").
		First(&event, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "event not found"})
		return
	}

	var oldSnap, newSnap identitySnapshotJSON
	json.Unmarshal([]byte(event.OldIdentitySnapshot), &oldSnap)
	json.Unmarshal([]byte(event.NewIdentitySnapshot), &newSnap)

	// Camera records go out as the same credential-free DTO the list endpoint
	// uses — the review UI only needs name/ip/port/maintainer, never login or
	// password.
	var newCamDTO *identityCamDTO
	if event.NewCamera != nil {
		dto := toIdentityCamDTO(*event.NewCamera)
		newCamDTO = &dto
	}

	c.JSON(http.StatusOK, gin.H{
		"ID":              event.ID,
		"TriggerType":     event.TriggerType,
		"Confidence":      event.Confidence,
		"ConfirmingRuns":  event.ConfirmingRuns,
		"Status":          event.Status,
		"Outcome":         event.Outcome,
		"OldCamera":       toIdentityCamDTO(event.OldCamera),
		"NewCamera":       newCamDTO,
		"OldCameraOnline": event.OldCamera.ID != 0 && !oldCameraOffline(event.OldCamera),
		"OldIdentity":     oldSnap,
		"NewIdentity":     newSnap,
		"CreatedAt":       event.CreatedAt,
		"ResolvedAt":      event.ResolvedAt,
	})
}

// validResolveOutcomes is the fixed (TriggerType, Outcome) lookup table —
// the only source of truth for what a resolve action is allowed to do.
// Nothing in this handler infers intent from live camera state (except the
// extra-confirmation gate on A_moved/moved — see ResolveIdentityEvent).
//
//   - A_moved/moved      → full replace: OldCamera keeps its id (and its own
//     duplicates) and takes over NewCamera's ip:port; NewCamera's history is
//     folded into OldCamera and NewCamera is deleted.
//   - A_moved/duplicate  → NewCamera becomes a duplicate of OldCamera (both
//     records kept), for a device genuinely reachable at two endpoints.
//   - A_moved/new_camera → false match: flag the serial/MAC unreliable.
//   - B_replaced/new_camera → nothing (identity row already recorded).
//   - C_offline/offline  → OldCamera marked invalid.
var validResolveOutcomes = map[string]map[string]bool{
	"A_moved":    {"moved": true, "duplicate": true, "new_camera": true},
	"B_replaced": {"new_camera": true},
	"C_offline":  {"offline": true},
}

// errEventNotPending is returned inside ResolveIdentityEvent's transaction
// when another request resolved the same event between the pre-check and the
// atomic status claim — mapped to 409 so a double-click can't run the
// camera-record side effects twice.
var errEventNotPending = errors.New("event no longer pending")

// errConfirmRequired is returned when A_moved/moved is requested for an
// OldCamera that isn't confirmed dead — a full replace on a still-live point
// is destructive, so the client must re-submit with {"confirm": true}.
var errConfirmRequired = errors.New("confirmation required")

// oldCameraOffline reports whether a camera's failure streak has reached the
// silence threshold — i.e. the point is confirmed dead, which is what makes
// an A_moved a real "move" rather than a dual endpoint.
func oldCameraOffline(cam models.Camera) bool {
	return cam.ConsecutiveFailures >= snapshot.IdentityFailureThreshold
}

// POST /identity/events/:id/resolve  body: {"outcome": "...", "confirm": bool}
func ResolveIdentityEvent(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	var body struct {
		Outcome string `json:"outcome"`
		Confirm bool   `json:"confirm"`
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

	var resolvedByID *uint
	var user models.User
	if err := db.Where("username = ?", username).First(&user).Error; err == nil {
		resolvedByID = &user.ID
	}
	now := time.Now()

	// A "moved" resolution deletes NewCamera and moves OldCamera onto its
	// address, so capture both address pairs now for the post-commit photo
	// relocation (see moveCameraPhotosForMerge).
	var oldAddr, newAddr [2]string
	if event.TriggerType == "A_moved" && body.Outcome == "moved" && event.NewCameraID != nil {
		var oc, nc models.Camera
		if db.Select("ip, port").First(&oc, event.OldCameraID).Error == nil &&
			db.Select("ip, port").First(&nc, *event.NewCameraID).Error == nil {
			oldAddr, newAddr = [2]string{oc.IP, oc.Port}, [2]string{nc.IP, nc.Port}
		}
	}

	// One transaction: claim the event first (UPDATE ... WHERE status =
	// 'pending'), then run the outcome's side effects. A concurrent resolve
	// that already flipped it makes the claim affect 0 rows, so the loser
	// bails with errEventNotPending before touching a single camera record.
	// Any error in the side effects rolls the whole tx back, restoring the
	// pending status.
	txErr := db.Transaction(func(tx *gorm.DB) error {
		claim := tx.Model(&models.IdentityEvent{}).
			Where("id = ? AND status = ?", event.ID, "pending").
			Updates(map[string]any{
				"status":         "resolved",
				"outcome":        body.Outcome,
				"resolved_by_id": resolvedByID,
				"resolved_at":    now,
			})
		if claim.Error != nil {
			return claim.Error
		}
		if claim.RowsAffected == 0 {
			return errEventNotPending
		}

		switch {
		case event.TriggerType == "A_moved" && body.Outcome == "moved":
			return resolveAMovedFull(tx, event, body.Confirm)

		case event.TriggerType == "A_moved" && body.Outcome == "duplicate":
			return resolveAMovedDuplicate(tx, event)

		case event.TriggerType == "A_moved" && body.Outcome == "new_camera":
			var oldSnap, newSnap identitySnapshotJSON
			json.Unmarshal([]byte(event.OldIdentitySnapshot), &oldSnap)
			json.Unmarshal([]byte(event.NewIdentitySnapshot), &newSnap)
			if oldSnap.Serial != "" && oldSnap.Serial == newSnap.Serial {
				markUnreliableIdentifier(tx, oldSnap.Serial, "serial", username, event.ID)
			}
			if oldSnap.MAC != "" && oldSnap.MAC == newSnap.MAC {
				markUnreliableIdentifier(tx, oldSnap.MAC, "mac", username, event.ID)
			}

		case event.TriggerType == "B_replaced" && body.Outcome == "new_camera":
			// DeviceIdentity row was already created at detection time — no further write needed.

		case event.TriggerType == "C_offline" && body.Outcome == "offline":
			if err := tx.Model(&models.Camera{}).Where("id = ?", event.OldCameraID).
				Updates(map[string]any{
					"status":               "invalid",
					"is_defined":           0,
					"consecutive_failures": 0,
				}).Error; err != nil {
				return fmt.Errorf("mark camera invalid: %w", err)
			}
		}
		return nil
	})

	switch {
	case txErr == nil:
		if oldAddr[0] != "" && newAddr[0] != "" {
			moveCameraPhotosForMerge(oldAddr[0], oldAddr[1], newAddr[0], newAddr[1], username)
		}
		helpers.LogSuccess(fmt.Sprintf("Identity event %d resolved as %s", event.ID, body.Outcome), username)
		c.JSON(http.StatusOK, gin.H{"ok": true, "outcome": body.Outcome})
	case errors.Is(txErr, errEventNotPending):
		c.JSON(http.StatusConflict, gin.H{"error": "event already resolved"})
	case errors.Is(txErr, errConfirmRequired):
		c.JSON(http.StatusConflict, gin.H{"error": "old camera still online — full replace needs confirmation", "requiresConfirm": true})
	case errors.Is(txErr, errOldCameraOffline):
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot mark a duplicate of an offline camera"})
	default:
		helpers.LogError("ResolveIdentityEvent: transaction failed", username, txErr.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
	}
}

// moveCameraPhotosForMerge relocates the surviving camera's pre-move photos
// after an A_moved/moved resolution. NewCamera's photos already live under
// data/photos/<newIP>/ and are picked up automatically once OldCamera sits at
// that address; OldCamera's own photos (still under data/photos/<oldIP>/) are
// re-addressed to the new ip:port and get a "_moved" suffix before the
// extension — e.g. "<oldIP>_<oldPort>_snap_ch0.jpg" becomes
// "<newIP>_<newPort>_snap_ch0_moved.jpg". The new prefix means the live
// gallery (GetCams matches "<ip>_<port>_" + ".jpg") still shows them; the
// suffix keeps them distinct from the new camera's own current snapshots.
// Best-effort and run after the DB commit — a partial move only leaves stale
// files, never inconsistent records.
func moveCameraPhotosForMerge(oldIP, oldPort, newIP, newPort, username string) {
	if oldIP == newIP && oldPort == newPort {
		return
	}
	srcDir := filepath.Join("data", "photos", oldIP)
	dstDir := filepath.Join("data", "photos", newIP)
	oldPrefix := fmt.Sprintf("%s_%s_", oldIP, oldPort)
	newPrefix := fmt.Sprintf("%s_%s_", newIP, newPort)

	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return // no photos for the old address
	}
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		helpers.LogError("moveCameraPhotosForMerge: mkdir", username, err.Error())
		return
	}
	moved := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), oldPrefix) {
			continue
		}
		rest := strings.TrimPrefix(e.Name(), oldPrefix) // e.g. "snap_ch0.jpg"
		ext := filepath.Ext(rest)
		stem := strings.TrimSuffix(rest, ext)
		newName := newPrefix + stem + "_moved" + ext
		if err := os.Rename(filepath.Join(srcDir, e.Name()), filepath.Join(dstDir, newName)); err != nil {
			helpers.LogError("moveCameraPhotosForMerge: rename", username, err.Error())
			continue
		}
		moved++
	}
	if remaining, err := os.ReadDir(srcDir); err == nil && len(remaining) == 0 {
		os.Remove(srcDir)
	}
	if moved > 0 {
		helpers.LogSuccess(fmt.Sprintf("A_moved: re-addressed %d photos %s:%s → %s:%s (_moved suffix)", moved, oldIP, oldPort, newIP, newPort), username)
	}
}

// resolveAMovedFull performs the "full replace" outcome: OldCamera keeps its
// id and takes over NewCamera's ip:port, NewCamera's dependent rows are
// re-parented onto OldCamera, and NewCamera is hard-deleted (freeing the
// unique (ip,port) index). Failure streaks and pending C_offline/B_replaced
// events for OldCamera are cleared so the triggers re-evaluate the point from
// scratch after the move.
func resolveAMovedFull(tx *gorm.DB, event models.IdentityEvent, confirm bool) error {
	if event.NewCameraID == nil {
		return fmt.Errorf("event %d missing new camera", event.ID)
	}
	oldID, newID := event.OldCameraID, *event.NewCameraID
	if oldID == newID {
		return fmt.Errorf("event %d: old and new camera are the same (%d)", event.ID, oldID)
	}

	var oldCam models.Camera
	if err := tx.Select("id, consecutive_failures").First(&oldCam, oldID).Error; err != nil {
		return fmt.Errorf("old camera %d not found: %w", oldID, err)
	}
	if !oldCameraOffline(oldCam) && !confirm {
		return errConfirmRequired
	}

	var newCam models.Camera
	if err := tx.Select("id, ip, port").First(&newCam, newID).Error; err != nil {
		return fmt.Errorf("new camera %d not found: %w", newID, err)
	}

	// Re-parent NewCamera's dependents onto OldCamera.
	if err := tx.Model(&models.DeviceIdentity{}).Where("camera_id = ?", newID).
		Update("camera_id", oldID).Error; err != nil {
		return fmt.Errorf("reparent device_identities: %w", err)
	}
	if err := tx.Model(&models.SnapshotResult{}).Where("camera_id = ?", newID).
		Update("camera_id", oldID).Error; err != nil {
		return fmt.Errorf("reparent snapshot_results: %w", err)
	}
	// NewCamera's own duplicates now point at OldCamera.
	if err := tx.Model(&models.Camera{}).Where("canonical_id = ?", newID).
		Update("canonical_id", oldID).Error; err != nil {
		return fmt.Errorf("reparent duplicates: %w", err)
	}
	// Other pending review items about NewCamera are moot once it's gone.
	if err := tx.Where("status = ? AND id != ? AND (old_camera_id = ? OR new_camera_id = ?)",
		"pending", event.ID, newID, newID).Delete(&models.IdentityEvent{}).Error; err != nil {
		return fmt.Errorf("drop pending events for new camera: %w", err)
	}
	// Resolved history referencing NewCamera stays, re-pointed onto OldCamera
	// (this event included — its new_camera_id becomes oldID once claimed).
	if err := tx.Model(&models.IdentityEvent{}).Where("old_camera_id = ?", newID).
		Update("old_camera_id", oldID).Error; err != nil {
		return fmt.Errorf("reparent event old_camera_id: %w", err)
	}
	if err := tx.Model(&models.IdentityEvent{}).Where("new_camera_id = ?", newID).
		Update("new_camera_id", oldID).Error; err != nil {
		return fmt.Errorf("reparent event new_camera_id: %w", err)
	}

	if err := tx.Unscoped().Delete(&models.Camera{}, newID).Error; err != nil {
		return fmt.Errorf("delete new camera: %w", err)
	}

	if err := tx.Model(&models.Camera{}).Where("id = ?", oldID).
		Updates(map[string]any{"ip": newCam.IP, "port": newCam.Port, "consecutive_failures": 0}).Error; err != nil {
		return fmt.Errorf("move old camera to new ip/port: %w", err)
	}

	// Pin OldCamera's current identity to the one observed at NewCamera as a
	// fresh (highest-id) DeviceIdentity row — but only if the row that's now
	// latest for OldCamera doesn't already carry it. NewCamera's migrated rows
	// are normally the newest already; this only matters if OldCamera's own
	// identity drifted between the event being raised and now, which would
	// otherwise make the next snapshot run re-fire B_replaced on the merged move.
	var newSnap identitySnapshotJSON
	if json.Unmarshal([]byte(event.NewIdentitySnapshot), &newSnap) == nil &&
		(newSnap.Serial != "" || newSnap.MAC != "") {
		var latest models.DeviceIdentity
		latestErr := tx.Where("camera_id = ?", oldID).Order("id DESC").First(&latest).Error
		if latestErr != nil || latest.SerialNumber != newSnap.Serial || latest.MACAddress != newSnap.MAC {
			now := time.Now()
			if err := tx.Create(&models.DeviceIdentity{
				CameraID:        oldID,
				SerialNumber:    newSnap.Serial,
				MACAddress:      newSnap.MAC,
				DeviceModel:     newSnap.Model,
				FirmwareVersion: newSnap.Firmware,
				FirstSeenAt:     now,
				LastConfirmedAt: now,
			}).Error; err != nil {
				return fmt.Errorf("pin moved identity: %w", err)
			}
		}
	}

	if err := clearPendingTriggersFor(tx, oldID); err != nil {
		return err
	}
	return nil
}

// errOldCameraOffline is returned when A_moved/duplicate is requested for an
// OldCamera that is already confirmed dead — a dead point can't be a live
// "dual endpoint", so that outcome only makes sense for an online OldCamera.
var errOldCameraOffline = errors.New("old camera is offline — only a full move applies")

// resolveAMovedDuplicate performs the "dual endpoint" outcome: NewCamera is
// kept and linked to OldCamera as a duplicate. Both cameras' failure streaks
// and pending C_offline/B_replaced events are cleared so the triggers keep
// evaluating both points. Only valid when OldCamera is still online.
func resolveAMovedDuplicate(tx *gorm.DB, event models.IdentityEvent) error {
	if event.NewCameraID == nil {
		return fmt.Errorf("event %d missing new camera", event.ID)
	}
	oldID, newID := event.OldCameraID, *event.NewCameraID

	var oldCam models.Camera
	if err := tx.Select("id, consecutive_failures").First(&oldCam, oldID).Error; err != nil {
		return fmt.Errorf("old camera %d not found: %w", oldID, err)
	}
	if oldCameraOffline(oldCam) {
		return errOldCameraOffline
	}

	if err := tx.Model(&models.Camera{}).Where("id = ?", newID).
		Updates(map[string]any{"canonical_id": oldID, "status": "duplicate", "consecutive_failures": 0}).Error; err != nil {
		return fmt.Errorf("mark new camera duplicate: %w", err)
	}
	if err := tx.Model(&models.Camera{}).Where("id = ?", oldID).
		Update("consecutive_failures", 0).Error; err != nil {
		return fmt.Errorf("reset old camera failures: %w", err)
	}
	if err := clearPendingTriggersFor(tx, oldID, newID); err != nil {
		return err
	}
	return nil
}

// clearPendingTriggersFor drops pending C_offline / B_replaced events for the
// given cameras (as OldCameraID) so, after a move/duplicate resolution, the
// detection triggers start over rather than leaving a stale "went silent" or
// "identity changed" item that now describes a superseded state.
func clearPendingTriggersFor(tx *gorm.DB, cameraIDs ...uint) error {
	return tx.Where("status = ? AND trigger_type IN ? AND old_camera_id IN ?",
		"pending", []string{"C_offline", "B_replaced"}, cameraIDs).
		Delete(&models.IdentityEvent{}).Error
}

// POST /identity/events/resolve_all_offline — bulk-resolves every pending
// C_offline event the same way ResolveIdentityEvent would one at a time:
// marks each old camera invalid/undefined and the event resolved with
// outcome "offline". Lets a moderator clear the whole silence backlog in one
// action instead of clicking through every event individually.
func ResolveAllOfflineEvents(c *gin.Context) {
	_, _, username := middleware.CheckAuth(c)
	db := initializers.DB

	var events []models.IdentityEvent
	if err := db.Where("trigger_type = ? AND status = ?", "C_offline", "pending").Find(&events).Error; err != nil {
		helpers.LogError("ResolveAllOfflineEvents: db error", username, err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}
	if len(events) == 0 {
		c.JSON(http.StatusOK, gin.H{"ok": true, "resolved": 0})
		return
	}

	cameraIDs := make([]uint, 0, len(events))
	eventIDs := make([]uint, 0, len(events))
	for _, ev := range events {
		cameraIDs = append(cameraIDs, ev.OldCameraID)
		eventIDs = append(eventIDs, ev.ID)
	}

	var resolvedByID *uint
	var user models.User
	if err := db.Where("username = ?", username).First(&user).Error; err == nil {
		resolvedByID = &user.ID
	}
	now := time.Now()

	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.Camera{}).Where("id IN ?", cameraIDs).
			Updates(map[string]any{"status": "invalid", "is_defined": 0, "consecutive_failures": 0}).Error; err != nil {
			return err
		}
		return tx.Model(&models.IdentityEvent{}).Where("id IN ?", eventIDs).
			Updates(map[string]any{
				"status":         "resolved",
				"outcome":        "offline",
				"resolved_by_id": resolvedByID,
				"resolved_at":    now,
			}).Error
	})
	if err != nil {
		helpers.LogError("ResolveAllOfflineEvents: failed to bulk resolve", username, err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}

	helpers.LogSuccess(fmt.Sprintf("Bulk-resolved %d C_offline events as offline", len(events)), username)
	c.JSON(http.StatusOK, gin.H{"ok": true, "resolved": len(events)})
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

	// Skipping a C_offline event means "this silence isn't real / not worth
	// acting on". Reset the camera's failure streak so detection re-arms: the
	// counter is edge-triggered at exactly identityFailureThreshold, so
	// without this it would climb past the threshold and never raise another
	// C_offline event even if the camera really does stay dark.
	err = db.Transaction(func(tx *gorm.DB) error {
		if event.TriggerType == "C_offline" {
			if e := tx.Model(&models.Camera{}).Where("id = ?", event.OldCameraID).
				Update("consecutive_failures", 0).Error; e != nil {
				return e
			}
		}
		return tx.Delete(&event).Error
	})
	if err != nil {
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
