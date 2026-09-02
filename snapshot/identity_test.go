package snapshot

import (
	"testing"
	"time"

	"Troot0Fobia/samar/initializers"
	"Troot0Fobia/samar/models"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// withTestDB points initializers.DB at a fresh in-memory database for the
// duration of one test (detectIdentityChange reads the package global).
func withTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"),
		&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.AutoMigrate(
		&models.Country{}, &models.Region{}, &models.City{}, &models.Maintainer{},
		&models.Camera{}, &models.SnapshotRun{}, &models.SnapshotResult{},
		&models.DeviceIdentity{}, &models.IdentityEvent{}, &models.UnreliableIdentifier{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	prev := initializers.DB
	initializers.DB = db
	t.Cleanup(func() {
		initializers.DB = prev
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
	return db
}

func cam(t *testing.T, db *gorm.DB, ip string) models.Camera {
	t.Helper()
	c := models.Camera{IP: ip, Port: "37777", Name: ip, Status: "valid"}
	if err := db.Create(&c).Error; err != nil {
		t.Fatalf("create camera: %v", err)
	}
	return c
}

// TestDetectIdentityChange_FirstCaptureRaisesTriggerA covers the "moved onto a
// brand-new record" case: X has never been snapshotted, and its first-ever
// identity already belongs to another camera Y → trigger A, not a silent
// baseline.
func TestDetectIdentityChange_FirstCaptureRaisesTriggerA(t *testing.T) {
	db := withTestDB(t)
	y := cam(t, db, "10.0.0.5")
	x := cam(t, db, "10.0.0.99")
	db.Create(&models.DeviceIdentity{CameraID: y.ID, SerialNumber: "SER-1", LastConfirmedAt: time.Now()})

	(&Engine{}).detectIdentityChange(x, camResult{Serial: "SER-1", Connected: true}, 1)

	var ev models.IdentityEvent
	if err := db.Where("trigger_type = ?", "A_moved").First(&ev).Error; err != nil {
		t.Fatalf("expected an A_moved event: %v", err)
	}
	if ev.OldCameraID != y.ID || ev.NewCameraID == nil || *ev.NewCameraID != x.ID {
		t.Fatalf("A_moved links wrong cameras: old=%d new=%v (want old=%d new=%d)", ev.OldCameraID, ev.NewCameraID, y.ID, x.ID)
	}
}

// TestDetectIdentityChange_FirmwareOnlyChangeIsNoEvent covers #3: model/
// firmware drift without a serial/MAC change refreshes the row in place and
// raises nothing.
func TestDetectIdentityChange_FirmwareOnlyChangeIsNoEvent(t *testing.T) {
	db := withTestDB(t)
	z := cam(t, db, "10.0.0.7")
	db.Create(&models.DeviceIdentity{
		CameraID: z.ID, SerialNumber: "SER-Z", FirmwareVersion: "1.0",
		FirstSeenAt: time.Now(), LastConfirmedAt: time.Now(),
	})

	(&Engine{}).detectIdentityChange(z, camResult{Serial: "SER-Z", Firmware: "2.0", Connected: true}, 2)

	var rows []models.DeviceIdentity
	db.Where("camera_id = ?", z.ID).Find(&rows)
	if len(rows) != 1 {
		t.Fatalf("expected the row to be updated in place, got %d rows", len(rows))
	}
	if rows[0].FirmwareVersion != "2.0" {
		t.Fatalf("firmware not refreshed in place: %q", rows[0].FirmwareVersion)
	}
	var cnt int64
	db.Model(&models.IdentityEvent{}).Where("trigger_type = ?", "B_replaced").Count(&cnt)
	if cnt != 0 {
		t.Fatalf("firmware-only change raised %d B_replaced events, want 0", cnt)
	}
}

// TestDetectIdentityChange_SerialChangeRaisesTriggerB: a real serial change at
// the same point, with no cross-camera match, is a B_replaced.
func TestDetectIdentityChange_SerialChangeRaisesTriggerB(t *testing.T) {
	db := withTestDB(t)
	z := cam(t, db, "10.0.0.8")
	db.Create(&models.DeviceIdentity{
		CameraID: z.ID, SerialNumber: "SER-OLD",
		FirstSeenAt: time.Now(), LastConfirmedAt: time.Now(),
	})

	(&Engine{}).detectIdentityChange(z, camResult{Serial: "SER-NEW", Connected: true}, 3)

	var ev models.IdentityEvent
	if err := db.Where("trigger_type = ?", "B_replaced").First(&ev).Error; err != nil {
		t.Fatalf("expected a B_replaced event: %v", err)
	}
	if ev.OldCameraID != z.ID {
		t.Fatalf("B_replaced old_camera_id = %d, want %d", ev.OldCameraID, z.ID)
	}
}
