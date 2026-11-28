package postgres

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"app-grabber-backend/internal/comparison"
)

func TestComparisonCollectionsIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to test against PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var user string
	err = pool.QueryRow(ctx, `INSERT INTO public.users(email,nickname,password_hash) VALUES ($1,'compare','test') RETURNING id`, fmt.Sprintf("compare-%d@example.org", time.Now().UnixNano())).Scan(&user)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM public.users WHERE id=$1`, user)
	repo := NewComparisonRepository(pool)
	pair := comparison.Pair{GooglePlay: "com.first", RuStore: "ru.second"}
	for i := 0; i < 2; i++ {
		if err := repo.Record(ctx, user, pair); err != nil {
			t.Fatal(err)
		}
		if err := repo.SetFavorite(ctx, user, pair, true); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		fav   bool
		count int
	}{{false, 2}, {true, 1}} {
		items, err := repo.List(ctx, user, tc.fav, 10, 0)
		if err != nil || len(items) != tc.count {
			t.Fatalf("favorites=%v: %+v %v", tc.fav, items, err)
		}
		for _, item := range items {
			if item.Pair != pair || item.SavedAt.IsZero() {
				t.Fatal(item)
			}
		}
	}
	for i := 0; i < 2; i++ {
		if err := repo.SetFavorite(ctx, user, pair, false); err != nil {
			t.Fatal(err)
		}
	}
	items, err := repo.List(ctx, user, true, 10, 0)
	if err != nil || len(items) != 0 {
		t.Fatalf("after delete: %+v %v", items, err)
	}
}
