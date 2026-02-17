package handlers

import (
	"database/sql"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/4xmen/payambar/internal/models"
	"github.com/gin-gonic/gin"
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
	uploadDir     string
	maxUploadSize int64
	stunServers   string
	turnServer    string
	turnUsername  string
	turnPassword  string
}

func isLocalUploadPath(uploadDir, filePath string) bool {
	if filePath == "" {
		return false
	}
	absUploadDir, err := filepath.Abs(uploadDir)
	if err != nil {
		return false
	}
	absFilePath, err := filepath.Abs(filePath)
	if err != nil {
		return false
	}
	absUploadDir = filepath.Clean(absUploadDir)
	absFilePath = filepath.Clean(absFilePath)
	if absFilePath == absUploadDir {
		return true
	}
	return strings.HasPrefix(absFilePath, absUploadDir+string(os.PathSeparator))
}

func localPathFromAvatarURL(avatarURL, uploadDir string) (string, bool) {
	if !strings.HasPrefix(avatarURL, "/api/files/") {
		return "", false
	}
	fileName := filepath.Base(strings.TrimPrefix(avatarURL, "/api/files/"))
	if fileName == "." || fileName == "/" || fileName == "" {
		return "", false
	}
	return filepath.Join(uploadDir, fileName), true
}

func NewMessageHandler(db *sql.DB, onlineChecker OnlineChecker, uploadDir string, maxUploadSize int64, stunServers, turnServer, turnUsername, turnPassword string) *MessageHandler {
	var broadcaster MessageBroadcaster
	if b, ok := onlineChecker.(MessageBroadcaster); ok {
		broadcaster = b
	}
	return &MessageHandler{
		db:            db,
		onlineChecker: onlineChecker,
		broadcaster:   broadcaster,
		uploadDir:     uploadDir,
		maxUploadSize: maxUploadSize,
		stunServers:   stunServers,
		turnServer:    turnServer,
		turnUsername:  turnUsername,
		turnPassword:  turnPassword,
	}
}

// ConversationPreview represents a conversation in the list view
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

