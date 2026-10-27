package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAuthCORS(t *testing.T) {
	called := false
	handler := withCORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.Header.Get("Authorization") != "Bearer example" {
			t.Error("Authorization lost")
		}
		w.WriteHeader(http.StatusOK)
	}))
	r := httptest.NewRequest("OPTIONS", "/api/auth/login", nil)
	r.Header.Set("Origin", "http://localhost:3000")
	r.Header.Set("Access-Control-Request-Method", "POST")
	r.Header.Set("Access-Control-Request-Headers", "content-type, authorization")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent || called {
		t.Fatal("preflight reached handler")
	}
	if !strings.Contains(w.Header().Get("Access-Control-Allow-Methods"), "POST") || !strings.Contains(w.Header().Get("Access-Control-Allow-Headers"), "Authorization") {
		t.Fatal("auth requests blocked by CORS")
	}
	r = httptest.NewRequest("GET", "/api/auth/me", nil)
	r.Header.Set("Authorization", "Bearer example")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if !called || w.Code != http.StatusOK || w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatal("actual request failed")
	}
}

func TestFavoritesCORSPreflight(t *testing.T) {
	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			called := false
			handler := withCORS(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				called = true
			}))
			r := httptest.NewRequest(http.MethodOptions, "/api/favorites/google_play/com.uchi.app", nil)
			r.Header.Set("Origin", "http://localhost:3000")
			r.Header.Set("Access-Control-Request-Method", method)
			r.Header.Set("Access-Control-Request-Headers", "authorization")
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, r)

			if w.Code != http.StatusNoContent || called {
				t.Fatalf("preflight status = %d, downstream called = %t", w.Code, called)
			}
			if w.Header().Get("Access-Control-Allow-Origin") != "*" {
				t.Errorf("allow origin = %q, want *", w.Header().Get("Access-Control-Allow-Origin"))
			}
			if !headerContainsValue(w.Header().Get("Access-Control-Allow-Methods"), method) {
				t.Errorf("allow methods %q does not include %s", w.Header().Get("Access-Control-Allow-Methods"), method)
			}
			if !headerContainsValue(w.Header().Get("Access-Control-Allow-Headers"), "Authorization") {
				t.Errorf("allow headers %q does not include Authorization", w.Header().Get("Access-Control-Allow-Headers"))
			}
		})
	}
}

func headerContainsValue(header, want string) bool {
	for _, value := range strings.Split(header, ",") {
		if strings.TrimSpace(value) == want {
			return true
		}
	}
	return false
}
