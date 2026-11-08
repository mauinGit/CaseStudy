// Package config loads application configuration from environment variables.
// No secret is hardcoded: required values must be provided via env.
package config

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	AppEnv         string   `envconfig:"APP_ENV" default:"development"`
	Port           string   `envconfig:"PORT" default:"8080"`
	LogLevel       string   `envconfig:"LOG_LEVEL" default:"info"`
	TrustedProxies []string `envconfig:"TRUSTED_PROXIES"`
	CORSOrigins    []string `envconfig:"CORS_ALLOWED_ORIGINS"`

	DatabaseURL  string `envconfig:"DATABASE_URL" required:"true"`
	DBMaxConns   int32  `envconfig:"DB_MAX_CONNS" default:"10"`
	AutoMigrate  bool   `envconfig:"AUTO_MIGRATE" default:"false"`
	SeedOnStart  bool   `envconfig:"SEED_ON_START" default:"false"`
	SeedPassword string `envconfig:"SEED_PASSWORD"`

	JWTSecret  string        `envconfig:"JWT_SECRET" required:"true"`
	JWTIssuer  string        `envconfig:"JWT_ISSUER" default:"gdgoc-unsri-core-team-api"`
	JWTTTL     time.Duration `envconfig:"JWT_TTL" default:"2h"`
	BcryptCost int           `envconfig:"BCRYPT_COST" default:"12"`

	LoginRateLimit   int           `envconfig:"LOGIN_RATE_LIMIT" default:"5"`
	LoginIPRateLimit int           `envconfig:"LOGIN_IP_RATE_LIMIT" default:"30"`
	LoginRateWindow  time.Duration `envconfig:"LOGIN_RATE_WINDOW" default:"1m"`

	GoogleClientID     string `envconfig:"GOOGLE_CLIENT_ID"`
	GoogleClientSecret string `envconfig:"GOOGLE_CLIENT_SECRET"`
	GoogleRedirectURL  string `envconfig:"GOOGLE_REDIRECT_URL"`
	AllowedEmailDomain string `envconfig:"ALLOWED_EMAIL_DOMAIN"`
}

// Load reads and validates configuration from the environment.
func Load() (*Config, error) {
	var c Config
	if err := envconfig.Process("", &c); err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	c.AllowedEmailDomain = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(c.AllowedEmailDomain), "@"))
	c.TrustedProxies = cleanList(c.TrustedProxies)
	c.CORSOrigins = cleanList(c.CORSOrigins)
	if err := c.validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

// cleanList trims entries and drops empty ones (e.g. from "a, ,b" or "").
func cleanList(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func (c *Config) validate() error {
	var errs []error
	if len(c.JWTSecret) < 32 {
		errs = append(errs, errors.New("JWT_SECRET must be at least 32 characters"))
	}
	if c.BcryptCost < 10 || c.BcryptCost > 31 {
		errs = append(errs, errors.New("BCRYPT_COST must be between 10 and 31"))
	}
	if c.JWTTTL <= 0 {
		errs = append(errs, errors.New("JWT_TTL must be positive"))
	}
	if c.LoginRateLimit <= 0 || c.LoginIPRateLimit <= 0 || c.LoginRateWindow <= 0 {
		errs = append(errs, errors.New("LOGIN_RATE_LIMIT, LOGIN_IP_RATE_LIMIT and LOGIN_RATE_WINDOW must be positive"))
	}
	if c.DBMaxConns <= 0 {
		errs = append(errs, errors.New("DB_MAX_CONNS must be positive"))
	}
	if c.SeedOnStart && len(c.SeedPassword) < 8 {
		errs = append(errs, errors.New("SEED_PASSWORD must be at least 8 characters when SEED_ON_START=true"))
	}
	if c.GoogleEnabled() && c.AllowedEmailDomain == "" {
		errs = append(errs, errors.New("ALLOWED_EMAIL_DOMAIN is required when Google OAuth is enabled"))
	}
	return errors.Join(errs...)
}

// IsProduction reports whether the app runs in production mode.
func (c *Config) IsProduction() bool { return c.AppEnv == "production" }

// GoogleEnabled reports whether Google OAuth login is configured.
func (c *Config) GoogleEnabled() bool {
	return c.GoogleClientID != "" && c.GoogleClientSecret != "" && c.GoogleRedirectURL != ""
}
