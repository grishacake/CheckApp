package googleplay

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"

	"app-grabber-backend/internal/app"
	"app-grabber-backend/internal/model"
)

const searchURL = "https://play.google.com/store/search"

// Поиск в библиотеке сломан: Google поменял страницу поиска, и она отдаёт случайные приложения.
// Поэтому страницу поиска качаем сами и берём из неё только ссылки details?id=... —
// они идут в порядке выдачи Google Play. Карточки затем берём обычным GetApp.
var searchIDRe = regexp.MustCompile(`details\?id=([A-Za-z][A-Za-z0-9_]*(?:\.[A-Za-z0-9_]+)+)`)

// Search ищет приложения по словам, как поиск в Google Play, и возвращает до limit карточек.
// Ничего не нашлось — пустой список без ошибки.
func (c *Client) Search(ctx context.Context, query string, limit int) ([]model.App, error) {
	ids, err := c.searchIDs(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	if len(ids) > limit {
		ids = ids[:limit]
	}

	cards := make([]model.App, 0, len(ids))
	for _, id := range ids {
		card, err := c.GetApp(ctx, id)
		if errors.Is(err, app.ErrNotFound) {
			continue // есть в выдаче, но карточка не открылась — пропускаем
		}
		if err != nil {
			if len(cards) > 0 {
				break // Google перестал отвечать — отдаём то, что успели собрать
			}
			return nil, err
		}
		cards = append(cards, card)
	}
	return cards, nil
}

// searchIDs скачивает страницу поиска и возвращает package name в порядке выдачи, без повторов.
func (c *Client) searchIDs(ctx context.Context, query string) ([]string, error) {
	if err := c.wait(ctx); err != nil {
		return nil, err
	}

	q := url.Values{"q": {query}, "c": {"apps"}, "hl": {c.Language}, "gl": {c.Countries[0]}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	// Тот же клиент, что у библиотеки: общий тайм-аут из main и подмена в тестах.
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search request error: %s", resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var ids []string
	seen := make(map[string]bool)
	for _, m := range searchIDRe.FindAllSubmatch(body, -1) {
		id := string(m[1])
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids, nil
}
