package postgres

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"app-grabber-backend/internal/comparison"
)

func TestComparisonItemsIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to test against PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var users [2]string
	for i := range users {
		err := pool.QueryRow(ctx, `INSERT INTO users(email,nickname,password_hash) VALUES($1,'comparison','test') RETURNING id`, fmt.Sprintf("items-%d-%d@example.org", time.Now().UnixNano(), i)).Scan(&users[i])
		if err != nil {
			t.Fatal(err)
		}
		defer pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, users[i])
	}
	repo := NewComparisonRepository(pool)
	items := []comparison.Item{{Store: "rustore", PackageName: "com.same"}, {Store: "google_play", PackageName: "com.same"}, {Store: "rustore", PackageName: "com.other"}}
	for _, user := range users {
		if err := repo.AddItems(ctx, user, items); err != nil {
			t.Fatal(err)
		}
	}
	before, err := repo.ListItems(ctx, users[0])
	if err != nil || len(before) != 3 {
		t.Fatal(before, err)
	}
	for i := range items {
		if before[i].Item != items[i] {
			t.Fatal("insertion order lost", before)
		}
	}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(reverse bool) {
			defer wg.Done()
			batch := items
			if reverse {
				batch = []comparison.Item{items[2], items[1], items[0]}
			}
			errs <- repo.AddItems(ctx, users[0], batch)
		}(i%2 == 0)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	after, err := repo.ListItems(ctx, users[0])
	if err != nil || len(after) != 3 {
		t.Fatal(after, err)
	}
	for i := range before {
		if before[i] != after[i] {
			t.Fatal("duplicate insert changed order or timestamp")
		}
	}
	if err := repo.AddItems(ctx, users[0], []comparison.Item{{Store: "rustore", PackageName: "com.new"}, {Store: "bad", PackageName: "com.bad"}}); err == nil {
		t.Fatal("invalid batch accepted")
	}
	if current, _ := repo.ListItems(ctx, users[0]); len(current) != 3 {
		t.Fatal("batch partially persisted")
	}
	for i := 0; i < 2; i++ {
		if err := repo.RemoveItem(ctx, users[0], items[0]); err != nil {
			t.Fatal(err)
		}
	}
	remaining, err := repo.ListItems(ctx, users[0])
	if err != nil || len(remaining) != 2 || remaining[0].Store != "google_play" {
		t.Fatal(remaining, err)
	}
	for i := 0; i < 2; i++ {
		if err := repo.ClearItems(ctx, users[0]); err != nil {
			t.Fatal(err)
		}
	}
	remaining, err = repo.ListItems(ctx, users[0])
	if err != nil || remaining == nil || len(remaining) != 0 {
		t.Fatal(remaining, err)
	}
	other, err := repo.ListItems(ctx, users[1])
	if err != nil || len(other) != 3 {
		t.Fatal("other user's data changed", other, err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id=$1`, users[1]); err != nil {
		t.Fatal(err)
	}
	other, err = repo.ListItems(ctx, users[1])
	if err != nil || len(other) != 0 {
		t.Fatal("user deletion did not cascade", other, err)
	}
}
