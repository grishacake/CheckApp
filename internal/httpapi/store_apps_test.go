package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"app-grabber-backend/internal/app"
	"app-grabber-backend/internal/httpapi"
	"app-grabber-backend/internal/model"
)

func TestStoreAppRoutes(t *testing.T) {
	const id = "com.example.MixedCase"
	calls := map[string]int{}
	history := new(historyRepoStub)
	handlers := map[string]*httpapi.Handler{}
	for _, store := range []string{"google_play", "rustore"} {
		handlers[store] = httpapi.NewHandler(providerFunc(func(_ context.Context, gotID string) (model.App, error) {
			calls[store]++
			if gotID != id {
				t.Errorf("provider ID = %q", gotID)
			}
			return model.App{PackageName: gotID, Name: store}, nil
		}), time.Second).WithHistory(history, store)
	}
	mux := http.NewServeMux()
	mux.Handle("GET /api/apps/{store}/{id}", httpapi.StoreAppHandler(handlers))
	for _, store := range []string{"google_play", "rustore"} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/apps/"+store+"/"+id, nil))
		var card model.App
		if err := json.Unmarshal(w.Body.Bytes(), &card); err != nil {
			t.Fatal(err)
		}
		if w.Code != 200 || card.Name != store || card.PackageName != id || history.store != store || history.packageName != id {
			t.Fatalf("%s: HTTP %d %+v history=%+v", store, w.Code, card, history)
		}
	}
	if calls["google_play"] != 1 || calls["rustore"] != 1 {
		t.Fatal(calls)
	}
	for _, path := range []string{"/api/apps/unknown/" + id, "/api/apps/rustore/invalid"} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 400 {
			t.Fatalf("%s: HTTP %d", path, w.Code)
		}
	}
	if history.records != 2 || calls["google_play"] != 1 || calls["rustore"] != 1 {
		t.Fatal("invalid input reached provider/history")
	}
}

func TestRuStoreAppErrorsNameCorrectSource(t *testing.T) {
	for _, tc := range []struct {
		err     error
		code    int
		message string
	}{
		{app.ErrNotFound, 404, "Приложение не найдено в RuStore"},
		{context.DeadlineExceeded, 502, "Превышено время ожидания RuStore"},
	} {
		h := httpapi.NewHandler(providerFunc(func(context.Context, string) (model.App, error) { return model.App{}, tc.err }), time.Second).WithHistory(nil, "rustore")
		mux := http.NewServeMux()
		mux.Handle("GET /api/apps/{store}/{id}", httpapi.StoreAppHandler(map[string]*httpapi.Handler{"rustore": h}))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/apps/rustore/ru.ivi.client", nil))
		assertError(t, w, tc.code, tc.message)
	}
}
