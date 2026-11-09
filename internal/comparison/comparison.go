package comparison

import (
	"context"
	"errors"
	"log"
	"time"

	"app-grabber-backend/internal/app"
	"app-grabber-backend/internal/model"
)

type Pair struct {
	GooglePlay string `json:"google_play"`
	RuStore    string `json:"rustore"`
}

type Entry struct {
	Pair
	SavedAt time.Time `json:"saved_at"`
}

type Result struct {
	Status    string     `json:"status"`
	App       *model.App `json:"app,omitempty"`
	FetchedAt *time.Time `json:"fetched_at,omitempty"`
	Stale     bool       `json:"stale"`
}

type Response struct {
	GooglePlay Result `json:"google_play"`
	RuStore    Result `json:"rustore"`
}

type Cache interface {
	Lookup(ctx context.Context, store, id string) (model.App, time.Time, error)
	Save(context.Context, string, []model.App) error
}

type Service struct {
	GooglePlay, RuStore app.Provider
	Cache               Cache
	TTL, StoreTimeout   time.Duration
}

func (s *Service) Compare(ctx context.Context, p Pair) (Response, error) {
	type outcome struct {
		result Result
		err    error
	}
	google := make(chan outcome, 1)
	go func() {
		result, err := s.lookup(ctx, "google_play", p.GooglePlay, s.GooglePlay)
		google <- outcome{result, err}
	}()
	ru, err := s.lookup(ctx, "rustore", p.RuStore, s.RuStore)
	gp := <-google
	return Response{GooglePlay: gp.result, RuStore: ru}, errors.Join(gp.err, err)
}

func (s *Service) lookup(ctx context.Context, store, id string, provider app.Provider) (Result, error) {
	cached, at, err := s.Cache.Lookup(ctx, store, id)
	hit := err == nil
	if !hit && !errors.Is(err, app.ErrNotFound) {
		return Result{}, err
	}
	age := time.Since(at)
	if hit && age >= 0 && age < s.TTL {
		return Result{Status: "ok", App: &cached, FetchedAt: &at}, nil
	}
	remote, cancel := context.WithTimeout(ctx, s.StoreTimeout)
	defer cancel()
	card, err := provider.GetApp(remote, id)
	if ctx.Err() != nil {
		return Result{}, ctx.Err()
	}
	if errors.Is(err, app.ErrNotFound) {
		return Result{Status: "not_found"}, nil
	}
	if err != nil {
		log.Printf("compare store=%q package_name=%q: %v", store, id, err)
		if hit {
			return Result{Status: "ok", App: &cached, FetchedAt: &at, Stale: true}, nil
		}
		return Result{Status: "unavailable"}, nil
	}
	fetched := time.Now().UTC()
	if err := s.Cache.Save(ctx, store, []model.App{card}); err != nil {
		return Result{}, err
	}
	return Result{Status: "ok", App: &card, FetchedAt: &fetched}, nil
}
