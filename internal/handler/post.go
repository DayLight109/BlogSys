package handler

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/lilce/blog-api/internal/middleware"
	"github.com/lilce/blog-api/internal/model"
	"github.com/lilce/blog-api/internal/service"
)

type PostHandler struct {
	svc *service.PostService
}

func NewPostHandler(svc *service.PostService) *PostHandler {
	return &PostHandler{svc: svc}
}

type postReq struct {
	Title     string   `json:"title" binding:"required,max=255"`
	Slug      string   `json:"slug" binding:"max=255"`
	Summary   *string  `json:"summary"`
	Content   string   `json:"content" binding:"required"`
	CoverURL  *string  `json:"coverUrl"`
	Status    string   `json:"status"`
	Tags      []string `json:"tags"`
	Pinned    *bool    `json:"pinned"`
	Publish   bool     `json:"publish"`
	PublishAt *string  `json:"publishAt"` // RFC3339;留空 = 立即发布
}

func (h *PostHandler) ListPublic(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "10"))
	tag := c.Query("tag")

	posts, total, err := h.svc.ListPublic(tag, page, size)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	for i := range posts {
		posts[i].ContentMD = ""
		posts[i].ContentHTML = ""
	}
	c.JSON(http.StatusOK, gin.H{"items": posts, "total": total, "page": page, "size": size})
}

func (h *PostHandler) GetBySlug(c *gin.Context) {
	slug := decodeParam(c.Param("slug"))
	p, err := h.svc.GetPublishedBySlug(slug, c.ClientIP())
	if err != nil {
		if errors.Is(err, service.ErrPostNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "post not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, p)
}

func (h *PostHandler) ListAdmin(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	status := c.Query("status")
	tag := c.Query("tag")

	posts, total, err := h.svc.ListAdmin(status, tag, page, size)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	for i := range posts {
		posts[i].ContentHTML = ""
	}
	c.JSON(http.StatusOK, gin.H{"items": posts, "total": total, "page": page, "size": size})
}

func (h *PostHandler) GetAdminByID(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	p, err := h.svc.GetByID(id)
	if err != nil {
		if errors.Is(err, service.ErrPostNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "post not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, p)
}

func (h *PostHandler) Create(c *gin.Context) {
	var req postReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	authorID := c.GetUint64(middleware.CtxUserID)
	p, err := h.svc.Create(authorID, reqToInput(req))
	if err != nil {
		if errors.Is(err, service.ErrSlugTaken) {
			c.JSON(http.StatusConflict, gin.H{"error": "slug already taken"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, p)
}

func (h *PostHandler) Update(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	var req postReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	p, err := h.svc.Update(id, reqToInput(req))
	if err != nil {
		switch {
		case errors.Is(err, service.ErrPostNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "post not found"})
		case errors.Is(err, service.ErrSlugTaken):
			c.JSON(http.StatusConflict, gin.H{"error": "slug already taken"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}
	c.JSON(http.StatusOK, p)
}

func (h *PostHandler) Delete(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if err := h.svc.Delete(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ListTrash 列出软删的文章。
func (h *PostHandler) ListTrash(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	posts, total, err := h.svc.ListTrash(page, size)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	for i := range posts {
		posts[i].ContentMD = ""
		posts[i].ContentHTML = ""
	}
	c.JSON(http.StatusOK, gin.H{"items": posts, "total": total, "page": page, "size": size})
}

func (h *PostHandler) Restore(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if err := h.svc.Restore(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *PostHandler) Purge(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if err := h.svc.Purge(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func reqToInput(req postReq) service.PostInput {
	in := service.PostInput{
		Title:    req.Title,
		Slug:     req.Slug,
		Summary:  req.Summary,
		Content:  req.Content,
		CoverURL: req.CoverURL,
		Status:   req.Status,
		Tags:     req.Tags,
		Pinned:   req.Pinned,
		Publish:  req.Publish,
	}
	if req.PublishAt != nil && *req.PublishAt != "" {
		if t, err := time.Parse(time.RFC3339, *req.PublishAt); err == nil {
			in.PublishAt = &t
		}
	}
	return in
}

// GetNeighbors returns { prev, next } for the given slug.
func (h *PostHandler) GetNeighbors(c *gin.Context) {
	slug := decodeParam(c.Param("slug"))
	prev, next, err := h.svc.GetNeighbors(slug)
	if err != nil {
		if errors.Is(err, service.ErrPostNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "post not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	prev = stripBody(prev)
	next = stripBody(next)
	c.JSON(http.StatusOK, gin.H{"prev": prev, "next": next})
}

// GetRelated returns up to `limit` posts sharing tags with the given slug.
func (h *PostHandler) GetRelated(c *gin.Context) {
	slug := decodeParam(c.Param("slug"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "3"))
	if limit <= 0 || limit > 12 {
		limit = 3
	}
	posts, err := h.svc.GetRelated(slug, limit)
	if err != nil {
		if errors.Is(err, service.ErrPostNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "post not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	for i := range posts {
		posts[i].ContentMD = ""
		posts[i].ContentHTML = ""
	}
	c.JSON(http.StatusOK, gin.H{"items": posts})
}

// Archive returns all published posts grouped by year.
func (h *PostHandler) Archive(c *gin.Context) {
	entries, err := h.svc.Archive()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	for i := range entries {
		for j := range entries[i].Posts {
			entries[i].Posts[j].ContentMD = ""
			entries[i].Posts[j].ContentHTML = ""
		}
	}
	c.JSON(http.StatusOK, gin.H{"items": entries})
}

// Search — ?q=xxx&page=1&size=20
func (h *PostHandler) Search(c *gin.Context) {
	q := c.Query("q")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	posts, total, err := h.svc.Search(q, page, size)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	for i := range posts {
		posts[i].ContentMD = ""
		posts[i].ContentHTML = ""
	}
	c.JSON(http.StatusOK, gin.H{
		"items": posts,
		"total": total,
		"page":  page,
		"size":  size,
		"q":     q,
	})
}

func stripBody(p *model.Post) *model.Post {
	if p == nil {
		return nil
	}
	p.ContentMD = ""
	p.ContentHTML = ""
	return p
}
