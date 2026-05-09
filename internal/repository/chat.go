package repository

import (
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/lilce/blog-api/internal/model"
)

// ────────────────────────── ChatSessionRepository ──────────────────────────

type ChatSessionRepository struct {
	db *gorm.DB
}

func NewChatSessionRepository(db *gorm.DB) *ChatSessionRepository {
	return &ChatSessionRepository{db: db}
}

// ListByUser returns the user's sessions ordered by most-recent activity.
// No pagination — a personal blog admin won't accumulate enough threads to
// justify the complexity, and the index page renders all of them anyway.
func (r *ChatSessionRepository) ListByUser(userID uint64) ([]model.ChatSession, error) {
	var out []model.ChatSession
	err := r.db.
		Where("user_id = ?", userID).
		Order("updated_at DESC, id DESC").
		Find(&out).Error
	return out, err
}

// FindByClientID resolves a (user, client_id) pair to the row, or nil if
// missing. Used to translate front-end ids before message upsert.
func (r *ChatSessionRepository) FindByClientID(userID uint64, clientID string) (*model.ChatSession, error) {
	var s model.ChatSession
	err := r.db.Where("user_id = ? AND client_id = ?", userID, clientID).First(&s).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &s, nil
}

// Upsert creates or updates a session keyed on (user_id, client_id). The
// caller is the only writer for their own user_id, so there's no coordination
// concern — last write wins on title/pinned.
func (r *ChatSessionRepository) Upsert(s *model.ChatSession) error {
	return r.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}, {Name: "client_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"title", "pinned", "updated_at",
		}),
	}).Create(s).Error
}

// Touch bumps updated_at without changing other fields — used after appending
// messages so the index sidebar re-sorts correctly.
func (r *ChatSessionRepository) Touch(userID uint64, clientID string) error {
	return r.db.Model(&model.ChatSession{}).
		Where("user_id = ? AND client_id = ?", userID, clientID).
		Update("updated_at", time.Now()).Error
}

func (r *ChatSessionRepository) DeleteByClientID(userID uint64, clientID string) error {
	return r.db.Where("user_id = ? AND client_id = ?", userID, clientID).
		Delete(&model.ChatSession{}).Error
}

// ────────────────────────── ChatMessageRepository ──────────────────────────

type ChatMessageRepository struct {
	db *gorm.DB
}

func NewChatMessageRepository(db *gorm.DB) *ChatMessageRepository {
	return &ChatMessageRepository{db: db}
}

func (r *ChatMessageRepository) ListBySession(sessionID uint64) ([]model.ChatMessage, error) {
	var out []model.ChatMessage
	err := r.db.
		Where("session_id = ?", sessionID).
		Order("created_at ASC, id ASC").
		Find(&out).Error
	return out, err
}

// Upsert creates or replaces a message keyed on (session_id, client_id).
// We update the content/attachments/tools so a streaming finalize can
// safely overwrite an earlier placeholder write — though in practice the
// frontend only writes once-per-message (when streaming completes) to keep
// network chatter low.
func (r *ChatMessageRepository) Upsert(m *model.ChatMessage) error {
	return r.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "session_id"}, {Name: "client_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"role", "content", "attachments", "tools",
		}),
	}).Create(m).Error
}

func (r *ChatMessageRepository) DeleteByClientID(sessionID uint64, clientID string) error {
	return r.db.Where("session_id = ? AND client_id = ?", sessionID, clientID).
		Delete(&model.ChatMessage{}).Error
}

// CountUserMessagesInSession counts non-system, non-assistant rows for the
// memory-extraction trigger ("every 4 user turns").
func (r *ChatMessageRepository) CountUserMessagesInSession(sessionID uint64) (int64, error) {
	var n int64
	err := r.db.Model(&model.ChatMessage{}).
		Where("session_id = ? AND role = ?", sessionID, "user").
		Count(&n).Error
	return n, err
}