// GetConversation retrieves message history between two users
func (h *MessageHandler) GetConversation(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": __("unauthorized")})
		return
	}

	otherUserIDStr := c.Query("user_id")
	if otherUserIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": __("user_id query parameter required")})
		return
	}

	otherUserID, err := strconv.Atoi(otherUserIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": __("invalid user_id")})
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

	// Ensure conversation exists to prevent stale UI states
	pattern1 := strconv.Itoa(currentUserID) + "," + strconv.Itoa(otherUserID)
	pattern2 := strconv.Itoa(otherUserID) + "," + strconv.Itoa(currentUserID)
	var convExists bool
	if err := h.db.QueryRow(
		"SELECT EXISTS(SELECT 1 FROM conversations WHERE participants = ? OR participants = ?)",
		pattern1, pattern2,
	).Scan(&convExists); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": __("failed to check conversation")})
		return
	}
	if !convExists {
		c.JSON(http.StatusNotFound, gin.H{"error": __("conversation not found")})
		return
	}

	// Get messages between the two users with file attachments in single query (fixes N+1)
	rows, err := h.db.Query(`
		SELECT m.id, m.sender_id, m.receiver_id, m.content, m.status, m.created_at, m.delivered_at, m.read_at,
		       f.file_name, f.file_path
		FROM messages m
		LEFT JOIN files f ON f.message_id = m.id
		WHERE (m.sender_id = ? AND m.receiver_id = ?) OR (m.sender_id = ? AND m.receiver_id = ?)
		ORDER BY m.created_at DESC
		LIMIT ? OFFSET ?
	`, currentUserID, otherUserID, otherUserID, currentUserID, limit, offset)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": __("failed to fetch messages")})
		return
	}
	defer rows.Close()

	var messages []*models.Message
	for rows.Next() {
		msg := &models.Message{}
		var fileName, filePath sql.NullString
		if err := rows.Scan(&msg.ID, &msg.SenderID, &msg.ReceiverID, &msg.Content, &msg.Status, &msg.CreatedAt, &msg.DeliveredAt, &msg.ReadAt, &fileName, &filePath); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": __("failed to scan message")})
			return
		}
		// Set file attachment if present
		if fileName.Valid {
			msg.FileName = &fileName.String
			fileURL := "/api/files/" + filepath.Base(filePath.String)
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
		c.JSON(http.StatusUnauthorized, gin.H{"error": __("unauthorized")})
		return
	}

	currentUserID := userID.(int)

	var conversations []*ConversationPreview

	// Optimized approach: Fetch conversations for this user, then batch-load related data
	// Step 1: Get user's conversations in one query
	rows, err := h.db.Query(`
		SELECT id, participants FROM conversations 
		WHERE participants LIKE ? OR participants LIKE ?
	`, strconv.Itoa(currentUserID)+",%", "%,"+strconv.Itoa(currentUserID))

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": __("failed to fetch conversations")})
		return
	}

	type convData struct {
		id           int
		participants string
		otherUserID  int
	}
	var userConvs []convData
	var otherUserIDs []int

	for rows.Next() {
		var cd convData
		if err := rows.Scan(&cd.id, &cd.participants); err != nil {
			continue
		}

		// Parse participants to find the other user
		parts := strings.Split(cd.participants, ",")
		for _, p := range parts {
			pid, err := strconv.Atoi(strings.TrimSpace(p))
			if err != nil {
				continue
			}
			if pid != currentUserID {
				cd.otherUserID = pid
				break
			}
		}

		if cd.otherUserID > 0 {
			userConvs = append(userConvs, cd)
			otherUserIDs = append(otherUserIDs, cd.otherUserID)
		}
	}
	rows.Close()

	if len(userConvs) == 0 {
		c.JSON(http.StatusOK, gin.H{"conversations": []*ConversationPreview{}})
		return
	}

	// Step 2: Batch fetch user info for all other users
	userInfoMap := make(map[int]struct {
		username    string
		displayName sql.NullString
		avatarURL   sql.NullString
	})

	// Build placeholders for IN clause
	placeholders := strings.Repeat("?,", len(otherUserIDs))
	placeholders = placeholders[:len(placeholders)-1]

	args := make([]interface{}, len(otherUserIDs))
	for i, id := range otherUserIDs {
		args[i] = id
	}

	userRows, err := h.db.Query(
		`SELECT id, username, display_name, avatar_url FROM users WHERE id IN (`+placeholders+`)`,
		args...,
	)
	if err == nil {
		for userRows.Next() {
			var id int
			var info struct {
				username    string
				displayName sql.NullString
				avatarURL   sql.NullString
			}
			if err := userRows.Scan(&id, &info.username, &info.displayName, &info.avatarURL); err == nil {
				userInfoMap[id] = info
			}
		}
		userRows.Close()
	}

	// Step 3: Build conversations with the fetched data
	// For last_message_at and unread_count, we still need individual queries but only for filtered convs
	for _, cd := range userConvs {
		userInfo, ok := userInfoMap[cd.otherUserID]
		if !ok {
			continue
		}

		var lastMessageAt sql.NullTime
		var unreadCount int

		h.db.QueryRow(`
			SELECT MAX(created_at) FROM messages
			WHERE (sender_id = ? AND receiver_id = ?) OR (sender_id = ? AND receiver_id = ?)
		`, currentUserID, cd.otherUserID, cd.otherUserID, currentUserID).Scan(&lastMessageAt)

		h.db.QueryRow(`
			SELECT COUNT(*) FROM messages
			WHERE receiver_id = ? AND sender_id = ? AND read_at IS NULL
		`, currentUserID, cd.otherUserID).Scan(&unreadCount)

		// Parse participants for response
		parts := strings.Split(cd.participants, ",")
		var participantIDs []int
		for _, p := range parts {
			pid, _ := strconv.Atoi(strings.TrimSpace(p))
			if pid > 0 {
				participantIDs = append(participantIDs, pid)
			}
		}

		conv := &ConversationPreview{
			ID:           cd.id,
			UserID:       cd.otherUserID,
			Username:     userInfo.username,
			IsOnline:     h.onlineChecker != nil && h.onlineChecker.IsUserOnline(cd.otherUserID),
			UnreadCount:  unreadCount,
			Participants: participantIDs,
		}

		if userInfo.displayName.Valid {
			conv.DisplayName = &userInfo.displayName.String
		}
		if userInfo.avatarURL.Valid {
			conv.AvatarURL = &userInfo.avatarURL.String
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
	for i := 0; i < len(conversations)-1; i++ {
		for j := i + 1; j < len(conversations); j++ {
			if conversations[j].LastMessageAt.After(conversations[i].LastMessageAt) {
				conversations[i], conversations[j] = conversations[j], conversations[i]
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{"conversations": conversations})
}

// MarkAsDelivered marks a message as delivered
func (h *MessageHandler) MarkAsDelivered(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": __("unauthorized")})
		return
	}

	messageIDStr := c.Param("id")
	messageID, err := strconv.Atoi(messageIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": __("invalid message id")})
		return
	}

	currentUserID := userID.(int)

	// Verify message belongs to current user as receiver
	var receiverID int
	err = h.db.QueryRow("SELECT receiver_id FROM messages WHERE id = ?", messageID).Scan(&receiverID)
	if err != nil || receiverID != currentUserID {
		c.JSON(http.StatusForbidden, gin.H{"error": __("cannot mark this message")})
		return
	}

	_, err = h.db.Exec(`
		UPDATE messages 
		SET status = 'delivered', delivered_at = CURRENT_TIMESTAMP
		WHERE id = ? AND status = 'sent'
	`, messageID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": __("failed to update message")})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "delivered"})
}

// MarkAsRead marks a message as read
func (h *MessageHandler) MarkAsRead(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": __("unauthorized")})
		return
	}

	messageIDStr := c.Param("id")
	messageID, err := strconv.Atoi(messageIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": __("invalid message id")})
		return
	}

	currentUserID := userID.(int)

	// Verify message belongs to current user as receiver
	var receiverID int
	err = h.db.QueryRow("SELECT receiver_id FROM messages WHERE id = ?", messageID).Scan(&receiverID)
	if err != nil || receiverID != currentUserID {
		c.JSON(http.StatusForbidden, gin.H{"error": __("cannot mark this message")})
		return
	}

	_, err = h.db.Exec(`
		UPDATE messages 
		SET status = 'read', read_at = CURRENT_TIMESTAMP
		WHERE id = ? AND receiver_id = ?
	`, messageID, currentUserID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": __("failed to update message")})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "read"})
}

// DeleteMessage deletes a message (only sender can delete)
func (h *MessageHandler) DeleteMessage(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": __("unauthorized")})
		return
	}

	messageIDStr := c.Param("id")
	messageID, err := strconv.Atoi(messageIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": __("invalid message id")})
		return
	}

	currentUserID := userID.(int)

	// Verify message belongs to current user as sender
	var senderID int
	err = h.db.QueryRow("SELECT sender_id FROM messages WHERE id = ?", messageID).Scan(&senderID)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": __("message not found")})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": __("failed to fetch message")})
		return
	}

	if senderID != currentUserID {
		c.JSON(http.StatusForbidden, gin.H{"error": __("can only delete own messages")})
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": __("failed to delete message")})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

// GetUserProfile retrieves public user profile
func (h *MessageHandler) GetUserProfile(c *gin.Context) {
	username := c.Param("username")
	username = strings.TrimSpace(username)

	if username == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": __("username required")})
		return
	}

	var user models.User
	err := h.db.QueryRow(`
		SELECT id, username, display_name, avatar_url, created_at FROM users WHERE username = ?
	`, username).Scan(&user.ID, &user.Username, &user.DisplayName, &user.AvatarURL, &user.CreatedAt)

	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": __("user not found")})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": __("failed to fetch user")})
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
		c.JSON(http.StatusUnauthorized, gin.H{"error": __("unauthorized")})
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": __("failed to fetch users")})
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
		c.JSON(http.StatusUnauthorized, gin.H{"error": __("unauthorized")})
		return
	}

	var req struct {
		ParticipantID int `json:"participant_id" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": __("invalid request")})
		return
	}

	if req.ParticipantID == userID.(int) {
		c.JSON(http.StatusBadRequest, gin.H{"error": __("cannot create conversation with yourself")})
		return
	}

	// Check if participant exists
	var exists2 bool
	err := h.db.QueryRow("SELECT EXISTS(SELECT 1 FROM users WHERE id = ?)", req.ParticipantID).Scan(&exists2)
	if err != nil || !exists2 {
		c.JSON(http.StatusNotFound, gin.H{"error": __("participant not found")})
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": __("failed to create conversation")})
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

// DeleteConversation deletes a conversation and its messages/files (participant-only)
func (h *MessageHandler) DeleteConversation(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": __("unauthorized")})
		return
	}

	convIDStr := c.Param("id")
	convID, err := strconv.Atoi(convIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": __("invalid conversation id")})
		return
	}

	currentUserID := userID.(int)

	tx, err := h.db.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": __("failed to start transaction")})
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	var participantsStr string
	err = tx.QueryRow("SELECT participants FROM conversations WHERE id = ?", convID).Scan(&participantsStr)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": __("conversation not found")})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": __("failed to fetch conversation")})
		return
	}

	parts := strings.Split(participantsStr, ",")
	participantIDs := make([]int, 0, len(parts))
	hasCurrent := false
	for _, p := range parts {
		pid, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || pid <= 0 {
			continue
		}
		participantIDs = append(participantIDs, pid)
		if pid == currentUserID {
			hasCurrent = true
		}
	}

	if !hasCurrent {
		c.JSON(http.StatusForbidden, gin.H{"error": __("not a participant")})
		return
	}
	if len(participantIDs) < 2 {
		c.JSON(http.StatusBadRequest, gin.H{"error": __("invalid participants")})
		return
	}
	if len(participantIDs) > 2 {
		participantIDs = participantIDs[:2]
	}

	p1 := participantIDs[0]
	p2 := participantIDs[1]

	filePaths := []string{}
	fileRows, err := tx.Query(`
		SELECT f.file_path FROM files f
		INNER JOIN messages m ON f.message_id = m.id
		WHERE (m.sender_id = ? AND m.receiver_id = ?) OR (m.sender_id = ? AND m.receiver_id = ?)
	`, p1, p2, p2, p1)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": __("failed to fetch files")})
		return
	}
	for fileRows.Next() {
		var fp string
		if err := fileRows.Scan(&fp); err == nil && fp != "" {
			filePaths = append(filePaths, fp)
		}
	}
	fileRows.Close()

	_, err = tx.Exec(`
		DELETE FROM files WHERE message_id IN (
			SELECT id FROM messages
			WHERE (sender_id = ? AND receiver_id = ?) OR (sender_id = ? AND receiver_id = ?)
		)
	`, p1, p2, p2, p1)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": __("failed to delete files")})
		return
	}

	_, err = tx.Exec(`
		DELETE FROM messages
		WHERE (sender_id = ? AND receiver_id = ?) OR (sender_id = ? AND receiver_id = ?)
	`, p1, p2, p2, p1)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": __("failed to delete messages")})
		return
	}

	_, err = tx.Exec("DELETE FROM conversations WHERE id = ?", convID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": __("failed to delete conversation")})
		return
	}

	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": __("failed to commit delete")})
		return
	}
	committed = true

	for _, fp := range filePaths {
		if isLocalUploadPath(h.uploadDir, fp) {
			_ = os.Remove(fp)
		}
	}

	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

// UploadFile handles file uploads
func (h *MessageHandler) UploadFile(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": __("unauthorized")})
		return
	}

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": __("file is required")})
		return
	}
	defer file.Close()

	if header.Size > h.maxUploadSize {
		c.JSON(http.StatusBadRequest, gin.H{"error": __("file too large")})
		return
	}

	receiverIDStr := c.PostForm("receiver_id")
	receiverID, err := strconv.Atoi(receiverIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": __("invalid receiver_id")})
		return
	}

	// Create message for the file
	result, err := h.db.Exec(`
		INSERT INTO messages (sender_id, receiver_id, content, status, created_at)
		VALUES (?, ?, ?, 'sent', CURRENT_TIMESTAMP)
	`, userID.(int), receiverID, "[فایل]")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": __("failed to create message")})
		return
	}

	messageID, _ := result.LastInsertId()

	// Generate unique filename with path traversal protection
	// filepath.Base() strips any directory components from the filename
	safeFilename := filepath.Base(header.Filename)
	filename := strconv.FormatInt(time.Now().UnixNano(), 10) + "_" + safeFilename
	uploadPath := filepath.Join(h.uploadDir, filename)

	// Save file
	if err := c.SaveUploadedFile(header, uploadPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": __("failed to save file")})
		return
	}

	// Save file record
	_, err = h.db.Exec(`
		INSERT INTO files (message_id, file_name, file_path, file_size, content_type, created_at)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	`, messageID, safeFilename, uploadPath, header.Size, header.Header.Get("Content-Type"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": __("failed to save file record")})
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
		c.JSON(http.StatusUnauthorized, gin.H{"error": __("unauthorized")})
		return
	}

	var req struct {
		DisplayName string `json:"display_name"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": __("invalid request")})
		return
	}

	_, err := h.db.Exec(`
		UPDATE users SET display_name = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?
	`, req.DisplayName, userID.(int))

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": __("failed to update profile")})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "updated", "display_name": req.DisplayName})
}

