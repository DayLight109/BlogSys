package model

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"
)

// JSONRaw is a passthrough JSON column type — we store whatever bytes the
// caller hands us, untouched. Used for chat message content (which can be a
// string OR a multimodal parts array) and arbitrary client-shaped attachment
// metadata. Empty bytes serialize to SQL NULL.
type JSONRaw []byte

func (j JSONRaw) Value() (driver.Value, error) {
	if len(j) == 0 {
		return nil, nil
	}
	return []byte(j), nil
}

func (j *JSONRaw) Scan(src any) error {
	if src == nil {
		*j = nil
		return nil
	}
	switch v := src.(type) {
	case []byte:
		// Copy — driver may reuse the buffer.
		out := make([]byte, len(v))
		copy(out, v)
		*j = out
	case string:
		*j = []byte(v)
	default:
		return errors.New("JSONRaw: unsupported scan type")
	}
	return nil
}

// MarshalJSON returns the raw bytes verbatim so they pass through API
// responses unmodified. Falls back to "null" on empty.
func (j JSONRaw) MarshalJSON() ([]byte, error) {
	if len(j) == 0 {
		return []byte("null"), nil
	}
	return []byte(j), nil
}

func (j *JSONRaw) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		*j = nil
		return nil
	}
	out := make([]byte, len(data))
	copy(out, data)
	*j = out
	return nil
}

// ChatSession is one server-side persisted chat thread for a logged-in admin.
// `ClientID` is the front-end-generated id used as the upsert idempotency key,
// so a refresh / second device doesn't duplicate rows.
type ChatSession struct {
	ID        uint64    `gorm:"primaryKey" json:"id"`
	UserID    uint64    `gorm:"not null;uniqueIndex:uk_chat_sessions_user_client,priority:1;index:idx_chat_sessions_user_updated,priority:1" json:"userId"`
	ClientID  string    `gorm:"size:64;not null;uniqueIndex:uk_chat_sessions_user_client,priority:2" json:"clientId"`
	Title     string    `gorm:"size:255;not null;default:Untitled" json:"title"`
	Pinned    bool      `gorm:"not null;default:false" json:"pinned"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `gorm:"index:idx_chat_sessions_user_updated,priority:2" json:"updatedAt"`
}

func (ChatSession) TableName() string { return "chat_sessions" }

// ChatMessage is a persisted message inside a server-side ChatSession. Content
// is stored as a string (the simple case) — the multimodal parts case is
// handled by `ContentJSON` instead, with role-specific routing on the wire.
// In practice the front-end ships `content` as either a JSON-encoded string
// or a JSON-encoded array; we store it raw and forward it verbatim.
type ChatMessage struct {
	ID          uint64    `gorm:"primaryKey" json:"id"`
	SessionID   uint64    `gorm:"not null;uniqueIndex:uk_chat_messages_session_client,priority:1;index:idx_chat_messages_session_created,priority:1" json:"sessionId"`
	ClientID    string    `gorm:"size:64;not null;uniqueIndex:uk_chat_messages_session_client,priority:2" json:"clientId"`
	Role        string    `gorm:"size:20;not null" json:"role"`
	Content     string    `gorm:"type:mediumtext;not null" json:"content"`
	Attachments JSONRaw   `gorm:"type:json" json:"attachments,omitempty"`
	Tools       JSONRaw   `gorm:"type:json" json:"tools,omitempty"`
	CreatedAt   time.Time `gorm:"index:idx_chat_messages_session_created,priority:2" json:"createdAt"`
}

func (ChatMessage) TableName() string { return "chat_messages" }

// ChatMemory is a single durable fact we extracted from a user's chat history
// to feed back into future requests as system context. Bounded by
// `maxMemoriesPerUser` in the service layer.
type ChatMemory struct {
	ID              uint64    `gorm:"primaryKey" json:"id"`
	UserID          uint64    `gorm:"not null;index:idx_chat_memories_user_created,priority:1" json:"userId"`
	Content         string    `gorm:"size:500;not null" json:"content"`
	SourceSessionID *uint64   `gorm:"" json:"sourceSessionId,omitempty"`
	CreatedAt       time.Time `gorm:"index:idx_chat_memories_user_created,priority:2" json:"createdAt"`
}

func (ChatMemory) TableName() string { return "chat_memories" }

// ChatShare is a public-readable snapshot of a conversation. `Hash` is the
// opaque URL slug; `Payload` is the full Conversation JSON the front-end
// chose to share (filtered for size by the client before posting).
type ChatShare struct {
	ID        uint64    `gorm:"primaryKey" json:"id"`
	Hash      string    `gorm:"size:32;not null;uniqueIndex" json:"hash"`
	CreatedBy uint64    `gorm:"not null;index" json:"createdBy"`
	Title     string    `gorm:"size:255;not null" json:"title"`
	Payload   JSONRaw   `gorm:"type:json;not null" json:"payload"`
	ViewCount uint32    `gorm:"not null;default:0" json:"viewCount"`
	CreatedAt time.Time `json:"createdAt"`
}

func (ChatShare) TableName() string { return "chat_shares" }

// Compile-time guard: JSONRaw satisfies the json marshaler/unmarshaler
// interfaces the API layer relies on for verbatim passthrough.
var (
	_ json.Marshaler   = JSONRaw{}
	_ json.Unmarshaler = (*JSONRaw)(nil)
)
