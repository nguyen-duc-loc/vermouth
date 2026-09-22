package vermouth //nolint:testpackage // The regression observes the drain at its broker commit boundary.

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth/internal/recoverytest"
)

var errTestDatabaseOutage = errors.New("database outage")

func TestDeadLetterEncodesMalformedEnvelopeBytes(t *testing.T) {
	t.Parallel()
	original := []byte("invalid envelope")
	body, err := json.Marshal(DeadLetter{EnvelopeEncoding: "base64", Envelope: original})
	require.NoError(t, err)

	var decoded struct {
		EnvelopeEncoding string `json:"envelope_encoding"`
		Envelope         []byte `json:"envelope"`
	}
	require.NoError(t, json.Unmarshal(body, &decoded))
	require.Equal(t, "base64", decoded.EnvelopeEncoding)
	require.Equal(t, original, decoded.Envelope)
}

func TestDrainRecordsParksMalformedEnvelopeAndContinues(t *testing.T) {
	t.Parallel()
	fixture := recoverytest.New(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	t.Cleanup(cancel)
	original := []byte("invalid envelope")
	valid, err := json.Marshal(Envelope{EventID: uuid.Must(uuid.NewV7()), EventVersion: 1})
	require.NoError(t, err)
	records := []*kgo.Record{
		{Topic: fixture.Topic, Partition: 0, Value: original},
		{Topic: fixture.Topic, Partition: 0, Value: valid},
	}
	require.NoError(t, fixture.Client.ProduceSync(ctx, records...).FirstErr())
	client, err := openConsumerClient(
		Config{BrokerSeeds: fixture.Seeds},
		fixture.Group,
		Consumer{Topics: []string{fixture.Topic}},
	)
	require.NoError(t, err)
	t.Cleanup(client.Close)
	fetched := make([]*kgo.Record, 0, 2)
	for len(fetched) < 2 && ctx.Err() == nil {
		fetches := client.PollRecords(ctx, 2-len(fetched))
		require.Empty(t, fetches.Errors())
		fetches.EachRecord(func(record *kgo.Record) { fetched = append(fetched, record) })
	}
	require.Len(t, fetched, 2)
	handled := make([]int64, 0, 1)
	consumer := Consumer{Handle: func(
		_ context.Context,
		_ pgx.Tx,
		_ Envelope,
		source SourcePosition,
	) error {
		handled = append(handled, source.Offset)
		return nil
	}}

	require.True(t, drainRecords(ctx, drain{
		client: client, pool: fixture.Pool, producer: fixture.Client,
		log:   slog.New(slog.DiscardHandler),
		cfg:   Config{RetryMax: 1, RetryBaseDelay: time.Millisecond},
		group: fixture.Group, consumer: consumer,
	}, fetched))
	require.Equal(t, []int64{fetched[1].Offset}, handled)
	offsets, err := fixture.Admin.FetchOffsets(ctx, fixture.Group)
	require.NoError(t, err)
	committed, ok := offsets.Lookup(fixture.Topic, 0)
	require.True(t, ok)
	require.Equal(t, fetched[1].Offset+1, committed.At)

	var category FailureCategory
	err = fixture.Pool.QueryRow(ctx, `
		SELECT failure_category
		FROM consumer_failures
		WHERE consumer_name = $1
			AND source_topic = $2
			AND source_partition = $3
			AND source_offset = $4
	`, fixture.Group, fixture.Topic, fetched[0].Partition, fetched[0].Offset).Scan(&category)
	require.NoError(t, err)
	require.Equal(t, FailureDecode, category)

	dlqClient, err := kgo.NewClient(
		kgo.SeedBrokers(fixture.Seeds...),
		kgo.ConsumeTopics(DLQTopic(fixture.Topic)),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
	)
	require.NoError(t, err)
	t.Cleanup(dlqClient.Close)
	var parked *kgo.Record
	for parked == nil && ctx.Err() == nil {
		fetches := dlqClient.PollRecords(ctx, 1)
		require.Empty(t, fetches.Errors())
		fetches.EachRecord(func(fetched *kgo.Record) { parked = fetched })
	}
	require.NotNil(t, parked)
	var letter DeadLetter
	require.NoError(t, json.Unmarshal(parked.Value, &letter))
	require.Equal(t, "base64", letter.EnvelopeEncoding)
	require.Equal(t, original, letter.Envelope)
}

func TestDrainRecordsRetriesFailedLedgerBeforeLaterOffsets(t *testing.T) {
	t.Parallel()
	fixture := recoverytest.New(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	t.Cleanup(cancel)
	_, err := fixture.Pool.Exec(ctx, `ALTER TABLE consumer_failures ADD CONSTRAINT ledger_unavailable CHECK (false)`)
	require.NoError(t, err)
	records := make([]*kgo.Record, 0, 2)
	for range 2 {
		env := Envelope{EventID: uuid.New(), EventVersion: 1}
		value, encodeErr := json.Marshal(env)
		require.NoError(t, encodeErr)
		records = append(records, &kgo.Record{Topic: fixture.Topic, Partition: 0, Value: value})
	}
	require.NoError(t, fixture.Client.ProduceSync(ctx, records...).FirstErr())
	client, err := openConsumerClient(Config{BrokerSeeds: fixture.Seeds}, fixture.Group, Consumer{Topics: []string{fixture.Topic}})
	require.NoError(t, err)
	t.Cleanup(client.Close)
	fetched := make([]*kgo.Record, 0, 2)
	for len(fetched) < 2 && ctx.Err() == nil {
		fetches := client.PollRecords(ctx, 2-len(fetched))
		require.Empty(t, fetches.Errors())
		fetches.EachRecord(func(record *kgo.Record) { fetched = append(fetched, record) })
	}
	require.Len(t, fetched, 2)
	var recovered bool
	var handled []int64
	consumer := Consumer{Handle: func(_ context.Context, _ pgx.Tx, _ Envelope, source SourcePosition) error {
		if !recovered {
			return errTestDatabaseOutage
		}
		handled = append(handled, source.Offset)
		return nil
	}}
	log := slog.New(&retryLogHandler{
		Handler: slog.DiscardHandler,
		onRetry: func() {
			offsets, readErr := fixture.Admin.FetchOffsets(ctx, fixture.Group)
			require.NoError(t, readErr)
			offset, ok := offsets.Lookup(fixture.Topic, 0)
			require.True(t, !ok || offset.At <= 0, "the barrier must remain behind the unrecorded failure")
			require.Empty(t, handled)
			_, repairErr := fixture.Pool.Exec(ctx, `ALTER TABLE consumer_failures DROP CONSTRAINT ledger_unavailable`)
			require.NoError(t, repairErr)
			recovered = true
		},
	})
	require.True(t, drainRecords(ctx, drain{
		client: client, pool: fixture.Pool, producer: fixture.Client, log: log,
		cfg: Config{RetryMax: 1, RetryBaseDelay: time.Millisecond}, group: fixture.Group, consumer: consumer,
	}, fetched))
	offsets, err := fixture.Admin.FetchOffsets(ctx, fixture.Group)
	require.NoError(t, err)
	offset, ok := offsets.Lookup(fixture.Topic, 0)
	require.True(t, ok)
	require.Equal(t, int64(2), offset.At)
	require.Equal(t, []int64{0, 1}, handled, "committed progress must contain both source records in order")
}

func TestDrainRecordsCancelsWhileRetryingLedger(t *testing.T) {
	t.Parallel()
	fixture := recoverytest.New(t)
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	_, err := fixture.Pool.Exec(ctx, `ALTER TABLE consumer_failures ADD CONSTRAINT ledger_unavailable CHECK (false)`)
	require.NoError(t, err)
	log := slog.New(&retryLogHandler{
		Handler: slog.DiscardHandler, onRetry: cancel,
	})
	stopped := make(chan bool, 1)
	go func() {
		stopped <- drainRecords(ctx, drain{
			pool: fixture.Pool, producer: fixture.Client, log: log,
			cfg: Config{RetryMax: 1, RetryBaseDelay: time.Hour}, group: fixture.Group,
		}, []*kgo.Record{{Topic: fixture.Topic, Value: []byte("invalid envelope")}})
	}()
	select {
	case keepPolling := <-stopped:
		require.False(t, keepPolling)
	case <-time.After(time.Second):
		t.Fatal("cancellation must interrupt the retry delay")
	}
}

type retryLogHandler struct {
	slog.Handler

	onRetry func()
}

func (*retryLogHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *retryLogHandler) Handle(ctx context.Context, record slog.Record) error {
	if record.Message == "Message neither handled nor parked, retrying" {
		h.onRetry()
	}
	return h.Handler.Handle(ctx, record)
}
