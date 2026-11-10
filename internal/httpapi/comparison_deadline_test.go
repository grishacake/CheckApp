package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"app-grabber-backend/internal/app"
	"app-grabber-backend/internal/auth"
	"app-grabber-backend/internal/comparison"
	"app-grabber-backend/internal/httpapi"
	"app-grabber-backend/internal/model"
)

func waitForComparison(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type delayedComparisonUsers struct{}

func (delayedComparisonUsers) GetByID(ctx context.Context, id string) (model.User, error) {
	return model.User{ID: id}, waitForComparison(ctx, 4*time.Second)
}

type delayedComparisonCache struct{}

func (delayedComparisonCache) Lookup(ctx context.Context, _, _ string) (model.App, time.Time, error) {
	if err := waitForComparison(ctx, 3*time.Second); err != nil {
		return model.App{}, time.Time{}, err
	}
	return model.App{}, time.Time{}, app.ErrNotFound
}
func (delayedComparisonCache) Save(context.Context, string, []model.App) error { return nil }

type comparisonDeadlineRecords struct{ httpapi.ComparisonStore }

func (comparisonDeadlineRecords) Record(context.Context, string, comparison.Pair) error { return nil }

func TestAuthenticatedComparisonSharesRequestDeadline(t *testing.T) {
	tokens, err := auth.NewTokenManager(strings.Repeat("x", 32), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	provider := providerFunc(func(ctx context.Context, id string) (model.App, error) {
		err := waitForComparison(ctx, 8500*time.Millisecond)
		return model.App{PackageName: id, Name: "card"}, err
	})
	h := httpapi.ComparisonHandler{
		Service: &comparison.Service{GooglePlay: provider, RuStore: provider, Cache: delayedComparisonCache{}, TTL: time.Hour, StoreTimeout: 9 * time.Second},
		Store:   comparisonDeadlineRecords{}, History: httpapi.NewHistoryHandler(nil, delayedComparisonUsers{}, tokens, 5*time.Second),
	}
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	server := httptest.NewUnstartedServer(mux)
	server.Config.WriteTimeout = 15 * time.Second
	server.Start()
	defer server.Close()
	token, err := tokens.Issue("test-user")
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest("GET", server.URL+"/api/compare?google_play=com.example&rustore=ru.example", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(&http.Cookie{Name: httpapi.AuthCookieName, Value: token})
	client := &http.Client{Timeout: 20 * time.Second}
	start := time.Now()
	resp, err := client.Do(req)
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("expected JSON timeout response, got %v", err)
	}
	if resp.StatusCode != http.StatusGatewayTimeout {
		t.Fatalf("status=%d, want 504", resp.StatusCode)
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Error != "Превышено время ожидания" {
		t.Fatalf("unexpected body: %+v", body)
	}
	if elapsed := time.Since(start); elapsed >= 14*time.Second {
		t.Fatalf("request took %s; expected deadline before server's write timeout", elapsed)
	}
}

func TestComparisonContextExpiresDuringAuthentication(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(map[bool]string{false: "deadline", true: "client canceled"}[canceled], func(t *testing.T) {
			tokens := newHandlerTokens(t)
			token, err := tokens.Issue("test-user")
			if err != nil {
				t.Fatal(err)
			}
			h := httpapi.ComparisonHandler{History: httpapi.NewHistoryHandler(nil, delayedComparisonUsers{}, tokens, 5*time.Second)}
			mux := http.NewServeMux()
			h.RegisterRoutes(mux)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			if canceled {
				cancel()
			}
			req := httptest.NewRequest("GET", "/api/compare?google_play=com.example&rustore=ru.example", nil).WithContext(ctx)
			req.AddCookie(&http.Cookie{Name: httpapi.AuthCookieName, Value: token})
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if canceled {
				if w.Body.Len() != 0 {
					t.Fatalf("response written after client cancellation: %s", w.Body.String())
				}
				return
			}
			assertError(t, w, http.StatusGatewayTimeout, "Превышено время ожидания")
		})
	}
}
