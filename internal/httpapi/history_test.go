package httpapi_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"app-grabber-backend/internal/app"
	"app-grabber-backend/internal/auth"
	"app-grabber-backend/internal/history"
	"app-grabber-backend/internal/httpapi"
	"app-grabber-backend/internal/model"
)

type historyRepoStub struct {
	views                    []model.View
	err                      error
	user, store, packageName string
	limit, offset            int
	records, lists           int
}

type historyRepoFunc struct {
	record func(context.Context, string, string, string) error
	list   func(context.Context, string, int, int) ([]model.View, error)
}

func (s historyRepoFunc) Record(ctx context.Context, user, store, packageName string) error {
	return s.record(ctx, user, store, packageName)
}
func (s historyRepoFunc) List(ctx context.Context, user string, limit, offset int) ([]model.View, error) {
	return s.list(ctx, user, limit, offset)
}

func (s *historyRepoStub) Record(_ context.Context, user, store, packageName string) error {
	s.user, s.store, s.packageName = user, store, packageName
	s.records++
	return s.err
}
func (s *historyRepoStub) List(_ context.Context, user string, limit, offset int) ([]model.View, error) {
	s.user, s.limit, s.offset = user, limit, offset
	s.lists++
	return s.views, s.err
}

func historyCardRouter(t *testing.T, repo *historyRepoStub, users HistoryUsersForTest, providerCalls *int) http.Handler {
	t.Helper()
	tokens := newHandlerTokens(t)
	card := httpapi.NewHandler(providerFunc(func(context.Context, string) (model.App, error) {
		*providerCalls++
		return exampleApp(), nil
	}), time.Second).WithHistory(repo, "google_play")
	historyHandler := httpapi.NewHistoryHandler(repo, users, tokens, time.Second)
	mux := http.NewServeMux()
	mux.Handle("GET /api/apps/{id}", historyHandler.WrapCard(http.HandlerFunc(card.GetApp)))
	historyHandler.RegisterRoutes(mux)
	return mux
}

type HistoryUsersForTest interface {
	GetByID(context.Context, string) (model.User, error)
}

func TestCardHistoryRecordsSuccessfulGETAndSkipsHeadAndProviderErrors(t *testing.T) {
	repo := &historyRepoStub{}
	calls := 0
	mux := historyCardRouter(t, repo, favoriteUsersStub{}, &calls)
	get := httptest.NewRecorder()
	mux.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/apps/com.uchi.app", nil))
	if get.Code != http.StatusOK || get.Header().Get("Cache-Control") != "no-store" || repo.records != 1 || repo.user != "" || repo.store != "google_play" {
		t.Fatalf("anonymous GET: status=%d records=%d user=%q headers=%v", get.Code, repo.records, repo.user, get.Header())
	}
	second := httptest.NewRecorder()
	mux.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/api/apps/com.uchi.app", nil))
	if second.Code != http.StatusOK || repo.records != 2 {
		t.Fatalf("repeat GET did not record: %d/%d", second.Code, repo.records)
	}
	head := httptest.NewRecorder()
	mux.ServeHTTP(head, httptest.NewRequest(http.MethodHead, "/api/apps/com.uchi.app", nil))
	if repo.records != 2 {
		t.Fatalf("HEAD recorded a view: %d", repo.records)
	}
	if calls != 3 {
		t.Fatalf("provider calls=%d", calls)
	}
}

func TestCardAuthRejectsBeforeProviderAndAssociatesVerifiedUser(t *testing.T) {
	repo := &historyRepoStub{}
	calls := 0
	users := favoriteUsersFunc(func(_ context.Context, id string) (model.User, error) {
		if id == "deleted-user" {
			return model.User{}, auth.ErrUserNotFound
		}
		return model.User{ID: id}, nil
	})
	mux := historyCardRouter(t, repo, users, &calls)
	tokens := newHandlerTokens(t)
	valid, _ := tokens.Issue("live-user")
	deleted, _ := tokens.Issue("deleted-user")
	for _, authorization := range []string{"Bearer invalid", "", "Bearer " + valid + " extra"} {
		r := httptest.NewRequest(http.MethodGet, "/api/apps/com.uchi.app", nil)
		r.Header.Add("Authorization", authorization)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("bad auth status=%d for %q", w.Code, authorization)
		}
	}
	r := httptest.NewRequest(http.MethodGet, "/api/apps/com.uchi.app", nil)
	r.Header.Add("Authorization", "Bearer "+valid)
	r.Header.Add("Authorization", "Bearer "+valid)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("multiple Authorization headers accepted: %d", w.Code)
	}
	r = httptest.NewRequest(http.MethodGet, "/api/apps/com.uchi.app", nil)
	r.Header.Set("Authorization", "Bearer "+deleted)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized || calls != 0 || repo.records != 0 {
		t.Fatalf("deleted user reached provider: status=%d calls=%d records=%d", w.Code, calls, repo.records)
	}
	r = httptest.NewRequest(http.MethodGet, "/api/apps/com.uchi.app", nil)
	r.Header.Set("Authorization", "Bearer "+valid)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK || repo.user != "live-user" || calls != 1 {
		t.Fatalf("valid user not recorded: status=%d user=%s calls=%d", w.Code, repo.user, calls)
	}
}

