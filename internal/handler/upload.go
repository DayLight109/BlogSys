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

// extByDetected maps the actual sniffed MIME to an extension; the request's
// Content-Type and the filename are treated as hints only.
var extByDetected = map[string]string{
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

	// Sniff the real content type from the bytes — do NOT trust the
	// Content-Type header or filename extension. An attacker could send a
	// JS/HTML blob with `Content-Type: image/png` and `foo.png` filename;
	// http.DetectContentType looks at magic bytes.
	sniff := buf
	if len(sniff) > 512 {
		sniff = sniff[:512]
	}
	detected := strings.SplitN(http.DetectContentType(sniff), ";", 2)[0]
	ext, ok := extByDetected[detected]
	if !ok {
		c.JSON(http.StatusUnsupportedMediaType, gin.H{
			"error": "unsupported image type",
		})
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

	// Defence in depth: verify the path we're about to write is still inside
	// the upload root, preventing any sanitizeFilename bypass.
	absRoot, err := filepath.Abs(h.dir)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	absFinal, err := filepath.Abs(finalPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !strings.HasPrefix(absFinal, absRoot+string(os.PathSeparator)) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid path"})
		return
	}

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
