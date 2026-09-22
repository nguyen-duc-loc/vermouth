package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"
	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth/internal/recoverytest"
)

func TestCertifyRejectsRetentionLossDuringReplay(t *testing.T) {
	t.Parallel()
	fixture := recoverytest.New(t)
	ctx := t.Context()
	records := []*kgo.Record{
		{Topic: fixture.Topic, Partition: 0, Value: encodeTestJSON(t, vermouth.Envelope{EventID: uuid.New()})},
		{Topic: fixture.Topic, Partition: 0, Value: encodeTestJSON(t, vermouth.Envelope{EventID: uuid.New()})},
		{Topic: fixture.Topic, Partition: 1, Value: encodeTestJSON(t, vermouth.Envelope{EventID: uuid.New()})},
	}
	require.NoError(t, fixture.Client.ProduceSync(ctx, records...).FirstErr())
	seedManifest(t, fixture, "replaying")
	starts, err := fixture.Admin.ListStartOffsets(ctx, fixture.Topic)
	require.NoError(t, err)
	require.NoError(t, fixture.Admin.CommitAllOffsets(ctx, fixture.Group, starts.Offsets()))
	deleted, err := fixture.Admin.DeleteRecords(ctx, kadm.Offsets{
		fixture.Topic: {0: {Topic: fixture.Topic, Partition: 0, At: 1}},
	})
	require.NoError(t, err)
	require.NoError(t, deleted.Error())
	starts, err = fixture.Admin.ListStartOffsets(ctx, fixture.Topic)
	require.NoError(t, err)
	require.Equal(t, int64(1), starts[fixture.Topic][0].Offset)
	consumeRetainedFacts(t, fixture)
	err = runCertify(certifyArgs(t, fixture))
	require.ErrorIs(t, err, errRetentionGap)
	var state string
	require.NoError(t, fixture.Pool.QueryRow(ctx, `SELECT state FROM consumer_readiness WHERE consumer_name = $1`, fixture.Group).Scan(&state))
	require.Equal(t, "replaying", state)
}

func consumeRetainedFacts(t *testing.T, fixture *recoverytest.Fixture) {
	t.Helper()
	service, consumer, found := strings.Cut(fixture.Group, ".")
	require.True(t, found)
	ctx, cancel := context.WithCancel(t.Context())
	stopped := make(chan error, 1)
	go func() {
		stopped <- vermouth.RunConsumer(ctx, vermouth.Config{
			Service: service, BrokerSeeds: fixture.Seeds, RetryMax: 1, RetryBaseDelay: time.Millisecond,
		}, fixture.Pool, slog.New(slog.DiscardHandler), vermouth.Consumer{
			Name: consumer, Topics: []string{fixture.Topic},
			Handle: func(context.Context, pgx.Tx, vermouth.Envelope, vermouth.SourcePosition) error { return nil },
		})
	}()
	t.Cleanup(func() {
		cancel()
		require.NoError(t, <-stopped)
	})
	require.Eventually(t, func() bool {
		offsets, err := fixture.Admin.FetchOffsets(ctx, fixture.Group)
		if err != nil || offsets.Error() != nil {
			return false
		}
		first, hasFirst := offsets.Lookup(fixture.Topic, 0)
		second, hasSecond := offsets.Lookup(fixture.Topic, 1)
		return hasFirst && hasSecond && first.At == 2 && second.At == 1
	}, 20*time.Second, 20*time.Millisecond)
	var handled int
	require.NoError(t, fixture.Pool.QueryRow(ctx, `SELECT count(*) FROM handled_events WHERE consumer_name = $1`, fixture.Group).Scan(&handled))
	require.Equal(t, 2, handled, "the lost prefix was never projected even though committed progress reached the end")
}

func TestCertifyRefusesUnconfirmedReset(t *testing.T) {
	t.Parallel()
	fixture := recoverytest.New(t)
	seedManifest(t, fixture, "uncertified")
	require.ErrorIs(t, runCertify(certifyArgs(t, fixture)), errProjectionState)
}

