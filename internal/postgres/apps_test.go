package postgres

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"app-grabber-backend/internal/app"
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

func TestDecodeStoredApp(t *testing.T) {
	for _, metadata := range []string{
		`null`, `{}`, `{"package_name":"com.example.other","name":"Other"}`,
		`{"package_name":"com.example.app","name":""}`, `{"rating":"invalid"}`,
	} {
		if _, err := decodeStoredApp([]byte(metadata), "com.example.app"); err == nil {
			t.Fatalf("corrupt metadata accepted: %s", metadata)
		}
	}
	card, err := decodeStoredApp([]byte(`{"package_name":"com.example.app","name":"Cached","rating":4.2}`), "com.example.app")
	if err != nil || card.Name != "Cached" || card.Rating != 4.2 {
		t.Fatalf("valid cached card changed: %v", err)
	}
}

func TestGetFreshDisabledOrCanceled(t *testing.T) {
	repo := NewAppRepository(nil)
	if _, err := repo.GetFresh(context.Background(), "google_play", "com.example.app", 0); !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("disabled cache: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := repo.GetFresh(ctx, "google_play", "com.example.app", time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

func TestSearchByTextDisabledOrCanceled(t *testing.T) {
	repo := NewAppRepository(nil)
	for _, tc := range []struct {
		query  string
		limit  int
		maxAge time.Duration
	}{
		{"", 5, time.Hour},
		{"   ", 5, time.Hour},
		{"test", 0, time.Hour},
		{"test", -1, time.Hour},
		{"test", 5, 0},
		{"test", 5, -time.Hour},
	} {
		res, err := repo.SearchByText(context.Background(), "google_play", tc.query, tc.limit, tc.maxAge)
		if err != nil || res != nil {
			t.Fatalf("expected nil results for %+v, got res=%#v err=%v", tc, res, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := repo.SearchByText(ctx, "google_play", "test", 5, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}
