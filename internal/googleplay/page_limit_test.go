package googleplay

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type pageTransport struct{ body string }

func (p pageTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode:    http.StatusOK,
		Status:        "200 OK",
		Header:        make(http.Header),
		Body:          io.NopCloser(strings.NewReader(p.body)),
		ContentLength: -1,
		Request:       r,
	}, nil
}

func TestGetPageSizeLimit(t *testing.T) {
	for _, tt := range []struct {
		name      string
		size      int
		wantError bool
	}{
		{"below limit", maxPageSize - 1, false},
		{"at limit", maxPageSize, false},
		{"above limit", maxPageSize + 1, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			content := strings.Repeat("x", tt.size)
			c := newTestClientWith(pageTransport{body: content})
			body, err := c.get(context.Background(), detailURL)
			if tt.wantError {
				if err == nil {
					t.Fatal("oversized page accepted without an error")
				}
				if body != nil {
					t.Fatal("returned a truncated page alongside the error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if string(body) != content {
				t.Fatal("page within limit was not returned intact")
			}
		})
	}
}

func TestOversizedPagesReturnUnavailable(t *testing.T) {
	for _, operation := range []string{"search", "details"} {
		t.Run(operation, func(t *testing.T) {
			// Данные после лимита нельзя принять за успешную пустую страницу.
			content := strings.Repeat(" ", maxPageSize) + `<a href="/store/apps/details?id=com.uchi.app">App</a>`
			c := newTestClientWith(pageTransport{body: content})
			var err error
			if operation == "search" {
				_, err = c.Search(context.Background(), "учи ру", 5)
			} else {
				_, err = c.GetApp(context.Background(), "com.uchi.app")
			}
			if !errors.Is(err, ErrUnavailable) {
				t.Fatalf("error = %v, want ErrUnavailable", err)
			}
		})
	}
}
