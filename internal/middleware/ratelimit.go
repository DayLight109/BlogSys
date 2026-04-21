package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// Limiter is a per-key fixed-window counter. Simple, no external deps, memory
// bounded by active keys during the window (plus GC sweep).
type Limiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	max     int
	window  time.Duration
}

type bucket struct {
	count   int
	resetAt time.Time
}

func NewLimiter(max int, window time.Duration) *Limiter {
	l := &Limiter{
		buckets: make(map[string]*bucket),
		max:     max,
		window:  window,
	}
	go l.sweep()
	return l
}

// Allow returns true and increments the counter if under the cap; false once
// the cap is hit. A new window starts when the bucket expires.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	b, ok := l.buckets[key]
	if !ok || now.After(b.resetAt) {
		l.buckets[key] = &bucket{count: 1, resetAt: now.Add(l.window)}
		return true
	}
	if b.count >= l.max {
		return false
	}
	b.count++
	return true
}

func (l *Limiter) sweep() {
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	for range t.C {
		l.mu.Lock()
		now := time.Now()
		for k, b := range l.buckets {
			if now.After(b.resetAt) {
				delete(l.buckets, k)
			}
		}
		l.mu.Unlock()
	}
}

// RateLimit returns a middleware that rejects requests from the same client IP
// after `max` hits within `window`.
func RateLimit(l *Limiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !l.Allow(c.ClientIP()) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "too many attempts, please slow down",
			})
			return
		}
		c.Next()
	}
}
