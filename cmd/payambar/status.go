package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/4xmen/payambar/pkg/config"
)

type appStatus struct {
	GeneratedAt     time.Time
	Environment     string
	Port            string
	DatabasePath    string
	FileStoragePath string
	Users           int64
	Conversations   int64
	Messages        int64
	UnreadMessages  int64
	Files           int64
	UploadedBytes   int64
	MessagesLast24h int64
	LatestMessageAt string
	DBSize          int64
	DBWALSize       int64
	DBSHMSize       int64
	UploadDirSize   int64
	UploadFileCount int64
	DBMetricsReady  bool
	DBWarning       string
	StorageWarnings []string
}

type statusOptions struct {
	JSON bool
}

func parseStatusArgs(args []string) (statusOptions, error) {
	opts := statusOptions{}
	for _, arg := range args {
		switch arg {
		case "--json", "-j":
			opts.JSON = true
		default:
			return opts, fmt.Errorf("unknown status flag: %s", arg)
		}
	}
	return opts, nil
}

func runStatus(cfg *config.Config, out io.Writer, args []string) error {
	opts, err := parseStatusArgs(args)
	if err != nil {
		return err
	}

	status := collectStatus(cfg)
	if opts.JSON {
		return printStatusJSON(out, status)
	}
	printStatus(out, status)
	return nil
}

func collectStatus(cfg *config.Config) appStatus {
	status := appStatus{
		GeneratedAt:     time.Now(),
		Environment:     cfg.Environment,
		Port:            cfg.Port,
		DatabasePath:    cfg.DatabasePath,
		FileStoragePath: cfg.FileStoragePath,
	}

	if size, err := fileSize(cfg.DatabasePath); err == nil {
		status.DBSize = size
	} else {
		status.StorageWarnings = append(status.StorageWarnings, fmt.Sprintf("database file: %v", err))
	}

	if size, err := fileSize(cfg.DatabasePath + "-wal"); err == nil {
		status.DBWALSize = size
	}

	if size, err := fileSize(cfg.DatabasePath + "-shm"); err == nil {
		status.DBSHMSize = size
	}

	if bytes, files, err := dirUsage(cfg.FileStoragePath); err == nil {
		status.UploadDirSize = bytes
		status.UploadFileCount = files
	} else {
		status.StorageWarnings = append(status.StorageWarnings, fmt.Sprintf("upload dir: %v", err))
	}

	if _, err := os.Stat(cfg.DatabasePath); err != nil {
		status.DBWarning = fmt.Sprintf("database unavailable: %v", err)
		return status
	}

	dbConn, err := sql.Open("sqlite3", cfg.DatabasePath)
	if err != nil {
		status.DBWarning = fmt.Sprintf("database unavailable: %v", err)
		return status
	}
	defer dbConn.Close()

	if err := dbConn.Ping(); err != nil {
		status.DBWarning = fmt.Sprintf("database unavailable: %v", err)
		return status
	}

	if status.Users, err = queryInt64(dbConn, "SELECT COUNT(*) FROM users"); err != nil {
		status.DBWarning = fmt.Sprintf("could not read database stats: %v", err)
		return status
	}

	if status.Conversations, err = queryInt64(dbConn, "SELECT COUNT(*) FROM conversations"); err != nil {
		status.DBWarning = fmt.Sprintf("could not read database stats: %v", err)
		return status
	}

	if status.Messages, err = queryInt64(dbConn, "SELECT COUNT(*) FROM messages"); err != nil {
		status.DBWarning = fmt.Sprintf("could not read database stats: %v", err)
		return status
	}

	if status.UnreadMessages, err = queryInt64(dbConn, "SELECT COUNT(*) FROM messages WHERE read_at IS NULL"); err != nil {
		status.DBWarning = fmt.Sprintf("could not read database stats: %v", err)
		return status
	}

	if status.Files, err = queryInt64(dbConn, "SELECT COUNT(*) FROM files"); err != nil {
		status.DBWarning = fmt.Sprintf("could not read database stats: %v", err)
		return status
	}

	if status.UploadedBytes, err = queryInt64(dbConn, "SELECT COALESCE(SUM(file_size), 0) FROM files"); err != nil {
		status.DBWarning = fmt.Sprintf("could not read database stats: %v", err)
		return status
	}

	if status.MessagesLast24h, err = queryInt64(dbConn, "SELECT COUNT(*) FROM messages WHERE datetime(created_at) >= datetime('now', '-1 day')"); err != nil {
		status.DBWarning = fmt.Sprintf("could not read database stats: %v", err)
		return status
	}

	if status.LatestMessageAt, err = queryString(dbConn, "SELECT COALESCE(MAX(created_at), '') FROM messages"); err != nil {
		status.DBWarning = fmt.Sprintf("could not read database stats: %v", err)
		return status
	}

	status.DBMetricsReady = true
	return status
}

func queryInt64(db *sql.DB, query string) (int64, error) {
	var value int64
	if err := db.QueryRow(query).Scan(&value); err != nil {
		return 0, err
	}
	return value, nil
}

