package apkpure

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"app-grabber-backend/internal/app"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestGetAppUpstreamStatus(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   error
	}{
		{http.StatusNotFound, app.ErrNotFound},
		{http.StatusGone, app.ErrNotFound},
		{http.StatusForbidden, nil},
		{http.StatusTooManyRequests, nil},
		{http.StatusOK, ErrInvalidPage},
	} {
		client := Client{HTTP: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.String() != "https://apkpure.com/p/com.example.Missing" {
				t.Errorf("request URL = %s", r.URL)
			}
			return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(""))}, nil
		})}}
		_, err := client.GetApp(context.Background(), "com.example.Missing")
		if err == nil || (tc.want != nil && !errors.Is(err, tc.want)) || (tc.want == nil && errors.Is(err, app.ErrNotFound)) {
			t.Errorf("status %d: got error %v, want %v or upstream error", tc.status, err, tc.want)
		}
	}
}

func TestGetAppFollowsOnlyAPKPureCardRedirects(t *testing.T) {
	const id = "org.telegram.messenger"
	page := savedPage(t, id)
	for _, tc := range []struct {
		name, location string
		wantOK         bool
	}{
		{"canonical card", "/telegram/" + id, true},
		{"other host", "https://example.org/telegram/" + id, false},
		{"different package", "https://apkpure.com/p/com.whatsapp", false},
		{"downgrade to HTTP", "http://apkpure.com/telegram/" + id, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := Client{HTTP: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return &http.Response{
						StatusCode: http.StatusMovedPermanently,
						Header:     http.Header{"Location": {tc.location}},
						Body:       http.NoBody,
					}, nil
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(page))}, nil
			})}}
			card, err := client.GetApp(context.Background(), id)
			if tc.wantOK {
				if err != nil || card.PackageName != id || calls != 2 {
					t.Fatalf("card=%+v err=%v calls=%d", card, err, calls)
				}
				return
			}
			if err == nil || errors.Is(err, app.ErrNotFound) || calls != 1 {
				t.Fatalf("unexpected redirect result err=%v calls=%d", err, calls)
			}
		})
	}
}
