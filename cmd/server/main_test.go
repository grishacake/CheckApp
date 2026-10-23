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
