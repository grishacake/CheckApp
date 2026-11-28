package comparison

import (
	"context"
	"fmt"
	"sort"
	"time"
)

type Favorite struct {
	ID      string    `json:"id"`
	Items   []Item    `json:"items"`
	SavedAt time.Time `json:"saved_at"`
}

type FavoriteRepository interface {
	SaveFavorite(context.Context, string, []Item) (Favorite, error)
	ListFavorites(context.Context, string, int, int) ([]Favorite, error)
	GetFavorite(context.Context, string, string) (Favorite, error)
	DeleteFavorite(context.Context, string, string) error
}

func CanonicalItems(items []Item) ([]Item, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("empty comparison")
	}
	result := make([]Item, 0, len(items))
	seen := make(map[Item]bool)
	for _, item := range items {
		if !item.Valid() {
			return nil, fmt.Errorf("invalid comparison item")
		}
		if !seen[item] {
			result = append(result, item)
			seen[item] = true
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Store != result[j].Store {
			return result[i].Store < result[j].Store
		}
		return result[i].PackageName < result[j].PackageName
	})
	return result, nil
}
