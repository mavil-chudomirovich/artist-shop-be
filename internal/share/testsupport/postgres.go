//go:build integration

// Package testsupport provides isolated PostgreSQL databases for integration
// tests via testcontainers. Requires Docker and the `integration` build tag.
package testsupport

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// disableReaper turns off the testcontainers reaper (ryuk). Docker Desktop on
// Windows exposes the daemon over a named pipe that the ryuk container cannot
// reach, so every container start fails after a 60s timeout. Containers are
// short-lived and each test terminates its own, so the reaper is redundant.
func disableReaper() {
	if os.Getenv("TESTCONTAINERS_RYUK_DISABLED") == "" {
		_ = os.Setenv("TESTCONTAINERS_RYUK_DISABLED", "true")
	}
}

// Postgres is a running PostgreSQL container for integration tests.
type Postgres struct {
	container testcontainers.Container
}

// StartPostgres boots a postgres:16 container. The caller owns the returned
// handle and must call Stop (typically from TestMain) when finished.
func StartPostgres(ctx context.Context) (*Postgres, error) {
	disableReaper()
	req := testcontainers.ContainerRequest{
		Image:        "postgres:16",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_USER":     "app",
			"POSTGRES_PASSWORD": "app",
			"POSTGRES_DB":       "test",
		},
		WaitingFor: wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).
			WithStartupTimeout(90 * time.Second),
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, err
	}
	return &Postgres{container: container}, nil
}

// DSN returns the connection string for the container database.
func (p *Postgres) DSN(ctx context.Context) (string, error) {
	host, err := p.container.Host(ctx)
	if err != nil {
		return "", fmt.Errorf("resolve container host: %w", err)
	}
	port, err := p.container.MappedPort(ctx, "5432")
	if err != nil {
		return "", fmt.Errorf("resolve container port: %w", err)
	}
	return fmt.Sprintf("postgres://app:app@%s:%s/test?sslmode=disable", host, port.Port()), nil
}

// Stop terminates the container.
func (p *Postgres) Stop(ctx context.Context) error {
	return p.container.Terminate(ctx)
}

// PostgresDSN starts an ephemeral PostgreSQL container and returns its DSN. The
// container is terminated automatically when the test finishes.
func PostgresDSN(t *testing.T) string {
	t.Helper()
	ctx := context.Background()

	pg, err := StartPostgres(ctx)
	if err != nil {
		t.Skipf("skipping integration test: cannot start postgres container: %v", err)
	}
	t.Cleanup(func() {
		_ = pg.Stop(context.Background())
	})

	dsn, err := pg.DSN(ctx)
	if err != nil {
		t.Fatalf("%v", err)
	}
	return dsn
}
