package vermouth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/twmb/franz-go/pkg/kgo"
)

const (
	// sessionTimeout is how long the coordinator keeps a member that has gone
	// quiet. Shorter than the default so a replay is not left waiting.
	sessionTimeout = 15 * time.Second
	// leaveTimeout bounds the graceful leave on shutdown.
	leaveTimeout = 10 * time.Second
	// pollMaxRecords caps one fetch, so a burst is handled in batches the
	// retry and park path can still walk through one record at a time.
	pollMaxRecords = 50
)

// Handle does one consumer's work for one event, inside the transaction that
// also records the event as handled. Taking the transaction is what makes
// idempotency real rather than hopeful (INV-5): the handled_events row and the
// projection write commit together, or neither does.
type Handle func(ctx context.Context, tx pgx.Tx, env Envelope) error

// Consumer is one named consumer inside a service. Its group name is
// <service>.<name> and matches the consumer_name in handled_events (STK-12),
// so a reset and a replay are one visible pair.
type Consumer struct {
	// Name is the consumer name, for example "recipients".
	Name string
	// Topics are the topics it reads.
	Topics []string
	// Accepts lists the event_version values it can absorb (STK-20).
	Accepts []int
	// Handle does the work.
	Handle Handle
}

// GroupName is the consumer group, and the value written into handled_events.
func GroupName(service, consumer string) string { return service + "." + consumer }

// RunConsumer reads a topic in a consumer group until the context is
// cancelled. Failures are retried in process a bounded number of times with
// growing delay, then parked in <topic>.dlq with the reason and the original
// envelope, and the offset moves on (STK-13, INV-13). Nothing is dropped and
// one bad message never blocks the stream.
func RunConsumer(ctx context.Context, cfg Config, pool *pgxpool.Pool, logger *slog.Logger, consumer Consumer) error {
	group := GroupName(cfg.Service, consumer.Name)
	log := logger.With(slog.String("consumer", group))

	producer, err := NewProducer(cfg.BrokerSeeds, cfg.Service)
	if err != nil {
		return err
	}
	defer producer.Close()

	client, err := openConsumerClient(cfg, group, consumer)
	if err != nil {
		return err
	}
	defer func() {
		// Leaving the group explicitly, on a context that outlives the one that
		// was just cancelled, is what makes a stop quick: without it the
		// coordinator holds the member until the session timeout expires and a
		// replay has to wait it out.
		leaveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), leaveTimeout)
		defer cancel()
		err := client.LeaveGroupContext(leaveCtx)
		if err != nil {
			log.WarnContext(ctx, "Leaving the consumer group did not finish", slog.String("error", err.Error()))
		}
		client.Close()
	}()

	log.InfoContext(ctx, "Consumer started", slog.Any("topics", consumer.Topics))
	for {
		fetches := client.PollRecords(ctx, pollMaxRecords)
		if fetches.IsClientClosed() || ctx.Err() != nil {
			log.InfoContext(ctx, "Consumer stopped")
			return nil
		}
		fetches.EachError(func(topic string, partition int32, err error) {
			log.WarnContext(ctx, "Fetch error",
				slog.String("topic", topic),
				slog.Int("partition", int(partition)),
				slog.String("error", err.Error()),
			)
		})

		var records []*kgo.Record
		fetches.EachRecord(func(record *kgo.Record) { records = append(records, record) })
		keepPolling := drainRecords(ctx, drain{
			client:   client,
			pool:     pool,
			producer: producer,
			log:      log,
			cfg:      cfg,
			group:    group,
			consumer: consumer,
		}, records)
		if !keepPolling {
			return nil
		}
	}
}

// openConsumerClient opens the group member itself. It sits apart from
// RunConsumer because every option here is a decision worth reading on its own.
func openConsumerClient(cfg Config, group string, consumer Consumer) (*kgo.Client, error) {
	client, err := kgo.NewClient(
		kgo.SeedBrokers(cfg.BrokerSeeds...),
		kgo.ClientID(group),
		kgo.ConsumerGroup(group),
		kgo.ConsumeTopics(consumer.Topics...),
		// Shorter than the default 45s, so a member that died without leaving
		// is forgotten quickly. That matters for a replay: it cannot reset a
		// group the coordinator still thinks has a member (STK-22).
		kgo.SessionTimeout(sessionTimeout),
		// A fresh group, and a group deleted by a replay, both start from the
		// beginning: retention is unlimited so the whole history is there
		// (INV-11).
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
		kgo.DisableAutoCommit(),
	)
	if err != nil {
		return nil, fmt.Errorf("open consumer %s: %w", group, err)
	}
	return client, nil
}

// drain is everything one fetch needs to be worked through. It travels as a
// struct because the read loop and the record loop are two halves of the same
// consumer and would otherwise pass eight arguments between them.
type drain struct {
	client   *kgo.Client
	pool     *pgxpool.Pool
	producer *kgo.Client
	log      *slog.Logger
	cfg      Config
	group    string
	consumer Consumer
}

