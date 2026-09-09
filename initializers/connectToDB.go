package initializers

import (
	"log"
	"runtime"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// DB is the write handle: a single connection (SQLite serialises writers at
// the file level anyway, so more than one just adds lock contention) used for
// every write, for AutoMigrate, for the CLI subcommands, and for any read
// that hasn't been explicitly moved to DBRead.
//
// DBRead is the read handle: a pool of read-only connections against the same
// file. Under WAL any number of readers run concurrently with each other and
// with the single writer, so a slow SELECT on a hot page (camera list, map
// data, the Cinema SSE probe) no longer queues behind — or blocks — everything
// else. Only SELECT-only code paths may use it; it physically cannot write
// (mode=ro).
var (
	DB     *gorm.DB
	DBRead *gorm.DB
)

const dbPath = "database/database.db"

// writeDSN — WAL (readers proceed during a write, instead of the default
// rollback journal's whole-file exclusive lock), busy_timeout (a writer that
// still contends waits and retries rather than failing), _txlock=immediate (a
// read-then-write transaction takes the write lock at BEGIN, so it can't
// deadlock another writer on the lock upgrade), synchronous=NORMAL (safe under
// WAL, and much faster than FULL).
const writeDSN = dbPath + "?_journal_mode=WAL&_busy_timeout=5000&_txlock=immediate&_synchronous=NORMAL"

// readDSN — same file, opened read-only. journal_mode is a persistent
// property of the file (set by the write handle, which opens first), so it
// isn't set here.
const readDSN = dbPath + "?mode=ro&_busy_timeout=5000"

func ConnectToDb() {
	var err error

	DB, err = gorm.Open(sqlite.Open(writeDSN), &gorm.Config{})
	if err != nil {
		log.Fatalf("Failed to connect to DB (write): %v\n", err)
	}
	writeSQL, err := DB.DB()
	if err != nil {
		log.Fatalf("Failed to get underlying write sql.DB: %v\n", err)
	}
	writeSQL.SetMaxOpenConns(1)
	writeSQL.SetConnMaxIdleTime(time.Hour)

	DBRead, err = gorm.Open(sqlite.Open(readDSN), &gorm.Config{})
	if err != nil {
		log.Fatalf("Failed to connect to DB (read): %v\n", err)
	}
	readSQL, err := DBRead.DB()
	if err != nil {
		log.Fatalf("Failed to get underlying read sql.DB: %v\n", err)
	}
	readSQL.SetMaxOpenConns(min(max(runtime.GOMAXPROCS(0), 4), 12))
	readSQL.SetConnMaxIdleTime(time.Hour)
}

// Read returns the read-only handle for SELECT-only query paths. It falls back
// to the write handle when the read pool isn't configured — tests that assign
// initializers.DB directly keep working without also wiring DBRead.
func Read() *gorm.DB {
	if DBRead != nil {
		return DBRead
	}
	return DB
}
