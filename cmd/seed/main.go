// Command seed provisions the single admin account from environment credentials.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/application/implement"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/infrastructure/implement/postgres"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/infrastructure/implement/token"
	authcli "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/presentation/cli"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/config"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database"
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
	db, err := database.New(ctx, cfg.Database)
	if err != nil {
		return err
	}
	defer db.Close()

	svc := implement.New(implement.Service{
		Users:  postgres.NewUserRepository(db.Pool),
		Hasher: token.Hasher{},
	})
	if err := authcli.Seed(ctx, svc, cfg.Auth.AdminEmail, cfg.Auth.AdminPassword); err != nil {
		return err
	}
	fmt.Println("admin account provisioned:", cfg.Auth.AdminEmail)
	return nil
}
