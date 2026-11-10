package comparison

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"testing"
	"time"

	"app-grabber-backend/internal/app"
	"app-grabber-backend/internal/model"
)

type failedProvider struct{ err error }

func (p failedProvider) GetApp(context.Context, string) (model.App, error) {
	return model.App{}, p.err
}

type cachedCard struct{ hit bool }

func (c cachedCard) Lookup(_ context.Context, _, id string) (model.App, time.Time, error) {
	if !c.hit {
		return model.App{}, time.Time{}, app.ErrNotFound
	}
	return model.App{PackageName: id, Name: "cached"}, time.Now().Add(-48 * time.Hour), nil
}

func (cachedCard) Save(context.Context, string, []model.App) error { return nil }

func TestStoreFailureDiagnostics(t *testing.T) {
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previous) })
	for _, tc := range []struct {
		name, status string
		hit          bool
		err          error
	}{
		{"cold cache", "unavailable", false, errors.New("invalid rustore page: missing application data")},
		{"stale cache", "ok", true, context.DeadlineExceeded},
		{"not found", "not_found", true, app.ErrNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output.Reset()
			s := Service{Cache: cachedCard{tc.hit}, TTL: 24 * time.Hour, StoreTimeout: time.Second}
			result, err := s.lookup(context.Background(), "rustore", "ru.example.app", failedProvider{tc.err})
			if err != nil || result.Status != tc.status || result.Stale != (tc.status == "ok") {
				t.Fatalf("result = %+v, error = %v", result, err)
			}
			if errors.Is(tc.err, app.ErrNotFound) {
				if output.Len() != 0 {
					t.Fatalf("not_found logged as outage: %s", &output)
				}
				return
			}
			for _, want := range []string{`store="rustore"`, `package_name="ru.example.app"`, tc.err.Error()} {
				if !strings.Contains(output.String(), want) {
					t.Errorf("log %q does not contain %q", output.String(), want)
				}
			}
		})
	}
}
