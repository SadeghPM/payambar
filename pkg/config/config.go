package config

import (
	"os"
	"strconv"
)

type Config struct {
	Port            string
	Environment     string
	DatabasePath    string
	JWTSecret       string
	CORSOrigins     string
	MaxUploadSize   int64
	FileStoragePath string
	StunServers     string
	TurnServer      string
	TurnUsername    string
	TurnPassword    string
}

func Load() *Config {
	return &Config{
		Port:            getEnv("PORT", "8080"),
		Environment:     getEnv("ENVIRONMENT", "development"),
		DatabasePath:    getEnv("DATABASE_PATH", "./data/payambar.db"),
		JWTSecret:       getEnv("JWT_SECRET", "your-secret-key-change-in-production"),
		CORSOrigins:     getEnv("CORS_ORIGINS", "*"),
		MaxUploadSize:   parseInt64(getEnv("MAX_UPLOAD_SIZE", "10485760")), // 10MB default
		FileStoragePath: getEnv("FILE_STORAGE_PATH", "./data/uploads"),
		StunServers:     getEnv("STUN_SERVERS", "stun:stun.l.google.com:19302"),
		TurnServer:      getEnv("TURN_SERVER", ""),
		TurnUsername:    getEnv("TURN_USERNAME", ""),
		TurnPassword:    getEnv("TURN_PASSWORD", ""),
	}
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

func parseInt64(s string) int64 {
	val, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 10485760 // 10MB default
	}
	return val
}