// UploadAvatar handles avatar image uploads
func (h *MessageHandler) UploadAvatar(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": __("unauthorized")})
		return
	}

	file, header, err := c.Request.FormFile("avatar")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": __("avatar file is required")})
		return
	}
	defer file.Close()

	// Validate content type
	contentType := header.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "image/") {
		c.JSON(http.StatusBadRequest, gin.H{"error": __("file must be an image")})
		return
	}

	// Limit file size to 2MB
	if header.Size > 500*1024 {
		c.JSON(http.StatusBadRequest, gin.H{"error": __("avatar must be smaller than 500KB")})
		return
	}

	// Generate unique filename with path traversal protection
	safeFilename := filepath.Base(header.Filename)
	ext := ".jpg"
	if strings.Contains(safeFilename, ".") {
		parts := strings.Split(safeFilename, ".")
		ext = "." + parts[len(parts)-1]
	}
	filename := "avatar_" + strconv.Itoa(userID.(int)) + "_" + strconv.FormatInt(time.Now().UnixNano(), 10) + ext
	uploadPath := filepath.Join(h.uploadDir, filename)

	// Save file
	if err := c.SaveUploadedFile(header, uploadPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": __("failed to save avatar")})
		return
	}

	// Update user's avatar_url
	avatarURL := "/api/files/" + filename
	_, err = h.db.Exec(`
		UPDATE users SET avatar_url = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?
	`, avatarURL, userID.(int))

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": __("failed to update avatar")})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"avatar_url": avatarURL,
	})
}

