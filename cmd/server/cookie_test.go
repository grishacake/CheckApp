package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"app-grabber-backend/internal/auth"
	"app-grabber-backend/internal/httpapi"
)

func TestAuthCookieFlowWithOriginProtection(t *testing.T) {
	const origin = "http://localhost:3000"
	t.Setenv("CORS_ORIGINS", origin)
	for _, secure := range []bool{false, true} {
		name := "HTTP"
		if secure {
			name = "HTTPS"
		}
		t.Run(name, func(t *testing.T) {
			tokens, err := auth.NewTokenManager(strings.Repeat("s", 32), time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			mux := http.NewServeMux()
			httpapi.NewAuthHandler(auth.NewService(auth.NewMemoryUserRepository()), tokens, time.Second).
				WithCookieOptions(secure, http.SameSiteLaxMode).RegisterRoutes(mux)
			server := httptest.NewUnstartedServer(withCORS(mux))
			if secure {
				server.StartTLS()
			} else {
				server.Start()
			}
			defer server.Close()
			client := server.Client()
			client.Jar, err = cookiejar.New(nil)
			if err != nil {
				t.Fatal(err)
			}
			request := func(method, path, body, requestOrigin string, want int) (*http.Response, []byte) {
				t.Helper()
				req, err := http.NewRequest(method, server.URL+path, strings.NewReader(body))
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("Origin", requestOrigin)
				if body != "" {
					req.Header.Set("Content-Type", "application/json")
				}
				res, err := client.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				defer res.Body.Close()
				data, err := io.ReadAll(res.Body)
				if err != nil {
					t.Fatal(err)
				}
				if res.StatusCode != want {
					t.Fatalf("%s %s: status %d, want %d", method, path, res.StatusCode, want)
				}
				if requestOrigin == origin && (res.Header.Get("Access-Control-Allow-Origin") != origin || res.Header.Get("Access-Control-Allow-Credentials") != "true") {
					t.Fatal("credentialed CORS response missing")
				}
				return res, data
			}
			request("POST", "/api/auth/register", `{"email":"cookie@example.com","nickname":"Артём","password":"long-example-password"}`, origin, 201)
			res, body := request("POST", "/api/auth/login", `{"email":"cookie@example.com","password":"long-example-password"}`, origin, 200)
			var login map[string]json.RawMessage
			if err := json.Unmarshal(body, &login); err != nil || len(login) != 1 || login["user"] == nil {
				t.Fatal("login must return only user, without JWT")
			}
			cookies := res.Cookies()
			if len(cookies) != 1 {
				t.Fatal("login cookie missing")
			}
			cookie := cookies[0]
			if cookie.Name != httpapi.AuthCookieName || !cookie.HttpOnly || cookie.Secure != secure || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" || cookie.Domain != "" || cookie.MaxAge != 3600 || !cookie.Expires.After(time.Now()) {
				t.Fatal("incorrect login cookie attributes")
			}
			request("GET", "/api/auth/me", "", origin, 200)
			res, _ = request("POST", "/api/auth/login", `{"email":"cookie@example.com","password":"wrong-password"}`, origin, 401)
			if len(res.Cookies()) != 0 {
				t.Fatal("failed login modified cookie")
			}
			res, _ = request("POST", "/api/auth/logout", "", "https://untrusted.example", 403)
			if len(res.Cookies()) != 0 {
				t.Fatal("untrusted origin modified cookie")
			}
			request("GET", "/api/auth/me", "", origin, 200)
			res, body = request("POST", "/api/auth/logout", "", origin, 204)
			if len(body) != 0 || res.Header.Get("Cache-Control") != "no-store" || len(res.Cookies()) != 1 {
				t.Fatal("incorrect logout response")
			}
			cleared := res.Cookies()[0]
			if cleared.Name != cookie.Name || cleared.Path != cookie.Path || cleared.Domain != cookie.Domain || cleared.HttpOnly != cookie.HttpOnly || cleared.Secure != cookie.Secure || cleared.SameSite != cookie.SameSite || cleared.Value != "" || cleared.MaxAge != -1 || !cleared.Expires.Before(time.Now()) {
				t.Fatal("logout did not expire matching cookie")
			}
			request("GET", "/api/auth/me", "", origin, 401)
			request("POST", "/api/auth/logout", "", origin, 204)
		})
	}
}

func TestCookieOriginProtection(t *testing.T) {
	t.Setenv("CORS_ORIGINS", "https://frontend.example")
	for _, tc := range []struct {
		name, origin, referer string
		want                  int
	}{
		{"allowed", "https://frontend.example", "", 204},
		{"same origin", "http://example.com", "", 204},
		{"evil", "https://evil.example", "", 403},
		{"suffix", "https://frontend.example.evil.test", "", 403},
		{"null", "null", "", 403},
		{"missing", "", "", 403},
		{"referer", "", "https://frontend.example/page", 204},
		{"bad referer", "", "https://evil.example/page", 403},
		{"origin overrides referer", "null", "https://frontend.example/page", 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			handler := withCORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(204) }))
			req := httptest.NewRequest("POST", "http://example.com/api/auth/logout", nil)
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			if tc.referer != "" {
				req.Header.Set("Referer", tc.referer)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != tc.want || called != (tc.want == 204) {
				t.Fatalf("status=%d called=%v", w.Code, called)
			}
			if tc.want == 403 && w.Header().Get("Access-Control-Allow-Origin") != "" {
				t.Fatal("untrusted origin allowed")
			}
		})
	}
}
