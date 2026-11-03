package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"app-grabber-backend/internal/model"
)

var (
	ErrPersistence = errors.New("app persistence failed")
	ErrCacheRead   = errors.New("app cache read failed")
)

const DefaultCacheTTL = 24 * time.Hour

// DefaultSearchTTL — максимальный возраст карточки для полнотекстового поиска по локальной базе.
// Карточки, обновлённые позже этого срока, не попадают в результаты — поиск уходит в Google Play.
const DefaultSearchTTL = 7 * 24 * time.Hour

func ParseCacheTTL(value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return DefaultCacheTTL, nil
	}
	ttl, err := time.ParseDuration(value)
	if err != nil || ttl < 0 {
		return 0, errors.New("APP_CACHE_TTL must be a non-negative duration, for example 24h or 0")
	}
	return ttl, nil
}

type CardRepository interface {
	Save(ctx context.Context, storeCode string, cards []model.App) error
	GetFresh(ctx context.Context, storeCode, packageName string, ttl time.Duration) (model.App, error)
	// SearchByText ищет карточки по полнотекстовому индексу среди записей,
	// обновлённых не позднее maxAge назад. Ничего не нашлось — пустой список без ошибки.
	SearchByText(ctx context.Context, storeCode, query string, limit int, maxAge time.Duration) ([]model.App, error)
}

// Catalog serves fresh stored cards and refreshes cache misses from the provider.
// Text searches first try a full-text search of the local database; when no
// recent results are found the search falls through to the external provider.
type Catalog struct {
	provider  Provider
	searcher  Searcher
	cards     CardRepository
	store     string
	cacheTTL  time.Duration
	searchTTL time.Duration
}

func NewCatalog(provider Provider, searcher Searcher, cards CardRepository, storeCode string, cacheTTL time.Duration) *Catalog {
	return &Catalog{provider: provider, searcher: searcher, cards: cards, store: storeCode, cacheTTL: cacheTTL, searchTTL: DefaultSearchTTL}
}

// WithSearchTTL задаёт максимальный возраст карточки для полнотекстового поиска.
func (c *Catalog) WithSearchTTL(d time.Duration) *Catalog {
	c.searchTTL = d
	return c
}

func (c *Catalog) GetApp(ctx context.Context, packageName string) (model.App, error) {
	if err := ctx.Err(); err != nil {
		return model.App{}, err
	}
	if c.cacheTTL > 0 {
		card, err := c.cards.GetFresh(ctx, c.store, packageName, c.cacheTTL)
		if err == nil {
			return card, nil
		}
		if !errors.Is(err, ErrNotFound) {
			return model.App{}, fmt.Errorf("%w: %w", ErrCacheRead, err)
		}
	}
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
	// Шаг 1: полнотекстовый поиск по локальной базе среди свежих карточек.
	if c.searchTTL > 0 {
		local, err := c.cards.SearchByText(ctx, c.store, query, limit, c.searchTTL)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrCacheRead, err)
		}
		if len(local) > 0 {
			return local, nil
		}
	}

	// Шаг 2: ничего не нашлось — ищем во внешнем источнике (Google Play).
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
