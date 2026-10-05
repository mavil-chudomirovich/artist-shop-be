package migrate

import (
	"testing"
	"time"
)

func TestNewAndClose(t *testing.T) {
	runner, err := New("postgres://app:app@localhost:5432/artist_shop?sslmode=disable", time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if runner == nil {
		t.Fatal("expected a runner")
	}
	if err := runner.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}
}

func TestNewTwiceReusesSetup(t *testing.T) {
	first, err := New("postgres://app:app@localhost:5432/artist_shop?sslmode=disable", time.Second)
	if err != nil {
		t.Fatalf("first New failed: %v", err)
	}
	defer first.Close()

	second, err := New("postgres://app:app@localhost:5432/artist_shop?sslmode=disable", time.Second)
	if err != nil {
		t.Fatalf("second New failed: %v", err)
	}
	defer second.Close()
}
