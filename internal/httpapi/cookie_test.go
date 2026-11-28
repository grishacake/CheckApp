package httpapi_test

import (
	"app-grabber-backend/internal/auth"
	"app-grabber-backend/internal/httpapi"
	"app-grabber-backend/internal/model"
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCookieIdentifiesFavoritesAndHistory(t *testing.T) {
	tokens := newHandlerTokens(t)
	token, _ := tokens.Issue("user-a")
	favorites := &favoriteRepoStub{}
	history := &historyRepoStub{}
	users := favoriteUsersStub{}
	mux := http.NewServeMux()
	httpapi.NewFavoriteHandler(favorites, users, tokens, time.Second).RegisterRoutes(mux)
	h := httpapi.NewHistoryHandler(history, users, tokens, time.Second)
	h.RegisterRoutes(mux)
	card := httpapi.NewHandler(providerFunc(func(context.Context, string) (model.App, error) { return exampleApp(), nil }), time.Second).WithHistory(history, "google_play")
	mux.Handle("GET /api/apps/{id}", h.WrapCard(http.HandlerFunc(card.GetApp)))
	for _, path := range []string{"/api/favorites", "/api/history", "/api/apps/com.uchi.app"} {
		r := httptest.NewRequest("GET", path, nil)
		r.AddCookie(&http.Cookie{Name: httpapi.AuthCookieName, Value: token})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("%s: %d", path, w.Code)
		}
	}
	if favorites.user != "user-a" || history.user != "user-a" || history.records != 1 {
		t.Fatal("cookie user not propagated")
	}
}

func TestCookieLoginLogout(t *testing.T) {
	mux := http.NewServeMux()
	tokens := newHandlerTokens(t)
	httpapi.NewAuthHandler(auth.NewService(auth.NewMemoryUserRepository()), tokens, 0).WithCookieOptions(false, http.SameSiteLaxMode).RegisterRoutes(mux)
	server := httptest.NewServer(mux)
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	client := server.Client()
	client.Jar = jar
	post := func(path, body string) *http.Response {
		t.Helper()
		res, err := client.Post(server.URL+path, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res
	}
	res := post("/api/auth/register", `{"email":"a@example.com","nickname":"Артём","password":"long-example-password"}`)
	if res.StatusCode != 201 {
		t.Fatal(res.StatusCode)
	}
	res = post("/api/auth/login", `{"email":"a@example.com","password":"long-example-password"}`)
	if res.StatusCode != 200 || len(res.Cookies()) != 1 {
		t.Fatal("login failed")
	}
	cookie := res.Cookies()[0]
	checkMe := func(want int) {
		t.Helper()
		res, err := client.Get(server.URL + "/api/auth/me")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != want {
			t.Fatalf("me=%d want=%d", res.StatusCode, want)
		}
	}
	checkMe(200)
	res = post("/api/auth/logout", "")
	if res.StatusCode != 204 || len(res.Cookies()) != 1 {
		t.Fatal("logout failed")
	}
	cleared := res.Cookies()[0]
	if cleared.Name != cookie.Name || cleared.Path != cookie.Path || cleared.Domain != cookie.Domain || cleared.MaxAge != -1 || !cleared.HttpOnly {
		t.Fatal("cookie not cleared")
	}
	checkMe(401)
	if post("/api/auth/logout", "").StatusCode != 204 {
		t.Fatal("logout is not idempotent")
	}
}

func TestCookieAuthRejectsAmbiguousCredentials(t *testing.T) {
	tokens := newHandlerTokens(t)
	token, _ := tokens.Issue("user-123")
	for _, mode := range []string{"duplicate", "bearer", "empty", "broken"} {
		t.Run(mode, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/private", nil)
			value := token
			if mode == "empty" {
				value = ""
			}
			if mode == "broken" || mode == "bearer" {
				value = "broken"
			}
			r.AddCookie(&http.Cookie{Name: httpapi.AuthCookieName, Value: value})
			if mode == "duplicate" {
				r.AddCookie(&http.Cookie{Name: httpapi.AuthCookieName, Value: token})
			}
			if mode == "bearer" {
				r.Header.Set("Authorization", "Bearer "+token)
			}
			w := httptest.NewRecorder()
			httpapi.WithAuth(tokens, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("called protected handler") })).ServeHTTP(w, r)
			if w.Code != 401 {
				t.Fatal(w.Code)
			}
		})
	}
}
