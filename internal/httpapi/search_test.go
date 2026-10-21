package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"app-grabber-backend/internal/httpapi"
	"app-grabber-backend/internal/model"
)

type searcherFunc func(context.Context, string, int) ([]model.App, error)

func (f searcherFunc) Search(ctx context.Context, query string, limit int) ([]model.App, error) {
	return f(ctx, query, limit)
}

func doSearch(t *testing.T, s searcherFunc, rawQuery string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/search", httpapi.NewSearchHandler(s, time.Second).Search)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/search?"+rawQuery, nil))
	return rec
}

func TestSearchSuccess(t *testing.T) {
	var gotQuery string
	var gotLimit int
	rec := doSearch(t, func(_ context.Context, q string, limit int) ([]model.App, error) {
		gotQuery, gotLimit = q, limit
		return []model.App{exampleApp()}, nil
	}, "q="+url.QueryEscape("  учи ру  "))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if gotQuery != "учи ру" {
		t.Errorf("query = %q, want trimmed %q", gotQuery, "учи ру")
	}
	if gotLimit != 5 {
		t.Errorf("limit = %d, want 5", gotLimit)
	}
	var body struct {
		Query   string      `json:"query"`
		Results []model.App `json:"results"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Query != "учи ру" || len(body.Results) != 1 || body.Results[0].PackageName != "com.uchi.app" {
		t.Errorf("body = %+v", body)
	}
}

// Ничего не нашлось — фронт должен получить [], а не null.
func TestSearchEmptyResultsIsArray(t *testing.T) {
	rec := doSearch(t, func(context.Context, string, int) ([]model.App, error) {
		return nil, nil
	}, "q=zzqq")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"results":[]`) {
		t.Errorf("body = %s, want results: []", rec.Body)
	}
}

func TestSearchBadQuery(t *testing.T) {
	never := searcherFunc(func(context.Context, string, int) ([]model.App, error) {
		t.Error("searcher must not be called")
		return nil, nil
	})
	for name, rawQuery := range map[string]string{
		"no q":        "",
		"empty":       "q=",
		"spaces":      "q=%20%20",
		"too long":    "q=" + strings.Repeat("a", 101),
		"not utf8":    "q=%FF%FE",
		"other param": "query=telegram",
	} {
		if rec := doSearch(t, never, rawQuery); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, rec.Code)
		}
	}
}

func TestSearchErrors(t *testing.T) {
	rec := doSearch(t, func(context.Context, string, int) ([]model.App, error) {
		return nil, errors.New("google is down")
	}, "q=telegram")
	if rec.Code != http.StatusBadGateway {
		t.Errorf("error: status = %d, want 502", rec.Code)
	}

	rec = doSearch(t, func(ctx context.Context, _ string, _ int) ([]model.App, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}, "q=telegram")
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "время ожидания") {
		t.Errorf("timeout: status = %d, body = %s", rec.Code, rec.Body)
	}
}
