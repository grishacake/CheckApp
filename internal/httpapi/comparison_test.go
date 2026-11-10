package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"app-grabber-backend/internal/app"
	"app-grabber-backend/internal/comparison"
	"app-grabber-backend/internal/httpapi"
	"app-grabber-backend/internal/model"
)

type comparisonCache struct{ gpAge time.Duration }

func (c comparisonCache) Lookup(_ context.Context, store, id string) (model.App, time.Time, error) {
	if store == "google_play" && c.gpAge != 0 {
		return model.App{PackageName: id, Name: "cached"}, time.Now().Add(-c.gpAge), nil
	}
	return model.App{}, time.Time{}, app.ErrNotFound
}
func (comparisonCache) Save(context.Context, string, []model.App) error { return nil }

type comparisonRecords struct {
	httpapi.ComparisonStore
	records []comparison.Pair
}

func (s *comparisonRecords) Record(_ context.Context, _ string, p comparison.Pair) error {
	s.records = append(s.records, p)
	return nil
}

func TestComparisonRoutes(t *testing.T) {
	outage := errors.New("store down")
	for _, tc := range []struct {
		name               string
		age                time.Duration
		gpErr, ruErr       error
		gpStatus, ruStatus string
		code               int
		stale              bool
		records            int
	}{
		{"both cold", 0, nil, nil, "ok", "ok", 200, false, 1},
		{"one timeout", 0, nil, context.DeadlineExceeded, "ok", "unavailable", 200, false, 1},
		{"fresh cache", time.Minute, outage, outage, "ok", "unavailable", 200, false, 1},
		{"expired cache", 48 * time.Hour, outage, outage, "ok", "unavailable", 200, true, 1},
		{"confirmed removal", 48 * time.Hour, app.ErrNotFound, outage, "not_found", "unavailable", 200, false, 0},
		{"outage", 0, outage, outage, "unavailable", "unavailable", 503, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := func(err error) providerFunc {
				return func(ctx context.Context, id string) (model.App, error) {
					if _, ok := ctx.Deadline(); !ok {
						t.Error("missing upstream deadline")
					}
					return model.App{PackageName: id, Name: "live"}, err
				}
			}
			records := new(comparisonRecords)
			tokens := newHandlerTokens(t)
			handler := httpapi.ComparisonHandler{Service: &comparison.Service{GooglePlay: provider(tc.gpErr), RuStore: provider(tc.ruErr), Cache: comparisonCache{tc.age}, TTL: 24 * time.Hour, StoreTimeout: time.Second}, Store: records, History: httpapi.NewHistoryHandler(nil, favoriteUsersStub{}, tokens, time.Second)}
			mux := http.NewServeMux()
			handler.RegisterRoutes(mux)
			url := "/api/compare?google_play=com.google&rustore=ru.store"
			token, err := tokens.Issue("test-user")
			if err != nil {
				t.Fatal(err)
			}
			request := func(method, path string, signed bool) *httptest.ResponseRecorder {
				req := httptest.NewRequest(method, path, nil)
				if signed {
					req.AddCookie(&http.Cookie{Name: httpapi.AuthCookieName, Value: token})
				}
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, req)
				return w
			}
			res := request("GET", url, true)
			var body comparison.Response
			if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if res.Code != tc.code || body.GooglePlay.Status != tc.gpStatus || body.RuStore.Status != tc.ruStatus || body.GooglePlay.Stale != tc.stale {
				t.Fatalf("HTTP %d: %+v", res.Code, body)
			}
			if body.GooglePlay.Status == "ok" && (body.GooglePlay.App == nil || body.GooglePlay.App.PackageName != "com.google" || body.GooglePlay.FetchedAt == nil) {
				t.Fatal(body.GooglePlay)
			}
			if body.RuStore.Status == "ok" && (body.RuStore.App == nil || body.RuStore.App.PackageName != "ru.store") {
				t.Fatal(body.RuStore)
			}
			request("HEAD", url, true)
			request("GET", url, false)
			if len(records.records) != tc.records {
				t.Fatal(records.records)
			}
			if tc.records == 1 && records.records[0] != (comparison.Pair{GooglePlay: "com.google", RuStore: "ru.store"}) {
				t.Fatal(records.records)
			}
			if w := request("GET", "/api/compare?google_play=bad", false); w.Code != 400 {
				t.Fatal(w.Code)
			}
			if w := request("PUT", "/api/compare/favorites", false); w.Code != 401 {
				t.Fatal(w.Code)
			}
			for _, query := range []string{
				"?google_play=com.1Google&rustore=ru.store",
				"?google_play=com.google&rustore=ru.1Store",
			} {
				for _, route := range []struct{ method, path string }{
					{"GET", "/api/compare"},
					{"PUT", "/api/compare/favorites"},
					{"DELETE", "/api/compare/favorites"},
				} {
					if w := request(route.method, route.path+query, true); w.Code != 400 {
						t.Fatalf("%s %s%s: HTTP %d", route.method, route.path, query, w.Code)
					}
				}
			}
		})
	}
}
