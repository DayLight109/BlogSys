package handler

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/lilce/blog-api/internal/middleware"
	"github.com/lilce/blog-api/internal/service"
)

type AuthHandler struct {
	svc    *service.AuthService
	secure bool
}

func NewAuthHandler(svc *service.AuthService, secureCookies bool) *AuthHandler {
	return &AuthHandler{svc: svc, secure: secureCookies}
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

func (h *AuthHandler) setRefreshCookie(c *gin.Context, token string, expiresAt time.Time) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(
		"refresh_token",
		token,
		int(time.Until(expiresAt).Seconds()),
		"/",
		"",
		h.secure,
		true,
	)
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

	h.setRefreshCookie(c, pair.RefreshToken, pair.RefreshExpiresAt)

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
	h.setRefreshCookie(c, pair.RefreshToken, pair.RefreshExpiresAt)
	c.JSON(http.StatusOK, tokenResp{
		AccessToken: pair.AccessToken,
		ExpiresAt:   pair.AccessExpiresAt.Unix(),
		User:        pair.User,
	})
}

func (h *AuthHandler) Logout(c *gin.Context) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("refresh_token", "", -1, "/", "", h.secure, true)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *AuthHandler) Me(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"userId":   c.GetUint64(middleware.CtxUserID),
		"username": c.GetString(middleware.CtxUsername),
		"role":     c.GetString(middleware.CtxRole),
	})
}

type changePasswordReq struct {
	Current string `json:"current" binding:"required"`
	Next    string `json:"next" binding:"required,min=8,max=100"`
}

func (h *AuthHandler) ChangePassword(c *gin.Context) {
	var req changePasswordReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	userID := c.GetUint64(middleware.CtxUserID)
	if err := h.svc.ChangePassword(userID, req.Current, req.Next); err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidCredentials):
			c.JSON(http.StatusUnauthorized, gin.H{"error": "current password incorrect"})
		case errors.Is(err, service.ErrWeakPassword):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "server error"})
		}
		return
	}
	// Invalidate refresh cookie so user must log in again with new password.
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("refresh_token", "", -1, "/", "", h.secure, true)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

