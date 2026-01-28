package db

import (
	"testing"
)

func TestWALMode(t *testing.T) {
	// Create test database
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer db.Close()

	// Verify WAL mode is enabled
	var journalMode string
	err = db.conn.QueryRow("PRAGMA journal_mode").Scan(&journalMode)
	if err != nil {
		t.Fatalf("Failed to query journal_mode: %v", err)
	}

	// Note: In-memory databases don't support WAL, so we expect "memory"
	// For file-based databases, this should return "wal"
	if journalMode != "memory" && journalMode != "wal" {
		t.Errorf("Expected journal_mode to be 'memory' or 'wal', got: %s", journalMode)
	}

	// Verify busy timeout is set
	var busyTimeout int
	err = db.conn.QueryRow("PRAGMA busy_timeout").Scan(&busyTimeout)
	if err != nil {
		t.Fatalf("Failed to query busy_timeout: %v", err)
	}

	if busyTimeout != 5000 {
		t.Errorf("Expected busy_timeout to be 5000, got: %d", busyTimeout)
	}

	// Verify synchronous mode
	var syncMode int
	err = db.conn.QueryRow("PRAGMA synchronous").Scan(&syncMode)
	if err != nil {
		t.Fatalf("Failed to query synchronous: %v", err)
	}

	// 1 = NORMAL, which is what we set
	if syncMode != 1 && syncMode != 2 {
		t.Errorf("Expected synchronous to be 1 (NORMAL) or 2 (FULL), got: %d", syncMode)
	}

	// Verify cache size
	var cacheSize int
	err = db.conn.QueryRow("PRAGMA cache_size").Scan(&cacheSize)
	if err != nil {
		t.Fatalf("Failed to query cache_size: %v", err)
	}

	if cacheSize != -64000 {
		t.Errorf("Expected cache_size to be -64000, got: %d", cacheSize)
	}
}

func TestWALModeWithFile(t *testing.T) {
	// Create temporary file database to test WAL
	tmpDB := t.TempDir() + "/test.db"

	db, err := New(tmpDB)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer db.Close()

	// Verify WAL mode is enabled for file-based database
	var journalMode string
	err = db.conn.QueryRow("PRAGMA journal_mode").Scan(&journalMode)
	if err != nil {
		t.Fatalf("Failed to query journal_mode: %v", err)
	}

	if journalMode != "wal" {
		t.Errorf("Expected journal_mode to be 'wal' for file database, got: %s", journalMode)
	}
}
