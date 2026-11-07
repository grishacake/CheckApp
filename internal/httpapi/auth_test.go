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
		cookies []string
		header  string
		query   string
		status  int
	}{
		{"valid cookie", []string{raw}, "", "", http.StatusNoContent},
		{"missing cookie", nil, "", "", http.StatusUnauthorized},
		{"empty cookie", []string{""}, "", "", http.StatusUnauthorized},
		{"malformed token", []string{"broken"}, "", "", http.StatusUnauthorized},
		{"duplicate cookies", []string{raw, raw}, "", "", http.StatusUnauthorized},
		{"query only", nil, "", "?access_token=" + raw, http.StatusUnauthorized},
		{"bearer no longer authenticates", nil, "Bearer " + raw, "", http.StatusUnauthorized},
		{"header does not override cookie", []string{raw}, "Bearer broken", "", http.StatusNoContent},
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
			for _, cookie := range tt.cookies {
				req.AddCookie(&http.Cookie{Name: httpapi.AuthCookieName, Value: cookie})
			}
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
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
				if response.Header().Get("WWW-Authenticate") != "" {
					t.Error("unexpected authentication challenge")
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
