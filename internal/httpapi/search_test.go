package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"app-grabber-backend/internal/app"
	"app-grabber-backend/internal/httpapi"
	"app-grabber-backend/internal/model"
)

type searcherFunc func(context.Context, string, int) ([]model.App, error)

func (f searcherFunc) Search(ctx context.Context, query string, limit int) ([]model.App, error) {
	return f(ctx, query, limit)
}

// doSearch — запрос без ссылки: GetApp вызываться не должен.
func doSearch(t *testing.T, s searcherFunc, rawQuery string) *httptest.ResponseRecorder {
	t.Helper()
	never := providerFunc(func(context.Context, string) (model.App, error) {
		t.Error("provider must not be called")
		return model.App{}, nil
	})
	return doSearchWith(t, s, never, rawQuery)
}

func doSearchWith(t *testing.T, s searcherFunc, p providerFunc, rawQuery string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/search", httpapi.NewSearchHandler(s, p, time.Second).WithRuStore(p).Search)
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

// Ссылка на карточку → одна карточка через GetApp, поиск не вызывается.
func TestSearchByLink(t *testing.T) {
	noSearch := searcherFunc(func(context.Context, string, int) ([]model.App, error) {
		t.Error("searcher must not be called")
		return nil, nil
	})
	links := []string{
		"https://play.google.com/store/apps/details?id=com.uchi.app&hl=ru",
		"https://apps.rustore.ru/app/com.uchi.app",
		"https://www.rustore.ru/catalog/app/com.uchi.app",
		// длиннее 100 символов: ограничение длины для ссылок не действует
		"https://play.google.com/store/apps/details?id=com.uchi.app&hl=ru&gl=US&utm_source=telegram&utm_medium=post&utm_campaign=autumn",
	}
	for _, link := range links {
		var gotID string
		rec := doSearchWith(t, noSearch, func(_ context.Context, id string) (model.App, error) {
			gotID = id
			return exampleApp(), nil
		}, "q="+url.QueryEscape(link))

		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, body = %s", link, rec.Code, rec.Body)
		}
		if gotID != "com.uchi.app" {
			t.Errorf("%s: package name = %q, want com.uchi.app", link, gotID)
		}
		var body struct {
			Query   string      `json:"query"`
			Results []model.App `json:"results"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.Query != link || len(body.Results) != 1 || body.Results[0].PackageName != "com.uchi.app" {
			t.Errorf("%s: body = %+v", link, body)
		}
	}
}

func TestSearchByLinkSelectsStore(t *testing.T) {
	for _, tc := range []struct{ link, store, id string }{
		{"https://www.rustore.ru/catalog/app/com.example.calculatorKSA1211", "rustore", "com.example.calculatorKSA1211"},
		{"https://www.rustore.ru/catalog/app/ru.ivi.client", "rustore", "ru.ivi.client"},
		{"https://www.rustore.ru/catalog/app/ru.ivi.client/?utm_source=share", "rustore", "ru.ivi.client"},
		{"www.rustore.ru/catalog/app/ru.ivi.client", "rustore", "ru.ivi.client"},
		{"https://apps.rustore.ru/app/ru.ivi.client", "rustore", "ru.ivi.client"},
		{"https://play.google.com/store/apps/details?id=com.ivi.client&hl=ru", "google_play", "com.ivi.client"},
	} {
		t.Run(tc.link, func(t *testing.T) {
			calls := 0
			provider := func(store string) providerFunc {
				return func(_ context.Context, id string) (model.App, error) {
					calls++
					if store != tc.store || id != tc.id {
						t.Errorf("called %s/%s, want %s/%s", store, id, tc.store, tc.id)
					}
					return model.App{PackageName: id, StoreURL: tc.link, Name: "Иви"}, nil
				}
			}
			noSearch := searcherFunc(func(context.Context, string, int) ([]model.App, error) {
				t.Fatal("link reached text search")
				return nil, nil
			})
			h := httpapi.NewSearchHandler(noSearch, provider("google_play"), time.Second).WithRuStore(provider("rustore"))
			w := httptest.NewRecorder()
			h.Search(w, httptest.NewRequest("GET", "/api/search?q="+url.QueryEscape(tc.link), nil))
			var body struct {
				Results []model.App `json:"results"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if w.Code != http.StatusOK || calls != 1 || len(body.Results) != 1 || body.Results[0].StoreURL != tc.link {
				t.Fatalf("HTTP %d, calls %d, body %s", w.Code, calls, w.Body.String())
			}
		})
	}
}

