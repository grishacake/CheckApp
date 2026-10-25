package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"app-grabber-backend/internal/model"
)

func TestAppsIntegration(t *testing.T) {
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
	storeCode := fmt.Sprintf("test_apps_%d", time.Now().UnixNano())
	var storeID int32
	if err := pool.QueryRow(ctx, `INSERT INTO public.stores (code, name) VALUES ($1, $1) RETURNING id`, storeCode).Scan(&storeID); err != nil {
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
		if _, err := cleanup.Exec(cleanupCtx, `
			WITH deleted AS (
				DELETE FROM public.store_apps WHERE store_id = $1 RETURNING app_id
			)
			DELETE FROM public.apps WHERE id IN (SELECT app_id FROM deleted)`, storeID); err != nil {
			t.Errorf("cleanup apps: %v", err)
		}
		if _, err := cleanup.Exec(cleanupCtx, `DELETE FROM public.stores WHERE id = $1`, storeID); err != nil {
			t.Errorf("cleanup store: %v", err)
		}
	})

	type snapshot struct {
		id                        int64
		appID, name               string
		card                      model.App
		scraped, created, updated time.Time
	}
	read := func(packageName string) snapshot {
		t.Helper()
		var got snapshot
		var metadata []byte
		err := pool.QueryRow(ctx, `
			SELECT sa.id, sa.app_id::text, a.name, sa.metadata, sa.scraped_at, a.created_at, a.updated_at
			FROM public.store_apps sa JOIN public.apps a ON a.id = sa.app_id
			WHERE sa.store_id = $1 AND sa.store_app_id = $2`, storeID, packageName).
			Scan(&got.id, &got.appID, &got.name, &metadata, &got.scraped, &got.created, &got.updated)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(metadata, &got.card); err != nil {
			t.Fatal(err)
		}
		return got
	}
	card := model.App{
		PackageName: "com.test.first", Name: storeCode + " original",
		StoreURL:  "https://play.google.com/store/apps/details?id=com.test.first",
		Developer: "Developer's name", IconURL: "https://example.com/icon.png",
		Category: "Education", Rating: 4.2, AgeRating: "3+",
	}
	repo := NewAppRepository(pool)
	if err := repo.Save(ctx, storeCode, []model.App{card}); err != nil {
		t.Fatal(err)
	}
	first := read(card.PackageName)
	if first.card != card || first.name != card.Name || first.appID == "" || first.scraped.IsZero() {
		t.Fatal("initial card was not persisted correctly")
	}
	card.Name = storeCode + " updated"
	card.Rating = 4.8
	if err := repo.Save(ctx, storeCode, []model.App{card}); err != nil {
		t.Fatal(err)
	}
	updated := read(card.PackageName)
	if updated.id != first.id || updated.appID != first.appID || updated.card != card || updated.name != card.Name ||
		!updated.created.Equal(first.created) || !updated.scraped.After(first.scraped) || !updated.updated.After(first.updated) {
		t.Fatal("update replaced IDs, lost metadata, or failed to refresh timestamps")
	}
	second := card
	second.PackageName = "com.test.second"
	batch := []model.App{second, card}
	wantOrder := append([]model.App(nil), batch...)
	if err := repo.Save(ctx, storeCode, batch); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(batch, wantOrder) || read(second.PackageName).appID == first.appID {
		t.Fatal("repository reordered the response or merged packages by name")
	}

	// Concurrent overlapping batches in opposite orders must not deadlock or
	// leave duplicate store cards or unused parent products.
	results := make(chan error, 8)
	third := card
	third.PackageName = "com.test.concurrent"
	third.Name = storeCode + " concurrent"
	for i := 0; i < cap(results); i++ {
		go func(reverse bool) {
			cards := []model.App{card, third}
			if reverse {
				cards[0], cards[1] = cards[1], cards[0]
			}
			results <- repo.Save(ctx, storeCode, cards)
		}(i%2 == 0)
	}
	for i := 0; i < cap(results); i++ {
		if err := <-results; err != nil {
			t.Fatalf("concurrent save failed: %v", err)
		}
	}
	if got := read(third.PackageName); got.card != third || got.appID == first.appID {
		t.Fatal("concurrent creation did not preserve an independent product")
	}
	// Failure on the second card must roll back the first card and its parent.
	invalid := []model.App{
		{PackageName: "com.test.rollbacka", Name: storeCode + " rollback"},
		{PackageName: "com.test.rollbackz", Name: strings.Repeat("x", 256)},
	}
	if err := repo.Save(ctx, storeCode, invalid); err == nil {
		t.Fatal("oversized name should fail and roll back the batch")
	}
	var cardCount, appCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM public.store_apps WHERE store_id = $1`, storeID).Scan(&cardCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM public.apps WHERE starts_with(name, $1)`, storeCode).Scan(&appCount); err != nil {
		t.Fatal(err)
	}
	if cardCount != 3 || appCount != 3 {
		t.Fatalf("duplicate rows or partial writes: cards=%d products=%d", cardCount, appCount)
	}
	if _, err := pool.Exec(ctx, `UPDATE public.stores SET is_active = FALSE WHERE id = $1`, storeID); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, storeCode, []model.App{card}); err == nil {
		t.Fatal("inactive store accepted a save")
	}
}
