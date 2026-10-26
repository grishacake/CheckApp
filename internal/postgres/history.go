package postgres

import (
	"context"
	"errors"
	"fmt"

	"app-grabber-backend/internal/auth"
	"app-grabber-backend/internal/history"
	"app-grabber-backend/internal/model"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type HistoryRepository struct{ pool *pgxpool.Pool }

var _ history.Repository = (*HistoryRepository)(nil)

func NewHistoryRepository(pool *pgxpool.Pool) *HistoryRepository {
	return &HistoryRepository{pool: pool}
}

func historyUserID(id string) (pgtype.UUID, error) {
	var userID pgtype.UUID
	if id == "" {
		return userID, nil
	}
	if err := userID.Scan(id); err != nil {
		return pgtype.UUID{}, auth.ErrUserNotFound
	}
	return userID, nil
}

func (r *HistoryRepository) Record(ctx context.Context, id, store, packageName string) error {
	userID, err := historyUserID(id)
	if err != nil {
		return err
	}
	var insertedID int64
	err = r.pool.QueryRow(ctx, `
		INSERT INTO public.user_view_history(user_id, store_app_id)
		SELECT $1, sa.id
		FROM public.store_apps sa
		JOIN public.stores s ON s.id = sa.store_id
		WHERE s.code = $2 AND sa.store_app_id = $3
		RETURNING id`, userID, store, packageName).Scan(&insertedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return history.ErrCardNotFound
	}
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			switch pgErr.ConstraintName {
			case "user_view_history_user_id_fkey":
				return auth.ErrUserNotFound
			case "user_view_history_store_app_id_fkey":
				return history.ErrCardNotFound
			}
		}
		return fmt.Errorf("record PostgreSQL view: %w", err)
	}
	return nil
}

func (r *HistoryRepository) List(ctx context.Context, id string, limit, offset int) ([]model.View, error) {
	userID, err := historyUserID(id)
	if err != nil || id == "" {
		return nil, auth.ErrUserNotFound
	}
	rows, err := r.pool.Query(ctx, `
		SELECT uvh.id, sa.id, s.code, sa.metadata, sa.store_app_id, uvh.viewed_at
		FROM public.user_view_history uvh
		JOIN public.store_apps sa ON sa.id = uvh.store_app_id
		JOIN public.stores s ON s.id = sa.store_id
		WHERE uvh.user_id = $1
		ORDER BY uvh.viewed_at DESC, uvh.id DESC
		LIMIT $2 OFFSET $3`, userID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list PostgreSQL history: %w", err)
	}
	defer rows.Close()
	result := make([]model.View, 0)
	for rows.Next() {
		var item model.View
		var metadata []byte
		var packageName string
		if err := rows.Scan(&item.ID, &item.StoreAppID, &item.Store, &metadata, &packageName, &item.ViewedAt); err != nil {
			return nil, fmt.Errorf("scan PostgreSQL history: %w", err)
		}
		item.App, err = decodeStoredApp(metadata, packageName)
		if err != nil {
			return nil, fmt.Errorf("decode history app: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate PostgreSQL history: %w", err)
	}
	return result, nil
}
