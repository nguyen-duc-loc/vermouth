package vermouth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// errNoTopic is a programming mistake rather than a runtime condition: a
// service asked to publish an event it has no topic for.
var errNoTopic = errors.New("outbox: no topic to publish to")

// OutboxRow is one claimed, not yet published outbox row.
type OutboxRow struct {
	ID       int64
	Topic    string
	KeyValue uuid.UUID
	Envelope []byte
	EventID  uuid.UUID
	Name     string
}

// WriteOutbox inserts an event into the outbox inside the caller's
// transaction. The point of taking pgx.Tx rather than a pool is INV-3 and
// STK-4: the business write and this insert commit together, in the same
// function, or neither happens.
//
// It uses pgx directly rather than a per service sqlc package because the
// outbox table is the shared module's own (STK-3).
func WriteOutbox(ctx context.Context, tx pgx.Tx, topic string, env Envelope) error {
	if topic == "" {
		return fmt.Errorf("outbox: %s: %w", env.EventName, errNoTopic)
	}
	encoded, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("outbox: encode envelope %s: %w", env.EventName, err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO outbox (
			event_id, event_name, event_version, occurred_at,
			tutor_id, key_kind, key_value, request_id, topic, envelope
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`,
		env.EventID, env.EventName, env.EventVersion, env.OccurredAt,
		env.TutorID, string(env.Key.Kind), env.Key.Value, env.RequestID, topic, encoded,
	)
	if err != nil {
		return fmt.Errorf("outbox: insert %s: %w", env.EventName, err)
	}
	return nil
}
