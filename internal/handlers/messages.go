package handlers

import (
	"database/sql"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sadeghpm/payambar/internal/models"
)

// OnlineChecker interface for checking user online status
type OnlineChecker interface {
	IsUserOnline(userID int) bool
	GetOnlineUserIDs() []int
}

// MessageBroadcaster interface for broadcasting messages via WebSocket
type MessageBroadcaster interface {
	BroadcastMessage(messageID, senderID, receiverID int, content, status, fileName, fileURL string)
}

type MessageHandler struct {
	db            *sql.DB
	onlineChecker OnlineChecker
	broadcaster   MessageBroadcaster
}

func NewMessageHandler(db *sql.DB, onlineChecker OnlineChecker) *MessageHandler {
	var broadcaster MessageBroadcaster
	if b, ok := onlineChecker.(MessageBroadcaster); ok {
		broadcaster = b
	}
	return &MessageHandler{db: db, onlineChecker: onlineChecker, broadcaster: broadcaster}
}

// GetConversation retrieves message history between two users
func (h *MessageHandler) GetConversation(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	otherUserIDStr := c.Query("user_id")
	if otherUserIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user_id query parameter required"})
		return
	}

	otherUserID, err := strconv.Atoi(otherUserIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user_id"})
		return
	}

	limitStr := c.DefaultQuery("limit", "50")
	offsetStr := c.DefaultQuery("offset", "0")

	limit, _ := strconv.Atoi(limitStr)
	offset, _ := strconv.Atoi(offsetStr)

	if limit > 100 {
		limit = 100
	}

	currentUserID := userID.(int)

	// Get messages between the two users
	rows, err := h.db.Query(`
		SELECT id, sender_id, receiver_id, content, status, created_at, delivered_at, read_at
		FROM messages
		WHERE (sender_id = ? AND receiver_id = ?) OR (sender_id = ? AND receiver_id = ?)
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?
	`, currentUserID, otherUserID, otherUserID, currentUserID, limit, offset)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch messages"})
		return
	}
	defer rows.Close()

	var messages []*models.Message
	for rows.Next() {
		msg := &models.Message{}
		if err := rows.Scan(&msg.ID, &msg.SenderID, &msg.ReceiverID, &msg.Content, &msg.Status, &msg.CreatedAt, &msg.DeliveredAt, &msg.ReadAt); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to scan message"})
			return
		}
		// Check if this message has a file attachment
		var fileName, filePath sql.NullString
		h.db.QueryRow(`SELECT file_name, file_path FROM files WHERE message_id = ?`, msg.ID).Scan(&fileName, &filePath)
		if fileName.Valid {
			msg.FileName = &fileName.String
			fileURL := "/api/files/" + strings.TrimPrefix(filePath.String, "./data/uploads/")
			msg.FileURL = &fileURL
		}
		messages = append(messages, msg)
	}

	// Reverse to get oldest first
	for i := len(messages)/2 - 1; i >= 0; i-- {
		opp := len(messages) - 1 - i
		messages[i], messages[opp] = messages[opp], messages[i]
	}

	c.JSON(http.StatusOK, gin.H{"messages": messages})
}

