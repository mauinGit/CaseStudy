package middleware

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"

	"github.com/mauinGit/gdgoc-core-team-api/internal/apperror"
)

// SecurityHeaders sets conservative defaults for a JSON API.
func SecurityHeaders(production bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		if production {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		c.Next()
	}
}

// NoStore prevents caching of API responses that may contain personal data.
func NoStore() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Next()
	}
}

// BodyLimit caps request bodies; larger bodies fail with 413.
func BodyLimit(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.ContentLength > maxBytes {
			apperror.Abort(c, http.StatusRequestEntityTooLarge, apperror.Detail{
				Code: "PAYLOAD_TOO_LARGE", Message: "Body request terlalu besar",
			})
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		c.Next()
	}
}

// IsBodyTooLarge reports whether err comes from BodyLimit.
func IsBodyTooLarge(err error) bool {
	var mbe *http.MaxBytesError
	return errors.As(err, &mbe)
}

// CORS allows only the configured origins. With no origins configured the
// middleware is a no-op (same-origin only).
func CORS(origins []string) gin.HandlerFunc {
	if len(origins) == 0 {
		return func(c *gin.Context) { c.Next() }
	}
	return cors.New(cors.Config{
		AllowOrigins:     origins,
		AllowMethods:     []string{http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodPut, http.MethodDelete, http.MethodOptions},
		AllowHeaders:     []string{"Authorization", "Content-Type", RequestIDHeader},
		ExposeHeaders:    []string{RequestIDHeader, "Retry-After"},
		AllowCredentials: false,
		MaxAge:           12 * time.Hour,
	})
}
