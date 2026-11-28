package comparison

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"app-grabber-backend/internal/app"
	"app-grabber-backend/internal/applink"
)

type Item struct {
	Store       string `json:"store"`
	PackageName string `json:"package_name"`
}

var storePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,49}$`)

func (i Item) Valid() bool {
	return storePattern.MatchString(i.Store) && len(i.PackageName) <= 255 && app.ValidPackageName(i.PackageName)
}

type Input struct {
	URL         string `json:"url,omitempty"`
	Store       string `json:"store,omitempty"`
	PackageName string `json:"package_name,omitempty"`
}

func Normalize(inputs []Input) ([]Item, error) {
	if len(inputs) == 0 {
		return nil, fmt.Errorf("items не должен быть пустым")
	}
	items := make([]Item, 0, len(inputs))
	seen := make(map[Item]bool)
	for n, input := range inputs {
		item := Item{Store: input.Store, PackageName: input.PackageName}
		if input.URL != "" {
			if input.Store != "" || input.PackageName != "" {
				return nil, fmt.Errorf("items[%d]: передайте url либо store и package_name", n)
			}
			link, ok := applink.Parse(strings.TrimSpace(input.URL))
			if !ok {
				return nil, fmt.Errorf("items[%d]: некорректная ссылка на приложение", n)
			}
			item = Item{Store: link.Store, PackageName: link.PackageName}
		}
		if !item.Valid() {
			return nil, fmt.Errorf("items[%d]: укажите корректные store и package_name", n)
		}
		if !seen[item] {
			items = append(items, item)
			seen[item] = true
		}
	}
	return items, nil
}

type SavedItem struct {
	Item
	AddedAt time.Time `json:"added_at"`
}

type ItemResult struct {
	SavedItem
	Result
}

type Repository interface {
	AddItems(context.Context, string, []Item) error
	ListItems(context.Context, string) ([]SavedItem, error)
	RemoveItem(context.Context, string, Item) error
	ClearItems(context.Context, string) error
}
