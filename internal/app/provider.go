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

// Searcher ищет приложения по словам. Ничего не нашлось — пустой список без ошибки.
type Searcher interface {
	Search(ctx context.Context, query string, limit int) ([]model.App, error)
}
