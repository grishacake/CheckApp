package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestOpenInvalidConfig(t *testing.T) {
	for _, databaseURL := range []string{"", "  ", "postgres://user:secret-password@localhost:invalid/checkapp"} {
		t.Run(databaseURL, func(t *testing.T) {
			pool, err := Open(context.Background(), databaseURL)
			if pool != nil || err == nil {
				t.Fatal("expected invalid configuration to fail without a pool")
			}
			if strings.Contains(err.Error(), "secret-password") {
				t.Fatal("connection error exposed the password")
			}
		})
	}
}

func TestOpenCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	pool, err := Open(ctx, "postgres://checkapp:secret-password@127.0.0.1:5435/checkapp?sslmode=disable")
	if pool != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation without a pool, got %v", err)
	}
	if strings.Contains(err.Error(), "secret-password") {
		t.Fatal("connection error exposed the password")
	}
}
