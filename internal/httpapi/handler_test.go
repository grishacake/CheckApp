package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"app-grabber-backend/docs"
	"app-grabber-backend/internal/app"
	"app-grabber-backend/internal/httpapi"
	"app-grabber-backend/internal/model"
)

type providerFunc func(context.Context, string) (model.App, error)

func (f providerFunc) GetApp(ctx context.Context, packageName string) (model.App, error) {
	return f(ctx, packageName)
}

func newRouter(provider app.Provider, timeout time.Duration) *http.ServeMux {
	mux := http.NewServeMux()
	handler := httpapi.NewHandler(provider, timeout)
	mux.HandleFunc("GET /api/apps/{id}", handler.GetApp)
	return mux
}

func exampleApp() model.App {
	return model.App{
		PackageName: "com.uchi.app",
		StoreURL:    "https://play.google.com/store/apps/details?id=com.uchi.app&hl=ru",
		Name:        "Учи.ру",
		Developer:   "ООО \"Учи.ру\"",
		IconURL:     "https://example.com/icon.png",
		Category:    "Образование",
		Rating:      4.2,
		AgeRating:   "3+",
	}
}

func TestGetAppSuccess(t *testing.T) {
	var providerContext context.Context
	calls := 0
	router := newRouter(providerFunc(func(ctx context.Context, id string) (model.App, error) {
		calls++
		providerContext = ctx
		if id != "com.uchi.app" {
			t.Errorf("package name = %q", id)
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Error("provider received no deadline")
		}
		return exampleApp(), nil
	}), time.Second)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/apps/com.uchi.app", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatal("response is not JSON with UTF-8")
	}
	want := map[string]any{
		"package_name": "com.uchi.app",
		"store_url":    "https://play.google.com/store/apps/details?id=com.uchi.app&hl=ru",
		"name":         "Учи.ру",
		"developer":    "ООО \"Учи.ру\"",
		"icon_url":     "https://example.com/icon.png",
		"category":     "Образование",
		"rating":       4.2,
		"age_rating":   "3+",
	}
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("response = %#v, want %#v", got, want)
	}
	if calls != 1 {
		t.Fatalf("provider calls = %d, want 1", calls)
	}
	if !errors.Is(providerContext.Err(), context.Canceled) {
		t.Fatal("provider context was not canceled after returning")
	}

	docs.Register(router)
	specification := httptest.NewRecorder()
	router.ServeHTTP(specification, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	var spec struct {
		Components struct {
			Schemas map[string]struct {
				Required []string
			}
		}
	}
	if err := json.Unmarshal(specification.Body.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}
	required := spec.Components.Schemas["App"].Required
	if len(required) != len(got) {
		t.Fatalf("OpenAPI requires %d fields; response has %d", len(required), len(got))
	}
	for _, field := range required {
		if _, ok := got[field]; !ok {
			t.Errorf("response missing OpenAPI field %q", field)
		}
	}
}

func TestGetAppInvalidPackageName(t *testing.T) {
	for _, id := range []string{
		"invalid", "Com.uchi.app", "com.Uchi.app", "com.uchi.APP",
		"com..app", "com.1app", "com.app-name", "com.app%20", "com.app%2Fextra",
	} {
		t.Run(id, func(t *testing.T) {
			calls := 0
			router := newRouter(providerFunc(func(context.Context, string) (model.App, error) {
				calls++
				return exampleApp(), nil
			}), time.Second)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/apps/"+id, nil))
			assertError(t, w, http.StatusBadRequest, "Некорректный формат package_name")
			if calls != 0 {
				t.Fatal("invalid input reached provider")
			}
		})
	}
}

func TestGetAppAllowsDigitsAndUnderscores(t *testing.T) {
	const id = "com.example.my_app2"
	router := newRouter(providerFunc(func(_ context.Context, got string) (model.App, error) {
		if got != id {
			t.Errorf("provider got %q, want %q", got, id)
		}
		card := exampleApp()
		card.PackageName = got
		return card, nil
	}), time.Second)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/apps/"+id, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
}

