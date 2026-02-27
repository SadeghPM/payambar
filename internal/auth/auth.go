package auth

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

type Service struct {
	db        *sql.DB
	jwtSecret string
	tokenTTL  time.Duration
}

type Claims struct {
	UserID   int    `json:"user_id"`
	Username string `json:"username"`
	jwt.RegisteredClaims
}

func New(db *sql.DB, jwtSecret string) *Service {
	return NewWithTokenTTL(db, jwtSecret, 24*time.Hour)
}

func NewWithTokenTTL(db *sql.DB, jwtSecret string, tokenTTL time.Duration) *Service {
	if tokenTTL <= 0 {
		tokenTTL = 24 * time.Hour
	}

	return &Service{
		db:        db,
		jwtSecret: jwtSecret,
		tokenTTL:  tokenTTL,
	}
}

func (s *Service) Register(username, password string) (int, error) {
	// Validate inputs
	username = strings.TrimSpace(username)
	if len(username) < 3 || len(username) > 32 {
		return 0, fmt.Errorf("username must be between 3 and 32 characters")
	}

	// Username can only contain alphanumeric and underscore
	if !regexp.MustCompile(`^[a-zA-Z0-9_]+$`).MatchString(username) {
		return 0, fmt.Errorf("username can only contain letters, numbers, and underscores")
	}

	if len(password) < 6 {
		return 0, fmt.Errorf("password must be at least 6 characters")
	}

	// Hash password
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return 0, fmt.Errorf("failed to hash password: %w", err)
	}

	// Insert user
	result, err := s.db.Exec(
		"INSERT INTO users (username, password_hash) VALUES (?, ?)",
		username,
		string(hash),
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return 0, fmt.Errorf("username already exists")
		}
		return 0, fmt.Errorf("failed to register user: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("failed to get user id: %w", err)
	}

	return int(id), nil
}

func (s *Service) Login(username, password string) (string, error) {
	username = strings.TrimSpace(username)

	// Get user by username
	var userID int
	var passwordHash string

	err := s.db.QueryRow(
		"SELECT id, password_hash FROM users WHERE username = ?",
		username,
	).Scan(&userID, &passwordHash)

	if err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("invalid username or password")
		}
		return "", fmt.Errorf("failed to query user: %w", err)
	}

	// Verify password
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password)); err != nil {
		return "", fmt.Errorf("invalid username or password")
	}

	// Generate JWT token
	token, err := s.GenerateToken(userID, username)
	if err != nil {
		return "", fmt.Errorf("failed to generate token: %w", err)
	}

	return token, nil
}

func (s *Service) GenerateToken(userID int, username string) (string, error) {
	claims := Claims{
		UserID:   userID,
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(s.tokenTTL)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(s.jwtSecret))
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %w", err)
	}

	return tokenString, nil
}

func (s *Service) ValidateToken(tokenString string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(s.jwtSecret), nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}

	if !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}

	return claims, nil
}

func (s *Service) GetUserByUsername(username string) (int, error) {
	var userID int
	err := s.db.QueryRow("SELECT id FROM users WHERE username = ?", username).Scan(&userID)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, fmt.Errorf("user not found")
		}
		return 0, fmt.Errorf("failed to query user: %w", err)
	}
	return userID, nil
}

// UserExists checks if a user with the given ID exists
func (s *Service) UserExists(userID int) (bool, error) {
	var exists bool
	err := s.db.QueryRow("SELECT EXISTS(SELECT 1 FROM users WHERE id = ?)", userID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to query user: %w", err)
	}
	return exists, nil
}
