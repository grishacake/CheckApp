package app

import (
	"context"
	"errors"
	"regexp"

	"app-grabber-backend/internal/model"
)

var (
	ErrNotFound      = errors.New("app not found")
	ErrNotConfigured = errors.New("app provider not configured")
)

var packageNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)

// ValidPackageName проверяет формат package name: com.uchi.app.
func ValidPackageName(s string) bool {
	return packageNamePattern.MatchString(s)
}

type Provider interface {
	GetApp(ctx context.Context, packageName string) (model.App, error)
}

// Searcher ищет приложения по словам. Ничего не нашлось — пустой список без ошибки.
type Searcher interface {
	Search(ctx context.Context, query string, limit int) ([]model.App, error)
}