func TestGetAppProviderErrors(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		status  int
		message string
	}{
		{"not found", app.ErrNotFound, 404, "Приложение не найдено в Google Play"},
		{"wrapped not found", fmt.Errorf("lookup: %w", app.ErrNotFound), 404, "Приложение не найдено в Google Play"},
		{"unconfigured", fmt.Errorf("setup: %w", app.ErrNotConfigured), 501, "Получение данных из Google Play ещё не подключено"},
		{"deadline", fmt.Errorf("load: %w", context.DeadlineExceeded), 502, "Превышено время ожидания Google Play"},
		{"network timeout", fmt.Errorf("network: %w", &net.DNSError{IsTimeout: true}), 502, "Превышено время ожидания Google Play"},
		{"upstream", errors.New("private upstream details"), 502, "Не удалось получить данные из Google Play"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			router := newRouter(providerFunc(func(context.Context, string) (model.App, error) {
				return model.App{}, tt.err
			}), time.Second)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/apps/com.uchi.app", nil))
			assertError(t, w, tt.status, tt.message)
		})
	}
}

func TestGetAppTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	router := newRouter(providerFunc(func(ctx context.Context, _ string) (model.App, error) {
		<-ctx.Done()
		return model.App{}, ctx.Err()
	}), 20*time.Millisecond)
	w := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/apps/com.uchi.app", nil).WithContext(ctx)
	router.ServeHTTP(w, request)
	assertError(t, w, http.StatusBadGateway, "Превышено время ожидания Google Play")
}

func TestGetAppClientCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{})
	result := make(chan error, 1)
	router := newRouter(providerFunc(func(ctx context.Context, _ string) (model.App, error) {
		close(entered)
		<-ctx.Done()
		result <- ctx.Err()
		return model.App{}, ctx.Err()
	}), 2*time.Second)
	w := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/apps/com.uchi.app", nil).WithContext(ctx)
	done := make(chan struct{})
	go func() {
		router.ServeHTTP(w, request)
		close(done)
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("provider did not start")
	}
	cancel()
	select {
	case <-done:
		if err := <-result; !errors.Is(err, context.Canceled) {
			t.Fatalf("provider error = %v, want context.Canceled", err)
		}
		if w.Body.Len() != 0 || len(w.Header()) != 0 {
			t.Fatal("handler wrote a response after client cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation did not stop provider")
	}
}

func TestGetAppAlreadyCanceledRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	router := newRouter(providerFunc(func(context.Context, string) (model.App, error) {
		t.Error("canceled request reached provider")
		return model.App{}, nil
	}), time.Second)
	w := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/apps/com.uchi.app", nil).WithContext(ctx)
	router.ServeHTTP(w, request)
	if w.Body.Len() != 0 || len(w.Header()) != 0 {
		t.Fatal("handler wrote a response for an already canceled request")
	}
}

func TestGetAppInvalidJSON(t *testing.T) {
	router := newRouter(providerFunc(func(context.Context, string) (model.App, error) {
		card := exampleApp()
		card.Rating = math.NaN()
		return card, nil
	}), time.Second)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/apps/com.uchi.app", nil))
	assertError(t, w, http.StatusBadGateway, "Не удалось получить данные из Google Play")
}

type failingWriter struct {
	header   http.Header
	statuses []int
	writes   int
}

func (w *failingWriter) Header() http.Header    { return w.header }
func (w *failingWriter) WriteHeader(status int) { w.statuses = append(w.statuses, status) }
func (w *failingWriter) Write([]byte) (int, error) {
	w.writes++
	return 0, errors.New("connection closed")
}

func TestGetAppLogsWriteError(t *testing.T) {
	var logs bytes.Buffer
	original := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(original) })

	router := newRouter(providerFunc(func(context.Context, string) (model.App, error) {
		return exampleApp(), nil
	}), time.Second)
	w := &failingWriter{header: make(http.Header)}
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/apps/com.uchi.app", nil))

	if !strings.Contains(logs.String(), "write response: connection closed") {
		t.Fatalf("write error was not logged: %s", logs.String())
	}
	if w.writes != 1 || !reflect.DeepEqual(w.statuses, []int{http.StatusOK}) {
		t.Fatalf("unexpected second response: writes=%d, statuses=%v", w.writes, w.statuses)
	}
}

