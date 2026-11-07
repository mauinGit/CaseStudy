package middleware

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mauinGit/gdgoc-core-team-api/internal/apperror"
	"github.com/mauinGit/gdgoc-core-team-api/internal/domain"
)

// FixedWindowLimiter allows at most `limit` hits per key per window.
// It is in-memory (per instance); see README for the Redis upgrade path.
type FixedWindowLimiter struct {
	mu        sync.Mutex
	limit     int
	window    time.Duration
	now       func() time.Time
	entries   map[string]*windowEntry
	lastSweep time.Time
}

type windowEntry struct {
	count   int
	resetAt time.Time
}

func NewFixedWindowLimiter(limit int, window time.Duration) *FixedWindowLimiter {
	return &FixedWindowLimiter{
		limit:   limit,
		window:  window,
		now:     time.Now,
		entries: make(map[string]*windowEntry),
	}
}

// Allow records a hit for key and reports whether it is within the limit.
// When rejected it also returns how long until the window resets.
func (l *FixedWindowLimiter) Allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.sweep(now)

	e, ok := l.entries[key]
	if !ok || !now.Before(e.resetAt) {
		e = &windowEntry{resetAt: now.Add(l.window)}
		l.entries[key] = e
	}
	if e.count >= l.limit {
		return false, e.resetAt.Sub(now)
	}
	e.count++
	return true, 0
}

// sweep drops expired entries at most once per window to bound memory.
func (l *FixedWindowLimiter) sweep(now time.Time) {
	if now.Sub(l.lastSweep) < l.window {
		return
	}
	for k, e := range l.entries {
		if !now.Before(e.resetAt) {
			delete(l.entries, k)
		}
	}
	l.lastSweep = now
}

// LoginRateLimit throttles login attempts per (IP + email) and per IP. It
// peeks at the JSON body for the email and restores it for the handler.
func LoginRateLimit(perEmail, perIP *FixedWindowLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()

		if ok, retry := perIP.Allow("ip|" + ip); !ok {
			tooMany(c, retry)
			return
		}

		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			apperror.Respond(c, &apperror.ValidationError{Message: "Body request tidak dapat dibaca"})
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(body))

		var peek struct {
			Email string `json:"email"`
		}
		_ = json.Unmarshal(body, &peek) // malformed JSON is rejected later by the handler
		email := strings.ToLower(strings.TrimSpace(peek.Email))

		if ok, retry := perEmail.Allow(ip + "|" + email); !ok {
			tooMany(c, retry)
			return
		}
		c.Next()
	}
}

func tooMany(c *gin.Context, retry time.Duration) {
	secs := int(math.Ceil(retry.Seconds()))
	if secs < 1 {
		secs = 1
	}
	c.Header("Retry-After", strconv.Itoa(secs))
	apperror.Abort(c, http.StatusTooManyRequests, apperror.Detail{
		Code:    domain.ErrTooManyRequests.Code,
		Message: fmt.Sprintf("Terlalu banyak percobaan login, coba lagi dalam %d detik", secs),
	})
}
