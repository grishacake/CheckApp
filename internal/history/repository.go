package history

import (
	"context"
	"errors"

	"app-grabber-backend/internal/model"
)

var ErrCardNotFound = errors.New("history card not found")

type Repository interface {
	Record(ctx context.Context, userID, store, packageName string) error
	List(ctx context.Context, userID string, limit, offset int) ([]model.View, error)
}