// drainRecords works through one fetch, one record at a time, and moves each
// offset only once that record is either handled or parked. It reports whether
// the consumer should keep polling.
func drainRecords(ctx context.Context, d drain, records []*kgo.Record) bool {
	for _, record := range records {
		err := handleRecord(ctx, d.pool, d.producer, d.log, d.cfg, d.group, d.consumer, record)
		if err != nil {
			if ctx.Err() != nil {
				return false
			}
			// The park itself failed, so the offset must not move: the message
			// is neither handled nor parked, and retrying is the only honest
			// option.
			d.log.ErrorContext(ctx, "Message neither handled nor parked, retrying",
				slog.String("error", err.Error()),
				slog.Int64("offset", record.Offset),
			)
			time.Sleep(d.cfg.RetryBaseDelay)
			continue
		}
		err = d.client.CommitRecords(ctx, record)
		if err != nil && ctx.Err() == nil {
			d.log.ErrorContext(ctx, "Commit offset", slog.String("error", err.Error()))
		}
	}
	return true
}

func handleRecord(ctx context.Context, pool *pgxpool.Pool, producer *kgo.Client, log *slog.Logger, cfg Config, group string, consumer Consumer, record *kgo.Record) error {
	var env Envelope
	err := json.Unmarshal(record.Value, &env)
	if err != nil {
		// Undecodable bytes will never decode, so retrying is pointless.
		return park(ctx, producer, log, group, record, env, fmt.Errorf("envelope did not decode: %w", err), 0)
	}

	ctx = WithRequestID(ctx, env.RequestID)
	log = log.With(
		slog.String("request_id", env.RequestID),
		slog.String("event_name", env.EventName),
		slog.String("event_id", env.EventID.String()),
	)

	var lastErr error
	for attempt := 1; attempt <= cfg.RetryMax; attempt++ {
		handled, err := handleOnce(ctx, pool, group, consumer, env)
		if err == nil {
			if handled {
				log.InfoContext(ctx, "Event handled")
			} else {
				log.DebugContext(ctx, "Event already handled, doing nothing")
			}
			return nil
		}
		lastErr = err

		// An event_version this consumer does not recognise is the one failure
		// no retry can fix, so it is parked at once (INV-12, STK-20).
		if _, ok := errors.AsType[*UnknownVersionError](err); ok {
			return park(ctx, producer, log, group, record, env, err, attempt)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		log.WarnContext(ctx, "Handler failed, retrying",
			slog.Int("attempt", attempt),
			slog.Int("retry_max", cfg.RetryMax),
			slog.String("error", err.Error()),
		)
		if attempt < cfg.RetryMax {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(cfg.RetryBaseDelay * time.Duration(1<<(attempt-1))):
			}
		}
	}
	return park(ctx, producer, log, group, record, env, lastErr, cfg.RetryMax)
}

// handleOnce records the event as handled and does the work in one
// transaction. It reports false when the event was already handled, which is
// what makes handling the same message twice change nothing (INV-5).
func handleOnce(ctx context.Context, pool *pgxpool.Pool, group string, consumer Consumer, env Envelope) (bool, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `
		INSERT INTO handled_events (consumer_name, event_id)
		VALUES ($1, $2)
		ON CONFLICT (consumer_name, event_id) DO NOTHING
	`, group, env.EventID)
	if err != nil {
		return false, fmt.Errorf("record handled event: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return false, tx.Commit(ctx)
	}

	err = consumer.Handle(ctx, tx, env)
	if err != nil {
		return false, err
	}
	err = tx.Commit(ctx)
	if err != nil {
		return false, err
	}
	return true, nil
}

// DeadLetter is what a parked message looks like: the reason it failed and the
// envelope exactly as it arrived, so a replay has everything it needs.
type DeadLetter struct {
	FailedAt  time.Time       `json:"failed_at"`
	Consumer  string          `json:"consumer"`
	Reason    string          `json:"reason"`
	Attempts  int             `json:"attempts"`
	Topic     string          `json:"source_topic"`
	Partition int32           `json:"source_partition"`
	Offset    int64           `json:"source_offset"`
	Envelope  json.RawMessage `json:"envelope"`
}

func park(ctx context.Context, producer *kgo.Client, log *slog.Logger, group string, record *kgo.Record, env Envelope, cause error, attempts int) error {
	letter := DeadLetter{
		FailedAt:  time.Now().UTC(),
		Consumer:  group,
		Reason:    cause.Error(),
		Attempts:  attempts,
		Topic:     record.Topic,
		Partition: record.Partition,
		Offset:    record.Offset,
		Envelope:  record.Value,
	}
	body, err := json.Marshal(letter)
	if err != nil {
		return fmt.Errorf("park message: encode dead letter: %w", err)
	}
	key := record.Key
	if key == nil && env.Key.Value.String() != "" {
		key = env.Key.Bytes()
	}
	dlq := DLQTopic(record.Topic)
	err = producer.ProduceSync(ctx, &kgo.Record{Topic: dlq, Key: key, Value: body}).FirstErr()
	if err != nil {
		return fmt.Errorf("park message to %s: %w", dlq, err)
	}
	log.ErrorContext(ctx, "Message parked in dead letter topic",
		slog.String("dlq_topic", dlq),
		slog.Int("attempts", attempts),
		slog.String("reason", cause.Error()),
	)
	return nil
}
