// Package config loads and validates environment configuration.
package config

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

// Environment names supported by the service.
const (
	EnvDevelopment = "development"
	EnvStaging     = "staging"
	EnvProduction  = "production"
)

// Config is the fully-resolved runtime configuration. Every value originates
// from the environment; nothing environment-specific is compiled in.
type Config struct {
	AppEnv string `env:"APP_ENV" envDefault:"development"`

	HTTP       HTTPConfig `envPrefix:"HTTP_"`
	Database   DatabaseConfig
	Redis      RedisConfig
	Log        LogConfig        `envPrefix:"LOG_"`
	CORS       CORSConfig       `envPrefix:"CORS_"`
	RateLimit  RateLimitConfig  `envPrefix:"RATE_LIMIT_"`
	Audit      AuditConfig      `envPrefix:"AUDIT_"`
	Migrations MigrationsConfig `envPrefix:"MIGRATIONS_"`
	Auth       AuthConfig
	Media      MediaConfig   `envPrefix:"MEDIA_"`
	User       UserConfig    `envPrefix:"USER_"`
	Swagger    SwaggerConfig `envPrefix:"SWAGGER_"`

	// MaxBodyBytes is the pipeline's own ceiling on any request body. It is a
	// coarse early refusal: the wrapper runs before routing, so a route cannot lift
	// it, and it must therefore stay at or above every module's route-specific
	// limit or a legitimate upload would be refused before its own route could
	// apply the real rule.
	//
	// The default is 4 MiB, twice the 2 MB avatar ceiling FR-014 declares plus the
	// multipart envelope. The avatar upload route installs its own ceiling - the
	// image ceiling plus the room the multipart envelope needs - on its own
	// handler; this value is what has to leave room for it.
	//
	// This package supplies the number and its default and nothing else: it does
	// not know the avatar route exists, and by Constitution I a shared package may
	// not import a module to find out. The relation between the two ceilings is
	// enforced by the composition root, which is the only place that sees both
	// numbers: cmd/api calls RequireAvatarUploadCeiling before it opens anything,
	// and a shared ceiling below what the avatar route needs refuses to start.
	// Raising this value only widens what every other endpoint would accept, so the
	// coupling is a floor to maintain rather than a knob to raise freely.
	//
	// The rate limiter, the read ceilings and the per-field rules remain the real
	// protections against an abusive request; this value exists so one oversized
	// body is refused early rather than buffered.
	MaxBodyBytes int64 `env:"MAX_BODY_BYTES" envDefault:"4194304"`
}

// RedisConfig controls the shared cache connection (OTP + blacklist).
type RedisConfig struct {
	Addr     string `env:"REDIS_ADDR" envDefault:"localhost:6379"`
	Password string `env:"REDIS_PASSWORD"`
	DB       int    `env:"REDIS_DB" envDefault:"0"`
}

// AuthConfig controls authentication behavior for the auth module.
type AuthConfig struct {
	JWTSecret          string        `env:"JWT_SECRET"`
	AccessTokenTTL     time.Duration `env:"ACCESS_TOKEN_TTL" envDefault:"15m"`
	RefreshTokenTTL    time.Duration `env:"REFRESH_TOKEN_TTL" envDefault:"1080h"`
	OTPTTL             time.Duration `env:"OTP_TTL" envDefault:"15m"`
	OTPMaxAttempts     int           `env:"OTP_MAX_ATTEMPTS" envDefault:"3"`
	OTPBlockTTL        time.Duration `env:"OTP_BLOCK_TTL" envDefault:"60s"`
	OTPResendCooldown  time.Duration `env:"OTP_RESEND_COOLDOWN" envDefault:"60s"`
	PasswordResetTTL   time.Duration `env:"PASSWORD_RESET_TTL" envDefault:"30m"`
	LoginRatePerMinute int           `env:"AUTH_LOGIN_RATE_PER_MINUTE" envDefault:"10"`
	FlowRatePerMinute  int           `env:"AUTH_FLOW_RATE_PER_MINUTE" envDefault:"5"`
	LoginMaxFailures   int           `env:"AUTH_LOGIN_MAX_FAILURES" envDefault:"10"`
	LoginLockoutTTL    time.Duration `env:"AUTH_LOGIN_LOCKOUT_TTL" envDefault:"15m"`

	SMTPHost     string `env:"SMTP_HOST"`
	SMTPPort     int    `env:"SMTP_PORT" envDefault:"587"`
	SMTPUsername string `env:"SMTP_USERNAME"`
	SMTPPassword string `env:"SMTP_PASSWORD"`
	SMTPFrom     string `env:"SMTP_FROM" envDefault:"no-reply@artist-shop.local"`

	AdminEmail    string `env:"ADMIN_EMAIL"`
	AdminPassword string `env:"ADMIN_PASSWORD"`
}

