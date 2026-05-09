package handler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/lilce/blog-api/internal/middleware"
	"github.com/lilce/blog-api/internal/model"
	"github.com/lilce/blog-api/internal/repository"
	"github.com/lilce/blog-api/internal/service"
)

type ChatHandler struct {
	svc          *service.ChatService
	memSvc       *service.MemoryService               // optional; nil ⇒ memory features off
	memRepo      *repository.ChatMemoryRepository     // optional; nil ⇒ memory CRUD off
	sessionRepo  *repository.ChatSessionRepository    // optional; nil ⇒ session sync off
	messageRepo  *repository.ChatMessageRepository    // optional; nil ⇒ session sync off
	shareRepo    *repository.ChatShareRepository      // optional; nil ⇒ share off
}

func NewChatHandler(
	svc *service.ChatService,
	memSvc *service.MemoryService,
	memRepo *repository.ChatMemoryRepository,
	sessionRepo *repository.ChatSessionRepository,
	messageRepo *repository.ChatMessageRepository,
	shareRepo *repository.ChatShareRepository,
) *ChatHandler {
	return &ChatHandler{
		svc:         svc,
		memSvc:      memSvc,
		memRepo:     memRepo,
		sessionRepo: sessionRepo,
		messageRepo: messageRepo,
		shareRepo:   shareRepo,
	}
}

type chatReq struct {
	Messages     []service.ChatMessage `json:"messages" binding:"required"`
	Stream       *bool                 `json:"stream"`
	Model        string                `json:"model,omitempty"`
	WebSearch    bool                  `json:"web_search,omitempty"`
	DeepThinking bool                  `json:"deep_thinking,omitempty"`
}

func (h *ChatHandler) Complete(c *gin.Context) {
	var req chatReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	stream := true
	if req.Stream != nil {
		stream = *req.Stream
	}

	opts := service.ChatOptions{
		Model:        req.Model,
		WebSearch:    req.WebSearch,
		DeepThinking: req.DeepThinking,
	}

	// Soft-auth — when the request carried a valid Bearer token, the JWT
	// middleware will have stamped a userID/role onto the gin context.
	// Anonymous chats land here too (no abort); they get the legacy path.
	uid, isAdmin := adminFromCtx(c)

	// Inject the admin's persisted memories into the system prompt so the
	// model can refer back to durable user facts across sessions.
	msgs := req.Messages
	if isAdmin && h.memSvc != nil {
		msgs = h.memSvc.Inject(c.Request.Context(), uid, msgs)
	}

	if !stream {
		content, err := h.svc.Complete(c.Request.Context(), msgs, opts)
		if err != nil {
			h.logError(err)
			writeChatError(c, err)
			return
		}
		// Synchronous memory extraction on the trigger — same trade-off as
		// the streaming path: every Nth turn the response feels slightly
		// slower in exchange for durable memory.
		h.maybeExtractMemories(c, isAdmin, uid, req.Messages)
		c.JSON(http.StatusOK, gin.H{
			"message": gin.H{"role": "assistant", "content": content},
		})
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming unsupported"})
		return
	}

	emit := func(ev service.StreamEvent) error {
		switch ev.Kind {
		case service.StreamEventTool:
			b, err := json.Marshal(gin.H{"name": ev.Tool})
			if err != nil {
				return err
			}
			// Surface tool calls as their own SSE event so the client can
			// stamp a badge ("· web search used") without trying to parse
			// provider-specific shapes.
			if _, err := fmt.Fprintf(c.Writer, "event: tool\ndata: %s\n\n", b); err != nil {
				return err
			}
		default:
			b, err := json.Marshal(gin.H{"content": ev.Text})
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(c.Writer, "data: %s\n\n", b); err != nil {
				return err
			}
		}
		flusher.Flush()
		return nil
	}

	if err := h.svc.Stream(c.Request.Context(), msgs, opts, emit); err != nil {
		h.logError(err)
		b, _ := json.Marshal(gin.H{"error": publicChatError(err)})
		fmt.Fprintf(c.Writer, "event: error\ndata: %s\n\n", b)
		flusher.Flush()
		return
	}

	// Synchronously extract memories on the trigger turn before sending the
	// final `done`. The user sees the assistant content already streamed in
	// — the indicator stays "composing" for the 1-3s the extraction takes,
	// then transitions to done. Plan-confirmed trade-off.
	h.maybeExtractMemories(c, isAdmin, uid, req.Messages)

	fmt.Fprint(c.Writer, "event: done\ndata: {}\n\n")
	flusher.Flush()
}

