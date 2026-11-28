package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"app-grabber-backend/internal/app"
	"app-grabber-backend/internal/comparison"
	"github.com/jackc/pgx/v5"
)

var _ comparison.FavoriteRepository = (*ComparisonRepository)(nil)

func scanComparisonFavorite(row interface{ Scan(...any) error }) (comparison.Favorite, error) {
	var result comparison.Favorite
	var raw []byte
	err := row.Scan(&result.ID, &raw, &result.SavedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, app.ErrNotFound
	}
	if err != nil {
		return result, err
	}
	err = json.Unmarshal(raw, &result.Items)
	result.SavedAt = result.SavedAt.UTC()
	return result, err
}

func (r *ComparisonRepository) SaveFavorite(ctx context.Context, user string, items []comparison.Item) (comparison.Favorite, error) {
	canonical, err := comparison.CanonicalItems(items)
	if err != nil {
		return comparison.Favorite{}, err
	}
	raw, err := json.Marshal(canonical)
	if err != nil {
		return comparison.Favorite{}, err
	}
	return scanComparisonFavorite(r.pool.QueryRow(ctx, `
 INSERT INTO public.comparison_favorite_sets(user_id,items,fingerprint)
 VALUES($1,$2::jsonb,sha256(convert_to(($2::jsonb)::text,'UTF8')))
 ON CONFLICT(user_id,fingerprint) DO UPDATE SET items=EXCLUDED.items
 RETURNING id,items,saved_at`, user, raw))
}

func (r *ComparisonRepository) ListFavorites(ctx context.Context, user string, limit, offset int) ([]comparison.Favorite, error) {
	rows, err := r.pool.Query(ctx, `SELECT id,items,saved_at FROM public.comparison_favorite_sets WHERE user_id=$1 ORDER BY saved_at DESC,id LIMIT $2 OFFSET $3`, user, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]comparison.Favorite, 0)
	for rows.Next() {
		item, err := scanComparisonFavorite(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *ComparisonRepository) GetFavorite(ctx context.Context, user, id string) (comparison.Favorite, error) {
	return scanComparisonFavorite(r.pool.QueryRow(ctx, `SELECT id,items,saved_at FROM public.comparison_favorite_sets WHERE user_id=$1 AND id=$2`, user, id))
}

func (r *ComparisonRepository) DeleteFavorite(ctx context.Context, user, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM public.comparison_favorite_sets WHERE user_id=$1 AND id=$2`, user, id)
	return err
}
