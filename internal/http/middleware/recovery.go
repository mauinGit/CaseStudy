package middleware

import (
	"log/slog"
	"runtime/debug"

	"github.com/gin-gonic/gin"

	"github.com/mauinGit/gdgoc-core-team-api/internal/apperror"
)

// Recovery turns panics into a generic 500 JSON response. The stack trace is
// logged server-side only and never returned to the client.
func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.ErrorContext(c.Request.Context(), "panic recovered",
					"panic", rec,
					"request_id", c.GetString(RequestIDKey),
					"stack", string(debug.Stack()),
				)
				if !c.Writer.Written() {
					apperror.Internal(c)
				} else {
					c.Abort()
				}
			}
		}()
		c.Next()
	}
}
