// Command hash-password prints an Argon2id hash for the password in
// ADMIN_PASSWORD, for pasting into scripts/seed.vars.local.psql (see
// scripts/seed_admin.sql). It enforces the same password policy the admin
// account requires.
//
// It is a dev-only helper: it is never built into the Docker image and it
// prints only the hash, never the password.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/infrastructure/implement/token"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "hash-password:", err)
		os.Exit(1)
	}
}

func run() error {
	password := os.Getenv("ADMIN_PASSWORD")
	if password == "" {
		return errors.New("ADMIN_PASSWORD is required")
	}
	if err := model.ValidatePasswordPolicy(password); err != nil {
		return err
	}
	hash, err := token.Hasher{}.Hash(password)
	if err != nil {
		return err
	}
	fmt.Println(hash)
	return nil
}
