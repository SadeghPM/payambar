package ws

import (
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	_ "github.com/mattn/go-sqlite3"
)

func setupTestDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open test db: %v", err)
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT UNIQUE NOT NULL,
			password_hash TEXT NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			sender_id INTEGER NOT NULL,
			receiver_id INTEGER NOT NULL,
			content TEXT NOT NULL,
			status TEXT DEFAULT 'sent',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			delivered_at TIMESTAMP,
			read_at TIMESTAMP
		);
	`)
	if err != nil {
		t.Fatalf("Failed to create tables: %v", err)
	}

	// Create test users
	db.Exec("INSERT INTO users (id, username, password_hash) VALUES (1, 'user1', 'hash1')")
	db.Exec("INSERT INTO users (id, username, password_hash) VALUES (2, 'user2', 'hash2')")

	return db
}

func TestHubCreation(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	hub := NewHub(db)
	if hub == nil {
		t.Fatal("NewHub returned nil")
	}

	if hub.clients == nil {
		t.Error("Hub clients map is nil")
	}
	if hub.broadcast == nil {
		t.Error("Hub broadcast channel is nil")
	}
	if hub.register == nil {
		t.Error("Hub register channel is nil")
	}
	if hub.unregister == nil {
		t.Error("Hub unregister channel is nil")
	}
}

func TestHubRun(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	hub := NewHub(db)
	go hub.Run()

	// Allow hub goroutine to start
	time.Sleep(10 * time.Millisecond)

	// Test registering a client
	client := &Client{
		userID: 1,
		hub:    hub,
		send:   make(chan interface{}, 256),
	}

	hub.register <- client

	// Wait for registration
	time.Sleep(10 * time.Millisecond)

	hub.mu.RLock()
	if _, ok := hub.clients[1]; !ok {
		t.Error("Client was not registered")
	}
	hub.mu.RUnlock()

	// Test unregistering
	hub.unregister <- client

	time.Sleep(10 * time.Millisecond)

	hub.mu.RLock()
	if _, ok := hub.clients[1]; ok {
		t.Error("Client was not unregistered")
	}
	hub.mu.RUnlock()
}

func TestMessageEvent(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	hub := NewHub(db)
	go hub.Run()

	time.Sleep(10 * time.Millisecond)

	// Register two clients
	client1 := &Client{
		userID: 1,
		hub:    hub,
		send:   make(chan interface{}, 256),
	}
	client2 := &Client{
		userID: 2,
		hub:    hub,
		send:   make(chan interface{}, 256),
	}

	hub.register <- client1
	hub.register <- client2

	time.Sleep(10 * time.Millisecond)

	// Send a message from user 1 to user 2
	msg := &MessageEvent{
		Type:       "message",
		MessageID:  1,
		SenderID:   1,
		ReceiverID: 2,
		Content:    "Hello!",
		Status:     "sent",
		CreatedAt:  time.Now(),
	}

	hub.broadcast <- msg

	// Wait for message delivery
	time.Sleep(50 * time.Millisecond)

	// Check if client2 received the message
	select {
	case received := <-client2.send:
		receivedMsg, ok := received.(*MessageEvent)
		if !ok {
			t.Error("Received wrong type")
		}
		if receivedMsg.Content != "Hello!" {
			t.Errorf("Expected 'Hello!', got '%s'", receivedMsg.Content)
		}
	default:
		t.Error("Client2 did not receive the message")
	}

	// Check if client1 also received a copy (for confirmation)
	select {
	case received := <-client1.send:
		receivedMsg, ok := received.(*MessageEvent)
		if !ok {
			t.Error("Received wrong type")
		}
		if receivedMsg.Type != "message" && receivedMsg.Type != "status_update" {
			t.Errorf("Expected message or status_update, got '%s'", receivedMsg.Type)
		}
	default:
		// It's okay if sender doesn't receive immediate copy
	}
}

func TestStatusUpdateBroadcast(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	hub := NewHub(db)
	go hub.Run()

	time.Sleep(10 * time.Millisecond)

	client1 := &Client{
		userID: 1,
		hub:    hub,
		send:   make(chan interface{}, 256),
	}
	client2 := &Client{
		userID: 2,
		hub:    hub,
		send:   make(chan interface{}, 256),
	}

	hub.register <- client1
	hub.register <- client2

	time.Sleep(10 * time.Millisecond)

	// Send status update
	statusUpdate := &MessageEvent{
		Type:       "status_update",
		MessageID:  1,
		Status:     "read",
		SenderID:   1,
		ReceiverID: 2,
	}

	hub.broadcast <- statusUpdate

	time.Sleep(50 * time.Millisecond)

	// Both clients should receive the status update
	receivedCount := 0

	select {
	case <-client1.send:
		receivedCount++
	default:
	}

	select {
	case <-client2.send:
		receivedCount++
	default:
	}

	if receivedCount == 0 {
		t.Error("No clients received the status update")
	}
}

func TestWebSocketIntegration(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupTestDB(t)
	defer db.Close()

	hub := NewHub(db)
	go hub.Run()

	// Create a test server
	router := gin.New()

	// Simple middleware that sets user_id from query param for testing
	router.GET("/ws", func(c *gin.Context) {
		c.Set("user_id", 1)
		hub.HandleWebSocket(c)
	})

	server := httptest.NewServer(router)
	defer server.Close()

	// Convert http:// to ws://
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"

	// Connect to WebSocket
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect to WebSocket: %v", err)
	}
	defer ws.Close()

	// Wait for connection to be registered
	time.Sleep(50 * time.Millisecond)

	// Check if client is registered
	hub.mu.RLock()
	_, connected := hub.clients[1]
	hub.mu.RUnlock()

	if !connected {
		t.Error("WebSocket client was not registered in hub")
	}
}

func TestMessageSaveToDatabase(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	hub := NewHub(db)
	go hub.Run()

	time.Sleep(10 * time.Millisecond)

	// Create a test client
	client := &Client{
		userID: 1,
		hub:    hub,
		send:   make(chan interface{}, 256),
	}

	// Simulate handling a message event
	event := map[string]interface{}{
		"type":        "message",
		"receiver_id": float64(2),
		"content":     "Test message",
	}

	client.handleMessageEvent(event)

	// Check if message was saved to database
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM messages WHERE sender_id = 1 AND receiver_id = 2").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query messages: %v", err)
	}

	if count != 1 {
		t.Errorf("Expected 1 message in database, got %d", count)
	}

	// Verify message content
	var content string
	err = db.QueryRow("SELECT content FROM messages WHERE sender_id = 1 AND receiver_id = 2").Scan(&content)
	if err != nil {
		t.Fatalf("Failed to query message content: %v", err)
	}

	if content != "Test message" {
		t.Errorf("Expected 'Test message', got '%s'", content)
	}
}

func TestMarkDelivered(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	// Insert a test message
	result, err := db.Exec(`
		INSERT INTO messages (sender_id, receiver_id, content, status) 
		VALUES (1, 2, 'Test', 'sent')
	`)
	if err != nil {
		t.Fatalf("Failed to insert message: %v", err)
	}
	msgID, _ := result.LastInsertId()

	hub := NewHub(db)
	go hub.Run()

	time.Sleep(10 * time.Millisecond)

	// Create client as receiver
	client := &Client{
		userID: 2,
		hub:    hub,
		send:   make(chan interface{}, 256),
	}

	event := map[string]interface{}{
		"type":       "mark_delivered",
		"message_id": float64(msgID),
	}

	client.handleMarkDelivered(event)

	// Check status in database
	var status string
	err = db.QueryRow("SELECT status FROM messages WHERE id = ?", msgID).Scan(&status)
	if err != nil {
		t.Fatalf("Failed to query status: %v", err)
	}

	if status != "delivered" {
		t.Errorf("Expected status 'delivered', got '%s'", status)
	}
}

func TestMarkRead(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	// Insert a test message
	result, err := db.Exec(`
		INSERT INTO messages (sender_id, receiver_id, content, status) 
		VALUES (1, 2, 'Test', 'delivered')
	`)
	if err != nil {
		t.Fatalf("Failed to insert message: %v", err)
	}
	msgID, _ := result.LastInsertId()

	hub := NewHub(db)
	go hub.Run()

	time.Sleep(10 * time.Millisecond)

	// Create client as receiver
	client := &Client{
		userID: 2,
		hub:    hub,
		send:   make(chan interface{}, 256),
	}

	event := map[string]interface{}{
		"type":       "mark_read",
		"message_id": float64(msgID),
	}

	client.handleMarkRead(event)

	// Check status in database
	var status string
	err = db.QueryRow("SELECT status FROM messages WHERE id = ?", msgID).Scan(&status)
	if err != nil {
		t.Fatalf("Failed to query status: %v", err)
	}

	if status != "read" {
		t.Errorf("Expected status 'read', got '%s'", status)
	}
}

func TestInvalidMessageEvent(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	hub := NewHub(db)
	go hub.Run()

	client := &Client{
		userID: 1,
		hub:    hub,
		send:   make(chan interface{}, 256),
	}

	tests := []struct {
		name  string
		event map[string]interface{}
	}{
		{
			name:  "missing receiver_id",
			event: map[string]interface{}{"type": "message", "content": "test"},
		},
		{
			name:  "missing content",
			event: map[string]interface{}{"type": "message", "receiver_id": float64(2)},
		},
		{
			name:  "empty content",
			event: map[string]interface{}{"type": "message", "receiver_id": float64(2), "content": ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			initialCount := countMessages(db)
			client.handleMessageEvent(tt.event)
			afterCount := countMessages(db)

			if afterCount != initialCount {
				t.Errorf("Message was saved despite invalid data")
			}
		})
	}
}

func countMessages(db *sql.DB) int {
	var count int
	db.QueryRow("SELECT COUNT(*) FROM messages").Scan(&count)
	return count
}

func TestMessageEventJSON(t *testing.T) {
	msg := &MessageEvent{
		Type:       "message",
		MessageID:  123,
		SenderID:   1,
		ReceiverID: 2,
		Content:    "Hello",
		Status:     "sent",
		CreatedAt:  time.Date(2026, 1, 25, 12, 0, 0, 0, time.UTC),
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	var decoded MessageEvent
	err = json.Unmarshal(data, &decoded)
	if err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if decoded.MessageID != 123 {
		t.Errorf("Expected message_id 123, got %d", decoded.MessageID)
	}
	if decoded.Content != "Hello" {
		t.Errorf("Expected content 'Hello', got '%s'", decoded.Content)
	}
}

func TestSignalingForwarding(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	hub := NewHub(db)
	go hub.Run()

	time.Sleep(10 * time.Millisecond)

	// Register two clients
	client1 := &Client{
		userID: 1,
		hub:    hub,
		send:   make(chan interface{}, 256),
	}
	client2 := &Client{
		userID: 2,
		hub:    hub,
		send:   make(chan interface{}, 256),
	}

	hub.register <- client1
	hub.register <- client2

	time.Sleep(10 * time.Millisecond)

	// Simulate a call_offer from user 1 to user 2
	offerEvent := map[string]interface{}{
		"type":        "call_offer",
		"receiver_id": float64(2),
		"payload": map[string]interface{}{
			"offer": "test-sdp-offer",
		},
	}

	client1.handleSignalingEvent(offerEvent)

	// Wait for delivery
	time.Sleep(50 * time.Millisecond)

	// Check if client2 received the offer
	select {
	case received := <-client2.send:
		receivedMsg, ok := received.(*MessageEvent)
		if !ok {
			t.Fatal("Received wrong type")
		}
		if receivedMsg.Type != "call_offer" {
			t.Errorf("Expected type 'call_offer', got '%s'", receivedMsg.Type)
		}
		if receivedMsg.Payload["offer"] != "test-sdp-offer" {
			t.Errorf("Expected offer 'test-sdp-offer', got '%v'", receivedMsg.Payload["offer"])
		}
	default:
		t.Error("Client2 did not receive the call_offer")
	}

	// Check that client1 did NOT receive the offer (signaling should be one-way)
	select {
	case <-client1.send:
		t.Error("Sender received their own signaling message")
	default:
		// Correct
	}
}
