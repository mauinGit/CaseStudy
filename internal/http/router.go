// Package http wires the Gin router, middleware and handlers.
package http

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mauinGit/gdgoc-core-team-api/internal/apperror"
	"github.com/mauinGit/gdgoc-core-team-api/internal/config"
	"github.com/mauinGit/gdgoc-core-team-api/internal/http/handler"
	"github.com/mauinGit/gdgoc-core-team-api/internal/http/middleware"
	"github.com/mauinGit/gdgoc-core-team-api/internal/repository"
	"github.com/mauinGit/gdgoc-core-team-api/internal/service"
)

const maxBodyBytes = 1 << 20 // 1 MiB

// Deps groups everything the router needs to build handlers.
type Deps struct {
	Config *config.Config
	DB     *pgxpool.Pool
	// Google overrides the Google OAuth provider (used by tests). When nil the
	// real provider is used if Google OAuth is configured.
	Google service.GoogleProvider
}

// NewRouter builds the HTTP handler with all middleware and routes.
func NewRouter(d Deps) (*gin.Engine, error) {
	cfg := d.Config
	if cfg.IsProduction() {
		gin.SetMode(gin.ReleaseMode)
	}
	handler.SetupValidator()

	// ---- services ----
	store := repository.NewStore(d.DB)
	tokens := service.NewTokenManager(cfg.JWTSecret, cfg.JWTIssuer, cfg.JWTTTL)
	google := d.Google
	if google == nil && cfg.GoogleEnabled() {
		google = service.NewGoogleProvider(cfg.GoogleClientID, cfg.GoogleClientSecret, cfg.GoogleRedirectURL, cfg.AllowedEmailDomain)
	}
	authSvc, err := service.NewAuthService(store, tokens, google, cfg.AllowedEmailDomain, cfg.BcryptCost)
	if err != nil {
		return nil, fmt.Errorf("auth service: %w", err)
	}
	userSvc := service.NewUserService(store, cfg.BcryptCost)

	// ---- handlers ----
	health := handler.NewHealthHandler(d.DB)
	authH := handler.NewAuthHandler(authSvc, cfg.IsProduction())
	userH := handler.NewUserHandler(userSvc)

	// ---- engine & global middleware ----
	r := gin.New()
	r.HandleMethodNotAllowed = true
	if err := r.SetTrustedProxies(cfg.TrustedProxies); err != nil {
		return nil, fmt.Errorf("trusted proxies: %w", err)
	}
	r.Use(
		middleware.RequestID(),
		middleware.Logger(),
		middleware.Recovery(),
		middleware.SecurityHeaders(cfg.IsProduction()),
		middleware.CORS(cfg.CORSOrigins),
		middleware.BodyLimit(maxBodyBytes),
	)
	r.NoRoute(func(c *gin.Context) {
		apperror.Abort(c, http.StatusNotFound, apperror.Detail{Code: apperror.CodeRouteNF, Message: "Endpoint tidak ditemukan"})
	})
	r.NoMethod(func(c *gin.Context) {
		apperror.Abort(c, http.StatusMethodNotAllowed, apperror.Detail{Code: apperror.CodeMethodNA, Message: "Method tidak diizinkan"})
	})

	r.GET("/healthz", health.Healthz)
	r.HEAD("/healthz", health.Healthz)

	// ---- API v1 ----
	api := r.Group("/api/v1", middleware.NoStore())

	loginPerEmail := middleware.NewFixedWindowLimiter(cfg.LoginRateLimit, cfg.LoginRateWindow)
	loginPerIP := middleware.NewFixedWindowLimiter(cfg.LoginIPRateLimit, cfg.LoginRateWindow)

	auth := api.Group("/auth")
	auth.POST("/login", middleware.LoginRateLimit(loginPerEmail, loginPerIP), authH.Login)
	auth.GET("/google/login", authH.GoogleLogin)
	auth.GET("/google/callback", authH.GoogleCallback)

	authed := api.Group("", middleware.Authenticate(authSvc))
	authed.GET("/me", userH.GetMe)

	return r, nil
}