// GetConversations retrieves all conversations for the current user
func (h *MessageHandler) GetConversations(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	currentUserID := userID.(int)

	type ConversationPreview struct {
		ID            int       `json:"id"`
		UserID        int       `json:"user_id"`
		Username      string    `json:"username"`
		DisplayName   *string   `json:"display_name,omitempty"`
		AvatarURL     *string   `json:"avatar_url,omitempty"`
		IsOnline      bool      `json:"is_online"`
		LastMessageAt time.Time `json:"last_message_at"`
		UnreadCount   int       `json:"unread_count"`
		Participants  []int     `json:"participants"`
	}

	var conversations []*ConversationPreview

	// Get all conversations and filter in Go for correct participant matching
	// Then batch fetch user info and message stats
	rows, err := h.db.Query(`SELECT id, participants, created_at FROM conversations`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch conversations"})
		return
	}

	type convData struct {
		id           int
		participants string
		createdAt    time.Time
		otherUserID  int
	}
	var userConvs []convData

	for rows.Next() {
		var cd convData
		if err := rows.Scan(&cd.id, &cd.participants, &cd.createdAt); err != nil {
			continue
		}

		// Parse participants and check if current user is involved
		parts := strings.Split(cd.participants, ",")
		isParticipant := false
		for _, p := range parts {
			pid, err := strconv.Atoi(strings.TrimSpace(p))
			if err != nil {
				continue
			}
			if pid == currentUserID {
				isParticipant = true
			} else {
				cd.otherUserID = pid
			}
		}

		if isParticipant && cd.otherUserID > 0 {
			userConvs = append(userConvs, cd)
		}
	}
	rows.Close()

	// Now batch fetch all needed data for filtered conversations
	for _, cd := range userConvs {
		var username string
		var displayName, avatarURL sql.NullString
		var lastMessageAt sql.NullTime
		var unreadCount int

		// Get user info
		err := h.db.QueryRow("SELECT username, display_name, avatar_url FROM users WHERE id = ?", cd.otherUserID).
			Scan(&username, &displayName, &avatarURL)
		if err != nil {
			continue
		}

		// Get last message time
		h.db.QueryRow(`
			SELECT MAX(created_at) FROM messages
			WHERE (sender_id = ? AND receiver_id = ?) OR (sender_id = ? AND receiver_id = ?)
		`, currentUserID, cd.otherUserID, cd.otherUserID, currentUserID).Scan(&lastMessageAt)

		// Get unread count
		h.db.QueryRow(`
			SELECT COUNT(*) FROM messages
			WHERE receiver_id = ? AND sender_id = ? AND read_at IS NULL
		`, currentUserID, cd.otherUserID).Scan(&unreadCount)

		// Parse participants for response
		parts := strings.Split(cd.participants, ",")
		var participants []int
		for _, p := range parts {
			pid, _ := strconv.Atoi(strings.TrimSpace(p))
			if pid > 0 {
				participants = append(participants, pid)
			}
		}

		conv := &ConversationPreview{
			ID:           cd.id,
			UserID:       cd.otherUserID,
			Username:     username,
			IsOnline:     h.onlineChecker != nil && h.onlineChecker.IsUserOnline(cd.otherUserID),
			UnreadCount:  unreadCount,
			Participants: participants,
		}

		if displayName.Valid {
			conv.DisplayName = &displayName.String
		}
		if avatarURL.Valid {
			conv.AvatarURL = &avatarURL.String
		}
		if lastMessageAt.Valid {
			conv.LastMessageAt = lastMessageAt.Time
		}

		conversations = append(conversations, conv)
	}

	if conversations == nil {
		conversations = []*ConversationPreview{}
	}

	// Sort by last_message_at descending
	sort.Slice(conversations, func(i, j int) bool {
		return conversations[i].LastMessageAt.After(conversations[j].LastMessageAt)
	})

	c.JSON(http.StatusOK, gin.H{"conversations": conversations})
}

// MarkAsDelivered marks a message as delivered
func (h *MessageHandler) MarkAsDelivered(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	messageIDStr := c.Param("id")
	messageID, err := strconv.Atoi(messageIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid message id"})
		return
	}

	currentUserID := userID.(int)

	// Verify message belongs to current user as receiver
	var receiverID int
	err = h.db.QueryRow("SELECT receiver_id FROM messages WHERE id = ?", messageID).Scan(&receiverID)
	if err != nil || receiverID != currentUserID {
		c.JSON(http.StatusForbidden, gin.H{"error": "cannot mark this message"})
		return
	}

	_, err = h.db.Exec(`
		UPDATE messages 
		SET status = 'delivered', delivered_at = CURRENT_TIMESTAMP
		WHERE id = ? AND status = 'sent'
	`, messageID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update message"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "delivered"})
}

// MarkAsRead marks a message as read
func (h *MessageHandler) MarkAsRead(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	messageIDStr := c.Param("id")
	messageID, err := strconv.Atoi(messageIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid message id"})
		return
	}

	currentUserID := userID.(int)

	// Verify message belongs to current user as receiver
	var receiverID int
	err = h.db.QueryRow("SELECT receiver_id FROM messages WHERE id = ?", messageID).Scan(&receiverID)
	if err != nil || receiverID != currentUserID {
		c.JSON(http.StatusForbidden, gin.H{"error": "cannot mark this message"})
		return
	}

	_, err = h.db.Exec(`
		UPDATE messages 
		SET status = 'read', read_at = CURRENT_TIMESTAMP
		WHERE id = ? AND receiver_id = ?
	`, messageID, currentUserID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update message"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "read"})
}

// DeleteMessage deletes a message (only sender can delete)
func (h *MessageHandler) DeleteMessage(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	messageIDStr := c.Param("id")
	messageID, err := strconv.Atoi(messageIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid message id"})
		return
	}

	currentUserID := userID.(int)

	// Verify message belongs to current user as sender
	var senderID int
	err = h.db.QueryRow("SELECT sender_id FROM messages WHERE id = ?", messageID).Scan(&senderID)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "message not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch message"})
		return
	}

	if senderID != currentUserID {
		c.JSON(http.StatusForbidden, gin.H{"error": "can only delete own messages"})
		return
	}

	// Delete associated file if exists
	var filePath sql.NullString
	h.db.QueryRow("SELECT file_path FROM files WHERE message_id = ?", messageID).Scan(&filePath)
	if filePath.Valid && filePath.String != "" {
		// Try to delete the file (ignore errors)
		os.Remove(filePath.String)
		h.db.Exec("DELETE FROM files WHERE message_id = ?", messageID)
	}

	// Delete the message
	_, err = h.db.Exec("DELETE FROM messages WHERE id = ? AND sender_id = ?", messageID, currentUserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete message"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

// GetUserProfile retrieves public user profile
func (h *MessageHandler) GetUserProfile(c *gin.Context) {
	username := c.Param("username")
	username = strings.TrimSpace(username)

	if username == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username required"})
		return
	}

	var user models.User
	err := h.db.QueryRow(`
		SELECT id, username, display_name, avatar_url, created_at FROM users WHERE username = ?
	`, username).Scan(&user.ID, &user.Username, &user.DisplayName, &user.AvatarURL, &user.CreatedAt)

	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch user"})
		return
	}

	// Return user with online status
	isOnline := h.onlineChecker != nil && h.onlineChecker.IsUserOnline(user.ID)
	c.JSON(http.StatusOK, gin.H{
		"id":           user.ID,
		"username":     user.Username,
		"display_name": user.DisplayName,
		"avatar_url":   user.AvatarURL,
		"is_online":    isOnline,
		"created_at":   user.CreatedAt,
	})
}

