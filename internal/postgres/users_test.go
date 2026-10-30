package postgres

import (
	"context"
	"errors"
	"testing"

	"app-grabber-backend/internal/auth"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type errorUserDB struct{ err error }

func (db errorUserDB) QueryRow(context.Context, string, ...any) pgx.Row { return errorUserRow{db.err} }

type errorUserRow struct{ err error }

func (row errorUserRow) Scan(...any) error { return row.err }

func TestUserRepositoryErrors(t *testing.T) {
	otherConstraint := &pgconn.PgError{Code: "23505", ConstraintName: "users_pkey"}
	for _, tc := range []struct {
		name string
		err  error
		want error
	}{
		{"duplicate email", &pgconn.PgError{Code: "23505", ConstraintName: "users_email_key"}, auth.ErrEmailAlreadyExists},
		{"other constraint", otherConstraint, otherConstraint},
		{"canceled", context.Canceled, context.Canceled},
		{"timeout", context.DeadlineExceeded, context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := NewUserRepository(errorUserDB{tc.err})
			user, err := repo.Create(context.Background(), "test@example.com", "Test", "hash")
			if !errors.Is(err, tc.want) || user.ID != "" {
				t.Fatalf("unexpected creation result: %v", err)
			}
			if tc.name == "other constraint" && errors.Is(err, auth.ErrEmailAlreadyExists) {
				t.Fatal("an unrelated constraint was reported as a duplicate email")
			}
		})
	}

	for _, dbErr := range []error{pgx.ErrNoRows, context.Canceled, context.DeadlineExceeded} {
		want := dbErr
		if errors.Is(dbErr, pgx.ErrNoRows) {
			want = auth.ErrUserNotFound
		}
		repo := NewUserRepository(errorUserDB{dbErr})
		_, err := repo.GetByEmail(context.Background(), "missing@example.com")
		if !errors.Is(err, want) {
			t.Fatalf("GetByEmail: %v", err)
		}
		_, err = repo.GetByID(context.Background(), "f42a2020-4503-4606-a4f8-41fe05090717")
		if !errors.Is(err, want) {
			t.Fatalf("GetByID: %v", err)
		}
	}
}

func TestUserRepositoryInvalidID(t *testing.T) {
	// No DB is needed: malformed IDs should not become PostgreSQL syntax errors.
	repo := NewUserRepository(nil)
	_, err := repo.GetByID(context.Background(), "not-a-uuid")
	if !errors.Is(err, auth.ErrUserNotFound) {
		t.Fatalf("invalid ID: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = repo.GetByID(ctx, "not-a-uuid")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled request: %v", err)
	}
}
