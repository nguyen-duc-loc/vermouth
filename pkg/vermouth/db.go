package vermouth

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// OpenPool connects to this service's own database and nobody else's (STK-5).
func OpenPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open database pool: %w", err)
	}
	err = pool.Ping(ctx)
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}
