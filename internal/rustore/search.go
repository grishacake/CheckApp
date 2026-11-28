package rustore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"app-grabber-backend/internal/app"
	"app-grabber-backend/internal/model"
)

// Search uses catalog search for IDs, then loads the same HTML cards as GetApp.
func (c Client) Search(ctx context.Context, query string, limit int) ([]model.App, error) {
	if limit <= 0 || strings.TrimSpace(query) == "" {
		return []model.App{}, nil
	}
	if limit > 5 {
		limit = 5
	}
	params := url.Values{"query": {query}, "pageNumber": {"0"}, "pageSize": {strconv.Itoa(limit)}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://backapi.rustore.ru/applicationData/apps?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("ruStoreVerCode", "1000")
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("RuStore search returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPageSize+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxPageSize {
		return nil, errors.New("RuStore search response exceeds size limit")
	}
	var result struct {
		Code string `json:"code"`
		Body *struct {
			Content *[]struct {
				PackageName string `json:"packageName"`
			} `json:"content"`
		} `json:"body"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode RuStore search: %w", err)
	}
	if result.Code != "OK" || result.Body == nil || result.Body.Content == nil {
		return nil, errors.New("invalid RuStore search response")
	}
	cards := make([]model.App, 0, limit)
	seen := make(map[string]bool)
	items := *result.Body.Content
	if len(items) > limit {
		items = items[:limit]
	}
	for _, item := range items {
		if len(cards) >= limit {
			break
		}
		id := item.PackageName
		if !app.ValidPackageName(id) || seen[id] {
			continue
		}
		seen[id] = true
		card, err := c.GetApp(ctx, id)
		if errors.Is(err, app.ErrNotFound) {
			continue
		}
		if err != nil {
			if len(cards) > 0 {
				break
			}
			return nil, err
		}
		cards = append(cards, card)
	}
	if len(items) > 0 && len(seen) == 0 {
		return nil, errors.New("RuStore search contains no valid package names")
	}
	return cards, nil
}
