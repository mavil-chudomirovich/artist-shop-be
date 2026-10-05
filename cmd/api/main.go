// Command api is the composition root of the modular-monolith backend.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-chi/chi/v5"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/application/implement"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/infrastructure/implement/auditor"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/infrastructure/implement/email"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/infrastructure/implement/postgres"
	authredis "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/infrastructure/implement/redis"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/infrastructure/implement/token"
	authhttp "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/presentation/http"
	useradministrative "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/infrastructure/implement/administrative"
	userhttp "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/presentation/http"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/audit"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/cache"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/config"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database/migrate"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/httpserver"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/logging"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	_ = config.LoadDotenv()

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := cfg.ValidateForAPI(); err != nil {
		return err
	}
	logger := logging.New(cfg.Log.Level, os.Stdout)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db, err := database.New(ctx, cfg.Database)
	if err != nil {
		return err
	}
	defer db.Close()

	runner, err := migrate.New(cfg.Database.URL, cfg.Migrations.LockTimeout)
	if err != nil {
		return err
	}
	defer func() { _ = runner.Close() }()

	if cfg.Migrations.AutoApply {
		if err := runner.Up(ctx); err != nil {
			return err
		}
	}
	db.SetReady(true)

	redisCache, err := cache.New(ctx, cfg.Redis)
	if err != nil {
		return err
	}
	defer func() { _ = redisCache.Close() }()

	auditWriter := audit.NewWriter(
		audit.NewRepository(db.Pool),
		logger,
		cfg.Audit.QueueSize,
		cfg.Audit.MaxRetries,
		1,
	)
	auditWriter.Start(ctx)
	auditorAdapter := auditor.New(auditWriter)

	authService := implement.New(implement.Service{
		Users:         postgres.NewUserRepository(db.Pool),
		Sessions:      postgres.NewSessionRepository(db.Pool),
		Resets:        postgres.NewResetRepository(db.Pool),
		OTP:           authredis.NewOTPStore(redisCache, token.Hasher{}, cfg.Auth.OTPMaxAttempts, cfg.Auth.OTPTTL, cfg.Auth.OTPBlockTTL, cfg.Auth.OTPResendCooldown),
		Blacklist:     authredis.NewBlacklistStore(redisCache),
		Guard:         authredis.NewLoginGuard(redisCache, cfg.Auth.LoginMaxFailures, cfg.Auth.LoginLockoutTTL, cfg.Auth.LoginLockoutTTL),
		Hasher:        token.Hasher{},
		Access:        token.NewAccessIssuer(cfg.Auth.JWTSecret, cfg.Auth.AccessTokenTTL),
		RefreshTokens: token.Generator{},
		Email:         email.NewSender(cfg.Auth, logger),
		Audit:         auditorAdapter,
		Tx:            db,
		Config: implement.Config{
			RefreshTokenTTL:  cfg.Auth.RefreshTokenTTL,
			PasswordResetTTL: cfg.Auth.PasswordResetTTL,
		},
	})

	authHandler := authhttp.New(authService, auditorAdapter, logger)
	authHooks := authHandler.AuthHooks()

	// Module 02 (user). The administrative reference data is served directly from
	// the Divisions port: the dataset is embedded in the binary, so the answers
	// need no use case and no account. The /users group joins this composition
	// with the module's repositories, media store and use cases.
	userDivisionsHandler := userhttp.NewDivisionsHandler(useradministrative.New(), logger)

	router := httpserver.NewRouter(httpserver.Dependencies{
		Config:  cfg,
		Logger:  logger,
		DB:      db,
		Cache:   redisCache,
		Version: runner.Version,
		Auth:    authHooks,
		Mount: func(r chi.Router) {
			r.Mount("/auth", authHandler.Router(cfg.Auth))
			r.Mount("/divisions", userDivisionsHandler.Router(authHooks))
		},
	})
	server := httpserver.New(cfg, logger, router)

	errCh := make(chan error, 1)
	go func() { errCh <- server.Start() }()

	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	case err := <-errCh:
		if err != nil {
			return err
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", slog.Any("error", err))
	}
	auditWriter.Stop(shutdownCtx)
	return nil
}