// HTTPConfig controls the HTTP server lifecycle.
type HTTPConfig struct {
	Addr            string        `env:"ADDR" envDefault:":8080"`
	ReadTimeout     time.Duration `env:"READ_TIMEOUT" envDefault:"10s"`
	WriteTimeout    time.Duration `env:"WRITE_TIMEOUT" envDefault:"15s"`
	IdleTimeout     time.Duration `env:"IDLE_TIMEOUT" envDefault:"60s"`
	ShutdownTimeout time.Duration `env:"SHUTDOWN_TIMEOUT" envDefault:"10s"`
	// TrustedProxies lists IPs or CIDRs allowed to set X-Forwarded-For. Empty
	// (the default) means the header is ignored and the TCP peer address is used.
	TrustedProxies []string `env:"TRUSTED_PROXIES" envSeparator:","`
}

// DatabaseConfig controls the PostgreSQL connection pool.
type DatabaseConfig struct {
	URL            string        `env:"DATABASE_URL"`
	MaxConns       int32         `env:"DB_MAX_CONNS" envDefault:"10"`
	MinConns       int32         `env:"DB_MIN_CONNS" envDefault:"1"`
	ConnectTimeout time.Duration `env:"DB_CONNECT_TIMEOUT" envDefault:"5s"`
}

// LogConfig controls logging.
type LogConfig struct {
	Level string `env:"LEVEL" envDefault:"info"`
}

// CORSConfig controls the cross-origin policy.
type CORSConfig struct {
	AllowedOrigins []string `env:"ALLOWED_ORIGINS" envSeparator:","`
}

// RateLimitConfig controls in-process rate limiting.
type RateLimitConfig struct {
	RequestsPerSecond int `env:"RPS" envDefault:"20"`
	Burst             int `env:"BURST" envDefault:"40"`
}

// AuditConfig controls the asynchronous audit writer.
type AuditConfig struct {
	QueueSize  int `env:"QUEUE_SIZE" envDefault:"1024"`
	MaxRetries int `env:"MAX_RETRIES" envDefault:"5"`
}

// MigrationsConfig controls schema migration behavior.
type MigrationsConfig struct {
	AutoApply   bool          `env:"AUTO_APPLY" envDefault:"true"`
	LockTimeout time.Duration `env:"LOCK_TIMEOUT" envDefault:"30s"`
}

// MediaConfig controls the external media store used for profile avatars.
//
// It is intentionally absent from Validate and ValidateForAPI: the user module's
// composition validates it, because one-off commands (migrate, seed) must stay
// runnable without media credentials. A missing configuration disables avatar
// upload and nothing else — every other endpoint keeps working.
type MediaConfig struct {
	CloudName string `env:"CLOUD_NAME"`
	APIKey    string `env:"API_KEY"`
	APISecret string `env:"API_SECRET"`
	Folder    string `env:"FOLDER" envDefault:"artist-shop"`
}

// IsConfigured reports whether media credentials are present. Secrets are never
// echoed: callers only branch on the boolean.
func (m MediaConfig) IsConfigured() bool {
	return m.CloudName != "" && m.APIKey != "" && m.APISecret != ""
}

// UserConfig controls the user module's own rate limits.
//
// The shared RateLimitConfig is a coarse, per-second safety net applied to every
// /api/v1 route. The write-heavy routes of the user module need thresholds of a
// different shape — an avatar upload is a media-service call, and repeating it is
// the abuse case FR-025 names — so the module carries its own per-window limits
// rather than relying on the global one.
type UserConfig struct {
	// AvatarUploadRatePerHour caps avatar uploads per client IP and hour.
	AvatarUploadRatePerHour int `env:"AVATAR_UPLOAD_RATE_PER_HOUR" envDefault:"10"`
	// AddressWriteRatePerMinute caps address creates, edits, hides and
	// default-flag changes per client IP and minute.
	AddressWriteRatePerMinute int `env:"ADDRESS_WRITE_RATE_PER_MINUTE" envDefault:"30"`
}

// SwaggerConfig controls the interactive API reference served at /swagger.
//
// It is disabled by default because the UI exposes the whole endpoint surface
// and is meant for development and staging only. Production deployments leave
// it off; the generated specification under docs/swagger stays available for
// offline review either way.
type SwaggerConfig struct {
	Enabled bool `env:"ENABLED" envDefault:"false"`
}

