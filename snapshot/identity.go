package snapshot

import (
	"encoding/json"
	"time"

	"Troot0Fobia/samar/initializers"
	"Troot0Fobia/samar/models"

	"gorm.io/gorm"
)

// identityFailureThreshold is the number of consecutive connection failures
// (see updateFailures) before a camera's point is considered "gone silent"
// and a trigger-C IdentityEvent is raised. Resolved value, see
// DEVICE_IDENTITY_PLAN.md.
const identityFailureThreshold = 3

type identitySnapshot struct {
	Model      string    `json:"model"`
	Serial     string    `json:"serial"`
	MAC        string    `json:"mac"`
	Firmware   string    `json:"firmware"`
	CapturedAt time.Time `json:"capturedAt"` // when this identity was last confirmed on the camera
}

func marshalIdentitySnapshot(s identitySnapshot) string {
	b, err := json.Marshal(s)
	if err != nil {
		return ""
	}
	return string(b)
}

// detectIdentityChange is the read-only detection half of the device-identity
// feature. It writes only to Camera.ConsecutiveFailures (the one sanctioned
// automatic write — see DEVICE_IDENTITY_PLAN.md) and to the
// DeviceIdentity/IdentityEvent queue tables. It never touches any other
// Camera field (ip/port/status/canonical_id) — those mutations happen
// exclusively through the moderator resolution endpoint.
func (e *Engine) detectIdentityChange(cam models.Camera, result camResult, runID uint) {
	db := initializers.DB

	updateFailures(db, cam, result)

	hasIdentity := result.Serial != "" || result.MAC != "" || result.Model != "" || result.Firmware != ""
	if !hasIdentity {
		return
	}

	var latest models.DeviceIdentity
	foundErr := db.Where("camera_id = ?", cam.ID).Order("id DESC").First(&latest).Error
	now := time.Now()

	// A blank field this run means "we got no answer for it" (queryInfoStr
	// on Dahua silently swallows a per-field read error to ""; a Hikvision
	// XML field can likewise come back empty on a partial/odd response) —
	// not "the device's value is now blank." Carry the last known value
	// forward per-field so a transient single-field miss never reads as a
	// real identity change; only a fresh, non-empty, differing value does.
	effective := identitySnapshot{Serial: result.Serial, MAC: result.MAC, Model: result.Model, Firmware: result.Firmware, CapturedAt: now}
	if foundErr == nil {
		if effective.Serial == "" {
			effective.Serial = latest.SerialNumber
		}
		if effective.MAC == "" {
			effective.MAC = latest.MACAddress
		}
		if effective.Model == "" {
			effective.Model = latest.DeviceModel
		}
		if effective.Firmware == "" {
			effective.Firmware = latest.FirmwareVersion
		}
	}

	unchanged := foundErr == nil &&
		effective.Serial == latest.SerialNumber &&
		effective.MAC == latest.MACAddress &&
		effective.Model == latest.DeviceModel &&
		effective.Firmware == latest.FirmwareVersion

	if unchanged {
		db.Model(&models.DeviceIdentity{}).Where("id = ?", latest.ID).Update("last_confirmed_at", now)
		confirmMatchingEvent(db, cam, effective.Serial, effective.MAC)
		return
	}

	db.Create(&models.DeviceIdentity{
		CameraID:        cam.ID,
		SerialNumber:    effective.Serial,
		MACAddress:      effective.MAC,
		DeviceModel:     effective.Model,
		FirmwareVersion: effective.Firmware,
		FirstSeenAt:     now,
		LastConfirmedAt: now,
		SnapshotRunID:   &runID,
	})

	if foundErr != nil {
		// No prior row at all for this camera — first-ever capture, baseline only.
		return
	}

	newSnapJSON := marshalIdentitySnapshot(effective)

	if matched, ok := findCrossCameraMatch(db, cam.ID, effective.Serial, effective.MAC); ok {
		createTriggerAEvent(db, matched, cam, newSnapJSON, runID)
		return
	}

	oldSnapJSON := marshalIdentitySnapshot(identitySnapshot{
		Model: latest.DeviceModel, Serial: latest.SerialNumber, MAC: latest.MACAddress, Firmware: latest.FirmwareVersion,
		CapturedAt: latest.LastConfirmedAt,
	})
	createTriggerBEvent(db, cam, oldSnapJSON, newSnapJSON, runID)
}

