package app

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"app-grabber-backend/internal/model"
)

type StoreRef struct {
	Store       string `json:"store"`
	PackageName string `json:"package_name,omitempty"`
	Status      string `json:"status"`
}

type SearchResult struct {
	model.App
	Stores []StoreRef `json:"stores"`
}

type SearchSource struct {
	Store  string `json:"store"`
	Status string `json:"status"`
}

type StoreSearch struct {
	Searchers     map[string]Searcher
	Providers     map[string]Provider
	SearchTimeout time.Duration
	CheckTimeout  time.Duration
}

func NewStoreSearch(searchers map[string]Searcher, providers map[string]Provider) *StoreSearch {
	return &StoreSearch{Searchers: searchers, Providers: providers, SearchTimeout: 9 * time.Second, CheckTimeout: 2 * time.Second}
}

func ResultsForStore(store string, cards []model.App) []SearchResult {
	results := make([]SearchResult, 0, len(cards))
	for _, card := range cards {
		refs := []StoreRef{{Store: "google_play", Status: "unknown"}, {Store: "rustore", Status: "unknown"}}
		for i := range refs {
			if refs[i].Store == store {
				refs[i].PackageName = card.PackageName
				refs[i].Status = "ok"
			}
		}
		results = append(results, SearchResult{App: card, Stores: refs})
	}
	return results
}

func (s *StoreSearch) Search(ctx context.Context, query string, stores []string, limit int) ([]SearchResult, []SearchSource, error) {
	type outcome struct {
		index int
		cards []model.App
		err   error
	}
	responses := make(chan outcome, len(stores))
	for i, store := range stores {
		go func() {
			remote, cancel := context.WithTimeout(ctx, s.SearchTimeout)
			defer cancel()
			searcher := s.Searchers[store]
			if searcher == nil {
				responses <- outcome{index: i, err: ErrNotConfigured}
				return
			}
			cards, err := searcher.Search(remote, query, limit)
			if remote.Err() != nil {
				err = remote.Err()
			}
			responses <- outcome{index: i, cards: cards, err: err}
		}()
	}
	outcomes := make([]outcome, len(stores))
	for range stores {
		res := <-responses
		outcomes[res.index] = res
	}
	if ctx.Err() != nil {
		return nil, nil, ctx.Err()
	}
	results := make([]SearchResult, 0)
	sources := make([]SearchSource, 0, len(stores))
	positions := make(map[string]int)
	var failures []error
	successful := 0
	for i, res := range outcomes {
		store := stores[i]
		if res.err != nil {
			log.Printf("search store=%q: %v", store, res.err)
			if errors.Is(res.err, ErrCacheRead) || errors.Is(res.err, ErrPersistence) {
				return nil, nil, res.err
			}
			failures = append(failures, res.err)
			sources = append(sources, SearchSource{Store: store, Status: "unavailable"})
			continue
		}
		successful++
		sources = append(sources, SearchSource{Store: store, Status: "ok"})
		for _, result := range ResultsForStore(store, res.cards) {
			if pos, exists := positions[result.PackageName]; exists {
				for j, ref := range result.Stores {
					if ref.Status == "ok" {
						results[pos].Stores[j] = ref
					}
				}
				continue
			}
			positions[result.PackageName] = len(results)
			results = append(results, result)
		}
	}
	if successful == 0 {
		return nil, sources, errors.Join(failures...)
	}
	s.Enrich(ctx, results)
	return results, sources, nil
}

// Enrich verifies the same ID in the other store. Different IDs need an explicit mapping.
// Optional checks have one shared deadline and never discard successful search results.
func (s *StoreSearch) Enrich(ctx context.Context, results []SearchResult) {
	ctx, cancel := context.WithTimeout(ctx, s.CheckTimeout)
	defer cancel()
	type check struct{ result, store int }
	jobs := make(chan check, len(results)*2)
	for i, result := range results {
		for j, ref := range result.Stores {
			if ref.Status == "unknown" && s.Providers[ref.Store] != nil {
				jobs <- check{i, j}
			}
		}
	}
	close(jobs)
	var workers sync.WaitGroup
	for range min(4, len(jobs)) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for job := range jobs {
				if ctx.Err() != nil {
					return
				}
				result := &results[job.result]
				ref := &result.Stores[job.store]
				card, err := s.Providers[ref.Store].GetApp(ctx, result.PackageName)
				// A missing candidate ID may mean that the app uses a different ID.
				switch {
				case err == nil && card.PackageName == result.PackageName:
					ref.PackageName = result.PackageName
					ref.Status = "ok"
				case err != nil && !errors.Is(err, ErrNotFound):
					log.Printf("check search store=%q package=%q: %v", ref.Store, result.PackageName, err)
				}
			}
		}()
	}
	workers.Wait()
}
