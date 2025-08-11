package initializers

import (
	"Troot0Fobia/samar/models"
)

func SyncDatabase() {
	DB.AutoMigrate(
		&models.User{},
		&models.InviteToken{},
		&models.Session{},
		&models.Camera{},
		&models.Proxy{},
	)
}
