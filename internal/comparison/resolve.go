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
	for i, item := range items {
		results[i].SavedItem = item
		if !item.Item.Valid() {
			errs[i] = errors.New("invalid stored comparison item")
			continue
		}
		queues[item.Store] = append(queues[item.Store], i)
	}
	var workers sync.WaitGroup
	for store, queue := range queues {
		provider := s.GooglePlay
		if store == "rustore" {
			provider = s.RuStore
		}
		count := min(6/len(queues), len(queue))
		for w := 0; w < count; w++ {
			workers.Add(1)
			go func() {
				defer workers.Done()
				for pos := w; pos < len(queue); pos += count {
					i := queue[pos]
					remaining := (len(queue) - pos + count - 1) / count
					// Оставляем время следующим элементам этой очереди, даже если текущий зависнет.
					budget := time.Until(deadline) / time.Duration(remaining)
					itemCtx, cancelItem := context.WithTimeout(network, budget)
					results[i].Result, errs[i] = s.lookupWithin(ctx, itemCtx, store, items[i].PackageName, provider)
					cancelItem()
				}
			}()
		}
	}
	workers.Wait()
	return results, errors.Join(errs...)
}
