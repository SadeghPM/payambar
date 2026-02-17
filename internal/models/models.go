package models

import "time"

type User struct {
	ID          int       `json:"id"`
	Username    string    `json:"username"`
	DisplayName *string   `json:"display_name,omitempty"`
	AvatarURL   *string   `json:"avatar_url,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

type Message struct {
	ID          int        `json:"id"`
	SenderID    int        `json:"sender_id"`
	ReceiverID  int        `json:"receiver_id"`
	Content     string     `json:"content"`
	Status      string     `json:"status"` // sent, delivered, read
	CreatedAt   time.Time  `json:"created_at"`
	DeliveredAt *time.Time `json:"delivered_at,omitempty"`
	ReadAt      *time.Time `json:"read_at,omitempty"`
	FileName    *string    `json:"file_name,omitempty"`
	FileURL     *string    `json:"file_url,omitempty"`
	FileType    *string    `json:"file_content_type,omitempty"`
}

type File struct {
	ID          int       `json:"id"`
	MessageID   int       `json:"message_id"`
	FileName    string    `json:"file_name"`
	FilePath    string    `json:"-"`
	FileSize    int64     `json:"file_size"`
	ContentType string    `json:"content_type"`
	CreatedAt   time.Time `json:"created_at"`
}

type MessageWithFile struct {
	Message *Message `json:"message"`
	File    *File    `json:"file,omitempty"`
}
