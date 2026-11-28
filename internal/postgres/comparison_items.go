package postgres

import (
	"context"
	"fmt"

	"app-grabber-backend/internal/comparison"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ComparisonRepository struct{ pool *pgxpool.Pool }

func NewComparisonRepository(pool *pgxpool.Pool) *ComparisonRepository {
	return &ComparisonRepository{pool: pool}
}

var _ comparison.Repository = (*ComparisonRepository)(nil)

func (r *ComparisonRepository) AddItems(ctx context.Context, user string, items []comparison.Item) error {
	stores, packages := make([]string, 0, len(items)), make([]string, 0, len(items))
	for _, item := range items {
		if !item.Valid() {
			return fmt.Errorf("invalid comparison item")
		}
		stores = append(stores, item.Store)
		packages = append(packages, item.PackageName)
	}
	_, err := r.pool.Exec(ctx, `WITH owner AS (SELECT id FROM public.users WHERE id=$1 FOR NO KEY UPDATE)
 INSERT INTO public.comparison_items(user_id, store, package_name)
 SELECT owner.id, store, package_name FROM owner CROSS JOIN unnest($2::text[], $3::text[]) WITH ORDINALITY AS batch(store, package_name, position)
 ORDER BY position ON CONFLICT (user_id, store, package_name) DO NOTHING`, user, stores, packages)
	return err
}

func (r *ComparisonRepository) ListItems(ctx context.Context, user string) ([]comparison.SavedItem, error) {
	rows, err := r.pool.Query(ctx, `SELECT store, package_name, added_at FROM public.comparison_items WHERE user_id=$1 ORDER BY id`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]comparison.SavedItem, 0)
	for rows.Next() {
		var item comparison.SavedItem
		if err := rows.Scan(&item.Store, &item.PackageName, &item.AddedAt); err != nil {
			return nil, err
		}
		item.AddedAt = item.AddedAt.UTC()
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *ComparisonRepository) RemoveItem(ctx context.Context, user string, item comparison.Item) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM public.comparison_items WHERE user_id=$1 AND store=$2 AND package_name=$3`, user, item.Store, item.PackageName)
	return err
}

func (r *ComparisonRepository) ClearItems(ctx context.Context, user string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM public.comparison_items WHERE user_id=$1`, user)
	return err
}
