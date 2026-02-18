package main

import (
	"bytes"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/4xmen/payambar/pkg/config"
)

func createLegacyConversationDB(t *testing.T) string {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	dbConn, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	defer dbConn.Close()

	_, err = dbConn.Exec(`
		CREATE TABLE users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT UNIQUE NOT NULL,
			password_hash TEXT NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE conversations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			participants TEXT NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		t.Fatalf("failed to create legacy schema: %v", err)
	}

	_, err = dbConn.Exec(`
		INSERT INTO users (id, username, password_hash) VALUES (1, 'u1', 'x');
		INSERT INTO users (id, username, password_hash) VALUES (2, 'u2', 'x');
		INSERT INTO users (id, username, password_hash) VALUES (3, 'u3', 'x');
		INSERT INTO conversations (participants) VALUES ('1,2');
		INSERT INTO conversations (participants) VALUES ('2,3');
	`)
	if err != nil {
		t.Fatalf("failed to seed legacy data: %v", err)
	}

	return dbPath
}

func TestConversationParticipantsMigrationSuccess(t *testing.T) {
	dbPath := createLegacyConversationDB(t)

	var out bytes.Buffer
	err := runConversationParticipantsMigration(&out, conversationParticipantsMigrationOptions{DatabasePath: dbPath})
	if err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	if !strings.Contains(out.String(), "Migration completed") {
		t.Fatalf("expected completion output, got: %s", out.String())
	}

	dbConn, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("failed to open migrated database: %v", err)
	}
	defer dbConn.Close()

	hasLegacy, err := conversationsTableHasParticipantsColumn(dbConn)
	if err != nil {
		t.Fatalf("failed to inspect schema: %v", err)
	}
	if hasLegacy {
		t.Fatal("participants column should be removed after migration")
	}

	var conversations int
	if err := dbConn.QueryRow("SELECT COUNT(*) FROM conversations").Scan(&conversations); err != nil {
		t.Fatalf("failed to count conversations: %v", err)
	}
	if conversations != 2 {
		t.Fatalf("conversation count = %d, want 2", conversations)
	}

	var mappings int
	if err := dbConn.QueryRow("SELECT COUNT(*) FROM conversation_participants").Scan(&mappings); err != nil {
		t.Fatalf("failed to count participant mappings: %v", err)
	}
	if mappings != 4 {
		t.Fatalf("participant mapping count = %d, want 4", mappings)
	}
}

func TestConversationParticipantsMigrationIdempotent(t *testing.T) {
	dbPath := createLegacyConversationDB(t)

	if err := runConversationParticipantsMigration(&bytes.Buffer{}, conversationParticipantsMigrationOptions{DatabasePath: dbPath}); err != nil {
		t.Fatalf("first migration failed: %v", err)
	}

	var out bytes.Buffer
	if err := runConversationParticipantsMigration(&out, conversationParticipantsMigrationOptions{DatabasePath: dbPath}); err != nil {
		t.Fatalf("second migration should be idempotent, got error: %v", err)
	}
	if !strings.Contains(out.String(), "already migrated") {
		t.Fatalf("expected already migrated output, got: %s", out.String())
	}
}

func TestConversationParticipantsMigrationDryRun(t *testing.T) {
	dbPath := createLegacyConversationDB(t)

	var out bytes.Buffer
	err := runConversationParticipantsMigration(&out, conversationParticipantsMigrationOptions{
		DatabasePath: dbPath,
		DryRun:       true,
	})
	if err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	if !strings.Contains(out.String(), "Dry-run successful") {
		t.Fatalf("expected dry-run output, got: %s", out.String())
	}

	dbConn, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer dbConn.Close()

	hasLegacy, err := conversationsTableHasParticipantsColumn(dbConn)
	if err != nil {
		t.Fatalf("failed to inspect schema: %v", err)
	}
	if !hasLegacy {
		t.Fatal("dry-run should not modify legacy schema")
	}

	var tableExists int
	if err := dbConn.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'conversation_participants'`).Scan(&tableExists); err != nil {
		t.Fatalf("failed to inspect tables: %v", err)
	}
	if tableExists != 0 {
		t.Fatalf("dry-run should not create conversation_participants table")
	}
}

func TestConversationParticipantsMigrationInvalidData(t *testing.T) {
	dbPath := createLegacyConversationDB(t)

	dbConn, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	_, err = dbConn.Exec(`UPDATE conversations SET participants = '1,bad' WHERE id = 1`)
	dbConn.Close()
	if err != nil {
		t.Fatalf("failed to seed invalid data: %v", err)
	}

	err = runConversationParticipantsMigration(&bytes.Buffer{}, conversationParticipantsMigrationOptions{DatabasePath: dbPath})
	if err == nil {
		t.Fatal("expected migration to fail for invalid legacy data")
	}
	if !strings.Contains(err.Error(), "invalid participants in conversation ids") {
		t.Fatalf("unexpected error: %v", err)
	}

	dbConn, err = sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("failed to re-open database: %v", err)
	}
	defer dbConn.Close()

	hasLegacy, err := conversationsTableHasParticipantsColumn(dbConn)
	if err != nil {
		t.Fatalf("failed to inspect schema after failed migration: %v", err)
	}
	if !hasLegacy {
		t.Fatal("failed migration should leave legacy schema intact")
	}
}

func TestParseConversationParticipantsMigrationArgs(t *testing.T) {
	cfg := &config.Config{DatabasePath: "/tmp/default.db"}

	opts, err := parseConversationParticipantsMigrationArgs(cfg, []string{"--dry-run", "--database", "/tmp/override.db"})
	if err != nil {
		t.Fatalf("parse args failed: %v", err)
	}
	if !opts.DryRun {
		t.Fatal("expected dry-run to be true")
	}
	if opts.DatabasePath != "/tmp/override.db" {
		t.Fatalf("database path = %s, want /tmp/override.db", opts.DatabasePath)
	}

	if _, err := parseConversationParticipantsMigrationArgs(cfg, []string{"--database"}); err == nil {
		t.Fatal("expected error for missing --database value")
	}
	if _, err := parseConversationParticipantsMigrationArgs(cfg, []string{"--unknown"}); err == nil {
		t.Fatal("expected error for unknown flag")
	}
}

func TestEnsureConversationParticipantsMigrated(t *testing.T) {
	legacyDB := createLegacyConversationDB(t)

	err := ensureConversationParticipantsMigrated(legacyDB)
	if err == nil {
		t.Fatal("expected legacy schema check to fail")
	}
	if !strings.Contains(err.Error(), "migrate conversation-participants") {
		t.Fatalf("unexpected error: %v", err)
	}

	migratedDB := createLegacyConversationDB(t)
	if err := runConversationParticipantsMigration(&bytes.Buffer{}, conversationParticipantsMigrationOptions{DatabasePath: migratedDB}); err != nil {
		t.Fatalf("setup migration failed: %v", err)
	}
	if err := ensureConversationParticipantsMigrated(migratedDB); err != nil {
		t.Fatalf("expected migrated schema check to pass, got: %v", err)
	}
}

func TestRunCommandMigrateDryRun(t *testing.T) {
	dbPath := createLegacyConversationDB(t)
	cfg := &config.Config{DatabasePath: dbPath}

	if err := runCommand(cfg, []string{"migrate", "conversation-participants", "--dry-run"}); err != nil {
		t.Fatalf("runCommand migrate dry-run failed: %v", err)
	}
}
