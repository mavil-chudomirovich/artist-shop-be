// Command api is the composition root of the modular-monolith backend.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-chi/chi/v5"
	httpSwagger "github.com/swaggo/http-swagger/v2"

	_ "github.com/mavil-chudomirovich/artist-shop-be/docs/swagger"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/application/implement"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/infrastructure/implement/auditor"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/infrastructure/implement/email"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/infrastructure/implement/postgres"
	authredis "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/infrastructure/implement/redis"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/infrastructure/implement/token"
	authhttp "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/presentation/http"
	categoryimplement "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/application/implement"
	categorymapper "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/application/mapper"
	categoryauditor "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/infrastructure/implement/auditor"
	categorypostgres "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/infrastructure/implement/postgres"
	categoryvisibility "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/infrastructure/implement/visibility"
	categoryhttp "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/presentation/http"
	productimplement "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/implement"
	productmapper "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/mapper"
	productpostgres "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/infrastructure/implement/postgres"
	producthttp "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/presentation/http"
	userimplement "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/implement"
	userappinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/interface"
	usermapper "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/mapper"
	useradministrative "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/infrastructure/implement/administrative"
	userauditor "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/infrastructure/implement/auditor"
	userpostgres "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/infrastructure/implement/postgres"
	userhttp "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/presentation/http"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/audit"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/cache"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/config"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database/migrate"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/httpserver"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/logging"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/media"
)

