package initializers

import (
	"log"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

var DB *gorm.DB

func ConnectToDb() {
	var err error
	// WAL lets readers proceed while a write is in progress (default
	// rollback-journal mode takes an exclusive lock on the whole file for
	// every write, blocking all other DB access — camera cards, map data,
	// everything — until it's done). busy_timeout makes a write that does
	// contend with another write wait and retry instead of failing/hanging
	// unpredictably.
	DB, err = gorm.Open(sqlite.Open("database/database.db?_journal_mode=WAL&_busy_timeout=5000"), &gorm.Config{})
	if err != nil {
		log.Fatalf("Failed to connect to DB: %v\n", err)
	}

	// SQLite has no real use for a connection pool — concurrent connections
	// just contend for the same file lock. A single connection serialises
	// all access inside Go instead, which is the standard recommendation for
	// database/sql + SQLite.
	sqlDB, err := DB.DB()
	if err != nil {
		log.Fatalf("Failed to get underlying sql.DB: %v\n", err)
	}
	sqlDB.SetMaxOpenConns(1)
}
