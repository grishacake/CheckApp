package rustore

import (
	"context"
	"fmt"
	"net/http"

	"app-grabber-backend/internal/app"
	"app-grabber-backend/internal/model"
)

type Client struct{ HTTP *http.Client }

func (c Client) GetApp(ctx context.Context, id string) (model.App, error) {
	if !app.ValidPackageName(id) {
		return model.App{}, fmt.Errorf("invalid package name %q", id)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.rustore.ru/catalog/app/"+id, nil)
	if err != nil {
		return model.App{}, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return model.App{}, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		return Parse(resp.Body, id)
	case http.StatusNotFound:
		return model.App{}, app.ErrNotFound
	default:
		return model.App{}, fmt.Errorf("RuStore returned HTTP %d", resp.StatusCode)
	}
}
