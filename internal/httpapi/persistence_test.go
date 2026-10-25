package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"app-grabber-backend/internal/app"
	"app-grabber-backend/internal/model"
)

func TestPersistenceFailureIsNotGooglePlayFailure(t *testing.T) {
	err := fmt.Errorf("%w: internal database details: %w", app.ErrPersistence, context.DeadlineExceeded)
	router := newRouter(providerFunc(func(context.Context, string) (model.App, error) {
		return model.App{}, err
	}), time.Second)
	card := httptest.NewRecorder()
	router.ServeHTTP(card, httptest.NewRequest("GET", "/api/apps/com.example.app", nil))
	search := doSearch(t, func(context.Context, string, int) ([]model.App, error) {
		return nil, err
	}, "q=example")
	for _, response := range []*httptest.ResponseRecorder{card, search} {
		if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), "сохранить") {
			t.Fatalf("unexpected storage failure response: %d %s", response.Code, response.Body.String())
		}
		if strings.Contains(response.Body.String(), "internal database details") || strings.Contains(response.Body.String(), "Google Play") {
			t.Fatal("storage error leaked details or blamed Google Play")
		}
	}
}
