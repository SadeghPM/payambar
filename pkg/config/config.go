package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
	VAPIDPublicKey  string
	VAPIDPrivateKey string
}

func Load() *Config {
	fileEnv := loadFileEnv()

	return &Config{
		Port:            getEnv(fileEnv, "PORT", "8080"),
		Environment:     getEnv(fileEnv, "ENVIRONMENT", "development"),
		DatabasePath:    getEnv(fileEnv, "DATABASE_PATH", "./data/payambar.db"),
		JWTSecret:       getEnv(fileEnv, "JWT_SECRET", "your-secret-key-change-in-production"),
		CORSOrigins:     getEnv(fileEnv, "CORS_ORIGINS", "*"),
		MaxUploadSize:   parseInt64(getEnv(fileEnv, "MAX_UPLOAD_SIZE", "10485760")), // 10MB default
		FileStoragePath: getEnv(fileEnv, "FILE_STORAGE_PATH", "./data/uploads"),
		StunServers:     getEnv(fileEnv, "STUN_SERVERS", "stun:stun.l.google.com:19302"),
		TurnServer:      getEnv(fileEnv, "TURN_SERVER", ""),
		TurnUsername:    getEnv(fileEnv, "TURN_USERNAME", ""),
		TurnPassword:    getEnv(fileEnv, "TURN_PASSWORD", ""),
		VAPIDPublicKey:  getEnv(fileEnv, "VAPID_PUBLIC_KEY", ""),
		VAPIDPrivateKey: getEnv(fileEnv, "VAPID_PRIVATE_KEY", ""),
	}
}

func getEnv(fileEnv map[string]string, key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	if value, exists := fileEnv[key]; exists {
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

func loadFileEnv() map[string]string {
	candidates := envFileCandidates()
	for _, candidate := range candidates {
		values, ok := readEnvFile(candidate)
		if ok {
			return values
		}
	}
	return map[string]string{}
}

func envFileCandidates() []string {
	candidates := make([]string, 0, 3)
	if explicit := strings.TrimSpace(os.Getenv("PAYAMBAR_ENV_FILE")); explicit != "" {
		candidates = append(candidates, explicit)
	}

	candidates = append(candidates, "/etc/payambar/payambar.env", ".env")
	seen := make(map[string]struct{}, len(candidates))
	unique := make([]string, 0, len(candidates))

	for _, path := range candidates {
		cleaned := filepath.Clean(path)
		if _, exists := seen[cleaned]; exists {
			continue
		}
		seen[cleaned] = struct{}{}
		unique = append(unique, cleaned)
	}

	return unique
}

func readEnvFile(path string) (map[string]string, bool) {
	file, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer file.Close()

	values := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		if key == "" {
			continue
		}

		value := strings.TrimSpace(parts[1])
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}

		values[key] = value
	}

	return values, true
}