// DeleteAccount deletes the current user's account and all related data/files
func (h *MessageHandler) DeleteAccount(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": __("unauthorized")})
		return
	}

	currentUserID := userID.(int)

	tx, err := h.db.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": __("failed to start transaction")})
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	var avatarURL sql.NullString
	if err := tx.QueryRow("SELECT avatar_url FROM users WHERE id = ?", currentUserID).Scan(&avatarURL); err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": __("user not found")})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": __("failed to fetch user")})
		return
	}

	filePaths := []string{}
	fileRows, err := tx.Query(`
		SELECT f.file_path FROM files f
		INNER JOIN messages m ON f.message_id = m.id
		WHERE m.sender_id = ? OR m.receiver_id = ?
	`, currentUserID, currentUserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": __("failed to fetch files")})
		return
	}
	for fileRows.Next() {
		var fp string
		if err := fileRows.Scan(&fp); err == nil && fp != "" {
			filePaths = append(filePaths, fp)
		}
	}
	fileRows.Close()

	_, err = tx.Exec(`
		DELETE FROM files WHERE message_id IN (
			SELECT id FROM messages WHERE sender_id = ? OR receiver_id = ?
		)
	`, currentUserID, currentUserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": __("failed to delete files")})
		return
	}

	_, err = tx.Exec("DELETE FROM messages WHERE sender_id = ? OR receiver_id = ?", currentUserID, currentUserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": __("failed to delete messages")})
		return
	}

	pattern1 := strconv.Itoa(currentUserID) + ",%"
	pattern2 := "%," + strconv.Itoa(currentUserID)
	_, err = tx.Exec("DELETE FROM conversations WHERE participants LIKE ? OR participants LIKE ?", pattern1, pattern2)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": __("failed to delete conversations")})
		return
	}

	_, err = tx.Exec("DELETE FROM users WHERE id = ?", currentUserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": __("failed to delete user")})
		return
	}

	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": __("failed to commit delete")})
		return
	}
	committed = true

	for _, fp := range filePaths {
		if isLocalUploadPath(h.uploadDir, fp) {
			_ = os.Remove(fp)
		}
	}

	if avatarURL.Valid {
		if avatarPath, ok := localPathFromAvatarURL(avatarURL.String, h.uploadDir); ok {
			if isLocalUploadPath(h.uploadDir, avatarPath) {
				_ = os.Remove(avatarPath)
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

// GetMyProfile returns the current user's profile
func (h *MessageHandler) GetMyProfile(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": __("unauthorized")})
		return
	}

	var user models.User
	err := h.db.QueryRow(`
		SELECT id, username, display_name, avatar_url, created_at FROM users WHERE id = ?
	`, userID.(int)).Scan(&user.ID, &user.Username, &user.DisplayName, &user.AvatarURL, &user.CreatedAt)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": __("failed to fetch profile")})
		return
	}

	c.JSON(http.StatusOK, user)
}

// GetWebRTCConfig returns STUN/TURN server configuration
func (h *MessageHandler) GetWebRTCConfig(c *gin.Context) {
	iceServers := []gin.H{}

	// Add STUN servers
	stunServers := strings.Split(h.stunServers, ",")
	for _, s := range stunServers {
		s = strings.TrimSpace(s)
		if s != "" {
			iceServers = append(iceServers, gin.H{"urls": s})
		}
	}

	// Add TURN server if configured (provide both UDP and TCP for mobile compatibility)
	if h.turnServer != "" {
		// Parse the TURN server URL to generate both UDP and TCP variants
		// e.g., turn:turn.example.com:3478 becomes [turn:...:3478, turn:...:3478?transport=tcp]
		turnUrls := []string{h.turnServer}
		// Add TCP transport variant for mobile networks that block UDP
		if !strings.Contains(h.turnServer, "transport=") {
			turnUrls = append(turnUrls, h.turnServer+"?transport=tcp")
		}

		turnConfig := gin.H{
			"urls": turnUrls,
		}
		if h.turnUsername != "" {
			turnConfig["username"] = h.turnUsername
		}
		if h.turnPassword != "" {
			turnConfig["credential"] = h.turnPassword
		}
		iceServers = append(iceServers, turnConfig)
	}

	c.JSON(http.StatusOK, gin.H{"iceServers": iceServers})
}
