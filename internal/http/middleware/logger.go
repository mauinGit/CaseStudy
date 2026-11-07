package middleware

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
)

// Logger writes one structured log line per request. It never logs request
// bodies or Authorization headers.
func Logger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		status := c.Writer.Status()
		level := slog.LevelInfo
		switch {
		case status >= 500:
			level = slog.LevelError
		case status >= 400:
			level = slog.LevelWarn
		}
		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		slog.Log(c.Request.Context(), level, "http request",
			"request_id", c.GetString(RequestIDKey),
			"method", c.Request.Method,
			"route", route,
			"status", status,
			"duration_ms", time.Since(start).Milliseconds(),
			"ip", c.ClientIP(),
		)
	}
}
