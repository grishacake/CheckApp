package postgres

import (
	"errors"
	"testing"

	"app-grabber-backend/internal/auth"
)

func TestHistoryUserID(t *testing.T) {
	if got, err := historyUserID(""); err != nil || got.Valid {
		t.Fatalf("anonymous UUID: %#v, %v", got, err)
	}
	if _, err := historyUserID("not-a-uuid"); !errors.Is(err, auth.ErrUserNotFound) {
		t.Fatalf("invalid UUID error = %v", err)
	}
	if got, err := historyUserID("72e8cf08-9c94-453e-a14b-88e071ab2869"); err != nil || !got.Valid {
		t.Fatalf("valid UUID: %#v, %v", got, err)
	}
}