// LastNUserMessages returns the last N user turns from a session as raw
// content strings — fed to the memory extractor.
func (r *ChatMessageRepository) LastNMessages(sessionID uint64, n int) ([]model.ChatMessage, error) {
	if n <= 0 {
		n = 8
	}
	var out []model.ChatMessage
	err := r.db.
		Where("session_id = ?", sessionID).
		Order("created_at DESC, id DESC").
		Limit(n).
		Find(&out).Error
	if err != nil {
		return nil, err
	}
	// Reverse to chronological order.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// ────────────────────────── ChatMemoryRepository ──────────────────────────

type ChatMemoryRepository struct {
	db *gorm.DB
}

func NewChatMemoryRepository(db *gorm.DB) *ChatMemoryRepository {
	return &ChatMemoryRepository{db: db}
}

func (r *ChatMemoryRepository) ListByUser(userID uint64) ([]model.ChatMemory, error) {
	var out []model.ChatMemory
	err := r.db.
		Where("user_id = ?", userID).
		Order("created_at DESC, id DESC").
		Find(&out).Error
	return out, err
}

// LatestNByUser returns the most recent N memories — used when injecting into
// the system prompt. Reverse-chronological so newer facts override older.
func (r *ChatMemoryRepository) LatestNByUser(userID uint64, n int) ([]model.ChatMemory, error) {
	if n <= 0 {
		n = 20
	}
	var out []model.ChatMemory
	err := r.db.
		Where("user_id = ?", userID).
		Order("created_at DESC, id DESC").
		Limit(n).
		Find(&out).Error
	return out, err
}

func (r *ChatMemoryRepository) Create(m *model.ChatMemory) error {
	return r.db.Create(m).Error
}

func (r *ChatMemoryRepository) DeleteByID(userID, id uint64) error {
	return r.db.
		Where("id = ? AND user_id = ?", id, userID).
		Delete(&model.ChatMemory{}).Error
}

func (r *ChatMemoryRepository) CountByUser(userID uint64) (int64, error) {
	var n int64
	err := r.db.Model(&model.ChatMemory{}).Where("user_id = ?", userID).Count(&n).Error
	return n, err
}

// EvictOldest deletes the oldest rows for `userID` until total is `keep` or
// fewer — call after Create when the per-user cap is exceeded.
func (r *ChatMemoryRepository) EvictOldest(userID uint64, keep int) error {
	if keep < 0 {
		keep = 0
	}
	count, err := r.CountByUser(userID)
	if err != nil {
		return err
	}
	excess := int(count) - keep
	if excess <= 0 {
		return nil
	}
	// Subselect — find the oldest `excess` ids to delete in one statement.
	var ids []uint64
	if err := r.db.Model(&model.ChatMemory{}).
		Where("user_id = ?", userID).
		Order("created_at ASC, id ASC").
		Limit(excess).
		Pluck("id", &ids).Error; err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	return r.db.Where("id IN ?", ids).Delete(&model.ChatMemory{}).Error
}

// ExistsContent returns true if a memory for this user already contains the
// given content (case-insensitive exact match). Cheap dedupe before insert.
func (r *ChatMemoryRepository) ExistsContent(userID uint64, content string) (bool, error) {
	var n int64
	err := r.db.Model(&model.ChatMemory{}).
		Where("user_id = ? AND LOWER(content) = LOWER(?)", userID, content).
		Count(&n).Error
	return n > 0, err
}

// ────────────────────────── ChatShareRepository ──────────────────────────

type ChatShareRepository struct {
	db *gorm.DB
}

func NewChatShareRepository(db *gorm.DB) *ChatShareRepository {
	return &ChatShareRepository{db: db}
}

func (r *ChatShareRepository) Create(s *model.ChatShare) error {
	return r.db.Create(s).Error
}

func (r *ChatShareRepository) FindByHash(hash string) (*model.ChatShare, error) {
	var s model.ChatShare
	err := r.db.Where("hash = ?", hash).First(&s).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &s, nil
}

// IncrementView is fire-and-forget by intent — not transactional with the
// fetch. A racing read overcounting by one is fine; we don't promise exact
// counts on a public anonymous endpoint.
func (r *ChatShareRepository) IncrementView(hash string) error {
	return r.db.Model(&model.ChatShare{}).
		Where("hash = ?", hash).
		UpdateColumn("view_count", gorm.Expr("view_count + 1")).Error
}

func (r *ChatShareRepository) ListByUser(userID uint64) ([]model.ChatShare, error) {
	var out []model.ChatShare
	err := r.db.
		Where("created_by = ?", userID).
		Order("created_at DESC, id DESC").
		Find(&out).Error
	return out, err
}

func (r *ChatShareRepository) DeleteByHash(userID uint64, hash string) error {
	return r.db.
		Where("hash = ? AND created_by = ?", hash, userID).
		Delete(&model.ChatShare{}).Error
}