func TestSearchRejectsInvalidLinks(t *testing.T) {
	for _, link := range []string{
		"https://www.rustore.ru/catalog/app/ru.1ivi.client",
		"www.rustore.ru/catalog/app/ru.1ivi.client",
		"https://www.rustore.ru/catalog/app/",
		"https://play.google.com/store/apps/details?id=com.first&id=com.second",
		"https://example.com/catalog/app/ru.ivi.client",
	} {
		h := httpapi.NewSearchHandler(nil, nil, time.Second)
		w := httptest.NewRecorder()
		h.Search(w, httptest.NewRequest("GET", "/api/search?q="+url.QueryEscape(link), nil))
		assertError(t, w, 400, "Некорректная или неподдерживаемая ссылка на приложение")
	}
}

func TestSearchRuStoreNotConfigured(t *testing.T) {
	h := httpapi.NewSearchHandler(nil, nil, time.Second)
	w := httptest.NewRecorder()
	h.Search(w, httptest.NewRequest("GET", "/api/search?q="+url.QueryEscape("https://www.rustore.ru/catalog/app/ru.ivi.client"), nil))
	assertError(t, w, 501, "Получение данных из RuStore ещё не подключено")
}

// Приложения из ссылки нет → 200 и пустой results, как у поиска без результатов.
func TestSearchByLinkNotFound(t *testing.T) {
	rec := doSearchWith(t, nil, func(context.Context, string) (model.App, error) {
		return model.App{}, app.ErrNotFound
	}, "q="+url.QueryEscape("https://apps.rustore.ru/app/com.example.none"))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"results":[]`) {
		t.Errorf("status = %d, body = %s, want 200 and results: []", rec.Code, rec.Body)
	}
}

func TestSearchByLinkErrors(t *testing.T) {
	link := "q=" + url.QueryEscape("https://apps.rustore.ru/app/com.uchi.app")

	rec := doSearchWith(t, nil, func(context.Context, string) (model.App, error) {
		return model.App{}, errors.New("google is down")
	}, link)
	if rec.Code != http.StatusBadGateway {
		t.Errorf("error: status = %d, want 502", rec.Code)
	}

	rec = doSearchWith(t, nil, func(ctx context.Context, _ string) (model.App, error) {
		<-ctx.Done()
		return model.App{}, ctx.Err()
	}, link)
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "время ожидания") {
		t.Errorf("timeout: status = %d, body = %s", rec.Code, rec.Body)
	}
}

func TestSearchPersistenceErrorsAreInternal(t *testing.T) {
	for _, tc := range []struct {
		sentinel error
		message  string
	}{
		{app.ErrCacheRead, "Не удалось прочитать сохранённые данные приложения"},
		{app.ErrPersistence, "Не удалось сохранить результаты поиска"},
	} {
		t.Run(tc.sentinel.Error(), func(t *testing.T) {
			rec := doSearch(t, func(context.Context, string, int) ([]model.App, error) {
				return nil, fmt.Errorf("storage detail: %w; %w", tc.sentinel, context.DeadlineExceeded)
			}, "q=telegram")
			if rec.Code != http.StatusInternalServerError {
				t.Errorf("status = %d, want 500; body = %s", rec.Code, rec.Body)
			}
			var body struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Error != tc.message {
				t.Errorf("error = %q, want %q", body.Error, tc.message)
			}
		})
	}
}

func TestSearchByLinkPersistenceErrorsAreInternal(t *testing.T) {
	link := "q=" + url.QueryEscape("https://apps.rustore.ru/app/com.uchi.app")
	for _, sentinel := range []error{app.ErrCacheRead, app.ErrPersistence} {
		t.Run(sentinel.Error(), func(t *testing.T) {
			rec := doSearchWith(t, nil, func(context.Context, string) (model.App, error) {
				return model.App{}, fmt.Errorf("storage detail: %w; %w", sentinel, context.DeadlineExceeded)
			}, link)
			if rec.Code != http.StatusInternalServerError {
				t.Errorf("status = %d, want 500; body = %s", rec.Code, rec.Body)
			}
			if strings.Contains(rec.Body.String(), "storage detail") || strings.Contains(rec.Body.String(), "deadline exceeded") {
				t.Errorf("response exposes internal error: %s", rec.Body)
			}
		})
	}
}
