package postgres

import (
	"context"
	"errors"
	"testing"

	"app-grabber-backend/internal/auth"
)

func TestFavoritesRejectInvalidUserUUIDBeforeDatabaseAccess(t *testing.T) {
	repo := NewFavoritesRepository(nil)
	if err := repo.Add(context.Background(), "not-a-uuid", "google_play", "com.example.app"); !errors.Is(err, auth.ErrUserNotFound) {
		t.Fatalf("Add invalid UUID: %v", err)
	}
	if err := repo.Remove(context.Background(), "not-a-uuid", "google_play", "com.example.app"); !errors.Is(err, auth.ErrUserNotFound) {
		t.Fatalf("Remove invalid UUID: %v", err)
	}
	if _, err := repo.List(context.Background(), "not-a-uuid", 20, 0); !errors.Is(err, auth.ErrUserNotFound) {
		t.Fatalf("List invalid UUID: %v", err)
	}
}
