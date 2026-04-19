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
