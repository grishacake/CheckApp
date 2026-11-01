package httpapi_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"app-grabber-backend/internal/auth"
	"app-grabber-backend/internal/favorites"
	"app-grabber-backend/internal/httpapi"
	"app-grabber-backend/internal/model"
)

type favoriteRepoStub struct {
	items                            []model.Favorite
	err                              error
	user, store, packageName         string
	limit, offset                    int
	addCalls, removeCalls, listCalls int
}

func (s *favoriteRepoStub) Add(_ context.Context, user, store, packageName string) error {
	s.user, s.store, s.packageName = user, store, packageName
	s.addCalls++
	return s.err
}
func (s *favoriteRepoStub) Remove(_ context.Context, user, store, packageName string) error {
	s.user, s.store, s.packageName = user, store, packageName
	s.removeCalls++
	return s.err
}
func (s *favoriteRepoStub) List(_ context.Context, user string, limit, offset int) ([]model.Favorite, error) {
	s.user, s.limit, s.offset = user, limit, offset
	s.listCalls++
	return s.items, s.err
}

type favoriteUsersStub struct{ err error }

func (s favoriteUsersStub) GetByID(context.Context, string) (model.User, error) {
	return model.User{}, s.err
}

type favoriteUsersFunc func(context.Context, string) (model.User, error)

func (f favoriteUsersFunc) GetByID(ctx context.Context, id string) (model.User, error) {
	return f(ctx, id)
}

func TestFavoriteHandlerListAndMutation(t *testing.T) {
	tokens := newHandlerTokens(t)
	token, err := tokens.Issue("user-1")
	if err != nil {
		t.Fatal(err)
	}
	repo := &favoriteRepoStub{}
	mux := http.NewServeMux()
	httpapi.NewFavoriteHandler(repo, favoriteUsersStub{}, tokens, 0).RegisterRoutes(mux)
	request := func(method, path, bearer string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, nil)
		if bearer != "" {
			r.Header.Set("Authorization", "Bearer "+bearer)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	if w := request("GET", "/api/favorites", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated: %d", w.Code)
	}
	w := request("GET", "/api/favorites?limit=101", token)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid pagination: %d", w.Code)
	}
	w = request("GET", "/api/favorites", token)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"favorites":[]`) || !strings.Contains(w.Body.String(), `"limit":20`) || w.Header().Get("Cache-Control") != "no-store" || repo.user != "user-1" {
		t.Fatalf("empty list: %d %s", w.Code, w.Body.String())
	}
	w = request("GET", "/api/favorites?limit=7&offset=3", token)
	if w.Code != http.StatusOK || repo.limit != 7 || repo.offset != 3 {
		t.Fatalf("pagination: status=%d limit=%d offset=%d", w.Code, repo.limit, repo.offset)
	}
	w = request("PUT", "/api/favorites/google_play/com.uchi.app", token)
	if w.Code != http.StatusNoContent || repo.user != "user-1" || repo.store != "google_play" || repo.packageName != "com.uchi.app" || repo.addCalls != 1 {
		t.Fatalf("add: %d", w.Code)
	}
	w = request("DELETE", "/api/favorites/google_play/com.uchi.app", token)
	if w.Code != http.StatusNoContent || repo.removeCalls != 1 || repo.user != "user-1" {
		t.Fatalf("delete: %d", w.Code)
	}
	for _, path := range []string{"/api/favorites/Bad/com.uchi.app", "/api/favorites/google_play/Bad"} {
		w = request("PUT", path, token)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("invalid path %s: %d", path, w.Code)
		}
	}
	repo.err = favorites.ErrCardNotFound
	w = request("PUT", "/api/favorites/google_play/com.uchi.app", token)
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing card: %d", w.Code)
	}
	repo.err = errors.New("secret database detail")
	w = request("GET", "/api/favorites", token)
	if w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), "secret database detail") {
		t.Fatalf("database error leaked: %d %s", w.Code, w.Body.String())
	}
}

func TestFavoriteHandlerRejectsDeletedUser(t *testing.T) {
	tokens := newHandlerTokens(t)
	token, err := tokens.Issue("user-1")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	httpapi.NewFavoriteHandler(&favoriteRepoStub{}, favoriteUsersStub{err: auth.ErrUserNotFound}, tokens, 0).RegisterRoutes(mux)
	r := httptest.NewRequest("GET", "/api/favorites", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("deleted user: %d", w.Code)
	}
}

func TestFavoriteHandlerUserScopeTimeoutAndCancellation(t *testing.T) {
	tokens := newHandlerTokens(t)
	tokenA, err := tokens.Issue("user-a")
	if err != nil {
		t.Fatal(err)
	}
	tokenB, err := tokens.Issue("user-b")
	if err != nil {
		t.Fatal(err)
	}
	repo := &favoriteRepoStub{}
	mux := http.NewServeMux()
	httpapi.NewFavoriteHandler(repo, favoriteUsersStub{}, tokens, 0).RegisterRoutes(mux)
	for _, tc := range []struct{ token, id string }{{tokenA, "user-a"}, {tokenB, "user-b"}} {
		r := httptest.NewRequest("GET", "/api/favorites", nil)
		r.Header.Set("Authorization", "Bearer "+tc.token)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != http.StatusOK || repo.user != tc.id {
			t.Fatalf("user scope: status=%d id=%q", w.Code, repo.user)
		}
	}
	timedUsers := favoriteUsersFunc(func(ctx context.Context, _ string) (model.User, error) { <-ctx.Done(); return model.User{}, ctx.Err() })
	timeoutMux := http.NewServeMux()
	httpapi.NewFavoriteHandler(&favoriteRepoStub{}, timedUsers, tokens, time.Millisecond).RegisterRoutes(timeoutMux)
	r := httptest.NewRequest("GET", "/api/favorites", nil)
	r.Header.Set("Authorization", "Bearer "+tokenA)
	w := httptest.NewRecorder()
	timeoutMux.ServeHTTP(w, r)
	if w.Code != http.StatusGatewayTimeout {
		t.Fatalf("timeout status=%d body=%s", w.Code, w.Body.String())
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	r = httptest.NewRequest("GET", "/api/favorites", nil).WithContext(canceled)
	r.Header.Set("Authorization", "Bearer "+tokenA)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK || w.Body.Len() != 0 {
		t.Fatalf("canceled request wrote a response: %d %q", w.Code, w.Body.String())
	}
}
