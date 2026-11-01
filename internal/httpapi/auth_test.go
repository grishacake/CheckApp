package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"app-grabber-backend/internal/auth"
	"app-grabber-backend/internal/httpapi"
)

func TestWithAuth(t *testing.T) {
	tokens, err := auth.NewTokenManager(strings.Repeat("s", 32), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := tokens.Issue("user-123")
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name    string
		headers []string
		query   string
		status  int
	}{
		{"valid", []string{"Bearer " + raw}, "", http.StatusNoContent},
		{"case insensitive scheme", []string{"bearer " + raw}, "", http.StatusNoContent},
		{"missing", nil, "", http.StatusUnauthorized},
		{"empty", []string{""}, "", http.StatusUnauthorized},
		{"missing token", []string{"Bearer"}, "", http.StatusUnauthorized},
		{"wrong scheme", []string{"Basic " + raw}, "", http.StatusUnauthorized},
		{"malformed token", []string{"Bearer broken"}, "", http.StatusUnauthorized},
		{"extra token", []string{"Bearer " + raw + " extra"}, "", http.StatusUnauthorized},
		{"duplicate headers", []string{"Bearer " + raw, "Bearer " + raw}, "", http.StatusUnauthorized},
		{"query only", nil, "?access_token=" + raw, http.StatusUnauthorized},
	} {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			type requestKey struct{}
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				if id, ok := httpapi.UserIDFromContext(r.Context()); !ok || id != "user-123" {
					t.Errorf("id=%q ok=%v", id, ok)
				}
				if r.Context().Value(requestKey{}) != "preserved" {
					t.Error("original context lost")
				}
				w.WriteHeader(http.StatusNoContent)
			})
			req := httptest.NewRequest(http.MethodGet, "/private"+tt.query, nil)
			req = req.WithContext(context.WithValue(req.Context(), requestKey{}, "preserved"))
			for _, header := range tt.headers {
				req.Header.Add("Authorization", header)
			}
			response := httptest.NewRecorder()
			httpapi.WithAuth(tokens, next).ServeHTTP(response, req)
			if response.Code != tt.status || called != (tt.status == http.StatusNoContent) {
				t.Fatalf("status=%d nextCalled=%v", response.Code, called)
			}
			if _, ok := httpapi.UserIDFromContext(req.Context()); ok {
				t.Error("original request context mutated")
			}
			if tt.status == http.StatusUnauthorized {
				if response.Header().Get("WWW-Authenticate") != "Bearer" {
					t.Error("missing Bearer challenge")
				}
				if response.Header().Get("Content-Type") != "application/json; charset=utf-8" {
					t.Error("expected JSON")
				}
				var body struct {
					Error string `json:"error"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Error != "Требуется авторизация" {
					t.Fatalf("unexpected error response: %s", response.Body.String())
				}
			}
		})
	}
}