// maybeExtractMemories runs the extractor when the cadence rule fires, on a
// best-effort basis. Errors are swallowed — extraction must never break the
// user's chat turn.
func (h *ChatHandler) maybeExtractMemories(c *gin.Context, isAdmin bool, uid uint64, all []service.ChatMessage) {
	if !isAdmin || h.memSvc == nil || uid == 0 {
		return
	}
	count := countUserMessages(all)
	if !service.ShouldExtract(count) {
		return
	}
	window := service.MemoryExtractWindow()
	recent := lastN(all, window)
	if _, err := h.memSvc.Extract(c.Request.Context(), uid, recent, nil); err != nil {
		log.Printf("memory: extract failed: %v", err)
	}
}

func countUserMessages(msgs []service.ChatMessage) int {
	n := 0
	for _, m := range msgs {
		if m.Role == "user" {
			n++
		}
	}
	return n
}

func lastN(msgs []service.ChatMessage, n int) []service.ChatMessage {
	if n <= 0 || len(msgs) <= n {
		return msgs
	}
	return msgs[len(msgs)-n:]
}

// adminFromCtx returns (userID, isAdmin) from a soft-authed gin context.
// Both zero / false when the request was anonymous.
func adminFromCtx(c *gin.Context) (uint64, bool) {
	uidV, _ := c.Get(middleware.CtxUserID)
	roleV, _ := c.Get(middleware.CtxRole)
	uid, _ := uidV.(uint64)
	role, _ := roleV.(string)
	return uid, role == "admin"
}

// Config exposes the chat capability descriptor — which models the client may
// pick and which is the default. The frontend uses it to render (or hide) the
// model picker pill.
func (h *ChatHandler) Config(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"models":     h.svc.AllowedModels(),
		"default":    h.svc.Model(),
		"configured": h.svc.Configured(),
	})
}

// ListMemories returns the full memory rolodex for the calling admin. The
// page UI is a single scrolled list — pagination would be over-engineered
// for the per-user cap (200 rows in practice).
func (h *ChatHandler) ListMemories(c *gin.Context) {
	if h.memRepo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "memory feature not enabled"})
		return
	}
	uid := c.GetUint64(middleware.CtxUserID)
	mems, err := h.memRepo.ListByUser(uid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": mems, "total": len(mems)})
}

// DeleteMemory removes one row by id, scoped to the calling admin. Returns
// 204 even if the row was already gone — DELETE should be idempotent.
func (h *ChatHandler) DeleteMemory(c *gin.Context) {
	if h.memRepo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "memory feature not enabled"})
		return
	}
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	uid := c.GetUint64(middleware.CtxUserID)
	if err := h.memRepo.DeleteByID(uid, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

// ─────────────────────── Server-side chat session sync ───────────────────────
//
// All /admin/chat/sessions/* endpoints below upsert by `client_id`, the
// frontend-generated session/message id used as the idempotency key. The
// frontend remains the source of truth for ordering and titling — server
// just persists the snapshot.

type sessionUpsertReq struct {
	ClientID string `json:"clientId" binding:"required"`
	Title    string `json:"title"`
	Pinned   bool   `json:"pinned"`
}

type sessionPatchReq struct {
	Title  *string `json:"title,omitempty"`
	Pinned *bool   `json:"pinned,omitempty"`
}

type messageUpsertReq struct {
	ClientID    string          `json:"clientId" binding:"required"`
	Role        string          `json:"role" binding:"required"`
	Content     string          `json:"content"`
	Attachments json.RawMessage `json:"attachments,omitempty"`
	Tools       json.RawMessage `json:"tools,omitempty"`
}

func (h *ChatHandler) ListSessions(c *gin.Context) {
	if h.sessionRepo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "chat sync not enabled"})
		return
	}
	uid := c.GetUint64(middleware.CtxUserID)
	out, err := h.sessionRepo.ListByUser(uid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": out, "total": len(out)})
}

