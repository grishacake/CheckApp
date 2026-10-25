package postgres

import (
	"context"
	"errors"
	"math"
	"testing"

	"app-grabber-backend/internal/model"
)

func TestSaveAppsInvalidInput(t *testing.T) {
	// Invalid data must be rejected before opening a transaction.
	repo := NewAppRepository(nil)
	for _, card := range []model.App{
		{Name: "Missing package"},
		{PackageName: "com.example.app"},
		{PackageName: "com.example.app", Name: "Example", Rating: math.NaN()},
	} {
		if err := repo.Save(context.Background(), "google_play", []model.App{card}); err == nil {
			t.Fatal("invalid card was accepted")
		}
	}
	if err := repo.Save(context.Background(), "google_play", nil); err != nil {
		t.Fatalf("empty batch: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := repo.Save(ctx, "google_play", []model.App{{PackageName: "com.example.app", Name: "Example"}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation was lost: %v", err)
	}
}
