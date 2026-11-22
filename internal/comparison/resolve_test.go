package comparison

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"app-grabber-backend/internal/app"
	"app-grabber-backend/internal/model"
)

type resolveProvider func(context.Context, string) (model.App, error)

func (f resolveProvider) GetApp(ctx context.Context, id string) (model.App, error) { return f(ctx, id) }

func TestResolveBoundsConcurrencyAndReturnsEveryItem(t *testing.T) {
	var active, maximum atomic.Int32
	provider := resolveProvider(func(ctx context.Context, id string) (model.App, error) {
		n := active.Add(1)
		defer active.Add(-1)
		for old := maximum.Load(); n > old && !maximum.CompareAndSwap(old, n); old = maximum.Load() {
		}
		<-ctx.Done()
		return model.App{}, ctx.Err()
	})
	s := Service{GooglePlay: provider, RuStore: provider, Cache: cachedCard{hit: true}, TTL: time.Hour, StoreTimeout: 30 * time.Millisecond}
	items := make([]SavedItem, 30)
	for i := range items {
		items[i] = SavedItem{Item: Item{Store: "rustore", PackageName: fmt.Sprintf("com.app%d", i)}}
	}
	start := time.Now()
	results, err := s.Resolve(context.Background(), items)
	if err != nil || len(results) != len(items) {
		t.Fatalf("%d results: %v", len(results), err)
	}
	if maximum.Load() > 6 || maximum.Load() < 2 {
		t.Fatalf("parallel calls=%d", maximum.Load())
	}
	if time.Since(start) > time.Second {
		t.Fatal("network timeout was restarted for each batch")
	}
	for i, result := range results {
		if result.Item != items[i].Item || result.Status != "ok" || !result.Stale || result.App == nil {
			t.Fatal(i, result)
		}
	}
}

func TestResolvePerItemFailures(t *testing.T) {
	provider := resolveProvider(func(_ context.Context, id string) (model.App, error) {
		switch id {
		case "com.missing":
			return model.App{}, app.ErrNotFound
		case "com.down":
			return model.App{}, errors.New("store down")
		}
		return model.App{PackageName: id, Name: "card"}, nil
	})
	s := Service{GooglePlay: provider, RuStore: provider, Cache: cachedCard{}, TTL: time.Hour, StoreTimeout: time.Second}
	items := []SavedItem{{Item: Item{"google_play", "com.ok"}}, {Item: Item{"rustore", "com.missing"}}, {Item: Item{"rustore", "com.down"}}}
	results, err := s.Resolve(context.Background(), items)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"ok", "not_found", "unavailable"} {
		if results[i].Status != want {
			t.Fatal(results)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = s.Resolve(ctx, items); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

func TestResolveSlowItemsDoNotStarveHealthyItem(t *testing.T) {
	for _, stores := range [][2]string{{"google_play", "rustore"}, {"rustore", "google_play"}, {"google_play", "google_play"}, {"rustore", "rustore"}} {
		t.Run(stores[0]+"_then_"+stores[1], func(t *testing.T) {
			var calls atomic.Int32
			provider := resolveProvider(func(ctx context.Context, id string) (model.App, error) {
				if id == "com.healthy" {
					if err := ctx.Err(); err != nil {
						return model.App{}, err
					}
					calls.Add(1)
					return model.App{PackageName: id, Name: "healthy"}, nil
				}
				<-ctx.Done()
				return model.App{}, ctx.Err()
			})
			s := Service{GooglePlay: provider, RuStore: provider, Cache: cachedCard{}, TTL: time.Hour, StoreTimeout: 300 * time.Millisecond}
			items := make([]SavedItem, 7)
			for i := 0; i < 6; i++ {
				items[i] = SavedItem{Item: Item{Store: stores[0], PackageName: fmt.Sprintf("com.slow%d", i)}}
			}
			items[6] = SavedItem{Item: Item{Store: stores[1], PackageName: "com.healthy"}}
			for attempt := 0; attempt < 2; attempt++ {
				results, err := s.Resolve(context.Background(), items)
				if err != nil {
					t.Fatal(err)
				}
				for i, result := range results {
					if result.Item != items[i].Item {
						t.Fatal("result order changed")
					}
				}
				if results[6].Status != "ok" || results[6].App == nil {
					t.Fatalf("attempt %d: healthy app was blocked: %+v", attempt+1, results[6])
				}
			}
			if calls.Load() != 2 {
				t.Fatalf("healthy calls=%d, want 2", calls.Load())
			}
		})
	}
}
