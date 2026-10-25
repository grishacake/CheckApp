package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"app-grabber-backend/internal/app"
	"app-grabber-backend/internal/model"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type AppRepository struct {
	pool *pgxpool.Pool
}

var _ app.CardRepository = (*AppRepository)(nil)

func NewAppRepository(pool *pgxpool.Pool) *AppRepository {
	return &AppRepository{pool: pool}
}

// Save writes the whole result in one transaction, keeping existing card IDs.
func (r *AppRepository) Save(ctx context.Context, storeCode string, cards []model.App) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(cards) == 0 {
		return nil
	}
	type preparedCard struct {
		card     model.App
		metadata []byte
	}
	prepared := make([]preparedCard, 0, len(cards))
	for _, card := range cards {
		if strings.TrimSpace(card.PackageName) == "" || strings.TrimSpace(card.Name) == "" {
			return errors.New("save app: package name and name are required")
		}
		metadata, err := json.Marshal(card)
		if err != nil {
			return fmt.Errorf("encode app metadata: %w", err)
		}
		prepared = append(prepared, preparedCard{card: card, metadata: metadata})
	}
	// A consistent lock order prevents deadlocks between overlapping searches.
	// Sort our copy, preserving the response order returned by Google Play.
	sort.SliceStable(prepared, func(i, j int) bool {
		return prepared[i].card.PackageName < prepared[j].card.PackageName
	})

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("begin app save: %w", err)
	}
	defer func() {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
	}()

	var storeID int32
	if err := tx.QueryRow(ctx, `
		SELECT id FROM public.stores
		WHERE code = $1 AND is_active = TRUE FOR SHARE`, storeCode).Scan(&storeID); err != nil {
		return fmt.Errorf("find active store: %w", err)
	}
	for _, item := range prepared {
		// Serialize concurrent creation of the same package before creating its
		// parent, so no unused apps rows are left by competing requests.
		lockKey := fmt.Sprintf("checkapp:store-app:%d:%s", storeID, item.card.PackageName)
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, lockKey); err != nil {
			return fmt.Errorf("lock app for save: %w", err)
		}
		var appID string
		err := tx.QueryRow(ctx, `
			SELECT app_id::text FROM public.store_apps
			WHERE store_id = $1 AND store_app_id = $2 FOR UPDATE`, storeID, item.card.PackageName).Scan(&appID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			if err := tx.QueryRow(ctx, `
				INSERT INTO public.apps (name) VALUES ($1) RETURNING id::text`, item.card.Name).Scan(&appID); err != nil {
				return fmt.Errorf("insert app: %w", err)
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO public.store_apps (app_id, store_id, store_app_id, metadata, scraped_at)
				VALUES ($1, $2, $3, $4, clock_timestamp())`, appID, storeID, item.card.PackageName, item.metadata); err != nil {
				return fmt.Errorf("insert store app: %w", err)
			}
		case err != nil:
			return fmt.Errorf("find store app: %w", err)
		default:
			if _, err := tx.Exec(ctx, `
				UPDATE public.apps SET name = $2, updated_at = clock_timestamp()
				WHERE id = $1`, appID, item.card.Name); err != nil {
				return fmt.Errorf("update app: %w", err)
			}
			if _, err := tx.Exec(ctx, `
				UPDATE public.store_apps SET metadata = $3, scraped_at = clock_timestamp()
				WHERE store_id = $1 AND store_app_id = $2`, storeID, item.card.PackageName, item.metadata); err != nil {
				return fmt.Errorf("update store app: %w", err)
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit app save: %w", err)
	}
	return nil
}