func (h *ChatHandler) UpsertSession(c *gin.Context) {
	if h.sessionRepo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "chat sync not enabled"})
		return
	}
	var req sessionUpsertReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Title == "" {
		req.Title = "Untitled"
	}
	uid := c.GetUint64(middleware.CtxUserID)
	s := &model.ChatSession{
		UserID:   uid,
		ClientID: req.ClientID,
		Title:    req.Title,
		Pinned:   req.Pinned,
	}
	if err := h.sessionRepo.Upsert(s); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, s)
}

func (h *ChatHandler) PatchSession(c *gin.Context) {
	if h.sessionRepo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "chat sync not enabled"})
		return
	}
	clientID := c.Param("client_id")
	if clientID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing client_id"})
		return
	}
	var req sessionPatchReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	uid := c.GetUint64(middleware.CtxUserID)
	existing, err := h.sessionRepo.FindByClientID(uid, clientID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if existing == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}
	if req.Title != nil {
		existing.Title = *req.Title
	}
	if req.Pinned != nil {
		existing.Pinned = *req.Pinned
	}
	if err := h.sessionRepo.Upsert(existing); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, existing)
}

func (h *ChatHandler) DeleteSession(c *gin.Context) {
	if h.sessionRepo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "chat sync not enabled"})
		return
	}
	clientID := c.Param("client_id")
	uid := c.GetUint64(middleware.CtxUserID)
	if err := h.sessionRepo.DeleteByClientID(uid, clientID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *ChatHandler) ListSessionMessages(c *gin.Context) {
	if h.sessionRepo == nil || h.messageRepo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "chat sync not enabled"})
		return
	}
	clientID := c.Param("client_id")
	uid := c.GetUint64(middleware.CtxUserID)
	sess, err := h.sessionRepo.FindByClientID(uid, clientID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if sess == nil {
		// Return an empty list rather than 404 — the frontend's "pull then
		// upsert" hydration relies on the GET being safe even when nothing
		// exists yet on the server.
		c.JSON(http.StatusOK, gin.H{"items": []any{}, "total": 0})
		return
	}
	out, err := h.messageRepo.ListBySession(sess.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": out, "total": len(out)})
}

func (h *ChatHandler) UpsertSessionMessage(c *gin.Context) {
	if h.sessionRepo == nil || h.messageRepo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "chat sync not enabled"})
		return
	}
	clientID := c.Param("client_id")
	var req messageUpsertReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Role != "user" && req.Role != "assistant" && req.Role != "system" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid role"})
		return
	}
	uid := c.GetUint64(middleware.CtxUserID)
	sess, err := h.sessionRepo.FindByClientID(uid, clientID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if sess == nil {
		// Create a stub session on-demand so callers can append messages
		// before they've explicitly created the session row. Keeps the
		// frontend code simpler — it just upserts whichever side it touches.
		sess = &model.ChatSession{UserID: uid, ClientID: clientID, Title: "Untitled"}
		if err := h.sessionRepo.Upsert(sess); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	msg := &model.ChatMessage{
		SessionID:   sess.ID,
		ClientID:    req.ClientID,
		Role:        req.Role,
		Content:     req.Content,
		Attachments: model.JSONRaw(req.Attachments),
		Tools:       model.JSONRaw(req.Tools),
	}
	if err := h.messageRepo.Upsert(msg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := h.sessionRepo.Touch(uid, clientID); err != nil {
		// Non-fatal — index ordering will rely on the most recent message
		// timestamp instead of the session's updated_at.
		log.Printf("chat sync: touch session failed: %v", err)
	}
	c.JSON(http.StatusOK, msg)
}

func (h *ChatHandler) DeleteSessionMessage(c *gin.Context) {
	if h.sessionRepo == nil || h.messageRepo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "chat sync not enabled"})
		return
	}
	clientID := c.Param("client_id")
	msgClientID := c.Param("msg_client_id")
	uid := c.GetUint64(middleware.CtxUserID)
	sess, err := h.sessionRepo.FindByClientID(uid, clientID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if sess == nil {
		c.Status(http.StatusNoContent)
		return
	}
	if err := h.messageRepo.DeleteByClientID(sess.ID, msgClientID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

// ─────────────────────── Share links ───────────────────────────────────────
//
// POST /admin/chat/shares  body: {title, payload}  → {hash, url}
// GET  /chat/shares/:hash                          public read
//
// Payload is the verbatim Conversation snapshot the frontend chose to share.
// Server doesn't introspect — it just hands back the same bytes on read.

type shareCreateReq struct {
	Title   string          `json:"title" binding:"required"`
	Payload json.RawMessage `json:"payload" binding:"required"`
}

const maxSharePayloadBytes = 2 * 1024 * 1024 // 2 MB cap on a single share

func (h *ChatHandler) CreateShare(c *gin.Context) {
	if h.shareRepo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "share feature not enabled"})
		return
	}
	var req shareCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(req.Payload) > maxSharePayloadBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{
			"error": "share payload too large; trim attachments before sharing",
		})
		return
	}
	uid := c.GetUint64(middleware.CtxUserID)
	hash, err := newShareHash()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	share := &model.ChatShare{
		Hash:      hash,
		CreatedBy: uid,
		Title:     truncate(req.Title, 255),
		Payload:   model.JSONRaw(req.Payload),
	}
	if err := h.shareRepo.Create(share); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"hash":  share.Hash,
		"title": share.Title,
	})
}

