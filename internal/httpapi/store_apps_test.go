package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
	for _, store := range []string{"google_play", "rustore", "apkpure"} {
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
	for _, store := range []string{"google_play", "rustore", "apkpure"} {
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
	if calls["google_play"] != 1 || calls["rustore"] != 1 || calls["apkpure"] != 1 {
		t.Fatal(calls)
	}
	for _, path := range []string{"/api/apps/unknown/" + id, "/api/apps/rustore/invalid", "/api/apps/apkpure/com." + strings.Repeat("a", 252)} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 400 {
			t.Fatalf("%s: HTTP %d", path, w.Code)
		}
	}
	if history.records != 3 || calls["google_play"] != 1 || calls["rustore"] != 1 || calls["apkpure"] != 1 {
		t.Fatal("invalid input reached provider/history")
	}
}

func TestStoreAppErrorsNameCorrectSource(t *testing.T) {
	for _, tc := range []struct {
		store   string
		err     error
		code    int
		message string
	}{
		{"rustore", app.ErrNotFound, 404, "Приложение не найдено в RuStore"},
		{"rustore", context.DeadlineExceeded, 502, "Превышено время ожидания RuStore"},
		{"apkpure", app.ErrNotFound, 404, "Приложение не найдено в APKPure"},
		{"apkpure", context.DeadlineExceeded, 502, "Превышено время ожидания APKPure"},
	} {
		h := httpapi.NewHandler(providerFunc(func(context.Context, string) (model.App, error) { return model.App{}, tc.err }), time.Second).WithHistory(nil, tc.store)
		mux := http.NewServeMux()
		mux.Handle("GET /api/apps/{store}/{id}", httpapi.StoreAppHandler(map[string]*httpapi.Handler{tc.store: h}))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/apps/"+tc.store+"/com.example.app", nil))
		assertError(t, w, tc.code, tc.message)
	}
}
