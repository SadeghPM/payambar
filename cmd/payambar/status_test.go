package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{input: 0, want: "0 B"},
		{input: 1023, want: "1023 B"},
		{input: 1024, want: "1.0 KiB"},
		{input: 1536, want: "1.5 KiB"},
		{input: 1048576, want: "1.0 MiB"},
	}

	for _, tt := range tests {
		got := formatBytes(tt.input)
		if got != tt.want {
			t.Fatalf("formatBytes(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFormatTimestamp(t *testing.T) {
	if got := formatTimestamp(""); got != "n/a" {
		t.Fatalf("formatTimestamp(empty) = %q, want %q", got, "n/a")
	}

	const ts = "2026-02-18 10:00:00"
	if got := formatTimestamp(ts); got != ts {
		t.Fatalf("formatTimestamp(value) = %q, want %q", got, ts)
	}
}

func TestDirUsage(t *testing.T) {
	root := t.TempDir()

	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}

	file1 := filepath.Join(root, "file1.txt")
	if err := os.WriteFile(file1, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write file1: %v", err)
	}

	file2 := filepath.Join(nested, "file2.txt")
	if err := os.WriteFile(file2, []byte("go"), 0o644); err != nil {
		t.Fatalf("write file2: %v", err)
	}

	bytes, files, err := dirUsage(root)
	if err != nil {
		t.Fatalf("dirUsage returned error: %v", err)
	}

	if files != 2 {
		t.Fatalf("dirUsage files = %d, want 2", files)
	}
	if bytes != 7 {
		t.Fatalf("dirUsage bytes = %d, want 7", bytes)
	}
}

func TestParseStatusArgs(t *testing.T) {
	opts, err := parseStatusArgs([]string{"--json"})
	if err != nil {
		t.Fatalf("parseStatusArgs returned error: %v", err)
	}
	if !opts.JSON {
		t.Fatalf("parseStatusArgs JSON = false, want true")
	}

	if _, err := parseStatusArgs([]string{"--bad"}); err == nil {
		t.Fatalf("parseStatusArgs expected error for unknown flag")
	}
}

func TestPrintStatusJSON(t *testing.T) {
	status := appStatus{
		GeneratedAt:     time.Date(2026, 2, 18, 10, 0, 0, 0, time.UTC),
		Environment:     "development",
		Port:            "8080",
		DatabasePath:    "/tmp/payambar.db",
		FileStoragePath: "/tmp/uploads",
		Users:           3,
	}

	var out bytes.Buffer
	if err := printStatusJSON(&out, status); err != nil {
		t.Fatalf("printStatusJSON returned error: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("invalid JSON output: %v", err)
	}

	if payload["environment"] != "development" {
		t.Fatalf("unexpected environment: %#v", payload["environment"])
	}
}
