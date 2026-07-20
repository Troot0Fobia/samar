package models

import (
	"time"

	"gorm.io/gorm"
)

// DeviceIdentity is a deduplicated history of distinct identity states
// (serial/MAC/model/firmware) observed for a camera. A new row is only
// inserted when the observed identity actually differs from the camera's
// latest row; otherwise LastConfirmedAt is bumped in place.
type DeviceIdentity struct {
	gorm.Model
	CameraID        uint   `gorm:"index"`
	Camera          Camera `gorm:"foreignKey:CameraID"`
	SerialNumber    string `gorm:"index"`
	MACAddress      string `gorm:"index"`
	DeviceModel     string
	FirmwareVersion string
	FirstSeenAt     time.Time
	LastConfirmedAt time.Time
	SnapshotRunID   *uint // run that first captured this distinct state
}

// IdentityEvent is a manual-review queue entry created when a snapshot run
// detects an identity discrepancy. Resolution is always a moderator/admin
// action — see camerasController.go ResolveIdentityEvent for the fixed
// (TriggerType, Outcome) -> DB-write lookup table.
type IdentityEvent struct {
	gorm.Model
	TriggerType         string // "A_moved" | "B_replaced" | "C_offline"
	Confidence          string // "low" | "medium" | "high"
	ConfirmingRuns      int
	OldCameraID         uint
	OldCamera           Camera  `gorm:"foreignKey:OldCameraID"`
	NewCameraID         *uint   // nil only for C_offline
	NewCamera           *Camera `gorm:"foreignKey:NewCameraID"`
	OldIdentitySnapshot string  // JSON, frozen at detection time
	NewIdentitySnapshot string  // JSON, frozen at detection time
	NewSnapshotRunID    *uint
	Status              string // "pending" | "resolved"
	Outcome             string // "" | "moved" | "new_camera" | "offline"
	ResolvedByID        *uint
	ResolvedByUser      *User `gorm:"foreignKey:ResolvedByID"`
	ResolvedAt          *time.Time
}

// UnreliableIdentifier marks a serial/MAC value as unfit for future
// trigger-A auto-matching, set when a moderator resolves an A_moved event
// as "new_camera" (i.e. the identity match was a false positive / dual
// access point rather than a real move).
type UnreliableIdentifier struct {
	gorm.Model
	Value       string `gorm:"uniqueIndex:idx_unreliable_value_kind"`
	Kind        string `gorm:"uniqueIndex:idx_unreliable_value_kind"` // "serial" | "mac"
	Reason      string
	CreatedByID uint
}
