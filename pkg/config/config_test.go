package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeEnvFile(t *testing.T, dir string, body string) string {
	t.Helper()
	path := filepath.Join(dir, "test.env")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("failed to write env file: %v", err)
	}
	return path
}

func TestLoadReadsExplicitEnvFile(t *testing.T) {
	for _, key := range []string{
		"PORT", "ENVIRONMENT", "DATABASE_PATH", "JWT_SECRET", "CORS_ORIGINS", "MAX_UPLOAD_SIZE",
		"FILE_STORAGE_PATH", "STUN_SERVERS", "TURN_SERVER", "TURN_USERNAME", "TURN_PASSWORD",
	} {
		_ = os.Unsetenv(key)
	}

	envPath := writeEnvFile(t, t.TempDir(), `
PORT=9090
ENVIRONMENT=production
DATABASE_PATH=/var/lib/payambar/payambar.db
JWT_SECRET=super-secret
CORS_ORIGINS=https://example.com
MAX_UPLOAD_SIZE=2048
FILE_STORAGE_PATH=/var/lib/payambar/uploads
STUN_SERVERS=stun:example.org:3478
TURN_SERVER=turn:example.org:3478
TURN_USERNAME=turn-user
TURN_PASSWORD=turn-pass
`)
	t.Setenv("PAYAMBAR_ENV_FILE", envPath)

	cfg := Load()

	if cfg.Port != "9090" {
		t.Fatalf("Port = %q, want %q", cfg.Port, "9090")
	}
	if cfg.Environment != "production" {
		t.Fatalf("Environment = %q, want %q", cfg.Environment, "production")
	}
	if cfg.DatabasePath != "/var/lib/payambar/payambar.db" {
		t.Fatalf("DatabasePath = %q", cfg.DatabasePath)
	}
	if cfg.FileStoragePath != "/var/lib/payambar/uploads" {
		t.Fatalf("FileStoragePath = %q", cfg.FileStoragePath)
	}
	if cfg.JWTSecret != "super-secret" {
		t.Fatalf("JWTSecret = %q", cfg.JWTSecret)
	}
	if cfg.CORSOrigins != "https://example.com" {
		t.Fatalf("CORSOrigins = %q", cfg.CORSOrigins)
	}
	if cfg.MaxUploadSize != 2048 {
		t.Fatalf("MaxUploadSize = %d, want 2048", cfg.MaxUploadSize)
	}
	if cfg.StunServers != "stun:example.org:3478" {
		t.Fatalf("StunServers = %q", cfg.StunServers)
	}
	if cfg.TurnServer != "turn:example.org:3478" {
		t.Fatalf("TurnServer = %q", cfg.TurnServer)
	}
	if cfg.TurnUsername != "turn-user" {
		t.Fatalf("TurnUsername = %q", cfg.TurnUsername)
	}
	if cfg.TurnPassword != "turn-pass" {
		t.Fatalf("TurnPassword = %q", cfg.TurnPassword)
	}
}

func TestLoadEnvVarOverridesEnvFile(t *testing.T) {
	for _, key := range []string{"PORT", "DATABASE_PATH", "FILE_STORAGE_PATH", "JWT_SECRET"} {
		_ = os.Unsetenv(key)
	}

	envPath := writeEnvFile(t, t.TempDir(), `
PORT=9090
DATABASE_PATH=/var/lib/payambar/payambar.db
FILE_STORAGE_PATH=/var/lib/payambar/uploads
JWT_SECRET=file-secret
`)
	t.Setenv("PAYAMBAR_ENV_FILE", envPath)
	t.Setenv("DATABASE_PATH", "/override.db")
	t.Setenv("PORT", "7777")

	cfg := Load()

	if cfg.Port != "7777" {
		t.Fatalf("Port = %q, want %q", cfg.Port, "7777")
	}
	if cfg.DatabasePath != "/override.db" {
		t.Fatalf("DatabasePath = %q, want %q", cfg.DatabasePath, "/override.db")
	}
	if cfg.FileStoragePath != "/var/lib/payambar/uploads" {
		t.Fatalf("FileStoragePath = %q", cfg.FileStoragePath)
	}
	if cfg.JWTSecret != "file-secret" {
		t.Fatalf("JWTSecret = %q", cfg.JWTSecret)
	}
}

func TestLoadFallsBackToDefaultsWhenNoEnvFile(t *testing.T) {
	for _, key := range []string{
		"PAYAMBAR_ENV_FILE", "PORT", "ENVIRONMENT", "DATABASE_PATH", "JWT_SECRET", "CORS_ORIGINS", "MAX_UPLOAD_SIZE",
		"FILE_STORAGE_PATH", "STUN_SERVERS", "TURN_SERVER", "TURN_USERNAME", "TURN_PASSWORD",
	} {
		_ = os.Unsetenv(key)
	}

	cfg := Load()

	if cfg.Port != "8080" {
		t.Fatalf("Port = %q, want %q", cfg.Port, "8080")
	}
	if cfg.DatabasePath != "./data/payambar.db" {
		t.Fatalf("DatabasePath = %q, want default", cfg.DatabasePath)
	}
	if cfg.FileStoragePath != "./data/uploads" {
		t.Fatalf("FileStoragePath = %q, want default", cfg.FileStoragePath)
	}
}
