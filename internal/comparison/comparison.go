package comparison

import (
	"context"
	"errors"
	"log"
	"time"

	"app-grabber-backend/internal/app"
	"app-grabber-backend/internal/model"
)

type Result struct {
	Status    string     `json:"status"`
	App       *model.App `json:"app,omitempty"`
	FetchedAt *time.Time `json:"fetched_at,omitempty"`
	Stale     bool       `json:"stale"`
}

type Cache interface {
	Lookup(ctx context.Context, store, id string) (model.App, time.Time, error)
	Save(context.Context, string, []model.App) error
}

type Service struct {
	Providers         map[string]app.Provider
	Cache             Cache
	TTL, StoreTimeout time.Duration
}

// Supports проверяет подключение провайдера; реестр заполняется до запуска сервера.
func (s *Service) Supports(store string) bool {
	return s.Providers[store] != nil
}

func (s *Service) lookupWithin(ctx, network context.Context, store, id string, provider app.Provider) (Result, error) {
	cached, at, err := s.Cache.Lookup(ctx, store, id)
	hit := err == nil
	if !hit && !errors.Is(err, app.ErrNotFound) {
		return Result{}, err
	}
	age := time.Since(at)
	if hit && age >= 0 && age < s.TTL {
		return Result{Status: "ok", App: &cached, FetchedAt: &at}, nil
	}
	remote, cancel := context.WithTimeout(network, s.StoreTimeout)
	defer cancel()
	var card model.App
	err = remote.Err()
	if err == nil {
		if provider == nil {
			err = app.ErrNotConfigured
		} else {
			card, err = provider.GetApp(remote, id)
		}
	}
	if ctx.Err() != nil {
		return Result{}, ctx.Err()
	}
	if remote.Err() != nil {
		err = remote.Err()
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
