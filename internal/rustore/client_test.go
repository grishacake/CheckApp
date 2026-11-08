package rustore_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"app-grabber-backend/internal/app"
	"app-grabber-backend/internal/rustore"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestClientGetApp(t *testing.T) {
	const id = "com.example.calculatorKSA1211"
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
