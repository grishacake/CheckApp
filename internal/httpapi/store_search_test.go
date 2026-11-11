package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"app-grabber-backend/internal/app"
	"app-grabber-backend/internal/httpapi"
	"app-grabber-backend/internal/model"
)

func TestSearchStoreFiltersAndMetadata(t *testing.T) {
	for _, tc := range []struct {
		name, filter string
		want         []string
	}{
		{"default", "", []string{"google_play"}},
		{"RuStore", "&stores=rustore", []string{"rustore"}},
		{"both", "&stores=google_play,rustore", []string{"google_play", "rustore"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := make(chan string, 2)
			searchers := make(map[string]app.Searcher)
			providers := make(map[string]app.Provider)
			for _, store := range []string{"google_play", "rustore"} {
				searchers[store] = searcherFunc(func(_ context.Context, q string, n int) ([]model.App, error) {
					called <- store
					if q != "telegram" || n != 5 {
						t.Errorf("q=%q n=%d", q, n)
					}
					return []model.App{{PackageName: "com.Same", Name: store}}, nil
				})
				providers[store] = providerFunc(func(_ context.Context, id string) (model.App, error) { return model.App{PackageName: id}, nil })
			}
			service := app.NewStoreSearch(searchers, providers)
			h := httpapi.NewSearchHandler(searchers["google_play"], providers["google_play"], time.Second).WithRuStore(providers["rustore"]).WithStoreSearch(service)
			w := httptest.NewRecorder()
			h.Search(w, httptest.NewRequest("GET", "/api/search?q=telegram"+tc.filter, nil))
			var body struct {
				Query   string             `json:"query"`
				Results []app.SearchResult `json:"results"`
				Sources []app.SearchSource `json:"sources"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if w.Code != 200 || body.Query != "telegram" || len(body.Results) != 1 || body.Results[0].Name != tc.want[0] || len(body.Sources) != len(tc.want) {
				t.Fatalf("HTTP %d body=%s", w.Code, w.Body)
			}
			if len(called) != len(tc.want) {
				t.Fatalf("called %d stores", len(called))
			}
			seen := map[string]bool{}
			for range tc.want {
				seen[<-called] = true
			}
			for _, store := range tc.want {
				if !seen[store] {
					t.Fatal(seen)
				}
			}
			for _, ref := range body.Results[0].Stores {
				if ref.Status != "ok" || ref.PackageName != "com.Same" {
					t.Fatal(ref)
				}
			}
		})
	}
}

func TestSearchRejectsInvalidStoreFilters(t *testing.T) {
	never := searcherFunc(func(context.Context, string, int) ([]model.App, error) {
		t.Error("invalid filter reached provider")
		return nil, nil
	})
	h := httpapi.NewSearchHandler(never, nil, time.Second).WithStoreSearch(app.NewStoreSearch(map[string]app.Searcher{"google_play": never}, nil))
	for _, query := range []string{"q=test&stores=", "q=test&stores=other", "q=test&stores=rustore,rustore", "q=test&stores=rustore&stores=google_play", "q=test&stores=google_play,"} {
		w := httptest.NewRecorder()
		h.Search(w, httptest.NewRequest("GET", "/api/search?"+query, nil))
		if w.Code != 400 {
			t.Errorf("%s: HTTP %d %s", query, w.Code, w.Body)
		}
	}
}

func TestSearchPartialAndTotalStoreFailure(t *testing.T) {
	for _, bothFail := range []bool{false, true} {
		gpErr := error(nil)
		if bothFail {
			gpErr = errors.New("GP private failure")
		}
		service := app.NewStoreSearch(map[string]app.Searcher{
			"google_play": searcherFunc(func(context.Context, string, int) ([]model.App, error) {
				return []model.App{{PackageName: "com.Good", Name: "app"}}, gpErr
			}),
			"rustore": searcherFunc(func(context.Context, string, int) ([]model.App, error) {
				return nil, errors.New("RuStore private failure")
			}),
		}, nil)
		h := httpapi.NewSearchHandler(nil, nil, time.Second).WithStoreSearch(service)
		w := httptest.NewRecorder()
		h.Search(w, httptest.NewRequest("GET", "/api/search?q=test&stores=google_play,rustore", nil))
		if bothFail {
			assertError(t, w, 502, "Не удалось выполнить поиск в выбранных магазинах")
			continue
		}
		var body struct {
			Results []app.SearchResult `json:"results"`
			Sources []app.SearchSource `json:"sources"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if w.Code != 200 || len(body.Results) != 1 || len(body.Sources) != 2 || body.Sources[1].Status != "unavailable" {
			t.Fatalf("HTTP %d %s", w.Code, w.Body)
		}
	}
}
