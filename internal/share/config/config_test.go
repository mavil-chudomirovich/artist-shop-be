package config

import (
	"strings"
	"testing"
)

func setValidEnv(t *testing.T) {
	t.Helper()
	t.Setenv("APP_ENV", "development")
	t.Setenv("HTTP_ADDR", ":8080")
	t.Setenv("DATABASE_URL", "postgres://app:app@localhost:5432/artist_shop")
	t.Setenv("DB_MAX_CONNS", "10")
	t.Setenv("DB_MIN_CONNS", "1")
	t.Setenv("LOG_LEVEL", "info")
}

func TestLoadValid(t *testing.T) {
	setValidEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.AppEnv != EnvDevelopment {
		t.Fatalf("expected development, got %s", cfg.AppEnv)
	}
	if cfg.Database.URL == "" || cfg.HTTP.Addr == "" {
		t.Fatal("expected required fields to be populated")
	}
}

// The pipeline body ceiling must stay above every route-level ceiling a module
// declares for itself, because the pipeline wraps the body before routing and a
// route cannot lift it. The avatar upload is the one route that raises its own
// limit: the 2 MB image ceiling of FR-014 plus room for the multipart envelope.
func TestTheDefaultBodyCeilingLeavesRoomForAnUploadRoute(t *testing.T) {
	setValidEnv(t)
	t.Setenv("MAX_BODY_BYTES", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected the defaults to resolve, got %v", err)
	}
	const (
		avatarImageCeiling = int64(2 << 20)
		multipartOverhead  = int64(64 << 10)
	)
	if cfg.MaxBodyBytes <= avatarImageCeiling+multipartOverhead {
		t.Fatalf("the default body ceiling %d must stay above the avatar route limit %d",
			cfg.MaxBodyBytes, avatarImageCeiling+multipartOverhead)
	}
}

func TestLoadMissingDatabaseURL(t *testing.T) {
	setValidEnv(t)
	t.Setenv("DATABASE_URL", "")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for missing DATABASE_URL")
	}
	if !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("error should name the offending field, got %q", err.Error())
	}
}

func TestLoadInvalidAppEnv(t *testing.T) {
	setValidEnv(t)
	t.Setenv("APP_ENV", "staging-typo")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "APP_ENV") {
		t.Fatalf("expected APP_ENV error, got %v", err)
	}
}

func TestLoadInvalidLogLevel(t *testing.T) {
	setValidEnv(t)
	t.Setenv("LOG_LEVEL", "verbose")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "LOG_LEVEL") {
		t.Fatalf("expected LOG_LEVEL error, got %v", err)
	}
}

func TestValidateForAPI(t *testing.T) {
	if err := (&Config{}).ValidateForAPI(); err == nil {
		t.Fatal("expected missing Redis/JWT errors")
	}

	shortSecret := &Config{Redis: RedisConfig{Addr: "localhost:6379"}, Auth: AuthConfig{JWTSecret: "short"}}
	if err := shortSecret.ValidateForAPI(); err == nil {
		t.Fatal("expected JWT_SECRET length error")
	}

	noRedis := &Config{Auth: AuthConfig{JWTSecret: "0123456789abcdef0123456789abcdef"}}
	if err := noRedis.ValidateForAPI(); err == nil {
		t.Fatal("expected REDIS_ADDR error")
	}

	valid := &Config{Redis: RedisConfig{Addr: "localhost:6379"}, Auth: AuthConfig{JWTSecret: "0123456789abcdef0123456789abcdef"}}
	if err := valid.ValidateForAPI(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestAuthDefaults(t *testing.T) {
	setValidEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Auth.OTPMaxAttempts != 3 {
		t.Fatalf("expected OTP attempts default 3, got %d", cfg.Auth.OTPMaxAttempts)
	}
	if cfg.Auth.LoginMaxFailures != 10 {
		t.Fatalf("expected login max failures default 10, got %d", cfg.Auth.LoginMaxFailures)
	}
	if cfg.Auth.AccessTokenTTL.String() != "15m0s" {
		t.Fatalf("expected access TTL 15m, got %s", cfg.Auth.AccessTokenTTL)
	}
}

func TestLoadInvalidRange(t *testing.T) {
	setValidEnv(t)
	t.Setenv("DB_MIN_CONNS", "50")
	t.Setenv("DB_MAX_CONNS", "10")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "DB_MIN_CONNS") {
		t.Fatalf("expected DB_MIN_CONNS range error, got %v", err)
	}
}

func TestMediaConfigIsOptionalForEveryCommand(t *testing.T) {
	setValidEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load without media credentials: %v", err)
	}
	if cfg.Media.IsConfigured() {
		t.Fatal("expected media to be unconfigured when MEDIA_* is absent")
	}
	if cfg.Media.Folder != "artist-shop" {
		t.Fatalf("expected the default upload folder, got %q", cfg.Media.Folder)
	}
}

func TestMediaConfigReadsTheMediaPrefix(t *testing.T) {
	setValidEnv(t)
	t.Setenv("MEDIA_CLOUD_NAME", "artist-shop")
	t.Setenv("MEDIA_API_KEY", "key")
	t.Setenv("MEDIA_API_SECRET", "secret")
	t.Setenv("MEDIA_FOLDER", "avatars")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Media.CloudName != "artist-shop" || cfg.Media.APIKey != "key" || cfg.Media.APISecret != "secret" {
		t.Fatalf("MEDIA_* was not read: %+v", cfg.Media)
	}
	if cfg.Media.Folder != "avatars" {
		t.Fatalf("expected the configured folder, got %q", cfg.Media.Folder)
	}
	if !cfg.Media.IsConfigured() {
		t.Fatal("expected media to be configured")
	}
	partial := MediaConfig{CloudName: "artist-shop"}
	if partial.IsConfigured() {
		t.Fatal("expected partial credentials to report unconfigured")
	}
}

func TestUserRateLimitsDefault(t *testing.T) {
	setValidEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.User.AvatarUploadRatePerHour != 10 {
		t.Fatalf("expected 10 avatar uploads per hour, got %d", cfg.User.AvatarUploadRatePerHour)
	}
	if cfg.User.AddressWriteRatePerMinute != 30 {
		t.Fatalf("expected 30 address writes per minute, got %d", cfg.User.AddressWriteRatePerMinute)
	}
}

func TestUserRateLimitsReadTheUserPrefix(t *testing.T) {
	setValidEnv(t)
	t.Setenv("USER_AVATAR_UPLOAD_RATE_PER_HOUR", "3")
	t.Setenv("USER_ADDRESS_WRITE_RATE_PER_MINUTE", "7")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.User.AvatarUploadRatePerHour != 3 {
		t.Fatalf("USER_AVATAR_UPLOAD_RATE_PER_HOUR was not read: %d", cfg.User.AvatarUploadRatePerHour)
	}
	if cfg.User.AddressWriteRatePerMinute != 7 {
		t.Fatalf("USER_ADDRESS_WRITE_RATE_PER_MINUTE was not read: %d", cfg.User.AddressWriteRatePerMinute)
	}
}

func TestValidateRejectsNonPositiveUserRateLimits(t *testing.T) {
	setValidEnv(t)
	t.Setenv("USER_AVATAR_UPLOAD_RATE_PER_HOUR", "0")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "USER rate-limit thresholds") {
		t.Fatalf("expected a USER rate-limit error, got %v", err)
	}
}
