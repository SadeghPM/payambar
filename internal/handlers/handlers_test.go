package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/4xmen/payambar/internal/auth"
	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
)

var (
	testDB        *sql.DB
	testAuthSvc   *auth.Service
	testRouter    *gin.Engine
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
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS conversation_participants (
			conversation_id INTEGER NOT NULL,
			user_id INTEGER NOT NULL,
			joined_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (conversation_id, user_id)
		);

		CREATE TABLE IF NOT EXISTS messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			sender_id INTEGER NOT NULL,
			receiver_id INTEGER NOT NULL,
			content TEXT NOT NULL,
			encrypted INTEGER NOT NULL DEFAULT 0,
			e2ee_v INTEGER,
			alg TEXT,
			sender_device_id TEXT,
			key_id TEXT,
			iv TEXT,
			ciphertext TEXT,
			aad TEXT,
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

		CREATE TABLE IF NOT EXISTS user_device_keys (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			device_id TEXT NOT NULL,
			algorithm TEXT NOT NULL,
			public_key TEXT NOT NULL,
			key_id TEXT NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			revoked_at TIMESTAMP,
			UNIQUE (user_id, device_id, key_id)
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
		protected.POST("/keys/devices", msgHandler.UpsertDeviceKey)
		protected.GET("/keys/users/:id/devices", msgHandler.GetUserDeviceKeys)
		protected.GET("/users", msgHandler.GetUsers)
		protected.POST("/conversations", msgHandler.CreateConversation)
		protected.DELETE("/conversations/:id", msgHandler.DeleteConversation)
		protected.PUT("/messages/:id/delivered", msgHandler.MarkAsDelivered)
		protected.PUT("/messages/:id/read", msgHandler.MarkAsRead)
		protected.DELETE("/profile", msgHandler.DeleteAccount)
	}

	return router
}

func clearTestData() {
	testDB.Exec("DELETE FROM files")
	testDB.Exec("DELETE FROM messages")
	testDB.Exec("DELETE FROM conversation_participants")
	testDB.Exec("DELETE FROM conversations")
	testDB.Exec("DELETE FROM users")
}

func insertDirectConversation(t *testing.T, user1ID, user2ID int) int64 {
	t.Helper()

	result, err := testDB.Exec("INSERT INTO conversations DEFAULT VALUES")
	if err != nil {
		t.Fatalf("Failed to create conversation: %v", err)
	}
	convID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("Failed to get conversation id: %v", err)
	}
	_, err = testDB.Exec(
		`INSERT INTO conversation_participants (conversation_id, user_id) VALUES (?, ?), (?, ?)`,
		convID, user1ID, convID, user2ID,
	)
	if err != nil {
		t.Fatalf("Failed to attach conversation participants: %v", err)
	}
	return convID
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
		insertDirectConversation(t, user1ID, user2ID)

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
	insertDirectConversation(t, user1ID, user2ID)

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
	insertDirectConversation(t, user11ID, user12ID)

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