func assertError(t *testing.T, w *httptest.ResponseRecorder, status int, message string) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, status, w.Body.String())
	}
	if w.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatal("error response is not JSON with UTF-8")
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body) != 1 || body["error"] != message {
		t.Fatalf("error response = %v, want error %q", body, message)
	}
}

func TestGetAppProviderDeadline(t *testing.T) {
	cases := []struct {
		name          string
		timeout       time.Duration
		wantTimeout   time.Duration
		parentTimeout time.Duration
	}{
		{name: "zero uses default", timeout: 0, wantTimeout: 10 * time.Second},
		{name: "negative uses default", timeout: -time.Second, wantTimeout: 10 * time.Second},
		{name: "configured timeout", timeout: 3 * time.Second, wantTimeout: 3 * time.Second},
		{name: "earlier parent deadline", timeout: time.Minute, parentTimeout: 5 * time.Second},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			parent := context.Background()
			if tt.parentTimeout > 0 {
				var cancel context.CancelFunc
				parent, cancel = context.WithTimeout(parent, tt.parentTimeout)
				defer cancel()
			}
			var providerContext context.Context
			var deadline time.Time
			router := newRouter(providerFunc(func(ctx context.Context, _ string) (model.App, error) {
				providerContext = ctx
				var ok bool
				deadline, ok = ctx.Deadline()
				if !ok {
					t.Error("provider context has no deadline")
				}
				if err := ctx.Err(); err != nil {
					t.Errorf("provider received expired context: %v", err)
				}
				return exampleApp(), nil
			}), tt.timeout)
			request := httptest.NewRequest(http.MethodGet, "/api/apps/com.uchi.app", nil).WithContext(parent)
			w := httptest.NewRecorder()
			before := time.Now()
			router.ServeHTTP(w, request)
			after := time.Now()
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
			}
			if tt.parentTimeout > 0 {
				want, _ := parent.Deadline()
				if !deadline.Equal(want) {
					t.Errorf("provider deadline = %v, want parent deadline %v", deadline, want)
				}
			} else if deadline.Before(before.Add(tt.wantTimeout)) || deadline.After(after.Add(tt.wantTimeout)) {
				t.Errorf("provider deadline = %v, want timeout %s", deadline, tt.wantTimeout)
			}
			if providerContext == nil || !errors.Is(providerContext.Err(), context.Canceled) {
				t.Error("provider context was not canceled after response")
			}
			if err := parent.Err(); err != nil {
				t.Errorf("handler canceled parent context: %v", err)
			}
		})
	}
}

func TestGetAppDiscardsLateProviderResult(t *testing.T) {
	for _, tt := range []struct {
		name string
		err  error
	}{
		{"success", nil},
		{"not found", app.ErrNotFound},
		{"unconfigured", app.ErrNotConfigured},
	} {
		t.Run(tt.name, func(t *testing.T) {
			parent, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			router := newRouter(providerFunc(func(ctx context.Context, _ string) (model.App, error) {
				<-ctx.Done()
				return exampleApp(), tt.err
			}), 20*time.Millisecond)
			w := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/api/apps/com.uchi.app", nil).WithContext(parent)
			router.ServeHTTP(w, request)
			assertError(t, w, http.StatusBadGateway, "Превышено время ожидания Google Play")
		})
	}
}

func TestGetAppDiscardsResultAfterClientCancellation(t *testing.T) {
	for _, tt := range []struct {
		name string
		err  error
	}{
		{"success", nil},
		{"not found", app.ErrNotFound},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			called := false
			router := newRouter(providerFunc(func(context.Context, string) (model.App, error) {
				called = true
				cancel()
				return exampleApp(), tt.err
			}), time.Second)
			w := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/api/apps/com.uchi.app", nil).WithContext(ctx)
			router.ServeHTTP(w, request)
			if !called {
				t.Fatal("provider was not called")
			}
			if w.Body.Len() != 0 || len(w.Header()) != 0 {
				t.Fatalf("response written after client cancellation: headers=%v body=%s", w.Header(), w.Body.String())
			}
		})
	}
}
