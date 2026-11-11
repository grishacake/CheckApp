package rustore_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"app-grabber-backend/internal/app"
	"app-grabber-backend/internal/rustore"
)

func TestSearchSavedResponse(t *testing.T) {
	data, err := os.ReadFile("testdata/search.telegram.json")
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	client := rustore.Client{HTTP: &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host == "backapi.rustore.ru" {
			if r.URL.Query().Get("query") != "телеграм & telegram" || r.URL.Query().Get("pageSize") != "5" || r.Header.Get("ruStoreVerCode") != "1000" {
				t.Errorf("bad search request %s headers=%v", r.URL, r.Header)
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(data)))}, nil
		}
		status, body := 404, ""
		if r.URL.Path == "/catalog/app/org.telegram.messenger.web" {
			status = 200
			body = string(savedPage(t, "org.telegram.messenger.web"))
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}}
	cards, err := client.Search(context.Background(), "телеграм & telegram", 5)
	if err != nil || len(cards) != 1 || cards[0].PackageName != "org.telegram.messenger.web" || cards[0].Developer != "Telegram FZ-LLC" || calls != 6 {
		t.Fatalf("cards=%+v calls=%d err=%v", cards, calls, err)
	}
}

func TestSearchRejectsUpstreamErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"rate limited", 429, `{}`},
		{"bad JSON", 200, `{`},
		{"API error", 200, `{"code":"ERROR","body":{"content":[]}}`},
		{"missing content", 200, `{"code":"OK","body":{}}`},
		{"invalid IDs", 200, `{"code":"OK","body":{"content":[{"packageName":"../bad"}]}}`},
		{"large body", 200, strings.Repeat(" ", 8<<20) + "x"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := rustore.Client{HTTP: &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})}}
			if _, err := client.Search(context.Background(), "query", 5); err == nil || errors.Is(err, app.ErrNotFound) {
				t.Fatalf("search failure classified incorrectly: %v", err)
			}
		})
	}
}

func TestSearchEmptyAndCancellation(t *testing.T) {
	client := rustore.Client{HTTP: &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		if err := r.Context().Err(); err != nil {
			return nil, err
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"code":"OK","body":{"content":[]}}`))}, nil
	})}}
	cards, err := client.Search(context.Background(), "nothing", 5)
	if err != nil || len(cards) != 0 {
		t.Fatalf("empty result: %v %v", cards, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Search(ctx, "query", 5); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled search: %v", err)
	}
}
