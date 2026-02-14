package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"github.com/4xmen/payambar/internal/auth"
)

var (
	testDB      *sql.DB
	testAuthSvc *auth.Service
	testRouter  *gin.Engine
	testUploadDir string
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)

	// Create in-memory SQLite database with shared cache mode
	// This ensures all connections in the pool share the same database
	var err error
	testDB, err = sql.Open("sqlite3", "file::memory:?cache=shared")
	if err != nil {
		panic(err)
	}

	// Run migrations
	_, err = testDB.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT UNIQUE NOT NULL,
			password_hash TEXT NOT NULL,
			display_name TEXT,
			avatar_url TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS conversations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			participants TEXT NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			sender_id INTEGER NOT NULL,
			receiver_id INTEGER NOT NULL,
			content TEXT NOT NULL,
			status TEXT DEFAULT 'sent',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			delivered_at TIMESTAMP,
			read_at TIMESTAMP,
			FOREIGN KEY (sender_id) REFERENCES users(id),
			FOREIGN KEY (receiver_id) REFERENCES users(id)
		);

		CREATE TABLE IF NOT EXISTS files (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			message_id INTEGER NOT NULL,
			file_name TEXT NOT NULL,
			file_path TEXT NOT NULL,
			file_size INTEGER NOT NULL,
			content_type TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (message_id) REFERENCES messages(id)
		);
	`)
	if err != nil {
		panic(err)
	}

	testUploadDir, err = os.MkdirTemp("", "payambar-test-uploads")
	if err != nil {
		panic(err)
	}

	testAuthSvc = auth.New(testDB, "test-jwt-secret")
	testRouter = setupTestRouter()

	code := m.Run()

	os.RemoveAll(testUploadDir)
	testDB.Close()
	os.Exit(code)
}

func setupTestRouter() *gin.Engine {
	router := gin.New()

	authHandler := NewAuthHandler(testAuthSvc)
	msgHandler := NewMessageHandler(testDB, nil, testUploadDir, 10_485_760, "", "", "", "")

	api := router.Group("/api")
	{
		api.POST("/auth/register", authHandler.Register)
		api.POST("/auth/login", authHandler.Login)
	}

	protected := api.Group("")
	protected.Use(authHandler.AuthMiddleware())
	{
		protected.GET("/messages", msgHandler.GetConversation)
		protected.GET("/conversations", msgHandler.GetConversations)
		protected.GET("/users", msgHandler.GetUsers)
		protected.POST("/conversations", msgHandler.CreateConversation)
		protected.PUT("/messages/:id/delivered", msgHandler.MarkAsDelivered)
		protected.PUT("/messages/:id/read", msgHandler.MarkAsRead)
	}

	return router
}

func clearTestData() {
	testDB.Exec("DELETE FROM files")
	testDB.Exec("DELETE FROM messages")
	testDB.Exec("DELETE FROM conversations")
	testDB.Exec("DELETE FROM users")
}

func TestRegister(t *testing.T) {
	clearTestData()

	tests := []struct {
		name       string
		body       map[string]string
		wantStatus int
		wantError  bool
	}{
		{
			name:       "valid registration",
			body:       map[string]string{"username": "testuser", "password": "password123"},
			wantStatus: http.StatusCreated,
			wantError:  false,
		},
		{
			name:       "duplicate username",
			body:       map[string]string{"username": "testuser", "password": "password123"},
			wantStatus: http.StatusBadRequest,
			wantError:  true,
		},
		{
			name:       "short username",
			body:       map[string]string{"username": "ab", "password": "password123"},
			wantStatus: http.StatusBadRequest,
			wantError:  true,
		},
		{
			name:       "short password",
			body:       map[string]string{"username": "newuser", "password": "12345"},
			wantStatus: http.StatusBadRequest,
			wantError:  true,
		},
		{
			name:       "invalid username characters",
			body:       map[string]string{"username": "test@user", "password": "password123"},
			wantStatus: http.StatusBadRequest,
			wantError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.body)
			req := httptest.NewRequest("POST", "/api/auth/register", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			testRouter.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("Register() status = %d, want %d", w.Code, tt.wantStatus)
			}

			var resp map[string]interface{}
			json.Unmarshal(w.Body.Bytes(), &resp)

			if tt.wantError {
				if _, ok := resp["error"]; !ok {
					t.Error("Expected error response")
				}
			} else {
				if _, ok := resp["token"]; !ok {
					t.Error("Expected token in response")
				}
				if _, ok := resp["user_id"]; !ok {
					t.Error("Expected user_id in response")
				}
			}
		})
	}
}

func TestLogin(t *testing.T) {
	clearTestData()

	// Create a test user first
	_, err := testAuthSvc.Register("loginuser", "password123")
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	tests := []struct {
		name       string
		body       map[string]string
		wantStatus int
		wantError  bool
	}{
		{
			name:       "valid login",
			body:       map[string]string{"username": "loginuser", "password": "password123"},
			wantStatus: http.StatusOK,
			wantError:  false,
		},
		{
			name:       "wrong password",
			body:       map[string]string{"username": "loginuser", "password": "wrongpassword"},
			wantStatus: http.StatusUnauthorized,
			wantError:  true,
		},
		{
			name:       "non-existent user",
			body:       map[string]string{"username": "nonexistent", "password": "password123"},
			wantStatus: http.StatusUnauthorized,
			wantError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.body)
			req := httptest.NewRequest("POST", "/api/auth/login", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			testRouter.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("Login() status = %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}

func TestConversations(t *testing.T) {
	clearTestData()

	// Create two test users
	user1ID, _ := testAuthSvc.Register("user1", "password123")
	user2ID, _ := testAuthSvc.Register("user2", "password123")

	token1, _ := testAuthSvc.GenerateToken(user1ID, "user1")
	token2, _ := testAuthSvc.GenerateToken(user2ID, "user2")

	t.Run("create conversation", func(t *testing.T) {
		body, _ := json.Marshal(map[string]int{"participant_id": user2ID})
		req := httptest.NewRequest("POST", "/api/conversations", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token1)
		w := httptest.NewRecorder()

		testRouter.ServeHTTP(w, req)

		if w.Code != http.StatusCreated && w.Code != http.StatusOK {
			t.Errorf("CreateConversation() status = %d, want 201 or 200", w.Code)
		}

		var resp map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &resp)

		if _, ok := resp["id"]; !ok {
			t.Error("Expected id in response")
		}
		if _, ok := resp["username"]; !ok {
			t.Error("Expected username in response")
		}
	})

	t.Run("duplicate conversation returns existing", func(t *testing.T) {
		body, _ := json.Marshal(map[string]int{"participant_id": user2ID})
		req := httptest.NewRequest("POST", "/api/conversations", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token1)
		w := httptest.NewRecorder()

		testRouter.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("CreateConversation duplicate status = %d, want 200", w.Code)
		}
	})

	t.Run("get conversations", func(t *testing.T) {
		// Ensure conversation exists (in case subtests run in isolation)
		testDB.Exec("INSERT OR IGNORE INTO conversations (participants) VALUES (?)",
			strconv.Itoa(user1ID)+","+strconv.Itoa(user2ID))

		req := httptest.NewRequest("GET", "/api/conversations", nil)
		req.Header.Set("Authorization", "Bearer "+token1)
		w := httptest.NewRecorder()

		testRouter.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("GetConversations() status = %d, want 200", w.Code)
		}

		var resp map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &resp)

		conversations, ok := resp["conversations"].([]interface{})
		if !ok {
			t.Errorf("Expected conversations array in response, got: %v", resp)
		}
		if len(conversations) == 0 {
			t.Error("Expected at least one conversation")
		}
	})

	t.Run("conversation only visible to participants", func(t *testing.T) {
		// Create a third user who is NOT in any conversation
		user3ID, _ := testAuthSvc.Register("user3", "password123")
		token3, _ := testAuthSvc.GenerateToken(user3ID, "user3")

		req := httptest.NewRequest("GET", "/api/conversations", nil)
		req.Header.Set("Authorization", "Bearer "+token3)
		w := httptest.NewRecorder()

		testRouter.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("GetConversations() status = %d, want 200", w.Code)
		}

		var resp map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &resp)

		conversations, ok := resp["conversations"].([]interface{})
		if !ok {
			t.Error("Expected conversations array in response")
		}
		if len(conversations) != 0 {
			t.Errorf("User3 should see 0 conversations, got %d", len(conversations))
		}
	})

	t.Run("cannot create conversation with self", func(t *testing.T) {
		body, _ := json.Marshal(map[string]int{"participant_id": user1ID})
		req := httptest.NewRequest("POST", "/api/conversations", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token1)
		w := httptest.NewRecorder()

		testRouter.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("CreateConversation with self status = %d, want 400", w.Code)
		}
	})

	_ = token2 // silence unused variable warning
}

func TestMessages(t *testing.T) {
	clearTestData()

	// Create two test users
	user1ID, _ := testAuthSvc.Register("msguser1", "password123")
	user2ID, _ := testAuthSvc.Register("msguser2", "password123")

	token1, _ := testAuthSvc.GenerateToken(user1ID, "msguser1")
	token2, _ := testAuthSvc.GenerateToken(user2ID, "msguser2")

	// Create a conversation
	_, err := testDB.Exec("INSERT INTO conversations (participants) VALUES (?)",
		strconv.Itoa(user1ID)+","+strconv.Itoa(user2ID))
	if err != nil {
		t.Fatalf("Failed to create conversation: %v", err)
	}

	// Insert a test message
	result, err := testDB.Exec(`
		INSERT INTO messages (sender_id, receiver_id, content, status) 
		VALUES (?, ?, ?, 'sent')
	`, user1ID, user2ID, "Hello!")
	if err != nil {
		t.Fatalf("Failed to insert message: %v", err)
	}
	msgID, _ := result.LastInsertId()

	t.Run("get messages", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/messages?user_id="+strconv.Itoa(user2ID), nil)
		req.Header.Set("Authorization", "Bearer "+token1)
		w := httptest.NewRecorder()

		testRouter.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("GetMessages() status = %d, want 200", w.Code)
		}

		var resp map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &resp)

		messages, ok := resp["messages"].([]interface{})
		if !ok {
			t.Error("Expected messages array in response")
		}
		if len(messages) == 0 {
			t.Error("Expected at least one message")
		}
	})

	t.Run("mark as delivered", func(t *testing.T) {
		req := httptest.NewRequest("PUT", "/api/messages/"+strconv.FormatInt(msgID, 10)+"/delivered", nil)
		req.Header.Set("Authorization", "Bearer "+token2)
		w := httptest.NewRecorder()

		testRouter.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("MarkAsDelivered() status = %d, want 200", w.Code)
		}
	})

	t.Run("mark as read", func(t *testing.T) {
		req := httptest.NewRequest("PUT", "/api/messages/"+strconv.FormatInt(msgID, 10)+"/read", nil)
		req.Header.Set("Authorization", "Bearer "+token2)
		w := httptest.NewRecorder()

		testRouter.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("MarkAsRead() status = %d, want 200", w.Code)
		}
	})

	t.Run("cannot mark other's message as delivered", func(t *testing.T) {
		// Insert another message from user2 to user1
		result2, _ := testDB.Exec(`
			INSERT INTO messages (sender_id, receiver_id, content, status) 
			VALUES (?, ?, ?, 'sent')
		`, user2ID, user1ID, "Reply!")
		msgID2, _ := result2.LastInsertId()

		// Try to mark it as delivered by user2 (who is the sender, not receiver)
		req := httptest.NewRequest("PUT", "/api/messages/"+strconv.FormatInt(msgID2, 10)+"/delivered", nil)
		req.Header.Set("Authorization", "Bearer "+token2)
		w := httptest.NewRecorder()

		testRouter.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("Mark other's message status = %d, want 403", w.Code)
		}
	})

	_ = token1 // silence unused variable warning
}

func TestGetUsers(t *testing.T) {
	clearTestData()

	// Create test users
	user1ID, _ := testAuthSvc.Register("listuser1", "password123")
	testAuthSvc.Register("listuser2", "password123")
	testAuthSvc.Register("listuser3", "password123")

	token1, _ := testAuthSvc.GenerateToken(user1ID, "listuser1")

	t.Run("get users excludes current user", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/users", nil)
		req.Header.Set("Authorization", "Bearer "+token1)
		w := httptest.NewRecorder()

		testRouter.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("GetUsers() status = %d, want 200", w.Code)
		}

		var users []map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &users)

		if len(users) != 2 {
			t.Errorf("Expected 2 users (excluding self), got %d", len(users))
		}

		for _, user := range users {
			if user["username"] == "listuser1" {
				t.Error("Current user should not be in the list")
			}
		}
	})
}

func TestAuthMiddleware(t *testing.T) {
	clearTestData()

	t.Run("no token", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/conversations", nil)
		w := httptest.NewRecorder()

		testRouter.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("No token status = %d, want 401", w.Code)
		}
	})

	t.Run("invalid token", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/conversations", nil)
		req.Header.Set("Authorization", "Bearer invalid-token")
		w := httptest.NewRecorder()

		testRouter.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Invalid token status = %d, want 401", w.Code)
		}
	})
}

// TestConversationIDMatching specifically tests that user IDs don't get confused
// (e.g., user 1 should not match conversations for user 11, 12, etc.)
func TestConversationIDMatching(t *testing.T) {
	clearTestData()

	// Create users with IDs that could cause LIKE pattern issues
	user1ID, _ := testAuthSvc.Register("user_one", "password123")
	user11ID, _ := testAuthSvc.Register("user_eleven", "password123")
	user12ID, _ := testAuthSvc.Register("user_twelve", "password123")

	// Create a conversation between user11 and user12 (not involving user1)
	_, err := testDB.Exec("INSERT INTO conversations (participants) VALUES (?)",
		strconv.Itoa(user11ID)+","+strconv.Itoa(user12ID))
	if err != nil {
		t.Fatalf("Failed to create conversation: %v", err)
	}

	// User1 should see ZERO conversations
	token1, _ := testAuthSvc.GenerateToken(user1ID, "user_one")

	req := httptest.NewRequest("GET", "/api/conversations", nil)
	req.Header.Set("Authorization", "Bearer "+token1)
	w := httptest.NewRecorder()

	testRouter.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GetConversations() status = %d, want 200", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	conversations, _ := resp["conversations"].([]interface{})
	if len(conversations) != 0 {
		t.Errorf("User1 (ID=%d) should see 0 conversations, but got %d (likely matched user %d or %d by mistake)",
			user1ID, len(conversations), user11ID, user12ID)
	}
}
