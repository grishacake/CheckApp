package postgres

import (
	"context"
	"errors"
	"fmt"

	"app-grabber-backend/internal/auth"
	"app-grabber-backend/internal/favorites"
	"app-grabber-backend/internal/model"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type FavoritesRepository struct{ pool *pgxpool.Pool }

var _ favorites.Repository = (*FavoritesRepository)(nil)

func NewFavoritesRepository(pool *pgxpool.Pool) *FavoritesRepository {
	return &FavoritesRepository{pool: pool}
}

func favoriteUserID(id string) (pgtype.UUID, error) {
	var userID pgtype.UUID
	if err := userID.Scan(id); err != nil {
		return pgtype.UUID{}, auth.ErrUserNotFound
	}
	return userID, nil
}

func (r *FavoritesRepository) Add(ctx context.Context, id, store, packageName string) error {
	userID, err := favoriteUserID(id)
	if err != nil {
		return err
	}
	var cardID int64
	err = r.pool.QueryRow(ctx, `
		WITH card AS (
			SELECT sa.id FROM public.store_apps sa
			JOIN public.stores s ON s.id = sa.store_id
			WHERE s.code = $2 AND s.is_active AND sa.store_app_id = $3
		), inserted AS (
			INSERT INTO public.user_favorites(user_id, store_app_id)
			SELECT $1, id FROM card
			ON CONFLICT(user_id, store_app_id) DO NOTHING
			RETURNING store_app_id
		)
		SELECT id FROM card`, userID, store, packageName).Scan(&cardID)
	if errors.Is(err, pgx.ErrNoRows) {
		return favorites.ErrCardNotFound
	}
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			switch pgErr.ConstraintName {
			case "user_favorites_user_id_fkey":
				return auth.ErrUserNotFound
			case "user_favorites_store_app_id_fkey":
				return favorites.ErrCardNotFound
			}
		}
		return fmt.Errorf("add PostgreSQL favorite: %w", err)
	}
	return nil
}

func (r *FavoritesRepository) Remove(ctx context.Context, id, store, packageName string) error {
	userID, err := favoriteUserID(id)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `
		DELETE FROM public.user_favorites uf
		USING public.store_apps sa, public.stores s
		WHERE uf.store_app_id = sa.id AND sa.store_id = s.id
		  AND uf.user_id = $1 AND s.code = $2 AND sa.store_app_id = $3`, userID, store, packageName)
	if err != nil {
		return fmt.Errorf("remove PostgreSQL favorite: %w", err)
	}
	return nil
}

func (r *FavoritesRepository) List(ctx context.Context, id string, limit, offset int) ([]model.Favorite, error) {
	userID, err := favoriteUserID(id)
	if err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `
		SELECT sa.id, s.code, sa.metadata, sa.store_app_id, uf.created_at
		FROM public.user_favorites uf
		JOIN public.store_apps sa ON sa.id = uf.store_app_id
		JOIN public.stores s ON s.id = sa.store_id
		WHERE uf.user_id = $1
		ORDER BY uf.created_at DESC, uf.store_app_id DESC
		LIMIT $2 OFFSET $3`, userID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list PostgreSQL favorites: %w", err)
	}
	defer rows.Close()
	result := make([]model.Favorite, 0)
	for rows.Next() {
		var item model.Favorite
		var metadata []byte
		var packageName string
		if err := rows.Scan(&item.StoreAppID, &item.Store, &metadata, &packageName, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan PostgreSQL favorite: %w", err)
		}
		item.App, err = decodeStoredApp(metadata, packageName)
		if err != nil {
			return nil, fmt.Errorf("decode favorite app: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate PostgreSQL favorites: %w", err)
	}
	return result, nil
}