// @title           Artist Shop API
// @version         1.0
// @description     Modular-monolith backend for the artist shop.
// @description     Every business endpoint is mounted under /api/v1 and uses the shared success/error envelope.
// @BasePath        /api/v1
// @securityDefinitions.apikey BearerAuth
// @in              header
// @name            Authorization
// @description     Send the access token as "Bearer <accessToken>".
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

	// The avatar ceilings are the domain's own (FR-014, FR-015). No environment
	// setting overrides them, so the composition leaves the zero value and
	// appinterface.Config falls back to the documented contract ceilings rather than
	// widening or narrowing them from here. The same value is handed to the service
	// and to the handler, because the handler sizes the route's body limit from the
	// very ceiling the use case enforces.
	userConfig := userappinterface.Config{}

	// The shared request-size ceiling wraps the body before routing, so a route
	// cannot lift it: if it sits below what an avatar upload needs, every upload is
	// refused by the pipeline first and the operator is told nothing about the
	// setting to change. The composition root is the only place that sees both
	// numbers, so it refuses to start here — before it opens a database, runs a
	// migration or binds a port (FR-019, FR-020).
	//
	// The figure comes from the module that owns the route, so there is one owner of
	// the number rather than a literal copied into the shared layer, which
	// Constitution I forbids from importing a module (research D2, plan.md
	// Complexity Tracking). The guard is pure, so it runs before any wiring without
	// a cost and each of its branches is unit tested in the module's own package.
	if err := userhttp.RequireAvatarUploadCeiling(
		cfg.MaxBodyBytes,
		userhttp.AvatarUploadCeiling(userConfig.AvatarMaxBytesOrDefault()),
		cfg.Media.IsConfigured(),
	); err != nil {
		return err
	}

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
		// The composition's own structured logger, so the classified diagnostic a
		// failed message delivery leaves carries the request correlation id the
		// rest of the service logs with (Constitution VI).
		Logger: logger,
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
	//
	// One Divisions adapter serves both consumers. The reference-data handler reads
	// it directly and the use cases reach it through the port, so there is a single
	// place that knows how the embedded dataset is ordered and validated.
	userDivisions := useradministrative.New()
	userDivisionsHandler := userhttp.NewDivisionsHandler(userDivisions, logger)

	// The media store is the real adapter in every configuration, never a nil
	// stand-in and never a reject-all placeholder. MediaConfig is deliberately kept
	// out of ValidateForAPI so migrate and seed stay runnable without provider
	// credentials, which means this service can legitimately start with none — and
	// it must still start. The fail-closed behaviour therefore lives in the adapter
	// (media.requireCredentials, reached through its named NewWithDefaults
	// constructor): without credentials every provider call is refused with the
	// module's retryable media error, so avatar upload answers UNAVAILABLE and
	// nothing else is affected.
	//
	// The two alternatives were rejected deliberately. A nil store would survive
	// the profile reads, which never call the provider, and panic on the first
	// upload instead of answering it; a second "media disabled" implementation in
	// the composition would be a second answer to keep in step with the real one and
	// would need its own tests to prove it says the same thing. Refusing inside the
	// adapter keeps one implementation, one error and one place that knows the rule.
	//
	// The logger is the composition's own, so a provider failure carries the request
	// correlation the rest of the service logs with.
	userMediaStore := media.NewWithDefaults(cfg.Media, logger)
	if !cfg.Media.IsConfigured() {
		// Named, never valued: the keys are safe to log, the secrets behind them are
		// not (Constitution V, VI).
		//
		// The ceiling guard above stayed silent in this mode on purpose, so this line
		// carries the consequence an operator would otherwise have to work out: with
		// no upload possible, MAX_BODY_BYTES cannot make an avatar upload fail
		// (FR-021). Logged once, at startup.
		logger.Warn("media credentials are absent: avatar upload answers USER_MEDIA_UNAVAILABLE, every other endpoint is unaffected, and MAX_BODY_BYTES is not checked against the avatar ceiling",
			slog.String("mediaConfigKeys", "MEDIA_CLOUD_NAME, MEDIA_API_KEY, MEDIA_API_SECRET"))
	}

	// The module reuses the foundation audit writer the auth module already writes
	// through: one queue, one retry policy, one shutdown. A second writer would be a
	// second queue competing for the same rows.
	userService := userimplement.New(userimplement.Service{
		Profiles:  userpostgres.NewProfileRepository(db.Pool),
		Addresses: userpostgres.NewAddressRepository(db.Pool),
		Divisions: userDivisions,
		Tx:        db,
		Audit:     userauditor.New(auditWriter),
		Media:     userMediaStore,
		Config:    userConfig,
		Mapper:    usermapper.New(userDivisions),
	})

	// *userimplement.Service satisfies internal/contracts.CustomerLookupService
	// directly, so the same instance is what another module would be handed here —
	// no adapter, and no second lookup that could answer differently from the route
	// (docs/system-design/contract-purity.md, research D8).
	userHandler := userhttp.New(userService, userConfig, logger)

	// Module 03 (category). The catalogue is read by customers and maintained by
	// an administrator, so the module has two route groups: the public
	// /categories group, which needs no session, and the /admin/categories group,
	// which carries the administrator role guard. Both are guarded by the same
	// foundation auth hooks the other modules use, so a role denial is audited by
	// the hook's OnDenied exactly as module 01 audits its own.
	//
	// The module reuses the foundation audit writer the auth and user modules
	// already write through: one queue, one retry policy, one shutdown.
	categoryRepository := categorypostgres.NewCategoryRepository(db.Pool)
	categoryService := categoryimplement.New(categoryimplement.Service{
		Categories: categoryRepository,
		Audit:      categoryauditor.New(auditWriter),
		Mapper:     categorymapper.New(),
	})
	categoryHandler := categoryhttp.New(categoryService, logger)

	// Module 04 (product). The catalogue is read by customers and maintained by
	// an administrator; this phase mounts the public /products group, which needs
	// no session. The public reads filter on the category's display state, which
	// module 03 owns, so the composition wires the cross-module contract over
	// module 03's own repository rather than letting module 04 read module 03's
	// table (research D1, Constitution I). The administrator group is a separate
	// mount added with US2.
	productService := productimplement.New(productimplement.Service{
		Products:   productpostgres.NewProductRepository(db.Pool),
		Visibility: categoryvisibility.New(categoryRepository),
		Mapper:     productmapper.New(),
	})
	productHandler := producthttp.New(productService, logger)

	// The interactive API reference is opt-in: the composition hands the shared
	// server a handler only when the feature is enabled, so a production start
	// leaves /swagger unregistered. The generated specification in docs/swagger
	// is the same document served here.
	var swaggerHandler http.Handler
	if cfg.Swagger.Enabled {
		swaggerHandler = httpSwagger.WrapHandler
	}

	router := httpserver.NewRouter(httpserver.Dependencies{
		Config:  cfg,
		Logger:  logger,
		DB:      db,
		Cache:   redisCache,
		Version: runner.Version,
		Auth:    authHooks,
		Swagger: swaggerHandler,
		Mount: func(r chi.Router) {
			r.Mount("/auth", authHandler.Router(cfg.Auth))
			r.Mount("/divisions", userDivisionsHandler.Router(authHooks))
			// The /users group carries the module's own rate limits and the session
			// guard its routes need; the static /me segment is resolved by chi ahead
			// of the /{userId} parameter inside it.
			r.Mount("/users", userHandler.Router(cfg.User, authHooks))
			// The catalogue: the public read carries no session, and the
			// /admin/categories group is addressed by identifier behind the
			// administrator role guard (research D7).
			r.Mount("/categories", categoryHandler.Router(authHooks))
			r.Mount("/admin/categories", categoryHandler.AdminRouter(authHooks))
			// The public catalogue: on sale or pre-order, in a category the
			// operator has left on display, addressed by slug. No session is
			// required (FR-001).
			r.Mount("/products", productHandler.Router(authHooks))
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