func TestCardHistoryFailureAndBadInputDoNotRecord(t *testing.T) {
	repo := &historyRepoStub{}
	calls := 0
	mux := historyCardRouter(t, repo, favoriteUsersStub{}, &calls)
	for _, path := range []string{"/api/apps/invalid", "/api/apps/com.uchi.app"} {
		if path == "/api/apps/com.uchi.app" {
			// The provider error path is covered by building a handler with a failing provider.
			failing := httpapi.NewHandler(providerFunc(func(context.Context, string) (model.App, error) { return model.App{}, app.ErrNotFound }), time.Second).WithHistory(repo, "google_play")
			mux2 := http.NewServeMux()
			tokens := newHandlerTokens(t)
			middleware := httpapi.NewHistoryHandler(repo, favoriteUsersStub{}, tokens, time.Second)
			mux2.Handle("GET /api/apps/{id}", middleware.WrapCard(http.HandlerFunc(failing.GetApp)))
			w := httptest.NewRecorder()
			mux2.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
			if w.Code != http.StatusNotFound {
				t.Fatalf("provider error status=%d", w.Code)
			}
		} else {
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
			if w.Code != http.StatusBadRequest {
				t.Fatalf("invalid package status=%d", w.Code)
			}
		}
	}
	if repo.records != 0 {
		t.Fatalf("failure path recorded %d views", repo.records)
	}
}

func TestCardHistoryRecordErrorsAndCancellation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
	}{{"database", errors.New("secret sql details"), http.StatusInternalServerError}, {"missing stored card", history.ErrCardNotFound, http.StatusInternalServerError}} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &historyRepoStub{err: tc.err}
			calls := 0
			mux := historyCardRouter(t, repo, favoriteUsersStub{}, &calls)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/apps/com.uchi.app", nil))
			if w.Code != tc.status || strings.Contains(w.Body.String(), "secret sql details") {
				t.Fatalf("record failure: %d %s", w.Code, w.Body.String())
			}
		})
	}
	// A short card deadline exercises record timeout handling; production uses a ten second card timeout.
	tokens := newHandlerTokens(t)
	provider := providerFunc(func(context.Context, string) (model.App, error) { return exampleApp(), nil })
	repo := historyRepoFunc{record: func(ctx context.Context, _ string, _, _ string) error { <-ctx.Done(); return ctx.Err() }}
	card := httpapi.NewHandler(provider, 50*time.Millisecond).WithHistory(repo, "google_play")
	historyHandler := httpapi.NewHistoryHandler(repo, favoriteUsersStub{}, tokens, time.Second)
	mux := http.NewServeMux()
	mux.Handle("GET /api/apps/{id}", historyHandler.WrapCard(http.HandlerFunc(card.GetApp)))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/apps/com.uchi.app", nil))
	if w.Code != http.StatusGatewayTimeout {
		t.Fatalf("record timeout status=%d body=%s", w.Code, w.Body.String())
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cancelingRepo := historyRepoFunc{record: func(context.Context, string, string, string) error { cancel(); return nil }}
	card = httpapi.NewHandler(provider, time.Second).WithHistory(cancelingRepo, "google_play")
	historyHandler = httpapi.NewHistoryHandler(cancelingRepo, favoriteUsersStub{}, tokens, time.Second)
	mux = http.NewServeMux()
	mux.Handle("GET /api/apps/{id}", historyHandler.WrapCard(http.HandlerFunc(card.GetApp)))
	r := httptest.NewRequest(http.MethodGet, "/api/apps/com.uchi.app", nil).WithContext(ctx)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK || w.Body.Len() != 0 {
		t.Fatalf("canceled record wrote response: %d %q", w.Code, w.Body.String())
	}
}

func TestHistoryListAuthPaginationAndErrors(t *testing.T) {
	tokens := newHandlerTokens(t)
	token, _ := tokens.Issue("user-a")
	tokenB, _ := tokens.Issue("user-b")
	repo := &historyRepoStub{}
	users := favoriteUsersFunc(func(_ context.Context, id string) (model.User, error) {
		if id == "deleted-user" {
			return model.User{}, auth.ErrUserNotFound
		}
		return model.User{ID: id}, nil
	})
	mux := http.NewServeMux()
	h := httpapi.NewHistoryHandler(repo, users, tokens, time.Second)
	h.RegisterRoutes(mux)
	call := func(path, bearer string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		if bearer != "" {
			r.Header.Set("Authorization", "Bearer "+bearer)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	if w := call("/api/history", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("no token: %d", w.Code)
	}
	for _, query := range []string{"?limit=", "?limit=1&limit=2", "?limit=101", "?offset=-1", "?offset=x"} {
		if w := call("/api/history"+query, token); w.Code != http.StatusBadRequest {
			t.Fatalf("%s status=%d", query, w.Code)
		}
	}
	w := call("/api/history?limit=7&offset=3", token)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"history":[]`) || repo.user != "user-a" || repo.limit != 7 || repo.offset != 3 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("list response: %d %s", w.Code, w.Body.String())
	}
	w = call("/api/history", tokenB)
	if w.Code != http.StatusOK || repo.user != "user-b" {
		t.Fatalf("second user scope: %d user=%s", w.Code, repo.user)
	}
	deletedToken, _ := tokens.Issue("deleted-user")
	listCallsBeforeDeletedUser := repo.lists
	w = call("/api/history", deletedToken)
	if w.Code != http.StatusUnauthorized || repo.lists != listCallsBeforeDeletedUser {
		t.Fatalf("deleted user reached history repository: status=%d list calls=%d", w.Code, repo.lists-listCallsBeforeDeletedUser)
	}
	for _, tc := range []struct {
		err    error
		status int
	}{{auth.ErrUserNotFound, http.StatusUnauthorized}, {errors.New("hidden sql details"), http.StatusInternalServerError}, {context.DeadlineExceeded, http.StatusGatewayTimeout}} {
		repo.err = tc.err
		w = call("/api/history", token)
		if w.Code != tc.status || strings.Contains(w.Body.String(), "hidden sql details") {
			t.Fatalf("error response: %d %s", w.Code, w.Body.String())
		}
	}
}
