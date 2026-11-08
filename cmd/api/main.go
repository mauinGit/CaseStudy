// Command api runs the GDGoC UNSRI Core Team REST API.
//
// Usage:
//
//	api            start the HTTP server (default)
//	api migrate    apply database migrations and exit
//	api seed       apply migrations, seed demo data and exit
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/mauinGit/gdgoc-core-team-api/internal/config"
	"github.com/mauinGit/gdgoc-core-team-api/internal/database"
	apihttp "github.com/mauinGit/gdgoc-core-team-api/internal/http"
	"github.com/mauinGit/gdgoc-core-team-api/seed"
)

// main starts the API. The annotations below are swag's general API info.
//
//	@title						GDGoC UNSRI Core Team API
//	@version					1.0
//	@description				REST API manajemen Core Team dan anggota GDGoC UNSRI: autentikasi (JWT + Google OAuth), RBAC (Lead, Division Head, Member), manajemen anggota, event dengan kuota, kehadiran, dan audit log.
//	@description
//	@description				Semua respons sukses berbentuk {"data": ...} (list: {"data": [...], "meta": {...}}), semua error berbentuk {"error": {"code", "message", "fields?"}}.
//	@BasePath					/
//	@securityDefinitions.apikey	BearerAuth
//	@in							header
//	@name						Authorization
//	@description				Isi dengan "Bearer <access_token>" dari POST /api/v1/auth/login.
func main() {
	// Local convenience only: load .env if present. Real env vars win.
	_ = godotenv.Load()

	if err := run(os.Args[1:]); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	slog.SetDefault(newLogger(cfg))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cmd := "serve"
	if len(args) > 0 {
		cmd = args[0]
	}

	switch cmd {
	case "serve":
		return serve(ctx, cfg)
	case "migrate":
		return migrate(cfg)
	case "seed":
		if err := migrate(cfg); err != nil {
			return err
		}
		return runSeed(ctx, cfg)
	default:
		return fmt.Errorf("unknown command %q (use: serve | migrate | seed)", cmd)
	}
}

func serve(ctx context.Context, cfg *config.Config) error {
	if cfg.AutoMigrate {
		if err := migrate(cfg); err != nil {
			return err
		}
	}

	pool, err := database.NewPool(ctx, cfg.DatabaseURL, cfg.DBMaxConns)
	if err != nil {
		return err
	}
	defer pool.Close()

	if cfg.SeedOnStart {
		if err := seed.Run(ctx, pool, seed.Options{Password: cfg.SeedPassword, BcryptCost: cfg.BcryptCost}); err != nil {
			return fmt.Errorf("seed: %w", err)
		}
		slog.Info("seed applied")
	}

	router, err := apihttp.NewRouter(apihttp.Deps{Config: cfg, DB: pool})
	if err != nil {
		return fmt.Errorf("build router: %w", err)
	}

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("http server listening", "addr", srv.Addr, "env", cfg.AppEnv)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
	}

	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func migrate(cfg *config.Config) error {
	v, err := database.Migrate(cfg.DatabaseURL)
	if err != nil {
		return err
	}
	slog.Info("migrations applied", "version", v)
	return nil
}

func runSeed(ctx context.Context, cfg *config.Config) error {
	pool, err := database.NewPool(ctx, cfg.DatabaseURL, cfg.DBMaxConns)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := seed.Run(ctx, pool, seed.Options{Password: cfg.SeedPassword, BcryptCost: cfg.BcryptCost}); err != nil {
		return fmt.Errorf("seed: %w", err)
	}
	slog.Info("seed applied")
	return nil
}

func newLogger(cfg *config.Config) *slog.Logger {
	var level slog.Level
	switch strings.ToLower(cfg.LogLevel) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: level}
	if cfg.IsProduction() {
		return slog.New(slog.NewJSONHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, opts))
}
