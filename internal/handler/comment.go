package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/lilce/blog-api/internal/service"
)

type CommentHandler struct {
	svc     *service.CommentService
	postSvc *service.PostService
}

func NewCommentHandler(svc *service.CommentService, postSvc *service.PostService) *CommentHandler {
	return &CommentHandler{svc: svc, postSvc: postSvc}
}

type commentReq struct {
	ParentID      *uint64 `json:"parentId"`
	AuthorName    string  `json:"authorName" binding:"required,min=1,max=100"`
	AuthorEmail   *string `json:"authorEmail" binding:"omitempty,email,max=100"`
	AuthorWebsite *string `json:"authorWebsite" binding:"omitempty,url,max=200"`
	Content       string  `json:"content" binding:"required,min=1,max=5000"`
}

func (h *CommentHandler) SubmitForSlug(c *gin.Context) {
	slug := c.Param("slug")
	p, err := h.postSvc.GetPublishedBySlug(slug)
	if err != nil {
		if errors.Is(err, service.ErrPostNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "post not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var req commentReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ip := c.ClientIP()
	ua := c.Request.UserAgent()
	in := service.CommentInput{
		PostID:        p.ID,
		ParentID:      req.ParentID,
		AuthorName:    req.AuthorName,
		AuthorEmail:   req.AuthorEmail,
		AuthorWebsite: req.AuthorWebsite,
		Content:       req.Content,
		IP:            &ip,
		UserAgent:     &ua,
	}
	created, err := h.svc.Submit(in)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, created)
}

func (h *CommentHandler) ListForSlug(c *gin.Context) {
	slug := c.Param("slug")
	p, err := h.postSvc.GetPublishedBySlug(slug)
	if err != nil {
		if errors.Is(err, service.ErrPostNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "post not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "50"))

	comments, total, err := h.svc.ListApprovedForPost(p.ID, page, size)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": comments, "total": total})
}

func (h *CommentHandler) ListAdmin(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	status := c.Query("status")
	postID, _ := strconv.ParseUint(c.Query("postId"), 10, 64)

	comments, total, err := h.svc.ListAdmin(status, postID, page, size)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": comments, "total": total, "page": page, "size": size})
}

type commentStatusReq struct {
	Status string `json:"status" binding:"required"`
}

func (h *CommentHandler) UpdateStatus(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	var req commentStatusReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.svc.UpdateStatus(id, req.Status); err != nil {
		if errors.Is(err, service.ErrCommentNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "comment not found"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *CommentHandler) Delete(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if err := h.svc.Delete(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

type adminReplyReq struct {
	PostID   uint64  `json:"postId" binding:"required"`
	ParentID *uint64 `json:"parentId"`
	Content  string  `json:"content" binding:"required,min=1,max=5000"`
}

func (h *CommentHandler) AdminReply(c *gin.Context) {
	var req adminReplyReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	created, err := h.svc.AdminReply(req.PostID, req.ParentID, req.Content)
	if err != nil {
		if errors.Is(err, service.ErrPostNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "post not found"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, created)
}
