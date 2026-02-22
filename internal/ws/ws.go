package ws

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

type Hub struct {
	clients    map[int]*Client
	broadcast  chan interface{}
	register   chan *Client
	unregister chan *Client
	db         *sql.DB
	mu         sync.RWMutex
}

type Client struct {
	userID int
	conn   *websocket.Conn
	hub    *Hub
	send   chan interface{}
}

type MessageEvent struct {
	Type           string                 `json:"type"` // "message", "status_update"
	MessageID      int                    `json:"message_id,omitempty"`
	ClientMsgID    string                 `json:"client_message_id,omitempty"`
	SenderID       int                    `json:"sender_id,omitempty"`
	ReceiverID     int                    `json:"receiver_id,omitempty"`
	Content        string                 `json:"content,omitempty"`
	Encrypted      bool                   `json:"encrypted,omitempty"`
	E2EEVersion    int                    `json:"e2ee_v,omitempty"`
	Algorithm      string                 `json:"alg,omitempty"`
	SenderDeviceID string                 `json:"sender_device_id,omitempty"`
	KeyID          string                 `json:"key_id,omitempty"`
	IV             string                 `json:"iv,omitempty"`
	Ciphertext     string                 `json:"ciphertext,omitempty"`
	AAD            string                 `json:"aad,omitempty"`
	Status         string                 `json:"status,omitempty"`
	CreatedAt      time.Time              `json:"created_at,omitempty"`
	DeliveredAt    *time.Time             `json:"delivered_at,omitempty"`
	ReadAt         *time.Time             `json:"read_at,omitempty"`
	FileName       string                 `json:"file_name,omitempty"`
	FileURL        string                 `json:"file_url,omitempty"`
	FileType       string                 `json:"file_content_type,omitempty"`
	Payload        map[string]interface{} `json:"payload,omitempty"`
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// In production, validate origin
		return true
	},
}

func NewHub(db *sql.DB) *Hub {
	return &Hub{
		clients:    make(map[int]*Client),
		broadcast:  make(chan interface{}, 256),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		db:         db,
	}
}

// IsUserOnline checks if a user is currently connected
func (h *Hub) IsUserOnline(userID int) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := h.clients[userID]
	return ok
}

// BroadcastMessage allows handlers to broadcast a message event to connected clients
func (h *Hub) BroadcastMessage(messageID, senderID, receiverID int, content, status, fileName, fileURL, fileType string) {
	msg := &MessageEvent{
		Type:       "message",
		MessageID:  messageID,
		SenderID:   senderID,
		ReceiverID: receiverID,
		Content:    content,
		Status:     status,
		CreatedAt:  time.Now(),
		FileName:   fileName,
		FileURL:    fileURL,
		FileType:   fileType,
	}
	h.broadcast <- msg
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client.userID] = client
			h.mu.Unlock()
			log.Printf("User %d connected (total: %d)", client.userID, len(h.clients))

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client.userID]; ok {
				delete(h.clients, client.userID)
				close(client.send)
			}
			h.mu.Unlock()
			log.Printf("User %d disconnected (total: %d)", client.userID, len(h.clients))

		case message := <-h.broadcast:
			h.broadcast_message(message)
		}
	}
}

func (h *Hub) broadcast_message(message interface{}) {
	switch msg := message.(type) {
	case *MessageEvent:
		if msg.Type == "message" {
			// Check if receiver is connected
			h.mu.RLock()
			receiverOnline := false
			if client, ok := h.clients[msg.ReceiverID]; ok {
				receiverOnline = true
				select {
				case client.send <- msg:
				default:
					log.Printf("Message channel full for user %d", msg.ReceiverID)
				}
			}
			// Also send to sender so they get the canonical message id
			if sender, ok := h.clients[msg.SenderID]; ok {
				select {
				case sender.send <- msg:
				default:
				}
			}
			h.mu.RUnlock()

			// Only send delivery confirmation if receiver is online
			if receiverOnline {
				if sender, ok := h.clients[msg.SenderID]; ok {
					select {
					case sender.send <- &MessageEvent{
						Type:       "status_update",
						MessageID:  msg.MessageID,
						Status:     "delivered",
						SenderID:   msg.SenderID,
						ReceiverID: msg.ReceiverID,
					}:
					default:
					}
				}
			}
		} else if msg.Type == "status_update" {
			// Broadcast status updates to both sender and receiver when available
			h.mu.RLock()
			if client, ok := h.clients[msg.SenderID]; ok {
				select {
				case client.send <- msg:
				default:
				}
			}
			if client, ok := h.clients[msg.ReceiverID]; ok {
				select {
				case client.send <- msg:
				default:
				}
			}
			h.mu.RUnlock()
		} else {
			// WebRTC signaling - forward only to receiver
			h.mu.RLock()
			if client, ok := h.clients[msg.ReceiverID]; ok {
				select {
				case client.send <- msg:
				default:
				}
			}
			h.mu.RUnlock()
		}
	}
}

func (h *Hub) HandleWebSocket(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": __("unauthorized")})
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("Upgrade error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": __("websocket upgrade failed")})
		return
	}

	client := &Client{
		userID: userID.(int),
		conn:   conn,
		hub:    h,
		send:   make(chan interface{}, 256),
	}

	h.register <- client

	go client.readPump()
	go client.writePump()
}