// updateFailures increments Camera.ConsecutiveFailures only on a genuine
// no-response error ("timeout"/"network_error"); anything else (success,
// wrong_creds — the device answered — or the ambiguous camera_error bucket)
// resets it. Crossing the threshold raises a trigger-C event exactly once
// (edge-triggered), since each camera is only ever handled by one worker at
// a time within a run, there's no read-modify-write race here.
func updateFailures(db *gorm.DB, cam models.Camera, result camResult) {
	newVal := 0
	if result.ErrorType == "timeout" || result.ErrorType == "network_error" {
		newVal = cam.ConsecutiveFailures + 1
	}
	if newVal == cam.ConsecutiveFailures {
		return
	}
	db.Model(&models.Camera{}).Where("id = ?", cam.ID).Update("consecutive_failures", newVal)

	if newVal != identityFailureThreshold {
		return
	}
	var existing models.IdentityEvent
	err := db.Where("trigger_type = ? AND old_camera_id = ? AND status = ?", "C_offline", cam.ID, "pending").
		First(&existing).Error
	if err == nil {
		return
	}
	db.Create(&models.IdentityEvent{
		TriggerType:    "C_offline",
		Confidence:     "high", // deterministic once the threshold is hit — no identity match to be uncertain about
		ConfirmingRuns: 1,
		OldCameraID:    cam.ID,
		Status:         "pending",
	})
}

// findCrossCameraMatch looks for another camera whose latest DeviceIdentity
// row shares a serial or MAC with the just-observed identity. Only ever
// called when this camera's own identity just changed (or was captured for
// the first time), so this isn't run on every snapshot. Blank-value guards
// keep cameras with no captured identity (e.g. RTSP-only) from matching each
// other, and UnreliableIdentifier entries are excluded so a previously
// false-matched serial/MAC can't keep re-triggering trigger A.
func findCrossCameraMatch(db *gorm.DB, cameraID uint, serial, mac string) (models.DeviceIdentity, bool) {
	var matches []models.DeviceIdentity
	db.Raw(`
		SELECT di.* FROM device_identities di
		JOIN (SELECT camera_id, MAX(id) AS max_id FROM device_identities GROUP BY camera_id) latest
		  ON di.camera_id = latest.camera_id AND di.id = latest.max_id
		WHERE di.camera_id != ?
		  AND ( (? != '' AND di.serial_number = ? AND di.serial_number NOT IN (SELECT value FROM unreliable_identifiers WHERE kind = 'serial'))
		     OR (? != '' AND di.mac_address   = ? AND di.mac_address   NOT IN (SELECT value FROM unreliable_identifiers WHERE kind = 'mac')) )
		ORDER BY di.last_confirmed_at DESC LIMIT 5
	`, cameraID, serial, serial, mac, mac).Scan(&matches)
	if len(matches) == 0 {
		return models.DeviceIdentity{}, false
	}
	return matches[0], true
}