// GetUsers retrieves a list of all users except current user, optionally filtered by search query
func (h *MessageHandler) GetUsers(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	searchQuery := strings.TrimSpace(c.Query("q"))

	var rows *sql.Rows
	var err error

	if searchQuery != "" {
		// Search by username (case-insensitive)
		rows, err = h.db.Query(`
			SELECT id, username, display_name, avatar_url, created_at FROM users 
			WHERE id != ? AND (username LIKE ? OR display_name LIKE ?)
			ORDER BY username LIMIT 20
		`, userID, "%"+searchQuery+"%", "%"+searchQuery+"%")
	} else {
		rows, err = h.db.Query(`
			SELECT id, username, display_name, avatar_url, created_at FROM users WHERE id != ? ORDER BY username LIMIT 20
		`, userID)
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch users"})
		return
	}
	defer rows.Close()

	type UserWithOnline struct {
		ID          int     `json:"id"`
		Username    string  `json:"username"`
		DisplayName *string `json:"display_name,omitempty"`
		AvatarURL   *string `json:"avatar_url,omitempty"`
		IsOnline    bool    `json:"is_online"`
	}

	var users []UserWithOnline
	for rows.Next() {
		var user models.User
		if err := rows.Scan(&user.ID, &user.Username, &user.DisplayName, &user.AvatarURL, &user.CreatedAt); err != nil {
			continue
		}
		u := UserWithOnline{
			ID:       user.ID,
			Username: user.Username,
			IsOnline: h.onlineChecker != nil && h.onlineChecker.IsUserOnline(user.ID),
		}
		if user.DisplayName != nil {
			u.DisplayName = user.DisplayName
		}
		if user.AvatarURL != nil {
			u.AvatarURL = user.AvatarURL
		}
		users = append(users, u)
	}

	if users == nil {
		users = []UserWithOnline{}
	}

	c.JSON(http.StatusOK, users)
}

