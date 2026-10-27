package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"app-grabber-backend/internal/model"
)

type cacheCardsStub struct {
	get  func(context.Context, string, string, time.Duration) (model.App, error)
	save saveCardsFunc
}

func (s cacheCardsStub) GetFresh(ctx context.Context, store, packageName string, ttl time.Duration) (model.App, error) {
	return s.get(ctx, store, packageName, ttl)
}
func (s cacheCardsStub) Save(ctx context.Context, store string, cards []model.App) error {
	return s.save(ctx, store, cards)
}

type catalogGetFunc func(context.Context, string) (model.App, error)

func (f catalogGetFunc) GetApp(ctx context.Context, packageName string) (model.App, error) {
	return f(ctx, packageName)
}

func TestParseCacheTTL(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  time.Duration
	}{
		{"", 24 * time.Hour}, {"  ", 24 * time.Hour},
		{"24h", 24 * time.Hour}, {" 5m ", 5 * time.Minute}, {"0", 0},
	} {
		got, err := ParseCacheTTL(tc.value)
		if err != nil || got != tc.want {
			t.Fatalf("ParseCacheTTL(%q) = %v, %v", tc.value, got, err)
		}
	}
	for _, value := range []string{"-1h", "tomorrow", "1d"} {
		if _, err := ParseCacheTTL(value); err == nil {
			t.Fatalf("invalid TTL accepted: %q", value)
		}
	}
}

func TestCatalogFreshCacheSkipsProviderAndSave(t *testing.T) {
	card := model.App{PackageName: "com.example.app", Name: "Cached"}
	repo := cacheCardsStub{
		get: func(_ context.Context, store, packageName string, ttl time.Duration) (model.App, error) {
			if store != "google_play" || packageName != card.PackageName || ttl != time.Hour {
				t.Fatal("wrong cache lookup parameters")
			}
			return card, nil
		},
		save: func(context.Context, string, []model.App) error {
			t.Fatal("cache hit must not change scraped_at")
			return nil
		},
	}
	provider := catalogGetFunc(func(context.Context, string) (model.App, error) {
		t.Fatal("fresh cache must not contact Google Play")
		return model.App{}, nil
	})
	catalog := NewCatalog(provider, nil, repo, "google_play", time.Hour)
	got, err := catalog.GetApp(context.Background(), card.PackageName)
	if err != nil || got != card {
		t.Fatalf("cached result changed: %v", err)
	}
}

func TestCatalogCacheReadFailureDoesNotContactProvider(t *testing.T) {
	storageErr := errors.New("database unavailable")
	repo := cacheCardsStub{get: func(context.Context, string, string, time.Duration) (model.App, error) {
		return model.App{}, storageErr
	}}
	provider := catalogGetFunc(func(context.Context, string) (model.App, error) {
		t.Fatal("storage errors must not be mistaken for cache misses")
		return model.App{}, nil
	})
	catalog := NewCatalog(provider, nil, repo, "google_play", time.Hour)
	got, err := catalog.GetApp(context.Background(), "com.example.app")
	if got != (model.App{}) || !errors.Is(err, ErrCacheRead) || !errors.Is(err, storageErr) {
		t.Fatalf("cache error was lost: %v", err)
	}
}

func TestCatalogDisabledCacheStillSaves(t *testing.T) {
	card := model.App{PackageName: "com.example.app", Name: "Fresh"}
	saves := 0
	repo := cacheCardsStub{
		get: func(context.Context, string, string, time.Duration) (model.App, error) {
			t.Fatal("disabled cache must not be read")
			return model.App{}, nil
		},
		save: func(_ context.Context, _ string, cards []model.App) error {
			saves++
			if len(cards) != 1 || cards[0] != card {
				t.Fatal("wrong parsed card saved")
			}
			return nil
		},
	}
	source := catalogSource{card: card}
	catalog := NewCatalog(source, source, repo, "google_play", 0)
	got, err := catalog.GetApp(context.Background(), card.PackageName)
	if err != nil || got != card || saves != 1 {
		t.Fatalf("disabled cache prevented normal parsing and saving: %v", err)
	}
}

func TestCatalogSearchDoesNotUseCardCache(t *testing.T) {
	card := model.App{PackageName: "com.example.app", Name: "Fresh search result"}
	source := catalogSource{cards: []model.App{card}}
	saves := 0
	repo := cacheCardsStub{
		get: func(context.Context, string, string, time.Duration) (model.App, error) {
			t.Fatal("text search must not be limited to cached cards")
			return model.App{}, nil
		},
		save: func(context.Context, string, []model.App) error { saves++; return nil },
	}
	catalog := NewCatalog(source, source, repo, "google_play", time.Hour)
	cards, err := catalog.Search(context.Background(), "example", 5)
	if err != nil || len(cards) != 1 || cards[0] != card || saves != 1 {
		t.Fatalf("text search stopped parsing and saving: %v", err)
	}
}

func TestCatalogMissRefreshesThenServesCache(t *testing.T) {
	card := model.App{PackageName: "com.example.app", Name: "Fresh"}
	var stored model.App
	parses, saves := 0, 0
	repo := cacheCardsStub{
		get: func(context.Context, string, string, time.Duration) (model.App, error) {
			if stored.PackageName == "" {
				return model.App{}, ErrNotFound
			}
			return stored, nil
		},
		save: func(_ context.Context, _ string, cards []model.App) error {
			saves++
			stored = cards[0]
			return nil
		},
	}
	provider := catalogGetFunc(func(context.Context, string) (model.App, error) {
		parses++
		return card, nil
	})
	catalog := NewCatalog(provider, nil, repo, "google_play", time.Hour)
	for i := 0; i < 2; i++ {
		if got, err := catalog.GetApp(context.Background(), card.PackageName); err != nil || got != card {
			t.Fatalf("request %d: %v", i, err)
		}
	}
	if parses != 1 || saves != 1 {
		t.Fatalf("repeated request reparsed or rewrote the card: parses=%d saves=%d", parses, saves)
	}
}

func TestCatalogCanceledRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	catalog := NewCatalog(nil, nil, nil, "google_play", time.Hour)
	if _, err := catalog.GetApp(ctx, "com.example.app"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled request reached dependencies: %v", err)
	}
}
