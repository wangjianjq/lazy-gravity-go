package database

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	_ "modernc.org/sqlite"
)

var (
	db   *sql.DB
	dbMu sync.Mutex
)

// GetDB returns the current database connection. May be nil if not initialized.
func GetDB() *sql.DB {
	dbMu.Lock()
	defer dbMu.Unlock()
	return db
}

func InitDB(workspace string) error {
	dbMu.Lock()
	defer dbMu.Unlock()

	// Close any existing connection before re-initializing
	if db != nil {
		db.Close()
		db = nil
	}

	// Ensure workspace directory exists
	if err := os.MkdirAll(workspace, 0700); err != nil {
		return fmt.Errorf("failed to create workspace directory %s: %w", workspace, err)
	}

	dbPath := filepath.Join(workspace, "lazy-gravity.db")

	var err error
	db, err = sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("failed to open database at %s: %w", dbPath, err)
	}

	// Create necessary tables if they don't exist
	const createTableQuery = `
	CREATE TABLE IF NOT EXISTS workspaces (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		platform_id TEXT NOT NULL,
		channel_id TEXT NOT NULL,
		project_path TEXT NOT NULL,
		UNIQUE(platform_id, channel_id)
	);`

	_, err = db.Exec(createTableQuery)
	if err != nil {
		return fmt.Errorf("failed to create tables: %w", err)
	}

	log.Println("[Local DB] SQLite database initialized successfully at: ", dbPath)
	return nil
}

func CloseDB() {
	dbMu.Lock()
	defer dbMu.Unlock()
	if db != nil {
		db.Close()
		db = nil
	}
}
