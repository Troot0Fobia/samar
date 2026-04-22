package initializers

import (
	"log"
	"os"

	"Troot0Fobia/samar/models"
)

// IsDevelopment is set by InitEnv(), after .env is loaded.
// Do not read APP_ENV directly — it may not be loaded yet at package init time.
var IsDevelopment bool

func InitEnv() {
	IsDevelopment = os.Getenv("APP_ENV") != "production"
}

func SyncDatabase() {
	// Order matters: Country → Region → City → Camera (FK dependencies)
	if err := DB.AutoMigrate(
		&models.Country{},
		&models.Region{},
		&models.City{},
		&models.Maintainer{},
		&models.Camera{},
		&models.User{},
		&models.InviteToken{},
		&models.Session{},
		&models.Proxy{},
	); err != nil {
		log.Fatalf("AutoMigrate failed: %v", err)
	}
}
