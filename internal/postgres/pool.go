package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const connectTimeout = 5 * time.Second

// Open creates a pool and verifies the connection before returning it.
// The caller owns the pool and must close it when the application stops.
func Open(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return nil, errors.New("DATABASE_URL is required")
	}
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		// pgx parsing errors can contain the connection string and password.
		return nil, errors.New("invalid DATABASE_URL: check PostgreSQL connection settings")
	}

	ctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, errors.New("create PostgreSQL pool: check DATABASE_URL")
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		if ctx.Err() != nil {
			return nil, fmt.Errorf("connect to PostgreSQL: %w", ctx.Err())
		}
		return nil, errors.New("connect to PostgreSQL: check database availability and DATABASE_URL credentials")
	}
	return pool, nil
}
