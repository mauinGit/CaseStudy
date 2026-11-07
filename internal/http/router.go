// Package http wires the Gin router, middleware and handlers.
package http

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mauinGit/gdgoc-core-team-api/internal/apperror"
	"github.com/mauinGit/gdgoc-core-team-api/internal/config"
	"github.com/mauinGit/gdgoc-core-team-api/internal/http/handler"
	"github.com/mauinGit/gdgoc-core-team-api/internal/http/middleware"
)

// Deps groups everything the router needs to build handlers.
type Deps struct {
	Config *config.Config
	DB     *pgxpool.Pool
}

// NewRouter builds the HTTP handler with all middleware and routes.
func NewRouter(d Deps) (*gin.Engine, error) {
	if d.Config.IsProduction() {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	r.HandleMethodNotAllowed = true
	if err := r.SetTrustedProxies(d.Config.TrustedProxies); err != nil {
		return nil, err
	}

	r.Use(
		middleware.RequestID(),
		middleware.Logger(),
		middleware.Recovery(),
	)

	r.NoRoute(func(c *gin.Context) {
		apperror.Abort(c, http.StatusNotFound, apperror.Detail{Code: apperror.CodeRouteNF, Message: "Endpoint tidak ditemukan"})
	})
	r.NoMethod(func(c *gin.Context) {
		apperror.Abort(c, http.StatusMethodNotAllowed, apperror.Detail{Code: apperror.CodeMethodNA, Message: "Method tidak diizinkan"})
	})

	health := handler.NewHealthHandler(d.DB)
	r.GET("/healthz", health.Healthz)
	r.HEAD("/healthz", health.Healthz)

	return r, nil
}
