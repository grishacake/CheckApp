package app

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"app-grabber-backend/internal/model"
)

type catalogSource struct {
	card  model.App
	cards []model.App
	err   error
}

func (s catalogSource) GetApp(context.Context, string) (model.App, error) { return s.card, s.err }
func (s catalogSource) Search(context.Context, string, int) ([]model.App, error) {
	return s.cards, s.err
}

type saveCardsFunc func(context.Context, string, []model.App) error

func (f saveCardsFunc) GetFresh(context.Context, string, string, time.Duration) (model.App, error) {
	return model.App{}, ErrNotFound
}

func (f saveCardsFunc) Save(ctx context.Context, store string, cards []model.App) error {
	return f(ctx, store, cards)
}

func TestCatalogSavesFreshResults(t *testing.T) {
	first := model.App{PackageName: "com.example.first", Name: "First"}
	second := model.App{PackageName: "com.example.second", Name: "Second"}
	source := catalogSource{card: first, cards: []model.App{second, first}}
	var saved [][]model.App
	ctx := context.Background()
	repo := saveCardsFunc(func(gotCtx context.Context, store string, cards []model.App) error {
		if gotCtx != ctx || store != "google_play" {
			t.Fatal("context or store was lost")
		}
		saved = append(saved, append([]model.App(nil), cards...))
		return nil
	})
	catalog := NewCatalog(source, source, repo, "google_play", time.Hour)
	card, err := catalog.GetApp(ctx, first.PackageName)
	if err != nil || card != first {
		t.Fatalf("GetApp changed the parsed response: %v", err)
	}
	cards, err := catalog.Search(ctx, "example", 5)
	if err != nil || !reflect.DeepEqual(cards, source.cards) {
		t.Fatalf("Search changed result order: %v", err)
	}
	if !reflect.DeepEqual(saved, [][]model.App{{first}, source.cards}) {
		t.Fatalf("unexpected saves: %#v", saved)
	}
}

func TestCatalogDoesNotSaveFailedOrEmptyResults(t *testing.T) {
	for _, sourceErr := range []error{ErrNotFound, context.Canceled, errors.New("parser failed")} {
		source := catalogSource{err: sourceErr}
		repo := saveCardsFunc(func(context.Context, string, []model.App) error {
			t.Fatal("failed parsing must not write to the database")
			return nil
		})
		catalog := NewCatalog(source, source, repo, "google_play", time.Hour)
		if _, err := catalog.GetApp(context.Background(), "com.example.app"); !errors.Is(err, sourceErr) {
			t.Fatalf("GetApp lost source error: %v", err)
		}
		if _, err := catalog.Search(context.Background(), "example", 5); !errors.Is(err, sourceErr) {
			t.Fatalf("Search lost source error: %v", err)
		}
	}
	source := catalogSource{}
	catalog := NewCatalog(source, source, saveCardsFunc(func(context.Context, string, []model.App) error {
		t.Fatal("empty search must not write to the database")
		return nil
	}), "google_play", time.Hour)
	if cards, err := catalog.Search(context.Background(), "missing", 5); err != nil || len(cards) != 0 {
		t.Fatalf("empty search failed: %v", err)
	}
}

func TestCatalogPersistenceFailure(t *testing.T) {
	card := model.App{PackageName: "com.example.app", Name: "Example"}
	source := catalogSource{card: card, cards: []model.App{card}}
	storageErr := errors.New("database unavailable")
	catalog := NewCatalog(source, source, saveCardsFunc(func(context.Context, string, []model.App) error {
		return storageErr
	}), "google_play", time.Hour)
	got, err := catalog.GetApp(context.Background(), card.PackageName)
	if got != (model.App{}) || !errors.Is(err, ErrPersistence) || !errors.Is(err, storageErr) {
		t.Fatalf("GetApp did not report storage failure: %v", err)
	}
	cards, err := catalog.Search(context.Background(), "example", 5)
	if len(cards) != 0 || !errors.Is(err, ErrPersistence) || !errors.Is(err, storageErr) {
		t.Fatalf("Search did not report storage failure: %v", err)
	}
}
