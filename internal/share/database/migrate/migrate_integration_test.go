//go:build integration

package migrate

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/testsupport"
)

func TestMigrateUpDownAndVersion(t *testing.T) {
	dsn := testsupport.PostgresDSN(t)
	ctx := context.Background()

	runner, err := New(dsn, 30*time.Second)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer runner.Close()

	if err := runner.Up(ctx); err != nil {
		t.Fatalf("Up: %v", err)
	}

	version, err := runner.Version(ctx)
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if version <= 0 {
		t.Fatalf("expected a positive schema version, got %d", version)
	}

	if err := runner.Down(ctx); err != nil {
		t.Fatalf("Down: %v", err)
	}
}

func TestConcurrentUpAppliesOnce(t *testing.T) {
	dsn := testsupport.PostgresDSN(t)
	ctx := context.Background()

	const instances = 2
	var wg sync.WaitGroup
	errs := make([]error, instances)

	for i := 0; i < instances; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			runner, err := New(dsn, 30*time.Second)
			if err != nil {
				errs[idx] = err
				return
			}
			defer runner.Close()
			errs[idx] = runner.Up(ctx)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("instance %d failed: %v", i, err)
		}
	}

	runner, err := New(dsn, 30*time.Second)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer runner.Close()

	version, err := runner.Version(ctx)
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if version <= 0 {
		t.Fatalf("expected schema applied once, got version %d", version)
	}
}
