package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"app-grabber-backend/internal/auth"
	"app-grabber-backend/internal/httpapi"
	"app-grabber-backend/internal/model"
)

// TEST_DATABASE_URL must point to a dedicated database with migrations applied.
func TestUsersIntegration(t *testing.T) {
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
	defer func() { pool.Close() }()

	email := fmt.Sprintf("checkapp-test-%d@example.com", time.Now().UnixNano())
	secondEmail := "second-" + email
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cleanupPool, err := Open(cleanupCtx, dsn)
		if err != nil {
			t.Errorf("open cleanup connection: %v", err)
			return
		}
		defer cleanupPool.Close()
		if _, err := cleanupPool.Exec(cleanupCtx, "DELETE FROM public.users WHERE email IN ($1, $2)", email, secondEmail); err != nil {
			t.Errorf("clean up test users: %v", err)
		}
	})

	tokens, err := auth.NewTokenManager(strings.Repeat("s", 32), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	newHandler := func() *http.ServeMux {
		mux := http.NewServeMux()
		service := auth.NewService(NewUserRepository(pool))
		httpapi.NewAuthHandler(service, tokens, 5*time.Second).RegisterRoutes(mux)
		return mux
	}
	request := func(mux *http.ServeMux, method, path string, body any, token string, wantStatus int) *httptest.ResponseRecorder {
		t.Helper()
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(method, path, strings.NewReader(string(payload)))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != wantStatus {
			t.Fatalf("%s %s: status %d, want %d; body %s", method, path, w.Code, wantStatus, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "password_hash") {
			t.Fatal("HTTP response exposed the password hash")
		}
		return w
	}

	const password = "test-password-123456789"
	registration := map[string]string{"email": email, "nickname": "Same nickname", "password": password}
	mux := newHandler()
	w := request(mux, "POST", "/api/auth/register", registration, "", http.StatusCreated)
	var registered model.User
	if err := json.Unmarshal(w.Body.Bytes(), &registered); err != nil || registered.ID == "" || registered.CreatedAt.IsZero() {
		t.Fatal("registration did not return a persisted user")
	}
	stored, err := NewUserRepository(pool).GetByEmail(ctx, email)
	if err != nil || stored.ID != registered.ID {
		t.Fatalf("read persisted user: %v", err)
	}
	if ok, err := auth.CheckPassword(password, stored.PasswordHash); err != nil || !ok {
		t.Fatal("database did not contain the expected password hash")
	}
	request(mux, "POST", "/api/auth/register", registration, "", http.StatusConflict)
	request(mux, "POST", "/api/auth/register", map[string]string{
		"email": secondEmail, "nickname": "Same nickname", "password": password,
	}, "", http.StatusCreated)

	credentials := map[string]string{"email": email, "password": password}
	w = request(mux, "POST", "/api/auth/login", credentials, "", http.StatusOK)
	var login struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &login); err != nil || login.AccessToken == "" {
		t.Fatal("login did not return a token")
	}

	// Recreate connections and handlers: the new service has no in-memory users.
	pool.Close()
	newPool, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	pool = newPool
	mux = newHandler()
	w = request(mux, "GET", "/api/auth/me", nil, login.AccessToken, http.StatusOK)
	var profile model.User
	if err := json.Unmarshal(w.Body.Bytes(), &profile); err != nil || profile != registered {
		t.Fatal("profile did not survive recreation of the pool and service")
	}
	request(mux, "POST", "/api/auth/login", credentials, "", http.StatusOK)
	request(mux, "POST", "/api/auth/login", map[string]string{
		"email": email, "password": "wrong-password-123456789",
	}, "", http.StatusUnauthorized)
	request(mux, "POST", "/api/auth/login", map[string]string{
		"email": "missing-" + email, "password": password,
	}, "", http.StatusUnauthorized)
	if _, err := pool.Exec(ctx, "DELETE FROM public.users WHERE id = $1", registered.ID); err != nil {
		t.Fatal(err)
	}
	request(mux, "GET", "/api/auth/me", nil, login.AccessToken, http.StatusUnauthorized)
}