func TestCertifyRequiresCompleteManifestOffsets(t *testing.T) {
	t.Parallel()
	for _, column := range []string{"earliest_offsets", "captured_end_offsets"} {
		t.Run(column, func(t *testing.T) {
			t.Parallel()
			fixture := recoverytest.New(t)
			seedManifest(t, fixture, "replaying")
			_, err := fixture.Pool.Exec(t.Context(),
				`UPDATE consumer_replay_manifests SET `+column+` = jsonb_build_object($1::text, '{"0":0}'::jsonb)`,
				fixture.Topic,
			)
			require.NoError(t, err)
			require.ErrorIs(t, runCertify(certifyArgs(t, fixture)), errIncompleteOffsets)
		})
	}
}

func TestCertifyCompletesRetainedReplay(t *testing.T) {
	t.Parallel()
	fixture := recoverytest.New(t)
	seedManifest(t, fixture, "replaying")
	require.NoError(t, runCertify(certifyArgs(t, fixture)))
	var state string
	var completed bool
	err := fixture.Pool.QueryRow(t.Context(), `
		SELECT r.state, m.completed_at IS NOT NULL AND m.completed_offsets IS NOT NULL
		FROM consumer_readiness r
		JOIN consumer_replay_manifests m USING (consumer_name, projection_generation)
		WHERE consumer_name = $1
	`, fixture.Group).Scan(&state, &completed)
	require.NoError(t, err)
	require.Equal(t, "certified", state)
	require.True(t, completed)
}

func TestValidateOffsetMapRequiresEveryLivePartition(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		name    string
		offsets map[string]map[string]int64
	}{
		{name: "no offsets"},
		{name: "missing topic", offsets: map[string]map[string]int64{"other": {"0": 0, "1": 0}}},
		{name: "missing partition", offsets: map[string]map[string]int64{"facts": {"0": 0}}},
		{name: "unexpected partition", offsets: map[string]map[string]int64{"facts": {"0": 0, "2": 0}}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			require.ErrorIs(t, validateOffsetMap(scenario.offsets, map[string][]int32{"facts": {0, 1}}), errIncompleteOffsets)
		})
	}
}

func seedManifest(t *testing.T, fixture *recoverytest.Fixture, state string) {
	t.Helper()
	ctx := t.Context()
	details, err := fixture.Admin.ListTopics(ctx, fixture.Topic)
	require.NoError(t, err)
	ends, err := fixture.Admin.ListEndOffsets(ctx, fixture.Topic)
	require.NoError(t, err)
	require.NoError(t, fixture.Admin.CommitAllOffsets(ctx, fixture.Group, ends.Offsets()))
	generation := uuid.New()
	identity := encodeTestJSON(t, map[string]string{fixture.Topic: details[fixture.Topic].ID.String()})
	partitions := encodeTestJSON(t, map[string][]int32{fixture.Topic: {0, 1}})
	starts := encodeTestJSON(t, map[string]map[string]int64{fixture.Topic: {"0": 0, "1": 0}})
	captured := encodeTestJSON(t, map[string]map[string]int64{fixture.Topic: {
		"0": ends[fixture.Topic][0].Offset, "1": ends[fixture.Topic][1].Offset,
	}})
	_, err = fixture.Pool.Exec(ctx, `INSERT INTO consumer_readiness (consumer_name, projection_generation, state) VALUES ($1, $2, $3)`, fixture.Group, generation, state)
	require.NoError(t, err)
	_, err = fixture.Pool.Exec(ctx, `
		INSERT INTO consumer_replay_manifests (
			consumer_name, projection_generation, source_topic, topic_identity,
			partition_set, earliest_offsets, captured_end_offsets, started_at, operator_identity
		) VALUES ($1, $2, $3, $4, $5, $6, $7, now(), 'test')
	`, fixture.Group, generation, fixture.Topic, string(identity), partitions, starts, captured)
	require.NoError(t, err)
}

func encodeTestJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	return encoded
}

func certifyArgs(t *testing.T, fixture *recoverytest.Fixture) []string {
	t.Helper()
	service, consumer, found := strings.Cut(fixture.Group, ".")
	require.True(t, found)
	return []string{
		"--service", service, "--consumer", consumer,
		"--database-url", fixture.DatabaseURL, "--brokers", strings.Join(fixture.Seeds, ","),
	}
}