// createTriggerAEvent records "identity from camera `matched.CameraID` now
// observed at camera `cam.ID`" — a moved-camera candidate. Guarded against
// duplicates so repeated runs before manual resolution don't spam the queue.
func createTriggerAEvent(db *gorm.DB, matched models.DeviceIdentity, cam models.Camera, newSnapJSON string, runID uint) {
	var existing models.IdentityEvent
	err := db.Where("trigger_type = ? AND old_camera_id = ? AND new_camera_id = ? AND status = ?",
		"A_moved", matched.CameraID, cam.ID, "pending").First(&existing).Error
	if err == nil {
		return
	}
	oldSnapJSON := marshalIdentitySnapshot(identitySnapshot{
		Model: matched.DeviceModel, Serial: matched.SerialNumber, MAC: matched.MACAddress, Firmware: matched.FirmwareVersion,
		CapturedAt: matched.LastConfirmedAt,
	})
	newCameraID := cam.ID
	snapshotRunID := runID
	db.Create(&models.IdentityEvent{
		TriggerType:         "A_moved",
		Confidence:          "low",
		ConfirmingRuns:      1,
		OldCameraID:         matched.CameraID,
		NewCameraID:         &newCameraID,
		OldIdentitySnapshot: oldSnapJSON,
		NewIdentitySnapshot: newSnapJSON,
		NewSnapshotRunID:    &snapshotRunID,
		Status:              "pending",
	})
}

// createTriggerBEvent records "camera `cam.ID`'s identity changed at the
// same ip:port" — a hardware-replacement candidate. High confidence: this is
// a direct observation on a known point, not a probabilistic cross-camera
// match. Guarded against duplicates like trigger A.
func createTriggerBEvent(db *gorm.DB, cam models.Camera, oldSnapJSON, newSnapJSON string, runID uint) {
	var existing models.IdentityEvent
	err := db.Where("trigger_type = ? AND old_camera_id = ? AND status = ?", "B_replaced", cam.ID, "pending").
		First(&existing).Error
	if err == nil {
		return
	}
	newCameraID := cam.ID
	snapshotRunID := runID
	db.Create(&models.IdentityEvent{
		TriggerType:         "B_replaced",
		Confidence:          "high",
		ConfirmingRuns:      1,
		OldCameraID:         cam.ID,
		NewCameraID:         &newCameraID,
		OldIdentitySnapshot: oldSnapJSON,
		NewIdentitySnapshot: newSnapJSON,
		NewSnapshotRunID:    &snapshotRunID,
		Status:              "pending",
	})
}

// confirmMatchingEvent bumps ConfirmingRuns/Confidence on a pending A_moved
// event when this run re-observes the same identity that originally
// triggered it — the doc's confidence tiers mature with repeat confirmation
// without blocking visibility/resolution.
func confirmMatchingEvent(db *gorm.DB, cam models.Camera, serial, mac string) {
	var ev models.IdentityEvent
	err := db.Where("trigger_type = ? AND new_camera_id = ? AND status = ?", "A_moved", cam.ID, "pending").
		Order("id DESC").First(&ev).Error
	if err != nil {
		return
	}
	var newSnap, oldSnap identitySnapshot
	if json.Unmarshal([]byte(ev.NewIdentitySnapshot), &newSnap) != nil {
		return
	}
	if newSnap.Serial != serial || newSnap.MAC != mac {
		return // identity drifted again since the event was raised; leave it as-is
	}
	_ = json.Unmarshal([]byte(ev.OldIdentitySnapshot), &oldSnap)

	confirmingRuns := ev.ConfirmingRuns + 1
	confidence := "low"
	if confirmingRuns >= 2 {
		serialMatch := oldSnap.Serial != "" && oldSnap.Serial == newSnap.Serial
		macMatch := oldSnap.MAC != "" && oldSnap.MAC == newSnap.MAC
		var oldCam models.Camera
		db.Select("id, region_id").First(&oldCam, ev.OldCameraID)
		sameRegion := oldCam.RegionID == cam.RegionID
		if serialMatch && macMatch && sameRegion {
			confidence = "high"
		} else {
			confidence = "medium"
		}
	}
	db.Model(&models.IdentityEvent{}).Where("id = ?", ev.ID).
		Updates(map[string]any{"confirming_runs": confirmingRuns, "confidence": confidence})
}
