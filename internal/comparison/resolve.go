package comparison

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Resolve ограничивает параллелизм и общий срок сетевых запросов ко всем магазинам.
func (s *Service) Resolve(ctx context.Context, items []SavedItem) ([]ItemResult, error) {
	results := make([]ItemResult, len(items))
	errs := make([]error, len(items))
	network, cancel := context.WithTimeout(ctx, s.StoreTimeout)
	defer cancel()
	deadline, _ := network.Deadline()
	queues := make(map[string][]int)
	var stores []string
	for i, item := range items {
		results[i].SavedItem = item
		if !item.Item.Valid() {
			errs[i] = errors.New("invalid stored comparison item")
			continue
		}
		if _, exists := queues[item.Store]; !exists {
			stores = append(stores, item.Store)
		}
		queues[item.Store] = append(queues[item.Store], i)
	}

	// Чередуем магазины перед распределением по шести рабочим очередям.
	jobs := make([]int, 0, len(items))
	for round := 0; ; round++ {
		added := false
		for _, store := range stores {
			if round < len(queues[store]) {
				jobs = append(jobs, queues[store][round])
				added = true
			}
		}
		if !added {
			break
		}
	}
	var workers sync.WaitGroup
	count := min(6, len(jobs))
	for w := 0; w < count; w++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for pos := w; pos < len(jobs); pos += count {
				i := jobs[pos]
				remaining := (len(jobs) - pos + count - 1) / count
				budget := time.Until(deadline) / time.Duration(remaining)
				itemCtx, cancelItem := context.WithTimeout(network, budget)
				item := items[i]
				results[i].Result, errs[i] = s.lookupWithin(ctx, itemCtx, item.Store, item.PackageName, s.Providers[item.Store])
				cancelItem()
			}
		}()
	}
	workers.Wait()
	return results, errors.Join(errs...)
}
