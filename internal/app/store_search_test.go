package app

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"app-grabber-backend/internal/model"
)

type storeSearchFunc func(context.Context, string, int) ([]model.App, error)

func (f storeSearchFunc) Search(ctx context.Context, q string, n int) ([]model.App, error) {
	return f(ctx, q, n)
}

type storeProviderFunc func(context.Context, string) (model.App, error)

func (f storeProviderFunc) GetApp(ctx context.Context, id string) (model.App, error) {
	return f(ctx, id)
}

func TestStoreSearchMergeAndKeepDifferentIDs(t *testing.T) {
	gp := []model.App{{PackageName: "com.Same", Name: "Google data"}, {PackageName: "org.telegram.messenger", Name: "Telegram"}}
	ru := []model.App{{PackageName: "com.Same", Name: "RuStore data"}, {PackageName: "org.telegram.messenger.web", Name: "Telegram"}}
	service := NewStoreSearch(map[string]Searcher{"google_play": catalogSource{cards: gp}, "rustore": catalogSource{cards: ru}}, nil)
	results, sources, err := service.Search(context.Background(), "query", []string{"google_play", "rustore"}, 5)
	if err != nil || len(results) != 3 || len(sources) != 2 {
		t.Fatalf("results=%+v sources=%+v err=%v", results, sources, err)
	}
	if results[0].Name != "Google data" || results[0].Stores[0].Status != "ok" || results[0].Stores[1].Status != "ok" {
		t.Fatal(results[0])
	}
	if results[1].Stores[1].Status != "unknown" || results[2].Stores[0].Status != "unknown" {
		t.Fatal(results)
	}
}

func TestStoreSearchRunsInParallelAndReturnsPartial(t *testing.T) {
	started := make(chan struct{})
	service := NewStoreSearch(map[string]Searcher{
		"google_play": storeSearchFunc(func(ctx context.Context, _ string, _ int) ([]model.App, error) {
			close(started)
			<-ctx.Done()
			return nil, fmt.Errorf("%w: %w", ErrPersistence, ctx.Err())
		}),
		"rustore": storeSearchFunc(func(ctx context.Context, _ string, _ int) ([]model.App, error) {
			select {
			case <-started:
				return []model.App{{PackageName: "ru.App", Name: "card"}}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}),
	}, nil)
	service.SearchTimeout = 100 * time.Millisecond
	results, sources, err := service.Search(context.Background(), "q", []string{"google_play", "rustore"}, 5)
	if err != nil || len(results) != 1 || !reflect.DeepEqual(sources, []SearchSource{{Store: "google_play", Status: "unavailable"}, {Store: "rustore", Status: "ok"}}) {
		t.Fatalf("%+v %+v %v", results, sources, err)
	}
}

func TestStoreSearchEnrichment(t *testing.T) {
	service := NewStoreSearch(nil, map[string]Provider{"rustore": storeProviderFunc(func(ctx context.Context, id string) (model.App, error) {
		switch id {
		case "com.exists":
			return model.App{PackageName: id}, nil
		case "com.missing":
			return model.App{}, ErrNotFound
		default:
			<-ctx.Done()
			return model.App{}, ctx.Err()
		}
	})})
	service.CheckTimeout = 50 * time.Millisecond
	results := ResultsForStore("google_play", []model.App{{PackageName: "com.exists"}, {PackageName: "com.missing"}, {PackageName: "com.slow"}})
	service.Enrich(context.Background(), results)
	for i, want := range []string{"ok", "unknown", "unknown"} {
		if results[i].Stores[1].Status != want {
			t.Fatalf("%+v", results)
		}
	}
	if results[0].Stores[1].PackageName != "com.exists" || results[1].Stores[1].PackageName != "" {
		t.Fatal(results)
	}
}

func TestStoreSearchErrorsAndEmptySuccess(t *testing.T) {
	failure := errors.New("offline")
	for _, tc := range []struct {
		name         string
		gpErr, ruErr error
		wantErr      error
	}{
		{"both fail", failure, failure, failure},
		{"empty success", nil, failure, nil},
		{"cache error", ErrCacheRead, nil, ErrCacheRead},
		{"save error", nil, ErrPersistence, ErrPersistence},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := NewStoreSearch(map[string]Searcher{"google_play": catalogSource{err: tc.gpErr}, "rustore": catalogSource{err: tc.ruErr}}, nil)
			results, _, err := service.Search(context.Background(), "q", []string{"google_play", "rustore"}, 5)
			if !errors.Is(err, tc.wantErr) || (tc.wantErr == nil && (results == nil || len(results) != 0)) {
				t.Fatalf("%+v %v", results, err)
			}
		})
	}
}
