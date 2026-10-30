package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"app-grabber-backend/internal/history"
	"app-grabber-backend/internal/model"
)

// TEST_DATABASE_URL must point to a dedicated database with migrations applied.
func TestHistoryIntegration(t *testing.T) {
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
	storeCode := "test_history_" + suffix
	emailA, emailB := "history-a-"+suffix+"@example.test", "history-b-"+suffix+"@example.test"
	appIDs := make([]string, 0, 2)
	var storeID int32
	if err := pool.QueryRow(ctx, `INSERT INTO public.stores(code,name) VALUES ($1,$1) RETURNING id`, storeCode).Scan(&storeID); err != nil {
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
		if _, err := cleanup.Exec(cleanupCtx, `DELETE FROM public.users WHERE email IN ($1,$2)`, emailA, emailB); err != nil {
			t.Errorf("cleanup users: %v", err)
		}
		if _, err := cleanup.Exec(cleanupCtx, `DELETE FROM public.store_apps WHERE store_id=$1`, storeID); err != nil {
			t.Errorf("cleanup store cards: %v", err)
		}
		for _, appID := range appIDs {
			if _, err := cleanup.Exec(cleanupCtx, `DELETE FROM public.apps WHERE id=$1`, appID); err != nil {
				t.Errorf("cleanup app: %v", err)
			}
		}
		if _, err := cleanup.Exec(cleanupCtx, `DELETE FROM public.stores WHERE id=$1`, storeID); err != nil {
			t.Errorf("cleanup store: %v", err)
		}
	})
	var userA, userB string
	for _, pair := range []struct {
		email, nickname string
		into            *string
	}{{emailA, "A", &userA}, {emailB, "B", &userB}} {
		if err := pool.QueryRow(ctx, `INSERT INTO public.users(email,nickname,password_hash) VALUES ($1,$2,'unused') RETURNING id::text`, pair.email, pair.nickname).Scan(pair.into); err != nil {
			t.Fatal(err)
		}
	}
	packages := []string{"com.test.history.one", "com.test.history.two"}
	cardIDs := make([]int64, len(packages))
	for i, packageName := range packages {
		card := model.App{PackageName: packageName, Name: storeCode + fmt.Sprintf(" app %d", i), StoreURL: "https://example.test", Developer: "Test", IconURL: "https://example.test/icon", Category: "Test", Rating: 4, AgeRating: "3+"}
		metadata, err := json.Marshal(card)
		if err != nil {
			t.Fatal(err)
		}
		var appID string
		if err := pool.QueryRow(ctx, `INSERT INTO public.apps(name) VALUES ($1) RETURNING id::text`, card.Name).Scan(&appID); err != nil {
			t.Fatal(err)
		}
		appIDs = append(appIDs, appID)
		if err := pool.QueryRow(ctx, `INSERT INTO public.store_apps(app_id,store_id,store_app_id,metadata) VALUES ($1,$2,$3,$4) RETURNING id`, appID, storeID, packageName, metadata).Scan(&cardIDs[i]); err != nil {
			t.Fatal(err)
		}
	}
	repo := NewHistoryRepository(pool)
	if err := repo.Record(ctx, "", storeCode, packages[0]); err != nil {
		t.Fatalf("anonymous event: %v", err)
	}
	if err := repo.Record(ctx, userA, storeCode, packages[0]); err != nil {
		t.Fatalf("first event: %v", err)
	}
	if err := repo.Record(ctx, userA, storeCode, packages[0]); err != nil {
		t.Fatalf("repeat event: %v", err)
	}
	if err := repo.Record(ctx, userA, storeCode, packages[1]); err != nil {
		t.Fatalf("second card: %v", err)
	}
	if err := repo.Record(ctx, userB, storeCode, packages[0]); err != nil {
		t.Fatalf("other user: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE public.user_view_history SET viewed_at='2026-01-01T00:00:00Z' WHERE user_id=$1`, userA); err != nil {
		t.Fatal(err)
	}
	views, err := repo.List(ctx, userA, 2, 0)
	if err != nil || len(views) != 2 || views[0].App.PackageName != packages[1] || views[1].App.PackageName != packages[0] || views[0].ID <= views[1].ID {
		t.Fatalf("first page: %#v err=%v", views, err)
	}
	if views[0].StoreAppID != cardIDs[1] || views[1].Store != "test_history_"+suffix {
		t.Fatalf("view data: %#v", views)
	}
	page, err := repo.List(ctx, userA, 2, 2)
	if err != nil || len(page) != 1 || page[0].App.PackageName != packages[0] || page[0].ID >= views[1].ID {
		t.Fatalf("third event page: %#v err=%v", page, err)
	}
	page, err = repo.List(ctx, userB, 20, 0)
	if err != nil || len(page) != 1 || page[0].App.PackageName != packages[0] {
		t.Fatalf("user scope: %#v err=%v", page, err)
	}
	if err := repo.Record(ctx, userA, storeCode, "com.test.history.missing"); err != history.ErrCardNotFound {
		t.Fatalf("missing card error=%v", err)
	}
	if _, err := repo.List(ctx, "invalid-uuid", 20, 0); err == nil {
		t.Fatal("invalid UUID was accepted")
	}
	// An inactive store remains visible in past history.
	if _, err := pool.Exec(ctx, `UPDATE public.stores SET is_active=FALSE WHERE id=$1`, storeID); err != nil {
		t.Fatal(err)
	}
	if views, err = repo.List(ctx, userA, 20, 0); err != nil || len(views) != 3 {
		t.Fatalf("inactive store history: %d, %v", len(views), err)
	}
	// ON DELETE SET NULL preserves events after user deletion.
	if _, err := pool.Exec(ctx, `DELETE FROM public.users WHERE id=$1`, userA); err != nil {
		t.Fatal(err)
	}
	var anonymousCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM public.user_view_history WHERE user_id IS NULL AND store_app_id=$1`, cardIDs[0]).Scan(&anonymousCount); err != nil || anonymousCount < 2 {
		t.Fatalf("SET NULL events=%d err=%v", anonymousCount, err)
	}
	// Deleting the store card cascades its history rows.
	if _, err := pool.Exec(ctx, `DELETE FROM public.store_apps WHERE id=$1`, cardIDs[1]); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM public.user_view_history WHERE store_app_id=$1`, cardIDs[1]).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("card cascade rows=%d err=%v", remaining, err)
	}
}
