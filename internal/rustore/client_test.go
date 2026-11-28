package rustore_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"app-grabber-backend/internal/app"
	"app-grabber-backend/internal/rustore"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestClientGetApp(t *testing.T) {
	const id = "ru.ozon.app.android"
	for _, status := range []int{200, 404, 429, 503} {
		client := rustore.Client{HTTP: &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
			if r.URL.String() != "https://www.rustore.ru/catalog/app/"+id || r.Method != "GET" {
				t.Fatalf("unexpected request %v", r)
			}
			var body string
			if status == 200 {
				body = string(savedPage(t, id))
			}
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}, nil
		})}}
		card, err := client.GetApp(context.Background(), id)
		if (status == 200) != (err == nil) || errors.Is(err, app.ErrNotFound) != (status == 404) || (status == 200 && card.PackageName != id) {
			t.Fatalf("HTTP %d: %+v %v", status, card, err)
		}
	}
}

func TestClientRejectsRedirects(t *testing.T) {
	for _, status := range []int{301, 302, 303, 307, 308} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			calls, redirectCalls := 0, 0
			httpClient := &http.Client{
				CheckRedirect: func(*http.Request, []*http.Request) error {
					redirectCalls++
					return nil
				},
				Transport: transport(func(r *http.Request) (*http.Response, error) {
					calls++
					code := status
					if r.URL.Path == "/missing" {
						code = http.StatusNotFound
					}
					return &http.Response{
						StatusCode: code,
						Header:     http.Header{"Location": []string{"/missing"}},
						Body:       io.NopCloser(strings.NewReader("")),
					}, nil
				}),
			}
			_, err := (rustore.Client{HTTP: httpClient}).GetApp(context.Background(), "ru.ozon.app.android")
			if err == nil || errors.Is(err, app.ErrNotFound) {
				t.Fatalf("redirect must be an upstream error, got %v", err)
			}
			if calls != 1 || redirectCalls != 0 {
				t.Fatalf("requests = %d, redirects = %d", calls, redirectCalls)
			}
			if err := httpClient.CheckRedirect(nil, nil); err != nil || redirectCalls != 1 {
				t.Fatal("supplied client's redirect policy was changed")
			}
		})
	}
}

func TestClientRejectsInvalidPackageName(t *testing.T) {
	client := rustore.Client{HTTP: &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid package name reached transport")
		return nil, nil
	})}}
	if _, err := client.GetApp(context.Background(), "com.example/invalid"); err == nil {
		t.Fatal("invalid package name accepted")
	}
}

func TestClientRecognizesUnavailablePageWithHTTP200(t *testing.T) {
	const id = "com.axlebolt.standoff2"
	client := rustore.Client{HTTP: &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(savedPage(t, id+".unavailable"))))}, nil
	})}}
	_, err := client.GetApp(context.Background(), id)
	if !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("HTTP 200 stub: got %v, want ErrNotFound", err)
	}
}
