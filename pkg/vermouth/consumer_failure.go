package vermouth

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// FailureCategory is the safe reason class stored for a parked consumer
// record. It never carries payload data or raw error text.
type FailureCategory string

const (
	// FailureDecode means the broker value was not a readable envelope.
	FailureDecode FailureCategory = "decode_failed"
	// FailureVersion means the envelope used an unsupported event version.
	FailureVersion FailureCategory = "version_unknown"
	// FailureHandler means projection handling exhausted its retry budget.
	FailureHandler FailureCategory = "handler_failed"
)

type failureSource struct {
	Consumer  string
	Topic     string
	Partition int32
	Offset    int64
}

// ConsumerRecoveryDB is the pgx surface shared recovery reads need.
type ConsumerRecoveryDB interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// ConsumerReadinessEvidence is the current replay generation and its manifest.
// Services read it through this shared module so recovery SQL lives once.
type ConsumerReadinessEvidence struct {
	ConsumerName         string
	ProjectionGeneration uuid.UUID
	State                string
	UpdatedAt            time.Time
	SourceTopic          string
	TopicIdentity        string
	PartitionSet         []byte
	EarliestOffsets      []byte
	CapturedEndOffsets   []byte
	CompletedOffsets     []byte
	StartedAt            time.Time
	CompletedAt          pgtype.Timestamptz
	OperatorIdentity     string
}

// UnresolvedConsumerFailure is the safe source coordinate exposed to a
// projection reader. It contains no payload, private value, or raw error.
type UnresolvedConsumerFailure struct {
	ConsumerName    string
	SourceTopic     string
	SourcePartition int32
	SourceOffset    int64
	EventID         pgtype.UUID
	TutorID         pgtype.UUID
	FailureCategory string
	FailedAt        time.Time
}

// ReadConsumerReadiness reads one current manifest. lockShare keeps replay
// reset and certification from crossing a calculation transaction.
func ReadConsumerReadiness(
	ctx context.Context,
	db ConsumerRecoveryDB,
	consumerName string,
) (ConsumerReadinessEvidence, error) {
	return readConsumerReadiness(ctx, db, consumerName, "")
}

// LockConsumerReadinessShare reads the current manifest while holding the
// replay coordination row through the caller's transaction.
func LockConsumerReadinessShare(
	ctx context.Context,
	db ConsumerRecoveryDB,
	consumerName string,
) (ConsumerReadinessEvidence, error) {
	return readConsumerReadiness(ctx, db, consumerName, " FOR SHARE OF r")
}

func readConsumerReadiness(
	ctx context.Context,
	db ConsumerRecoveryDB,
	consumerName string,
	lockClause string,
) (ConsumerReadinessEvidence, error) {
	query := `
		SELECT r.consumer_name,
			r.projection_generation,
			r.state,
			r.updated_at,
			m.source_topic,
			m.topic_identity,
			m.partition_set,
			m.earliest_offsets,
			m.captured_end_offsets,
			m.completed_offsets,
			m.started_at,
			m.completed_at,
			m.operator_identity
		FROM consumer_readiness r
		JOIN consumer_replay_manifests m
			ON m.consumer_name = r.consumer_name
			AND m.projection_generation = r.projection_generation
		WHERE r.consumer_name = $1
	`
	query += lockClause
	var evidence ConsumerReadinessEvidence
	err := db.QueryRow(ctx, query, consumerName).Scan(
		&evidence.ConsumerName,
		&evidence.ProjectionGeneration,
		&evidence.State,
		&evidence.UpdatedAt,
		&evidence.SourceTopic,
		&evidence.TopicIdentity,
		&evidence.PartitionSet,
		&evidence.EarliestOffsets,
		&evidence.CapturedEndOffsets,
		&evidence.CompletedOffsets,
		&evidence.StartedAt,
		&evidence.CompletedAt,
		&evidence.OperatorIdentity,
	)
	if err != nil {
		return ConsumerReadinessEvidence{}, fmt.Errorf("read consumer readiness: %w", err)
	}
	return evidence, nil
}

// ListUnresolvedConsumerFailures returns failures for one tutor plus failures
// whose tenant could not be decoded and therefore block every tutor.
func ListUnresolvedConsumerFailures(
	ctx context.Context,
	db ConsumerRecoveryDB,
	consumerName string,
	tutorID uuid.UUID,
) ([]UnresolvedConsumerFailure, error) {
	rows, err := db.Query(ctx, `
		SELECT consumer_name,
			source_topic,
			source_partition,
			source_offset,
			event_id,
			tutor_id,
			failure_category,
			failed_at
		FROM consumer_failures
		WHERE consumer_name = $1
			AND resolved_at IS NULL
			AND (tutor_id = $2 OR tutor_id IS NULL)
		ORDER BY source_topic, source_partition, source_offset
	`, consumerName, tutorID)
	if err != nil {
		return nil, fmt.Errorf("list unresolved consumer failures: %w", err)
	}
	defer rows.Close()
	result := make([]UnresolvedConsumerFailure, 0)
	for rows.Next() {
		var failure UnresolvedConsumerFailure
		err = rows.Scan(
			&failure.ConsumerName,
			&failure.SourceTopic,
			&failure.SourcePartition,
			&failure.SourceOffset,
			&failure.EventID,
			&failure.TutorID,
			&failure.FailureCategory,
			&failure.FailedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan unresolved consumer failure: %w", err)
		}
		result = append(result, failure)
	}
	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("iterate unresolved consumer failures: %w", err)
	}
	return result, nil
}

func recordConsumerFailure(
	ctx context.Context,
	pool *pgxpool.Pool,
	source failureSource,
	env Envelope,
	category FailureCategory,
) error {
	var eventID *uuid.UUID
	if env.EventID != uuid.Nil {
		eventID = &env.EventID
	}
	var tutorID *uuid.UUID
	if env.TutorID != uuid.Nil {
		tutorID = &env.TutorID
	}

	_, err := pool.Exec(ctx, `
		INSERT INTO consumer_failures (
			consumer_name,
			source_topic,
			source_partition,
			source_offset,
			event_id,
			tutor_id,
			failure_category
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (consumer_name, source_topic, source_partition, source_offset)
		DO NOTHING
	`, source.Consumer, source.Topic, source.Partition, source.Offset, eventID, tutorID, category)
	if err != nil {
		return fmt.Errorf("record consumer failure: %w", err)
	}
	return nil
}

func resolveConsumerFailure(ctx context.Context, tx pgx.Tx, source failureSource) error {
	_, err := tx.Exec(ctx, `
		UPDATE consumer_failures
		SET resolved_at = transaction_timestamp(),
			resolution = 'replayed',
			resolution_code = NULL,
			resolved_by = NULL,
			repair_reference = NULL
		WHERE consumer_name = $1
			AND source_topic = $2
			AND source_partition = $3
			AND source_offset = $4
			AND resolved_at IS NULL
	`, source.Consumer, source.Topic, source.Partition, source.Offset)
	if err != nil {
		return fmt.Errorf("resolve consumer failure: %w", err)
	}
	return nil
}
