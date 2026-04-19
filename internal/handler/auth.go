package handler

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/lilce/blog-api/internal/service"
)

type AuthHandler struct {
	svc *service.AuthService
}

func NewAuthHandler(svc *service.AuthService) *AuthHandler {
	return &AuthHandler{svc: svc}
}

type loginReq struct {
	Username string `json:"username" binding:"required,min=3,max=50"`
	Password string `json:"password" binding:"required,min=4,max=100"`
}

type tokenResp struct {
	AccessToken string `json:"accessToken"`
	ExpiresAt   int64  `json:"expiresAt"`
	User        any    `json:"user"`
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	pair, err := h.svc.Login(req.Username, req.Password)
	if err != nil {
		if errors.Is(err, service.ErrInvalidCredentials) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid username or password"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server error"})
		return
	}

	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(
		"refresh_token",
		pair.RefreshToken,
		int(time.Until(pair.RefreshExpiresAt).Seconds()),
		"/",
		"",
		false, // secure: true in production with HTTPS
		true,  // httpOnly
	)

	c.JSON(http.StatusOK, tokenResp{
		AccessToken: pair.AccessToken,
		ExpiresAt:   pair.AccessExpiresAt.Unix(),
		User:        pair.User,
	})
}

func (h *AuthHandler) Refresh(c *gin.Context) {
	rt, err := c.Cookie("refresh_token")
	if err != nil || rt == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing refresh token"})
		return
	}
	pair, err := h.svc.Refresh(rt)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid refresh token"})
		return
	}
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("refresh_token", pair.RefreshToken, int(time.Until(pair.RefreshExpiresAt).Seconds()), "/", "", false, true)
	c.JSON(http.StatusOK, tokenResp{
		AccessToken: pair.AccessToken,
		ExpiresAt:   pair.AccessExpiresAt.Unix(),
		User:        pair.User,
	})
}

func (h *AuthHandler) Logout(c *gin.Context) {
	c.SetCookie("refresh_token", "", -1, "/", "", false, true)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *AuthHandler) Me(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"userId":   c.GetUint64("ctx_user_id"),
		"username": c.GetString("ctx_username"),
		"role":     c.GetString("ctx_role"),
	})
}
