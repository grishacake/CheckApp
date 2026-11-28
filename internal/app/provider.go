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

var packageNamePattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]*(\.[a-zA-Z][a-zA-Z0-9_]*)+$`)

// store_apps.store_app_id is limited to 255 characters.
const maxPackageNameLength = 255

func ValidPackageName(s string) bool {
	return len(s) <= maxPackageNameLength && packageNamePattern.MatchString(s)
}

type Provider interface {
	GetApp(ctx context.Context, packageName string) (model.App, error)
}

// Searcher ищет приложения по словам. Ничего не нашлось — пустой список без ошибки.
type Searcher interface {
	Search(ctx context.Context, query string, limit int) ([]model.App, error)
}
