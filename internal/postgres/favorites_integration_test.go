package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"app-grabber-backend/internal/auth"
	"app-grabber-backend/internal/favorites"
	"app-grabber-backend/internal/model"
)

// TEST_DATABASE_URL must point to a dedicated database with migrations applied.
func TestFavoritesIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to test against PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	storeCode := "test_favorites_" + suffix
	emailA, emailB := "favorites-a-"+suffix+"@example.test", "favorites-b-"+suffix+"@example.test"
	var storeID int32
	if err := pool.QueryRow(ctx, `INSERT INTO public.stores(code, name) VALUES ($1, $1) RETURNING id`, storeCode).Scan(&storeID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cleanup, err := Open(cleanupCtx, dsn)
		if err != nil {
			t.Errorf("cleanup connection: %v", err)
			return
		}
		defer cleanup.Close()
		if _, err := cleanup.Exec(cleanupCtx, `DELETE FROM public.users WHERE email IN ($1, $2)`, emailA, emailB); err != nil {
			t.Errorf("cleanup test users: %v", err)
		}
		if _, err := cleanup.Exec(cleanupCtx, `WITH deleted AS (DELETE FROM public.store_apps WHERE store_id = $1 RETURNING app_id) DELETE FROM public.apps WHERE id IN (SELECT app_id FROM deleted)`, storeID); err != nil {
			t.Errorf("cleanup test apps: %v", err)
		}
		if _, err := cleanup.Exec(cleanupCtx, `DELETE FROM public.stores WHERE id = $1`, storeID); err != nil {
			t.Errorf("cleanup test store: %v", err)
		}
	})
	var userA, userB string
	if err := pool.QueryRow(ctx, `INSERT INTO public.users(email,nickname,password_hash) VALUES ($1,'A','unused') RETURNING id::text`, emailA).Scan(&userA); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO public.users(email,nickname,password_hash) VALUES ($1,'B','unused') RETURNING id::text`, emailB).Scan(&userB); err != nil {
		t.Fatal(err)
	}
	// Persist cards directly so the test exercises favorites SQL without external stores.
	packages := []string{"com.test.favorites.one", "com.test.favorites.two", "com.test.favorites.three"}
	cardIDs := make([]int64, len(packages))
	for i, packageName := range packages {
		card := model.App{PackageName: packageName, Name: storeCode + fmt.Sprintf(" app %d", i), StoreURL: "https://example.test/app", Developer: "Test", IconURL: "https://example.test/icon.png", Category: "Test", Rating: 4, AgeRating: "3+"}
		metadata, err := json.Marshal(card)
		if err != nil {
			t.Fatal(err)
		}
		var appID string
		if err := pool.QueryRow(ctx, `INSERT INTO public.apps(name) VALUES ($1) RETURNING id::text`, card.Name).Scan(&appID); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `INSERT INTO public.store_apps(app_id,store_id,store_app_id,metadata) VALUES ($1,$2,$3,$4) RETURNING id`, appID, storeID, packageName, metadata).Scan(&cardIDs[i]); err != nil {
			t.Fatal(err)
		}
	}
	repo := NewFavoritesRepository(pool)
	add := func(user, packageName string) error { return repo.Add(ctx, user, storeCode, packageName) }
	for _, packageName := range packages {
		if err := add(userA, packageName); err != nil {
			t.Fatalf("add %s: %v", packageName, err)
		}
	}
	var firstCreated time.Time
	if err := pool.QueryRow(ctx, `SELECT created_at FROM public.user_favorites WHERE user_id = $1 AND store_app_id = $2`, userA, cardIDs[0]).Scan(&firstCreated); err != nil {
		t.Fatal(err)
	}
	if err := add(userA, packages[0]); err != nil {
		t.Fatalf("duplicate add: %v", err)
	}
	var count int
	var duplicateCreated time.Time
	if err := pool.QueryRow(ctx, `SELECT count(*), min(created_at) FROM public.user_favorites WHERE user_id = $1 AND store_app_id = $2`, userA, cardIDs[0]).Scan(&count, &duplicateCreated); err != nil {
		t.Fatal(err)
	}
	if count != 1 || !duplicateCreated.Equal(firstCreated) {
		t.Fatalf("duplicate changed row/timestamp: count=%d before=%s after=%s", count, firstCreated, duplicateCreated)
	}
	if err := add(userB, packages[0]); err != nil {
		t.Fatalf("add for second user: %v", err)
	}

	// Force a tie for the first two records to verify store_app_id is the secondary order.
	if _, err := pool.Exec(ctx, `UPDATE public.user_favorites SET created_at = '2026-01-01T00:00:00Z' WHERE user_id = $1 AND store_app_id IN ($2,$3)`, userA, cardIDs[0], cardIDs[1]); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE public.user_favorites SET created_at = '2025-01-01T00:00:00Z' WHERE user_id = $1 AND store_app_id = $2`, userA, cardIDs[2]); err != nil {
		t.Fatal(err)
	}
	page, err := repo.List(ctx, userA, 2, 0)
	if err != nil || len(page) != 2 || page[0].StoreAppID != cardIDs[1] || page[1].StoreAppID != cardIDs[0] || page[0].App.PackageName != packages[1] {
		t.Fatalf("first page order: %#v, err=%v", page, err)
	}
	page, err = repo.List(ctx, userA, 2, 2)
	if err != nil || len(page) != 1 || page[0].StoreAppID != cardIDs[2] {
		t.Fatalf("second page: %#v, err=%v", page, err)
	}
	page, err = repo.List(ctx, userB, 20, 0)
	if err != nil || len(page) != 1 || page[0].StoreAppID != cardIDs[0] {
		t.Fatalf("user isolation: %#v, err=%v", page, err)
	}

	if err := repo.Remove(ctx, userA, storeCode, packages[0]); err != nil {
		t.Fatal(err)
	}
	page, err = repo.List(ctx, userB, 20, 0)
	if err != nil || len(page) != 1 || page[0].StoreAppID != cardIDs[0] {
		t.Fatalf("removing another user's favorite affected user B: %#v, err=%v", page, err)
	}
	if err := repo.Remove(ctx, userA, storeCode, packages[0]); err != nil {
		t.Fatalf("idempotent removal: %v", err)
	}
	if err := add(userA, "com.test.favorites.missing"); !errors.Is(err, favorites.ErrCardNotFound) {
		t.Fatalf("missing card: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE public.stores SET is_active = FALSE WHERE id = $1`, storeID); err != nil {
		t.Fatal(err)
	}
	if err := add(userA, packages[0]); !errors.Is(err, favorites.ErrCardNotFound) {
		t.Fatalf("inactive store add: %v", err)
	}
	page, err = repo.List(ctx, userA, 20, 0)
	if err != nil || len(page) != 2 {
		t.Fatalf("inactive store hid saved favorites: %#v, err=%v", page, err)
	}
	if err := repo.Remove(ctx, userA, storeCode, packages[1]); err != nil {
		t.Fatalf("remove from inactive store: %v", err)
	}
	if _, err := repo.List(ctx, userA, 20, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.List(ctx, "not-a-uuid", 20, 0); !errors.Is(err, auth.ErrUserNotFound) {
		t.Fatalf("invalid UUID: %v", err)
	}
}
