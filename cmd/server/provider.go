package main

import (
	"context"

	"app-grabber-backend/internal/app"
	"app-grabber-backend/internal/model"
)

// Временно используется до подключения адаптера Google Play.
type unconfiguredProvider struct{}

func (unconfiguredProvider) GetApp(ctx context.Context, _ string) (model.App, error) {
	if err := ctx.Err(); err != nil {
		return model.App{}, err
	}
	return model.App{}, app.ErrNotConfigured
}
