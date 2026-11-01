package auth_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"app-grabber-backend/internal/auth"
)

func TestMemoryRepositoryConcurrentCreate(t *testing.T) {
	repo := auth.NewMemoryUserRepository()
	var wg sync.WaitGroup
	results := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := repo.Create(context.Background(), "a@example.com", "Артём", "hash")
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	created := 0
	for err := range results {
		if err == nil {
			created++
		} else if !errors.Is(err, auth.ErrEmailAlreadyExists) {
			t.Fatal(err)
		}
	}
	if created != 1 {
		t.Fatalf("created %d users for one email", created)
	}
	user, err := repo.GetByEmail(context.Background(), "a@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if user.ID == "" || user.Nickname != "Артём" || user.PasswordHash != "hash" || user.CreatedAt.IsZero() {
		t.Fatal("incomplete user")
	}
	byID, err := repo.GetByID(context.Background(), user.ID)
	if err != nil || byID != user {
		t.Fatalf("lookup: %v", err)
	}
	user.Nickname = "changed"
	unchanged, _ := repo.GetByID(context.Background(), user.ID)
	if unchanged.Nickname != "Артём" {
		t.Fatal("returned user aliases stored data")
	}
	if _, err := repo.GetByID(context.Background(), "missing"); !errors.Is(err, auth.ErrUserNotFound) {
		t.Fatalf("missing ID: %v", err)
	}
	if _, err := repo.GetByEmail(context.Background(), "missing@example.com"); !errors.Is(err, auth.ErrUserNotFound) {
		t.Fatalf("missing email: %v", err)
	}
	if _, err := auth.NewMemoryUserRepository().GetByID(context.Background(), user.ID); !errors.Is(err, auth.ErrUserNotFound) {
		t.Fatal("repositories share data")
	}
}

func TestMemoryRepositoryCanceledContext(t *testing.T) {
	repo := auth.NewMemoryUserRepository()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := repo.Create(ctx, "a@example.com", "Артём", "hash"); !errors.Is(err, context.Canceled) {
		t.Fatalf("create: %v", err)
	}
	if _, err := repo.GetByID(ctx, "missing"); !errors.Is(err, context.Canceled) {
		t.Fatalf("get ID: %v", err)
	}
	if _, err := repo.GetByEmail(ctx, "a@example.com"); !errors.Is(err, context.Canceled) {
		t.Fatalf("get email: %v", err)
	}
	if _, err := repo.GetByEmail(context.Background(), "a@example.com"); !errors.Is(err, auth.ErrUserNotFound) {
		t.Fatal("canceled create saved user")
	}
}