func TestDeleteConversationCleansUpData(t *testing.T) {
	clearTestData()

	user1ID, _ := testAuthSvc.Register("delconv_user1", "password123")
	user2ID, _ := testAuthSvc.Register("delconv_user2", "password123")
	token1, _ := testAuthSvc.GenerateToken(user1ID, "delconv_user1")

	convID := insertDirectConversation(t, user1ID, user2ID)

	messageResult, err := testDB.Exec(`
		INSERT INTO messages (sender_id, receiver_id, content, status)
		VALUES (?, ?, ?, 'sent')
	`, user1ID, user2ID, "conversation delete test")
	if err != nil {
		t.Fatalf("Failed to insert message: %v", err)
	}
	messageID, _ := messageResult.LastInsertId()

	filePath := filepath.Join(testUploadDir, "delete-conversation.txt")
	if err := os.WriteFile(filePath, []byte("test"), 0o644); err != nil {
		t.Fatalf("Failed to create file fixture: %v", err)
	}
	_, err = testDB.Exec(`
		INSERT INTO files (message_id, file_name, file_path, file_size, content_type)
		VALUES (?, ?, ?, ?, ?)
	`, messageID, "delete-conversation.txt", filePath, 4, "text/plain")
	if err != nil {
		t.Fatalf("Failed to insert file record: %v", err)
	}

	req := httptest.NewRequest("DELETE", "/api/conversations/"+strconv.FormatInt(convID, 10), nil)
	req.Header.Set("Authorization", "Bearer "+token1)
	w := httptest.NewRecorder()

	testRouter.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("DeleteConversation() status = %d, want 200", w.Code)
	}

	var count int
	if err := testDB.QueryRow("SELECT COUNT(*) FROM conversations WHERE id = ?", convID).Scan(&count); err != nil {
		t.Fatalf("Failed to check conversations: %v", err)
	}
	if count != 0 {
		t.Fatalf("Expected conversation to be deleted, got count=%d", count)
	}

	if err := testDB.QueryRow("SELECT COUNT(*) FROM conversation_participants WHERE conversation_id = ?", convID).Scan(&count); err != nil {
		t.Fatalf("Failed to check conversation_participants: %v", err)
	}
	if count != 0 {
		t.Fatalf("Expected conversation participants to be deleted, got count=%d", count)
	}

	if err := testDB.QueryRow("SELECT COUNT(*) FROM messages WHERE (sender_id = ? AND receiver_id = ?) OR (sender_id = ? AND receiver_id = ?)", user1ID, user2ID, user2ID, user1ID).Scan(&count); err != nil {
		t.Fatalf("Failed to check messages: %v", err)
	}
	if count != 0 {
		t.Fatalf("Expected related messages to be deleted, got count=%d", count)
	}

	if err := testDB.QueryRow("SELECT COUNT(*) FROM files").Scan(&count); err != nil {
		t.Fatalf("Failed to check files: %v", err)
	}
	if count != 0 {
		t.Fatalf("Expected related files to be deleted, got count=%d", count)
	}

	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Fatalf("Expected uploaded file to be removed, err=%v", err)
	}
}

func TestDeleteAccountConversationMembershipCleanup(t *testing.T) {
	clearTestData()

	user1ID, _ := testAuthSvc.Register("delacct_user1", "password123")
	user2ID, _ := testAuthSvc.Register("delacct_user2", "password123")
	token1, _ := testAuthSvc.GenerateToken(user1ID, "delacct_user1")

	convID := insertDirectConversation(t, user1ID, user2ID)

	orphanResult, err := testDB.Exec("INSERT INTO conversations DEFAULT VALUES")
	if err != nil {
		t.Fatalf("Failed to create orphan candidate conversation: %v", err)
	}
	orphanConvID, _ := orphanResult.LastInsertId()
	if _, err := testDB.Exec(
		"INSERT INTO conversation_participants (conversation_id, user_id) VALUES (?, ?)",
		orphanConvID, user1ID,
	); err != nil {
		t.Fatalf("Failed to attach orphan candidate participant: %v", err)
	}

	avatarFile := filepath.Join(testUploadDir, "delacct-avatar.png")
	if err := os.WriteFile(avatarFile, []byte("avatar"), 0o644); err != nil {
		t.Fatalf("Failed to create avatar fixture: %v", err)
	}
	if _, err := testDB.Exec("UPDATE users SET avatar_url = ? WHERE id = ?", "/api/files/delacct-avatar.png", user1ID); err != nil {
		t.Fatalf("Failed to set avatar url: %v", err)
	}

	messageResult, err := testDB.Exec(`
		INSERT INTO messages (sender_id, receiver_id, content, status)
		VALUES (?, ?, ?, 'sent')
	`, user1ID, user2ID, "delete account test")
	if err != nil {
		t.Fatalf("Failed to insert message: %v", err)
	}
	messageID, _ := messageResult.LastInsertId()

	filePath := filepath.Join(testUploadDir, "delete-account.txt")
	if err := os.WriteFile(filePath, []byte("test"), 0o644); err != nil {
		t.Fatalf("Failed to create upload fixture: %v", err)
	}
	if _, err := testDB.Exec(`
		INSERT INTO files (message_id, file_name, file_path, file_size, content_type)
		VALUES (?, ?, ?, ?, ?)
	`, messageID, "delete-account.txt", filePath, 4, "text/plain"); err != nil {
		t.Fatalf("Failed to insert file record: %v", err)
	}

	req := httptest.NewRequest("DELETE", "/api/profile", nil)
	req.Header.Set("Authorization", "Bearer "+token1)
	w := httptest.NewRecorder()

	testRouter.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("DeleteAccount() status = %d, want 200", w.Code)
	}

	var count int
	if err := testDB.QueryRow("SELECT COUNT(*) FROM users WHERE id = ?", user1ID).Scan(&count); err != nil {
		t.Fatalf("Failed to check user delete: %v", err)
	}
	if count != 0 {
		t.Fatalf("Expected user to be deleted, got count=%d", count)
	}

	if err := testDB.QueryRow("SELECT COUNT(*) FROM conversation_participants WHERE user_id = ?", user1ID).Scan(&count); err != nil {
		t.Fatalf("Failed to check membership cleanup: %v", err)
	}
	if count != 0 {
		t.Fatalf("Expected user memberships to be deleted, got count=%d", count)
	}

	if err := testDB.QueryRow("SELECT COUNT(*) FROM conversations WHERE id = ?", orphanConvID).Scan(&count); err != nil {
		t.Fatalf("Failed to check orphan conversation cleanup: %v", err)
	}
	if count != 0 {
		t.Fatalf("Expected orphan conversation to be deleted, got count=%d", count)
	}

	if err := testDB.QueryRow("SELECT COUNT(*) FROM conversations WHERE id = ?", convID).Scan(&count); err != nil {
		t.Fatalf("Failed to check retained conversation: %v", err)
	}
	if count != 1 {
		t.Fatalf("Expected non-orphan conversation to remain, got count=%d", count)
	}

	if err := testDB.QueryRow("SELECT COUNT(*) FROM conversation_participants WHERE conversation_id = ? AND user_id = ?", convID, user2ID).Scan(&count); err != nil {
		t.Fatalf("Failed to check remaining participant: %v", err)
	}
	if count != 1 {
		t.Fatalf("Expected participant user2 to remain, got count=%d", count)
	}

	if err := testDB.QueryRow("SELECT COUNT(*) FROM messages WHERE sender_id = ? OR receiver_id = ?", user1ID, user1ID).Scan(&count); err != nil {
		t.Fatalf("Failed to check messages cleanup: %v", err)
	}
	if count != 0 {
		t.Fatalf("Expected user messages to be deleted, got count=%d", count)
	}

	if err := testDB.QueryRow("SELECT COUNT(*) FROM files").Scan(&count); err != nil {
		t.Fatalf("Failed to check files cleanup: %v", err)
	}
	if count != 0 {
		t.Fatalf("Expected user files to be deleted, got count=%d", count)
	}

	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Fatalf("Expected uploaded file to be removed, err=%v", err)
	}
	if _, err := os.Stat(avatarFile); !os.IsNotExist(err) {
		t.Fatalf("Expected avatar file to be removed, err=%v", err)
	}
}

