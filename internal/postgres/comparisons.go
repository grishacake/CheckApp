package postgres

import (
	"context"

	"app-grabber-backend/internal/comparison"

	"github.com/jackc/pgx/v5/pgxpool"
)

type ComparisonRepository struct{ pool *pgxpool.Pool }

func NewComparisonRepository(pool *pgxpool.Pool) *ComparisonRepository {
	return &ComparisonRepository{pool}
}

func (r *ComparisonRepository) Record(ctx context.Context, user string, p comparison.Pair) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO public.comparison_history(user_id,google_play,rustore) VALUES ($1,$2,$3)`, user, p.GooglePlay, p.RuStore)
	return err
}

func (r *ComparisonRepository) SetFavorite(ctx context.Context, user string, p comparison.Pair, add bool) error {
	query := `DELETE FROM public.comparison_favorites WHERE user_id=$1 AND google_play=$2 AND rustore=$3`
	if add {
		query = `INSERT INTO public.comparison_favorites(user_id,google_play,rustore) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`
	}
	_, err := r.pool.Exec(ctx, query, user, p.GooglePlay, p.RuStore)
	return err
}

func (r *ComparisonRepository) List(ctx context.Context, user string, favorites bool, limit, offset int) ([]comparison.Entry, error) {
	query := `SELECT google_play,rustore,saved_at FROM public.comparison_history WHERE user_id=$1 ORDER BY saved_at DESC,id DESC LIMIT $2 OFFSET $3`
	if favorites {
		query = `SELECT google_play,rustore,saved_at FROM public.comparison_favorites WHERE user_id=$1 ORDER BY saved_at DESC,google_play,rustore LIMIT $2 OFFSET $3`
	}
	rows, err := r.pool.Query(ctx, query, user, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := make([]comparison.Entry, 0)
	for rows.Next() {
		var e comparison.Entry
		if err := rows.Scan(&e.GooglePlay, &e.RuStore, &e.SavedAt); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
