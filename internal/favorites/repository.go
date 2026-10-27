package favorites

import (
	"context"
	"errors"

	"app-grabber-backend/internal/model"
)

var ErrCardNotFound = errors.New("favorite card not found")

type Repository interface {
	Add(ctx context.Context, userID, storeCode, packageName string) error
	Remove(ctx context.Context, userID, storeCode, packageName string) error
	List(ctx context.Context, userID string, limit, offset int) ([]model.Favorite, error)
}