func TestDeviceKeysAPI(t *testing.T) {
	clearTestData()

	result, err := testDB.Exec(`INSERT INTO users (username, password_hash) VALUES (?, ?), (?, ?)`, "keysuser1", "hash", "keysuser2", "hash")
	if err != nil {
		t.Fatalf("failed to insert users: %v", err)
	}
	lastID, _ := result.LastInsertId()
	user2ID := int(lastID)
	user1ID := user2ID - 1
	token1, _ := testAuthSvc.GenerateToken(user1ID, "keysuser1")

	body := map[string]interface{}{
		"device_id":  "web-device-1",
		"algorithm":  "X25519",
		"public_key": "base64pubkey",
		"key_id":     "k-2026-01",
	}
	jsonBody, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/keys/devices", bytes.NewBuffer(jsonBody))
	req.Header.Set("Authorization", "Bearer "+token1)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	testRouter.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("upsert device key status = %d, want 200", w.Code)
	}

	var count int
	if err := testDB.QueryRow("SELECT COUNT(*) FROM user_device_keys WHERE user_id = ?", user1ID).Scan(&count); err != nil {
		t.Fatalf("query device key count failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 device key row, got %d", count)
	}

	req = httptest.NewRequest("GET", "/api/keys/users/"+strconv.Itoa(user1ID)+"/devices", nil)
	req.Header.Set("Authorization", "Bearer "+token1)
	w = httptest.NewRecorder()
	testRouter.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get user device keys status = %d, want 200", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	devices, ok := resp["devices"].([]interface{})
	if !ok {
		t.Fatalf("expected devices array in response")
	}
	if len(devices) != 1 {
		t.Fatalf("expected 1 device key returned, got %d", len(devices))
	}

	req = httptest.NewRequest("GET", "/api/keys/users/"+strconv.Itoa(user2ID)+"/devices", nil)
	req.Header.Set("Authorization", "Bearer "+token1)
	w = httptest.NewRecorder()
	testRouter.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get empty user keys status = %d, want 200", w.Code)
	}
}
