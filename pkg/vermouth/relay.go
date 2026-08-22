package vermouth

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/twmb/franz-go/pkg/kgo"
)

// Relay drains the outbox into the broker. It is one goroutine inside the
// service binary, not a separate process (STK-18).
//
// It wakes on a ticker, claims the oldest unpublished row with FOR UPDATE SKIP
// LOCKED, publishes it, and marks that one row published before it looks at
// the next. One row at a time rather than a batch acknowledgement: a crash
// between publish and mark republishes exactly one event, which INV-5 already
// makes harmless.
type Relay struct {
	pool     *pgxpool.Pool
	producer *kgo.Client
	logger   *slog.Logger
	interval time.Duration
}

// NewRelay wires a relay to one service's own pool and producer. interval
// comes from configuration rather than a literal, so a slow machine can be
// told to poll less often (STK-18).
func NewRelay(pool *pgxpool.Pool, producer *kgo.Client, logger *slog.Logger, interval time.Duration) *Relay {
	return &Relay{pool: pool, producer: producer, logger: logger, interval: interval}
}

// Run blocks until the context is cancelled.
func (r *Relay) Run(ctx context.Context) error {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	r.logger.InfoContext(ctx, "Relay started", slog.Duration("interval", r.interval))
	for {
		select {
		case <-ctx.Done():
			r.logger.InfoContext(ctx, "Relay stopped")
			return nil
		case <-ticker.C:
			for {
				published, err := r.publishOne(ctx)
				if err != nil {
					if ctx.Err() != nil {
						return nil
					}
					r.logger.ErrorContext(ctx, "Relay tick failed", slog.String("error", err.Error()))
					break
				}
				if !published {
					break
				}
			}
		}
	}
}

// publishOne claims, publishes and marks a single row. It reports whether
// there was anything to do, so a busy outbox drains without waiting for the
// next tick.
func (r *Relay) publishOne(ctx context.Context) (bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() {
		// A commit further down closes the transaction, so the usual rollback
		// error here is "already closed" and means nothing. Anything else is
		// worth a line, because it points at the connection rather than at
		// this row.
		rollbackErr := tx.Rollback(ctx)
		if rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			r.logger.WarnContext(ctx, "Rollback after draining the outbox",
				slog.String("error", rollbackErr.Error()))
		}
	}()

	var row OutboxRow
	err = tx.QueryRow(ctx, `
		SELECT id, topic, key_value, envelope, event_id, event_name
		FROM outbox
		WHERE published_at IS NULL
		ORDER BY id
		FOR UPDATE SKIP LOCKED
		LIMIT 1
	`).Scan(&row.ID, &row.Topic, &row.KeyValue, &row.Envelope, &row.EventID, &row.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	record := &kgo.Record{
		Topic: row.Topic,
		Key:   []byte(row.KeyValue.String()),
		Value: row.Envelope,
	}
	err = r.producer.ProduceSync(ctx, record).FirstErr()
	if err != nil {
		return false, err
	}

	_, err = tx.Exec(ctx, `UPDATE outbox SET published_at = now() WHERE id = $1`, row.ID)
	if err != nil {
		// The event is on the broker but unmarked, so the next tick publishes
		// it again. INV-5 is what makes that safe, and saying so out loud here
		// is cheaper than wondering later.
		return false, err
	}
	err = tx.Commit(ctx)
	if err != nil {
		return false, err
	}

	r.logger.InfoContext(ctx, "Event published",
		slog.String("event_name", row.Name),
		slog.String("event_id", row.EventID.String()),
		slog.String("topic", row.Topic),
		slog.Int64("outbox_id", row.ID),
	)
	return true, nil
}
