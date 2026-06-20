package models

import (
	"time"

	"gorm.io/gorm"
)

type SnapshotRun struct {
	gorm.Model
	Status         string `gorm:"default:'idle'"`
	TotalCameras   int
	ProcessedCount int
	SuccessCount   int
	ErrorCount     int
	LastCameraID   uint   // deprecated: resume now uses SnapshotResult lookup
	Workers        int    `gorm:"default:1"`
	Filter         string `gorm:"default:'known'"` // "known" | "unknown" | "all"
	StartedByID    uint
	FinishedAt     *time.Time
}

type SnapshotResult struct {
	gorm.Model
	RunID          uint
	CameraID       uint
	Camera         Camera `gorm:"foreignKey:CameraID"`
	UsedMethod     string // "dahua" | "rtsp"
	WasUnknown     bool   // camera wasn't in "known" filter category
	GeneratedLink  string // non-empty when snapshot came from a generated RTSP URL
	MaintainerName string // snapshot-time copy of cam.MaintainerRef.Name
	ErrorType      string
	ErrorMsg       string
	ChannelsFound  int
	ChannelsDone   int
	ChannelErrors  string // JSON: [{ch,errType,errMsg}]
	SnapsJSON      string // JSON: [filename, ...] — files saved in this run
	PrevSnapsJSON  string // JSON: [filename, ...] — prev files from this run
	ProcessedAt    time.Time
}
