// Command seed provisions the single admin account from environment credentials.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/application/implement"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/infrastructure/implement/auditor"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/infrastructure/implement/postgres"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/infrastructure/implement/token"
	authcli "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/presentation/cli"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/audit"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/config"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/logging"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}
}

func run() error {
	_ = config.LoadDotenv()
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.Auth.AdminEmail == "" || cfg.Auth.AdminPassword == "" {
		return errors.New("ADMIN_EMAIL and ADMIN_PASSWORD are required")
	}

	ctx := context.Background()
	logger := logging.New(cfg.Log.Level, os.Stdout)

	db, err := database.New(ctx, cfg.Database)
	if err != nil {
		return err
	}
	defer db.Close()

	// Provisioning the admin account is an administrative mutation, so it is
	// recorded in audit_logs before the command exits.
	auditWriter := audit.NewWriter(
		audit.NewRepository(db.Pool),
		logger,
		cfg.Audit.QueueSize,
		cfg.Audit.MaxRetries,
		1,
	)
	auditWriter.Start(ctx)

	svc := implement.New(implement.Service{
		Users:  postgres.NewUserRepository(db.Pool),
		Hasher: token.Hasher{},
		Audit:  auditor.New(auditWriter),
	})
	if err := authcli.Seed(ctx, svc, cfg.Auth.AdminEmail, cfg.Auth.AdminPassword); err != nil {
		return err
	}
	auditWriter.Stop(ctx)
	logger.Info("admin account provisioned")
	return nil
}