func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}

		var event map[string]interface{}
		if err := json.Unmarshal(data, &event); err != nil {
			continue
		}

		eventType, ok := event["type"].(string)
		if !ok {
			continue
		}

		switch eventType {
		case "message":
			c.handleMessageEvent(event)
		case "mark_delivered":
			c.handleMarkDelivered(event)
		case "mark_read":
			c.handleMarkRead(event)
		case "call_offer", "call_answer", "ice_candidate", "call_reject", "call_hangup":
			c.handleSignalingEvent(event)
		}
	}
}

func (c *Client) handleMessageEvent(event map[string]interface{}) {
	receiverID, ok := event["receiver_id"].(float64)
	if !ok {
		return
	}

	clientMsgID, _ := event["client_message_id"].(string)
	encrypted, _ := event["encrypted"].(bool)

	var (
		content        string
		e2eeVersion    int
		algorithm      string
		senderDeviceID string
		keyID          string
		iv             string
		ciphertext     string
		aad            string
	)

	if encrypted {
		e2eeVersionRaw, ok := event["e2ee_v"].(float64)
		if !ok {
			return
		}
		e2eeVersion = int(e2eeVersionRaw)
		algorithm, _ = event["alg"].(string)
		senderDeviceID, _ = event["sender_device_id"].(string)
		keyID, _ = event["key_id"].(string)
		iv, _ = event["iv"].(string)
		ciphertext, _ = event["ciphertext"].(string)
		aad, _ = event["aad"].(string)
		if e2eeVersion <= 0 || algorithm == "" || senderDeviceID == "" || keyID == "" || iv == "" || ciphertext == "" {
			return
		}
		content = ""
	} else {
		content, ok = event["content"].(string)
		if !ok || content == "" {
			return
		}
	}

	// Save message to database
	result, err := c.hub.db.Exec(`
		INSERT INTO messages (sender_id, receiver_id, content, encrypted, e2ee_v, alg, sender_device_id, key_id, iv, ciphertext, aad, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'sent', CURRENT_TIMESTAMP)
	`, c.userID, int(receiverID), content, encrypted, e2eeVersion, algorithm, senderDeviceID, keyID, iv, ciphertext, aad)

	if err != nil {
		log.Printf("Failed to save message: %v", err)
		return
	}

	msgID, _ := result.LastInsertId()

	// Broadcast to hub
	msg := &MessageEvent{
		Type:           "message",
		MessageID:      int(msgID),
		SenderID:       c.userID,
		ReceiverID:     int(receiverID),
		ClientMsgID:    clientMsgID,
		Content:        content,
		Encrypted:      encrypted,
		E2EEVersion:    e2eeVersion,
		Algorithm:      algorithm,
		SenderDeviceID: senderDeviceID,
		KeyID:          keyID,
		IV:             iv,
		Ciphertext:     ciphertext,
		AAD:            aad,
		Status:         "sent",
		CreatedAt:      time.Now(),
	}

	c.hub.broadcast <- msg
}

func (c *Client) handleSignalingEvent(event map[string]interface{}) {
	receiverID, ok := event["receiver_id"].(float64)
	if !ok {
		return
	}

	eventType, _ := event["type"].(string)
	payload, _ := event["payload"].(map[string]interface{})

	msg := &MessageEvent{
		Type:       eventType,
		SenderID:   c.userID,
		ReceiverID: int(receiverID),
		Payload:    payload,
	}

	c.hub.broadcast <- msg
}

func (c *Client) handleMarkDelivered(event map[string]interface{}) {
	messageID, ok := event["message_id"].(float64)
	if !ok {
		return
	}

	// Update database
	_, err := c.hub.db.Exec(`
		UPDATE messages 
		SET status = 'delivered', delivered_at = CURRENT_TIMESTAMP
		WHERE id = ? AND receiver_id = ?
	`, int(messageID), c.userID)

	if err != nil {
		log.Printf("Failed to mark delivered: %v", err)
		return
	}

	// Get sender ID
	var senderID int
	c.hub.db.QueryRow("SELECT sender_id FROM messages WHERE id = ?", int(messageID)).Scan(&senderID)

	// Broadcast status update
	msg := &MessageEvent{
		Type:       "status_update",
		MessageID:  int(messageID),
		Status:     "delivered",
		SenderID:   senderID,
		ReceiverID: c.userID,
	}

	c.hub.broadcast <- msg
}

func (c *Client) handleMarkRead(event map[string]interface{}) {
	messageID, ok := event["message_id"].(float64)
	if !ok {
		return
	}

	// Update database
	_, err := c.hub.db.Exec(`
		UPDATE messages 
		SET status = 'read', read_at = CURRENT_TIMESTAMP
		WHERE id = ? AND receiver_id = ?
	`, int(messageID), c.userID)

	if err != nil {
		log.Printf("Failed to mark read: %v", err)
		return
	}

	// Get sender ID
	var senderID int
	c.hub.db.QueryRow("SELECT sender_id FROM messages WHERE id = ?", int(messageID)).Scan(&senderID)

	// Broadcast status update
	msg := &MessageEvent{
		Type:       "status_update",
		MessageID:  int(messageID),
		Status:     "read",
		SenderID:   senderID,
		ReceiverID: c.userID,
	}

	c.hub.broadcast <- msg
}

func (c *Client) writePump() {
	ticker := time.NewTicker(54 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}

			data, _ := json.Marshal(message)
			w.Write(data)

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
