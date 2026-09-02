package controllers

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"Troot0Fobia/samar/models"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"),
		&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := db.AutoMigrate(
		&models.Country{}, &models.Region{}, &models.City{}, &models.Maintainer{},
		&models.Camera{}, &models.User{},
		&models.SnapshotRun{}, &models.SnapshotResult{},
		&models.DeviceIdentity{}, &models.IdentityEvent{}, &models.UnreliableIdentifier{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
	return db
}

func mkCam(t *testing.T, db *gorm.DB, ip, port string, failures int) models.Camera {
	t.Helper()
	cam := models.Camera{IP: ip, Port: port, Name: ip, Status: "valid", ConsecutiveFailures: failures}
	if err := db.Create(&cam).Error; err != nil {
		t.Fatalf("create camera %s:%s: %v", ip, port, err)
	}
	return cam
}

func TestOldCameraOffline(t *testing.T) {
	for _, tc := range []struct {
		failures int
		want     bool
	}{{0, false}, {2, false}, {3, true}, {5, true}} {
		if got := oldCameraOffline(models.Camera{ConsecutiveFailures: tc.failures}); got != tc.want {
			t.Errorf("oldCameraOffline(%d) = %v, want %v", tc.failures, got, tc.want)
		}
	}
}

func TestResolveAMovedFull(t *testing.T) {
	db := newTestDB(t)

	oldCam := mkCam(t, db, "10.0.0.5", "37777", 3) // confirmed offline
	newCam := mkCam(t, db, "10.0.0.99", "37777", 0)
	dupOfNew := mkCam(t, db, "10.0.0.99", "8000", 0)
	db.Model(&dupOfNew).Update("canonical_id", newCam.ID)

	db.Create(&models.DeviceIdentity{CameraID: newCam.ID, SerialNumber: "S-NEW"})
	db.Create(&models.SnapshotResult{RunID: 1, CameraID: newCam.ID})
	db.Create(&models.IdentityEvent{TriggerType: "B_replaced", OldCameraID: newCam.ID, Status: "pending"})
	db.Create(&models.IdentityEvent{TriggerType: "A_moved", OldCameraID: 999, NewCameraID: &newCam.ID, Status: "resolved"})

	newID := newCam.ID
	event := models.IdentityEvent{
		TriggerType: "A_moved", OldCameraID: oldCam.ID, NewCameraID: &newID,
		Status: "pending", NewIdentitySnapshot: `{"serial":"S-NEW"}`,
	}
	db.Create(&event)

	if err := db.Transaction(func(tx *gorm.DB) error {
		return resolveAMovedFull(tx, event, false)
	}); err != nil {
		t.Fatalf("resolveAMovedFull: %v", err)
	}

	// NewCamera is gone entirely.
	var cnt int64
	db.Unscoped().Model(&models.Camera{}).Where("id = ?", newID).Count(&cnt)
	if cnt != 0 {
		t.Errorf("new camera still present (count=%d)", cnt)
	}

	// OldCamera moved onto NewCamera's address, streak reset.
	var got models.Camera
	db.First(&got, oldCam.ID)
	if got.IP != "10.0.0.99" || got.Port != "37777" {
		t.Errorf("old camera address = %s:%s, want 10.0.0.99:37777", got.IP, got.Port)
	}
	if got.ConsecutiveFailures != 0 {
		t.Errorf("old camera ConsecutiveFailures = %d, want 0", got.ConsecutiveFailures)
	}

	// Dependents re-parented.
	db.Model(&models.DeviceIdentity{}).Where("camera_id = ? AND serial_number = ?", oldCam.ID, "S-NEW").Count(&cnt)
	if cnt < 1 {
		t.Errorf("device_identity not re-parented to old camera")
	}
	db.Model(&models.SnapshotResult{}).Where("camera_id = ?", oldCam.ID).Count(&cnt)
	if cnt != 1 {
		t.Errorf("snapshot_results re-parented count = %d, want 1", cnt)
	}
	db.First(&dupOfNew, dupOfNew.ID)
	if dupOfNew.CanonicalID == nil || *dupOfNew.CanonicalID != oldCam.ID {
		t.Errorf("duplicate camera canonical_id not re-pointed to old camera")
	}

	// Pending review item for the (gone) new camera dropped; resolved history re-pointed.
	db.Model(&models.IdentityEvent{}).Where("trigger_type = ? AND old_camera_id = ? AND status = ?", "B_replaced", newID, "pending").Count(&cnt)
	if cnt != 0 {
		t.Errorf("pending B_replaced for new camera not dropped")
	}
	db.Model(&models.IdentityEvent{}).Where("status = ? AND new_camera_id = ?", "resolved", newID).Count(&cnt)
	if cnt != 0 {
		t.Errorf("resolved event still references deleted new camera id")
	}

	// Identity pinned: latest row for old camera is the moved device.
	var latest models.DeviceIdentity
	db.Where("camera_id = ?", oldCam.ID).Order("id DESC").First(&latest)
	if latest.SerialNumber != "S-NEW" {
		t.Errorf("latest identity serial = %q, want S-NEW", latest.SerialNumber)
	}
}

func TestResolveAMovedFull_ConfirmGate(t *testing.T) {
	db := newTestDB(t)
	oldCam := mkCam(t, db, "10.0.0.5", "37777", 0) // still online
	newCam := mkCam(t, db, "10.0.0.99", "37777", 0)
	newID := newCam.ID
	event := models.IdentityEvent{TriggerType: "A_moved", OldCameraID: oldCam.ID, NewCameraID: &newID, Status: "pending"}
	db.Create(&event)

	err := db.Transaction(func(tx *gorm.DB) error { return resolveAMovedFull(tx, event, false) })
	if !errors.Is(err, errConfirmRequired) {
		t.Fatalf("without confirm on a live old camera: got %v, want errConfirmRequired", err)
	}
	// still there after the rolled-back tx
	var cnt int64
	db.Model(&models.Camera{}).Where("id = ?", newID).Count(&cnt)
	if cnt != 1 {
		t.Fatalf("new camera should survive a rejected resolve")
	}

	if err := db.Transaction(func(tx *gorm.DB) error { return resolveAMovedFull(tx, event, true) }); err != nil {
		t.Fatalf("with confirm=true: %v", err)
	}
	db.Unscoped().Model(&models.Camera{}).Where("id = ?", newID).Count(&cnt)
	if cnt != 0 {
		t.Fatalf("with confirm=true the new camera should be gone")
	}
}

func TestResolveAMovedDuplicate(t *testing.T) {
	db := newTestDB(t)
	oldCam := mkCam(t, db, "10.0.0.5", "37777", 1)
	newCam := mkCam(t, db, "10.0.0.99", "37777", 4)
	other := mkCam(t, db, "10.0.0.7", "37777", 0)
	newID := newCam.ID

	db.Create(&models.IdentityEvent{TriggerType: "C_offline", OldCameraID: oldCam.ID, Status: "pending"})
	db.Create(&models.IdentityEvent{TriggerType: "B_replaced", OldCameraID: newCam.ID, Status: "pending"})
	db.Create(&models.IdentityEvent{TriggerType: "C_offline", OldCameraID: other.ID, Status: "pending"})

	event := models.IdentityEvent{TriggerType: "A_moved", OldCameraID: oldCam.ID, NewCameraID: &newID, Status: "pending"}
	db.Create(&event)

	if err := db.Transaction(func(tx *gorm.DB) error { return resolveAMovedDuplicate(tx, event) }); err != nil {
		t.Fatalf("resolveAMovedDuplicate: %v", err)
	}

	db.First(&newCam, newID)
	if newCam.Status != "duplicate" || newCam.CanonicalID == nil || *newCam.CanonicalID != oldCam.ID {
		t.Errorf("new camera not linked as duplicate: status=%q canonical=%v", newCam.Status, newCam.CanonicalID)
	}
	if newCam.ConsecutiveFailures != 0 {
		t.Errorf("new camera streak not reset: %d", newCam.ConsecutiveFailures)
	}
	db.First(&oldCam, oldCam.ID)
	if oldCam.ConsecutiveFailures != 0 {
		t.Errorf("old camera streak not reset: %d", oldCam.ConsecutiveFailures)
	}

	// The helper clears C_offline/B_replaced only — the A_moved event itself is
	// resolved by ResolveIdentityEvent's claim step, not here.
	var cnt int64
	db.Model(&models.IdentityEvent{}).Where("status = ? AND trigger_type IN ? AND old_camera_id IN ?",
		"pending", []string{"C_offline", "B_replaced"}, []uint{oldCam.ID, newID}).Count(&cnt)
	if cnt != 0 {
		t.Errorf("pending triggers for old/new not cleared (count=%d)", cnt)
	}
	db.Model(&models.IdentityEvent{}).Where("status = ? AND old_camera_id = ?", "pending", other.ID).Count(&cnt)
	if cnt != 1 {
		t.Errorf("unrelated camera's pending event was touched")
	}
}

func TestResolveAMovedDuplicate_RejectsOfflineOld(t *testing.T) {
	db := newTestDB(t)
	oldCam := mkCam(t, db, "10.0.0.5", "37777", 3) // offline
	newCam := mkCam(t, db, "10.0.0.99", "37777", 0)
	newID := newCam.ID
	event := models.IdentityEvent{TriggerType: "A_moved", OldCameraID: oldCam.ID, NewCameraID: &newID, Status: "pending"}
	db.Create(&event)

	err := db.Transaction(func(tx *gorm.DB) error { return resolveAMovedDuplicate(tx, event) })
	if !errors.Is(err, errOldCameraOffline) {
		t.Fatalf("got %v, want errOldCameraOffline", err)
	}
}

func TestClearPendingTriggersFor(t *testing.T) {
	db := newTestDB(t)
	a, b, c := mkCam(t, db, "1.1.1.1", "1", 0), mkCam(t, db, "2.2.2.2", "2", 0), mkCam(t, db, "3.3.3.3", "3", 0)
	bID := b.ID
	db.Create(&models.IdentityEvent{TriggerType: "C_offline", OldCameraID: a.ID, Status: "pending"})
	db.Create(&models.IdentityEvent{TriggerType: "B_replaced", OldCameraID: a.ID, Status: "pending"})
	db.Create(&models.IdentityEvent{TriggerType: "A_moved", OldCameraID: a.ID, NewCameraID: &bID, Status: "pending"}) // must survive
	db.Create(&models.IdentityEvent{TriggerType: "C_offline", OldCameraID: a.ID, Status: "resolved"})                 // must survive
	db.Create(&models.IdentityEvent{TriggerType: "C_offline", OldCameraID: c.ID, Status: "pending"})                  // other camera

	if err := clearPendingTriggersFor(db, a.ID, b.ID); err != nil {
		t.Fatalf("clearPendingTriggersFor: %v", err)
	}

	var cnt int64
	db.Model(&models.IdentityEvent{}).Where("trigger_type IN ? AND status = ? AND old_camera_id = ?",
		[]string{"C_offline", "B_replaced"}, "pending", a.ID).Count(&cnt)
	if cnt != 0 {
		t.Errorf("pending C_offline/B_replaced for a not cleared (count=%d)", cnt)
	}
	db.Model(&models.IdentityEvent{}).Where("trigger_type = ? AND status = ?", "A_moved", "pending").Count(&cnt)
	if cnt != 1 {
		t.Errorf("A_moved event was cleared")
	}
	db.Model(&models.IdentityEvent{}).Where("status = ?", "resolved").Count(&cnt)
	if cnt != 1 {
		t.Errorf("resolved event was cleared")
	}
	db.Model(&models.IdentityEvent{}).Where("old_camera_id = ? AND status = ?", c.ID, "pending").Count(&cnt)
	if cnt != 1 {
		t.Errorf("other camera's event was cleared")
	}
}

func TestMoveCameraPhotosForMerge(t *testing.T) {
	root := t.TempDir()
	cwd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(cwd) })

	oldIP, oldPort := "10.0.0.5", "37777"
	newIP, newPort := "10.0.0.99", "8000"
	oldDir := filepath.Join("data", "photos", oldIP)
	newDir := filepath.Join("data", "photos", newIP)
	os.MkdirAll(oldDir, 0o755)
	os.MkdirAll(newDir, 0o755)

	// old camera's own photos + a co-located other camera's photo that must be left alone
	os.WriteFile(filepath.Join(oldDir, oldIP+"_"+oldPort+"_snap_ch0.jpg"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(oldDir, oldIP+"_"+oldPort+"_snap_ch1.jpg"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(oldDir, oldIP+"_554_snap_ch0.jpg"), []byte("x"), 0o644) // different port, keep
	// new camera already has photos at its address
	os.WriteFile(filepath.Join(newDir, newIP+"_"+newPort+"_snap_ch0.jpg"), []byte("x"), 0o644)

	moveCameraPhotosForMerge(oldIP, oldPort, newIP, newPort, "tester")

	np := newIP + "_" + newPort + "_"
	// re-addressed to the new prefix, "_moved" before the extension → picked up
	// by the gallery, distinct from the new camera's own current snapshot.
	for _, name := range []string{np + "snap_ch0_moved.jpg", np + "snap_ch1_moved.jpg", np + "snap_ch0.jpg"} {
		if _, err := os.Stat(filepath.Join(newDir, name)); err != nil {
			t.Errorf("expected %s in new dir: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(oldDir, oldIP+"_554_snap_ch0.jpg")); err != nil {
		t.Errorf("co-located other-camera photo should be untouched: %v", err)
	}
	if _, err := os.Stat(filepath.Join(newDir, oldIP+"_"+oldPort+"_snap_ch0.jpg")); err == nil {
		t.Errorf("old-addressed name should not appear in new dir")
	}
	if _, err := os.Stat(filepath.Join(oldDir, oldIP+"_"+oldPort+"_snap_ch0.jpg")); err == nil {
		t.Errorf("moved photo should be gone from old dir")
	}
}
