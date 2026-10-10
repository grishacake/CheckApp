package app

import (
	"context"
	"errors"

	"app-grabber-backend/internal/model"
)

var (
	ErrNotFound      = errors.New("app not found")
	ErrNotConfigured = errors.New("app provider not configured")
)

type Provider interface {
	GetApp(ctx context.Context, packageName string) (model.App, error)
}
