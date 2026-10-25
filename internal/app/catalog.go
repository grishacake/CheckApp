package app

import (
	"context"
	"errors"
	"fmt"

	"app-grabber-backend/internal/model"
)

var ErrPersistence = errors.New("app persistence failed")

type CardRepository interface {
	Save(ctx context.Context, storeCode string, cards []model.App) error
}

// Catalog saves freshly parsed cards. It does not serve cached data yet.
type Catalog struct {
	provider Provider
	searcher Searcher
	cards    CardRepository
	store    string
}

func NewCatalog(provider Provider, searcher Searcher, cards CardRepository, storeCode string) *Catalog {
	return &Catalog{provider: provider, searcher: searcher, cards: cards, store: storeCode}
}

func (c *Catalog) GetApp(ctx context.Context, packageName string) (model.App, error) {
	card, err := c.provider.GetApp(ctx, packageName)
	if err != nil {
		return model.App{}, err
	}
	if err := c.cards.Save(ctx, c.store, []model.App{card}); err != nil {
		return model.App{}, fmt.Errorf("%w: %w", ErrPersistence, err)
	}
	return card, nil
}

func (c *Catalog) Search(ctx context.Context, query string, limit int) ([]model.App, error) {
	cards, err := c.searcher.Search(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	if len(cards) > 0 {
		if err := c.cards.Save(ctx, c.store, cards); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrPersistence, err)
		}
	}
	return cards, nil
}
