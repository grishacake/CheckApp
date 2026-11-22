package comparison

import (
	"context"
	"errors"
	"sync"
)

// Resolve ограничивает параллелизм и общий срок сетевых запросов ко всем магазинам.
func (s *Service) Resolve(ctx context.Context, items []SavedItem) ([]ItemResult, error) {
	results := make([]ItemResult, len(items))
	errs := make([]error, len(items))
	network, cancel := context.WithTimeout(ctx, s.StoreTimeout)
	defer cancel()
	jobs := make(chan int)
	var workers sync.WaitGroup
	for w := 0; w < min(6, len(items)); w++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for i := range jobs {
				item := items[i]
				results[i].SavedItem = item
				if !item.Item.Valid() {
					errs[i] = errors.New("invalid stored comparison item")
					continue
				}
				provider := s.GooglePlay
				if item.Store == "rustore" {
					provider = s.RuStore
				}
				results[i].Result, errs[i] = s.lookupWithin(ctx, network, item.Store, item.PackageName, provider)
			}
		}()
	}
	for i := range items {
		jobs <- i
	}
	close(jobs)
	workers.Wait()
	return results, errors.Join(errs...)
}
