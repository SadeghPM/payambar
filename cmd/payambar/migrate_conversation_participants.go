package main

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	_ "github.com/mattn/go-sqlite3"

	"github.com/4xmen/payambar/pkg/config"
)

type conversationParticipantsMigrationOptions struct {
	DatabasePath string
	DryRun       bool
}

type conversationParticipantRecord struct {
	ConversationID int64
	ParticipantIDs []int
}

type sqliteQueryer interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

func runMigrate(cfg *config.Config, out io.Writer, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing migration target (supported: conversation-participants)")
	}

	switch args[0] {
	case "conversation-participants":
		opts, err := parseConversationParticipantsMigrationArgs(cfg, args[1:])
		if err != nil {
			return err
		}
		return runConversationParticipantsMigration(out, opts)
	default:
		return fmt.Errorf("unknown migration target: %s", args[0])
	}
}

func parseConversationParticipantsMigrationArgs(cfg *config.Config, args []string) (conversationParticipantsMigrationOptions, error) {
	opts := conversationParticipantsMigrationOptions{DatabasePath: cfg.DatabasePath}

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--dry-run":
			opts.DryRun = true
		case "--database":
			i++
			if i >= len(args) || strings.TrimSpace(args[i]) == "" {
				return opts, fmt.Errorf("--database requires a path")
			}
			opts.DatabasePath = args[i]
		default:
			return opts, fmt.Errorf("unknown migration flag: %s", args[i])
		}
	}

	if strings.TrimSpace(opts.DatabasePath) == "" {
		return opts, fmt.Errorf("database path cannot be empty")
	}

	return opts, nil
}

func runConversationParticipantsMigration(out io.Writer, opts conversationParticipantsMigrationOptions) error {
	dbConn, err := sql.Open("sqlite3", opts.DatabasePath)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer dbConn.Close()

	if err := dbConn.Ping(); err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}

	if _, err := dbConn.Exec("BEGIN IMMEDIATE"); err != nil {
		return fmt.Errorf("failed to start migration transaction: %w", err)
	}
	inTx := true
	defer func() {
		if inTx {
			_, _ = dbConn.Exec("ROLLBACK")
		}
	}()

	hasLegacy, err := conversationsTableHasParticipantsColumn(dbConn)
	if err != nil {
		return fmt.Errorf("failed to inspect conversations schema: %w", err)
	}
	if !hasLegacy {
		if _, err := dbConn.Exec("COMMIT"); err != nil {
			return fmt.Errorf("failed to finish migration transaction: %w", err)
		}
		inTx = false
		fmt.Fprintln(out, "Conversation participants migration: already migrated (no legacy participants column).")
		return nil
	}

	records, conversationCount, participantCount, invalidConversationIDs, err := loadLegacyConversationParticipantRecords(dbConn)
	if err != nil {
		return err
	}
	if len(invalidConversationIDs) > 0 {
		sort.Slice(invalidConversationIDs, func(i, j int) bool { return invalidConversationIDs[i] < invalidConversationIDs[j] })
		return fmt.Errorf("invalid participants in conversation ids: %v", invalidConversationIDs)
	}

	if opts.DryRun {
		fmt.Fprintf(out, "Dry-run successful. Database: %s\n", opts.DatabasePath)
		fmt.Fprintf(out, "Would migrate %d conversations and %d conversation participants.\n", conversationCount, participantCount)
		if _, err := dbConn.Exec("ROLLBACK"); err != nil {
			return fmt.Errorf("failed to finish dry-run rollback: %w", err)
		}
		inTx = false
		return nil
	}

	if err := rebuildConversationSchema(dbConn); err != nil {
		return err
	}

	if err := backfillConversationParticipants(dbConn, records); err != nil {
		return err
	}

	if err := validateConversationMigration(dbConn, conversationCount, participantCount); err != nil {
		return err
	}

	if _, err := dbConn.Exec("COMMIT"); err != nil {
		return fmt.Errorf("failed to commit migration: %w", err)
	}
	inTx = false

	fmt.Fprintf(out, "Migration completed. Database: %s\n", opts.DatabasePath)
	fmt.Fprintf(out, "Migrated %d conversations and %d conversation participants.\n", conversationCount, participantCount)
	return nil
}

func ensureConversationParticipantsMigrated(databasePath string) error {
	if _, err := os.Stat(databasePath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to access database path: %w", err)
	}

	dbConn, err := sql.Open("sqlite3", databasePath)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer dbConn.Close()

	if err := dbConn.Ping(); err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}

	hasLegacy, err := conversationsTableHasParticipantsColumn(dbConn)
	if err != nil {
		return fmt.Errorf("failed to inspect conversations schema: %w", err)
	}
	if hasLegacy {
		return fmt.Errorf("legacy conversation schema detected. Run `payambar migrate conversation-participants --database %s` before starting server", databasePath)
	}

	return nil
}

func conversationsTableHasParticipantsColumn(q sqliteQueryer) (bool, error) {
	rows, err := q.Query("PRAGMA table_info(conversations)")
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name string
		var columnType string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return false, err
		}
		if name == "participants" {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}

	return false, nil
}

