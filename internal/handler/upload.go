package handler

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const maxUploadBytes = 5 * 1024 * 1024 // 5 MB

var allowedMIME = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/webp": ".webp",
	"image/gif":  ".gif",
}

type UploadHandler struct {
	dir       string
	publicURL string // e.g. "/uploads" — leading slash, no trailing slash
}

func NewUploadHandler(dir, publicURL string) *UploadHandler {
	if publicURL == "" {
		publicURL = "/uploads"
	}
	publicURL = strings.TrimRight(publicURL, "/")
	return &UploadHandler{dir: dir, publicURL: publicURL}
}

func (h *UploadHandler) Create(c *gin.Context) {
	fh, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing file"})
		return
	}
	if fh.Size > maxUploadBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "file too large (max 5 MB)"})
		return
	}

	mimetype := fh.Header.Get("Content-Type")
	ext, ok := allowedMIME[mimetype]
	if !ok {
		// Fall back to extension if browser didn't set a clean MIME.
		extFromName := strings.ToLower(filepath.Ext(fh.Filename))
		switch extFromName {
		case ".png", ".jpg", ".jpeg", ".webp", ".gif":
			ext = extFromName
			if ext == ".jpeg" {
				ext = ".jpg"
			}
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported image type"})
			return
		}
	}

	src, err := fh.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer src.Close()

	hasher := sha1.New()
	buf, err := io.ReadAll(io.TeeReader(src, hasher))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	hash := hex.EncodeToString(hasher.Sum(nil))[:12]

	now := time.Now().UTC()
	rel := fmt.Sprintf("%04d/%02d", now.Year(), int(now.Month()))
	destDir := filepath.Join(h.dir, rel)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	name := sanitizeFilename(fh.Filename)
	if name == "" {
		name = "upload" + ext
	}
	if strings.ToLower(filepath.Ext(name)) != ext {
		name = strings.TrimSuffix(name, filepath.Ext(name)) + ext
	}
	finalName := hash + "-" + name
	finalPath := filepath.Join(destDir, finalName)

	if err := os.WriteFile(finalPath, buf, 0o644); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	url := fmt.Sprintf("%s/%s/%s", h.publicURL, rel, finalName)
	c.JSON(http.StatusCreated, gin.H{
		"url":  url,
		"name": finalName,
		"size": len(buf),
	})
}

func sanitizeFilename(s string) string {
	base := filepath.Base(s)
	var b strings.Builder
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 80 {
		ext := filepath.Ext(out)
		out = out[:80-len(ext)] + ext
	}
	return out
}