// ReadShare is the public endpoint — no auth, no rate-limit conflict (mounted
// outside the admin group). View count increments are best-effort.
func (h *ChatHandler) ReadShare(c *gin.Context) {
	if h.shareRepo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "share feature not enabled"})
		return
	}
	hash := c.Param("hash")
	share, err := h.shareRepo.FindByHash(hash)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if share == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "share not found"})
		return
	}
	// Fire and forget — racing increments under-count by at most one and
	// that's fine for a public counter. The handler instance is created
	// once at startup so capturing `h` is safe.
	go func(repo *repository.ChatShareRepository, hash string) {
		if err := repo.IncrementView(hash); err != nil {
			log.Printf("share: view bump failed: %v", err)
		}
	}(h.shareRepo, hash)
	c.JSON(http.StatusOK, gin.H{
		"hash":      share.Hash,
		"title":     share.Title,
		"payload":   share.Payload,
		"viewCount": share.ViewCount,
		"createdAt": share.CreatedAt,
	})
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

func newShareHash() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (h *ChatHandler) logError(err error) {
	var upstream *service.ChatUpstreamError
	switch {
	case errors.As(err, &upstream):
		log.Printf(
			"chat: provider request failed: status=%d base_url=%s model=%s body=%s",
			upstream.StatusCode,
			h.svc.BaseURL(),
			h.svc.Model(),
			upstream.Body,
		)
	case errors.Is(err, service.ErrChatNotConfigured):
		log.Printf(
			"chat: not configured: openai_api_key_set=%t base_url=%s model=%s",
			h.svc.APIKeySet(),
			h.svc.BaseURL(),
			h.svc.Model(),
		)
	default:
		log.Printf("chat: request failed: base_url=%s model=%s error=%v", h.svc.BaseURL(), h.svc.Model(), err)
	}
}

func writeChatError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrChatNotConfigured):
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "AI chat is not configured"})
	case errors.Is(err, service.ErrChatBadRequest):
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid chat request"})
	default:
		c.JSON(http.StatusBadGateway, gin.H{"error": "AI provider request failed"})
	}
}

func publicChatError(err error) string {
	switch {
	case errors.Is(err, service.ErrChatNotConfigured):
		return "AI chat is not configured"
	case errors.Is(err, service.ErrChatBadRequest):
		return "invalid chat request"
	default:
		return "AI provider request failed"
	}
}