func loadLegacyConversationParticipantRecords(dbConn *sql.DB) ([]conversationParticipantRecord, int, int, []int64, error) {
	rows, err := dbConn.Query(`
		SELECT id, participants, created_at, updated_at
		FROM conversations
		ORDER BY id
	`)
	if err != nil {
		return nil, 0, 0, nil, fmt.Errorf("failed to read legacy conversations: %w", err)
	}
	defer rows.Close()

	records := make([]conversationParticipantRecord, 0)
	invalidConversationIDs := make([]int64, 0)
	totalParticipants := 0

	for rows.Next() {
		var conversationID int64
		var participantsRaw string
		var createdAt any
		var updatedAt any
		if err := rows.Scan(&conversationID, &participantsRaw, &createdAt, &updatedAt); err != nil {
			return nil, 0, 0, nil, fmt.Errorf("failed to scan legacy conversation: %w", err)
		}

		participantIDs, err := parseLegacyConversationParticipants(participantsRaw)
		if err != nil {
			invalidConversationIDs = append(invalidConversationIDs, conversationID)
			continue
		}

		records = append(records, conversationParticipantRecord{
			ConversationID: conversationID,
			ParticipantIDs: participantIDs,
		})
		totalParticipants += len(participantIDs)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, 0, nil, fmt.Errorf("failed while reading legacy conversations: %w", err)
	}

	return records, len(records), totalParticipants, invalidConversationIDs, nil
}

func parseLegacyConversationParticipants(raw string) ([]int, error) {
	parts := strings.Split(raw, ",")
	participantIDs := make([]int, 0, len(parts))
	seen := make(map[int]struct{})

	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}

		participantID, err := strconv.Atoi(trimmed)
		if err != nil || participantID <= 0 {
			return nil, fmt.Errorf("invalid participant id: %q", trimmed)
		}
		if _, exists := seen[participantID]; exists {
			continue
		}
		seen[participantID] = struct{}{}
		participantIDs = append(participantIDs, participantID)
	}

	if len(participantIDs) < 2 {
		return nil, fmt.Errorf("conversation must have at least two participants")
	}

	return participantIDs, nil
}

func rebuildConversationSchema(dbConn *sql.DB) error {
	if _, err := dbConn.Exec(`
		CREATE TABLE conversations_new (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		return fmt.Errorf("failed to create conversations_new table: %w", err)
	}

	if _, err := dbConn.Exec(`
		INSERT INTO conversations_new (id, created_at, updated_at)
		SELECT id, created_at, updated_at FROM conversations
	`); err != nil {
		return fmt.Errorf("failed to copy conversations data: %w", err)
	}

	if _, err := dbConn.Exec("DROP TABLE conversations"); err != nil {
		return fmt.Errorf("failed to drop legacy conversations table: %w", err)
	}

	if _, err := dbConn.Exec("ALTER TABLE conversations_new RENAME TO conversations"); err != nil {
		return fmt.Errorf("failed to rename conversations table: %w", err)
	}

	if _, err := dbConn.Exec("DROP TABLE IF EXISTS conversation_participants"); err != nil {
		return fmt.Errorf("failed to reset conversation_participants table: %w", err)
	}

	if _, err := dbConn.Exec(`
		CREATE TABLE conversation_participants (
			conversation_id INTEGER NOT NULL,
			user_id INTEGER NOT NULL,
			joined_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (conversation_id, user_id),
			FOREIGN KEY (conversation_id) REFERENCES conversations(id),
			FOREIGN KEY (user_id) REFERENCES users(id)
		)
	`); err != nil {
		return fmt.Errorf("failed to create conversation_participants table: %w", err)
	}

	if _, err := dbConn.Exec(`CREATE INDEX idx_conversation_participants_user_id ON conversation_participants(user_id)`); err != nil {
		return fmt.Errorf("failed to create idx_conversation_participants_user_id: %w", err)
	}

	if _, err := dbConn.Exec(`CREATE INDEX idx_conversation_participants_conversation_id ON conversation_participants(conversation_id)`); err != nil {
		return fmt.Errorf("failed to create idx_conversation_participants_conversation_id: %w", err)
	}

	return nil
}

func backfillConversationParticipants(dbConn *sql.DB, records []conversationParticipantRecord) error {
	stmt, err := dbConn.Prepare(`
		INSERT INTO conversation_participants (conversation_id, user_id)
		VALUES (?, ?)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare backfill statement: %w", err)
	}
	defer stmt.Close()

	for _, record := range records {
		for _, participantID := range record.ParticipantIDs {
			if _, err := stmt.Exec(record.ConversationID, participantID); err != nil {
				return fmt.Errorf("failed to insert participant mapping for conversation %d: %w", record.ConversationID, err)
			}
		}
	}

	return nil
}

func validateConversationMigration(dbConn *sql.DB, expectedConversationCount, expectedParticipantCount int) error {
	var conversationCount int
	if err := dbConn.QueryRow("SELECT COUNT(*) FROM conversations").Scan(&conversationCount); err != nil {
		return fmt.Errorf("failed to validate conversations count: %w", err)
	}
	if conversationCount != expectedConversationCount {
		return fmt.Errorf("conversation count mismatch after migration: got %d want %d", conversationCount, expectedConversationCount)
	}

	var participantCount int
	if err := dbConn.QueryRow("SELECT COUNT(*) FROM conversation_participants").Scan(&participantCount); err != nil {
		return fmt.Errorf("failed to validate participants count: %w", err)
	}
	if participantCount != expectedParticipantCount {
		return fmt.Errorf("participant count mismatch after migration: got %d want %d", participantCount, expectedParticipantCount)
	}

	hasLegacy, err := conversationsTableHasParticipantsColumn(dbConn)
	if err != nil {
		return fmt.Errorf("failed to validate conversations schema: %w", err)
	}
	if hasLegacy {
		return fmt.Errorf("legacy participants column still exists after migration")
	}

	return nil
}
