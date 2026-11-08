package middleware

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/mauinGit/gdgoc-core-team-api/internal/apperror"
	"github.com/mauinGit/gdgoc-core-team-api/internal/domain"
)

const actorKey = "actor"

// Authenticator validates a bearer token and returns the current actor.
type Authenticator interface {
	Authenticate(ctx context.Context, rawToken, ip string) (domain.Actor, error)
}

// Authenticate requires a valid "Authorization: Bearer <token>" header.
func Authenticate(auth Authenticator) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		scheme, token, ok := strings.Cut(header, " ")
		if !ok || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(token) == "" {
			c.Header("WWW-Authenticate", `Bearer realm="api"`)
			apperror.Respond(c, domain.ErrUnauthorized)
			return
		}
		actor, err := auth.Authenticate(c.Request.Context(), strings.TrimSpace(token), c.ClientIP())
		if err != nil {
			c.Header("WWW-Authenticate", `Bearer realm="api", error="invalid_token"`)
			apperror.Respond(c, err)
			return
		}
		c.Set(actorKey, actor)
		c.Next()
	}
}

// RequireRoles is a coarse route-level guard. Fine-grained checks (division
// ownership, IDOR) are always enforced again in the service layer.
func RequireRoles(roles ...domain.Role) gin.HandlerFunc {
	return func(c *gin.Context) {
		actor, ok := ActorFrom(c)
		if !ok {
			apperror.Respond(c, domain.ErrUnauthorized)
			return
		}
		for _, r := range roles {
			if actor.Role == r {
				c.Next()
				return
			}
		}
		apperror.Respond(c, domain.ErrForbidden)
	}
}

// ActorFrom returns the authenticated actor set by Authenticate.
func ActorFrom(c *gin.Context) (domain.Actor, bool) {
	v, ok := c.Get(actorKey)
	if !ok {
		return domain.Actor{}, false
	}
	a, ok := v.(domain.Actor)
	return a, ok
}
