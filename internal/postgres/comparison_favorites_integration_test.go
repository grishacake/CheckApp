package postgres

import (
	"app-grabber-backend/internal/app"
	"app-grabber-backend/internal/comparison"
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestComparisonFavoriteSetsIntegration(t *testing.T) {
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
		err := pool.QueryRow(ctx, `INSERT INTO users(email,nickname,password_hash) VALUES($1,'favorites','test') RETURNING id`, fmt.Sprintf("favorite-sets-%d-%d@example.org", time.Now().UnixNano(), i)).Scan(&users[i])
		if err != nil {
			t.Fatal(err)
		}
		defer pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, users[i])
	}
	repo := NewComparisonRepository(pool)
	items := []comparison.Item{{Store: "rustore", PackageName: "com.one"}, {Store: "google_play", PackageName: "com.two"}, {Store: "rustore", PackageName: "com.three"}}
	saved, err := repo.SaveFavorite(ctx, users[0], items)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := repo.SaveFavorite(ctx, users[0], []comparison.Item{items[2], items[0], items[1], items[0]})
	if err != nil || repeated.ID != saved.ID || !repeated.SavedAt.Equal(saved.SavedAt) {
		t.Fatal(repeated, err)
	}
	if _, err := repo.SaveFavorite(ctx, users[0], items[:1]); err != nil {
		t.Fatal(err)
	}
	list, err := repo.ListFavorites(ctx, users[0], 10, 0)
	if err != nil || len(list) != 2 {
		t.Fatal(list, err)
	}
	if _, err := repo.GetFavorite(ctx, users[1], saved.ID); !errors.Is(err, app.ErrNotFound) {
		t.Fatal(err)
	}
	if err := repo.DeleteFavorite(ctx, users[1], saved.ID); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.GetFavorite(ctx, users[0], saved.ID); err != nil || len(got.Items) != 3 {
		t.Fatal(got, err)
	}
	for i := 0; i < 2; i++ {
		if err := repo.DeleteFavorite(ctx, users[0], saved.ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repo.GetFavorite(ctx, users[0], saved.ID); !errors.Is(err, app.ErrNotFound) {
		t.Fatal(err)
	}
}