// CreateConversation creates a new conversation between two users
func (h *MessageHandler) CreateConversation(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req struct {
		ParticipantID int `json:"participant_id" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	if req.ParticipantID == userID.(int) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot create conversation with yourself"})
		return
	}

	// Check if participant exists
	var exists2 bool
	err := h.db.QueryRow("SELECT EXISTS(SELECT 1 FROM users WHERE id = ?)", req.ParticipantID).Scan(&exists2)
	if err != nil || !exists2 {
		c.JSON(http.StatusNotFound, gin.H{"error": "participant not found"})
		return
	}

	// Check if conversation already exists by checking both participant orderings
	currentUID := userID.(int)
	pattern1 := strconv.Itoa(currentUID) + "," + strconv.Itoa(req.ParticipantID)
	pattern2 := strconv.Itoa(req.ParticipantID) + "," + strconv.Itoa(currentUID)

	var existingID int
	err = h.db.QueryRow(`
		SELECT id FROM conversations 
		WHERE participants = ? OR participants = ?
		LIMIT 1
	`, pattern1, pattern2).Scan(&existingID)

	if err == nil {
		// Conversation already exists - get username
		var username string
		h.db.QueryRow("SELECT username FROM users WHERE id = ?", req.ParticipantID).Scan(&username)

		c.JSON(http.StatusOK, gin.H{
			"id":              existingID,
			"participants":    []int{currentUID, req.ParticipantID},
			"user_id":         req.ParticipantID,
			"username":        username,
			"last_message_at": time.Now(),
			"unread_count":    0,
		})
		return
	}

	// Create new conversation
	participants := strconv.Itoa(userID.(int)) + "," + strconv.Itoa(req.ParticipantID)
	result, err := h.db.Exec(`
		INSERT INTO conversations (participants) VALUES (?)
	`, participants)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create conversation"})
		return
	}

	id, _ := result.LastInsertId()
	participantIDs := []int{userID.(int), req.ParticipantID}

	// Get username for the response
	var username string
	h.db.QueryRow("SELECT username FROM users WHERE id = ?", req.ParticipantID).Scan(&username)

	c.JSON(http.StatusCreated, gin.H{
		"id":              id,
		"participants":    participantIDs,
		"user_id":         req.ParticipantID,
		"username":        username,
		"last_message_at": time.Now(),
		"unread_count":    0,
	})
}

// UploadFile handles file uploads
func (h *MessageHandler) UploadFile(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file is required"})
		return
	}
	defer file.Close()

	receiverIDStr := c.PostForm("receiver_id")
	receiverID, err := strconv.Atoi(receiverIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid receiver_id"})
		return
	}

	// Create message for the file
	result, err := h.db.Exec(`
		INSERT INTO messages (sender_id, receiver_id, content, status, created_at)
		VALUES (?, ?, ?, 'sent', CURRENT_TIMESTAMP)
	`, userID.(int), receiverID, "[فایل]")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create message"})
		return
	}

	messageID, _ := result.LastInsertId()

	// Generate unique filename
	filename := strconv.FormatInt(time.Now().UnixNano(), 10) + "_" + header.Filename
	filepath := "./data/uploads/" + filename

	// Save file
	if err := c.SaveUploadedFile(header, filepath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save file"})
		return
	}

	// Save file record
	_, err = h.db.Exec(`
		INSERT INTO files (message_id, file_name, file_path, file_size, content_type, created_at)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	`, messageID, header.Filename, filepath, header.Size, header.Header.Get("Content-Type"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save file record"})
		return
	}

	fileURL := "/api/files/" + filename

	// Broadcast file message to receiver via WebSocket
	if h.broadcaster != nil {
		h.broadcaster.BroadcastMessage(
			int(messageID),
			userID.(int),
			receiverID,
			"📎 "+header.Filename,
			"sent",
			header.Filename,
			fileURL,
		)
	}

	c.JSON(http.StatusOK, gin.H{
		"message_id": messageID,
		"file_name":  header.Filename,
		"file_size":  header.Size,
		"file_url":   fileURL,
	})
}

// UpdateProfile updates the current user's profile
func (h *MessageHandler) UpdateProfile(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req struct {
		DisplayName string `json:"display_name"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	_, err := h.db.Exec(`
		UPDATE users SET display_name = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?
	`, req.DisplayName, userID.(int))

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update profile"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "updated", "display_name": req.DisplayName})
}

// UploadAvatar handles avatar image uploads
func (h *MessageHandler) UploadAvatar(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	file, header, err := c.Request.FormFile("avatar")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "avatar file is required"})
		return
	}
	defer file.Close()

	// Validate content type
	contentType := header.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "image/") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file must be an image"})
		return
	}

	// Limit file size to 2MB
	if header.Size > 2*1024*1024 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "avatar must be smaller than 2MB"})
		return
	}

	// Generate unique filename
	ext := ".jpg"
	if strings.Contains(header.Filename, ".") {
		parts := strings.Split(header.Filename, ".")
		ext = "." + parts[len(parts)-1]
	}
	filename := "avatar_" + strconv.Itoa(userID.(int)) + "_" + strconv.FormatInt(time.Now().UnixNano(), 10) + ext
	filepath := "./data/uploads/" + filename

	// Save file
	if err := c.SaveUploadedFile(header, filepath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save avatar"})
		return
	}

	// Update user's avatar_url
	avatarURL := "/api/files/" + filename
	_, err = h.db.Exec(`
		UPDATE users SET avatar_url = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?
	`, avatarURL, userID.(int))

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update avatar"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"avatar_url": avatarURL,
	})
}

// GetMyProfile returns the current user's profile
func (h *MessageHandler) GetMyProfile(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var user models.User
	err := h.db.QueryRow(`
		SELECT id, username, display_name, avatar_url, created_at FROM users WHERE id = ?
	`, userID.(int)).Scan(&user.ID, &user.Username, &user.DisplayName, &user.AvatarURL, &user.CreatedAt)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch profile"})
		return
	}

	c.JSON(http.StatusOK, user)
}
