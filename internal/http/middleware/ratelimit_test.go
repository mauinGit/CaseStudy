package middleware

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestFixedWindowLimiter(t *testing.T) {
	now := time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC)
	l := NewFixedWindowLimiter(5, time.Minute)
	l.now = func() time.Time { return now }

	for i := 0; i < 5; i++ {
		ok, _ := l.Allow("1.2.3.4|a@x.test")
		assert.True(t, ok, "attempt %d should be allowed", i+1)
	}

	ok, retry := l.Allow("1.2.3.4|a@x.test")
	assert.False(t, ok, "6th attempt must be blocked")
	assert.Equal(t, time.Minute, retry)

	// Other keys are independent.
	ok, _ = l.Allow("1.2.3.4|b@x.test")
	assert.True(t, ok)

	// After the window resets the key is allowed again.
	now = now.Add(time.Minute)
	ok, _ = l.Allow("1.2.3.4|a@x.test")
	assert.True(t, ok)
}

func TestFixedWindowLimiterSweepsExpiredEntries(t *testing.T) {
	now := time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC)
	l := NewFixedWindowLimiter(1, time.Minute)
	l.now = func() time.Time { return now }

	for _, k := range []string{"a", "b", "c"} {
		l.Allow(k)
	}
	assert.Len(t, l.entries, 3)

	now = now.Add(2 * time.Minute)
	l.Allow("d")
	assert.Len(t, l.entries, 1, "expired keys must be dropped")
}