// LoadDotenv loads a local .env file when present for non-production
// environments. It never overrides variables that are already set and is
// intended to be called once, before Load.
func LoadDotenv() error {
	if os.Getenv("APP_ENV") == EnvProduction {
		return nil
	}
	if err := godotenv.Load(); err != nil {
		// A missing .env is expected in production and CI.
		return nil
	}
	return nil
}

// Load parses and validates configuration from the environment.
func Load() (*Config, error) {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Validate enforces the required configuration and returns an error that names
// the offending field but never reveals a value (FR-002).
func (c *Config) Validate() error {
	var errs []error

	switch c.AppEnv {
	case EnvDevelopment, EnvStaging, EnvProduction:
	default:
		errs = append(errs, fmt.Errorf("APP_ENV must be one of %q, %q, %q", EnvDevelopment, EnvStaging, EnvProduction))
	}

	if c.HTTP.Addr == "" {
		errs = append(errs, errors.New("HTTP_ADDR is required"))
	}
	if c.Database.URL == "" {
		errs = append(errs, errors.New("DATABASE_URL is required"))
	}
	if c.Database.MaxConns <= 0 {
		errs = append(errs, errors.New("DB_MAX_CONNS must be greater than zero"))
	}
	if c.Database.MinConns < 0 || c.Database.MinConns > c.Database.MaxConns {
		errs = append(errs, errors.New("DB_MIN_CONNS must be between zero and DB_MAX_CONNS"))
	}
	if c.MaxBodyBytes <= 0 {
		errs = append(errs, errors.New("MAX_BODY_BYTES must be greater than zero"))
	}
	if c.RateLimit.RequestsPerSecond <= 0 {
		errs = append(errs, errors.New("RATE_LIMIT_RPS must be greater than zero"))
	}
	if c.RateLimit.Burst <= 0 {
		errs = append(errs, errors.New("RATE_LIMIT_BURST must be greater than zero"))
	}
	if c.Audit.QueueSize <= 0 {
		errs = append(errs, errors.New("AUDIT_QUEUE_SIZE must be greater than zero"))
	}

	switch c.Log.Level {
	case "debug", "info", "warn", "error":
	default:
		errs = append(errs, errors.New("LOG_LEVEL must be one of debug, info, warn, error"))
	}

	if c.Auth.AccessTokenTTL <= 0 {
		errs = append(errs, errors.New("ACCESS_TOKEN_TTL must be greater than zero"))
	}
	if c.Auth.RefreshTokenTTL <= 0 {
		errs = append(errs, errors.New("REFRESH_TOKEN_TTL must be greater than zero"))
	}
	if c.Auth.OTPTTL <= 0 {
		errs = append(errs, errors.New("OTP_TTL must be greater than zero"))
	}
	if c.Auth.OTPMaxAttempts <= 0 {
		errs = append(errs, errors.New("OTP_MAX_ATTEMPTS must be greater than zero"))
	}
	if c.Auth.PasswordResetTTL <= 0 {
		errs = append(errs, errors.New("PASSWORD_RESET_TTL must be greater than zero"))
	}
	if c.Auth.LoginRatePerMinute <= 0 || c.Auth.FlowRatePerMinute <= 0 {
		errs = append(errs, errors.New("AUTH rate-limit thresholds must be greater than zero"))
	}
	if c.Auth.LoginMaxFailures <= 0 || c.Auth.LoginLockoutTTL <= 0 {
		errs = append(errs, errors.New("AUTH lockout thresholds must be greater than zero"))
	}
	if c.User.AvatarUploadRatePerHour <= 0 || c.User.AddressWriteRatePerMinute <= 0 {
		errs = append(errs, errors.New("USER rate-limit thresholds must be greater than zero"))
	}

	return errors.Join(errs...)
}

// ValidateForAPI enforces the settings the HTTP service needs to start: a
// reachable Redis address and a strong JWT secret. It is intentionally separate
// from Validate so that one-off commands (migrate, seed) do not require them.
func (c *Config) ValidateForAPI() error {
	var errs []error
	if c.Redis.Addr == "" {
		errs = append(errs, errors.New("REDIS_ADDR is required"))
	}
	if len(c.Auth.JWTSecret) < 32 {
		errs = append(errs, errors.New("JWT_SECRET must be at least 32 bytes"))
	}
	return errors.Join(errs...)
}

// IsProduction reports whether the service runs in production.
func (c *Config) IsProduction() bool { return c.AppEnv == EnvProduction }
