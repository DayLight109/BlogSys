package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/lilce/blog-api/internal/service"
)

type TagHandler struct {
	svc *service.PostService
}

func NewTagHandler(svc *service.PostService) *TagHandler {
	return &TagHandler{svc: svc}
}

func (h *TagHandler) List(c *gin.Context) {
	tags, err := h.svc.ListTags()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": tags})
}

type tagRenameReq struct {
	To string `json:"to" binding:"required,min=1,max=64"`
}

func (h *TagHandler) Rename(c *gin.Context) {
	from := c.Param("name")
	var req tagRenameReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.svc.RenameTag(from, req.To); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

type tagMergeReq struct {
	From []string `json:"from" binding:"required,min=1,dive,min=1,max=64"`
	To   string   `json:"to" binding:"required,min=1,max=64"`
}

func (h *TagHandler) Merge(c *gin.Context) {
	var req tagMergeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.svc.MergeTags(req.From, req.To); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *TagHandler) Delete(c *gin.Context) {
	name := c.Param("name")
	if err := h.svc.DeleteTag(name); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
