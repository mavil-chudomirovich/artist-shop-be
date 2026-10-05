// Command migrate applies or inspects schema migrations without starting the API.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/config"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database/migrate"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) < 2 {
		return fmt.Errorf("usage: migrate <up|down|status|version>")
	}

	_ = config.LoadDotenv()
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	runner, err := migrate.New(cfg.Database.URL, cfg.Migrations.LockTimeout)
	if err != nil {
		return err
	}
	defer func() { _ = runner.Close() }()

	ctx := context.Background()
	switch os.Args[1] {
	case "up":
		return runner.Up(ctx)
	case "down":
		return runner.Down(ctx)
	case "status":
		return runner.Status(ctx)
	case "version":
		version, err := runner.Version(ctx)
		if err != nil {
			return err
		}
		fmt.Println(version)
		return nil
	default:
		return fmt.Errorf("unknown command %q (use up|down|status|version)", os.Args[1])
	}
}