func queryString(db *sql.DB, query string) (string, error) {
	var value string
	if err := db.QueryRow(query).Scan(&value); err != nil {
		return "", err
	}
	return value, nil
}

func fileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	if info.IsDir() {
		return 0, fmt.Errorf("%s is a directory", path)
	}
	return info.Size(), nil
}

func dirUsage(root string) (int64, int64, error) {
	var totalBytes int64
	var totalFiles int64

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		totalBytes += info.Size()
		totalFiles++
		return nil
	})
	if err != nil {
		return 0, 0, err
	}

	return totalBytes, totalFiles, nil
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func formatTimestamp(value string) string {
	if value == "" {
		return "n/a"
	}
	return value
}

func printStatus(out io.Writer, status appStatus) {
	totalDB := status.DBSize + status.DBWALSize + status.DBSHMSize

	fmt.Fprintln(out, "Payambar Status")
	fmt.Fprintf(out, "Generated at: %s\n", status.GeneratedAt.Format(time.RFC3339))
	fmt.Fprintf(out, "Environment : %s\n", status.Environment)
	fmt.Fprintf(out, "Port        : %s\n", status.Port)
	fmt.Fprintf(out, "Database    : %s\n", status.DatabasePath)
	fmt.Fprintf(out, "Uploads dir : %s\n", status.FileStoragePath)
	fmt.Fprintln(out)

	fmt.Fprintln(out, "Data")
	if status.DBMetricsReady {
		fmt.Fprintf(out, "  Users             : %d\n", status.Users)
		fmt.Fprintf(out, "  Conversations     : %d\n", status.Conversations)
		fmt.Fprintf(out, "  Messages          : %d\n", status.Messages)
		fmt.Fprintf(out, "  Unread messages   : %d\n", status.UnreadMessages)
		fmt.Fprintf(out, "  File records      : %d\n", status.Files)
		fmt.Fprintf(out, "  Uploaded bytes DB : %s\n", formatBytes(status.UploadedBytes))
		fmt.Fprintf(out, "  Messages last 24h : %d\n", status.MessagesLast24h)
		fmt.Fprintf(out, "  Latest message at : %s\n", formatTimestamp(status.LatestMessageAt))
	} else {
		fmt.Fprintln(out, "  Database metrics  : n/a")
	}
	fmt.Fprintln(out)

	fmt.Fprintln(out, "Storage")
	fmt.Fprintf(out, "  DB file       : %s\n", formatBytes(status.DBSize))
	fmt.Fprintf(out, "  DB WAL file   : %s\n", formatBytes(status.DBWALSize))
	fmt.Fprintf(out, "  DB SHM file   : %s\n", formatBytes(status.DBSHMSize))
	fmt.Fprintf(out, "  DB footprint  : %s\n", formatBytes(totalDB))
	fmt.Fprintf(out, "  Upload files  : %d\n", status.UploadFileCount)
	fmt.Fprintf(out, "  Upload size   : %s\n", formatBytes(status.UploadDirSize))

	if status.DBWarning != "" {
		fmt.Fprintln(out)
		fmt.Fprintf(out, "Warning: %s\n", status.DBWarning)
	}

	if len(status.StorageWarnings) > 0 {
		fmt.Fprintln(out)
		for _, warning := range status.StorageWarnings {
			fmt.Fprintf(out, "Warning: %s\n", warning)
		}
	}
}

func printStatusJSON(out io.Writer, status appStatus) error {
	payload := map[string]any{
		"generated_at":      status.GeneratedAt.Format(time.RFC3339),
		"environment":       status.Environment,
		"port":              status.Port,
		"database_path":     status.DatabasePath,
		"file_storage_path": status.FileStoragePath,
		"metrics_ready":     status.DBMetricsReady,
		"metrics": map[string]any{
			"users":              status.Users,
			"conversations":      status.Conversations,
			"messages":           status.Messages,
			"unread_messages":    status.UnreadMessages,
			"files":              status.Files,
			"uploaded_bytes_db":  status.UploadedBytes,
			"messages_last_24h":  status.MessagesLast24h,
			"latest_message_at":  formatTimestamp(status.LatestMessageAt),
			"uploaded_bytes_hum": formatBytes(status.UploadedBytes),
		},
		"storage": map[string]any{
			"db_file_bytes":      status.DBSize,
			"db_wal_bytes":       status.DBWALSize,
			"db_shm_bytes":       status.DBSHMSize,
			"db_footprint_bytes": status.DBSize + status.DBWALSize + status.DBSHMSize,
			"upload_dir_bytes":   status.UploadDirSize,
			"upload_file_count":  status.UploadFileCount,
			"db_file_hum":        formatBytes(status.DBSize),
			"db_wal_hum":         formatBytes(status.DBWALSize),
			"db_shm_hum":         formatBytes(status.DBSHMSize),
			"db_footprint_hum":   formatBytes(status.DBSize + status.DBWALSize + status.DBSHMSize),
			"upload_dir_hum":     formatBytes(status.UploadDirSize),
		},
		"warnings": map[string]any{
			"database": status.DBWarning,
			"storage":  status.StorageWarnings,
		},
	}

	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(payload)
}
