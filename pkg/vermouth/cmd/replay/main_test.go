package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth/internal/recoverytest"
)

var errTestResetInterrupted = errors.New("reset interrupted")

func TestReplayResetCompletionGatesGeneration(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		name    string
		partial bool
		reset   bool
		success bool
	}{
		{name: "crash after preparation"},
		{name: "partial broker reset", partial: true},
		{name: "crash after broker reset", reset: true},
		{name: "complete broker reset", success: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			fixture := recoverytest.New(t)
			ctx := t.Context()
			records := []*kgo.Record{
				{Topic: fixture.Topic, Partition: 0, Value: []byte("one")},
				{Topic: fixture.Topic, Partition: 1, Value: []byte("two")},
			}
			require.NoError(t, fixture.Client.ProduceSync(ctx, records...).FirstErr())
			ends, err := fixture.Admin.ListEndOffsets(ctx, fixture.Topic)
			require.NoError(t, err)
			require.NoError(t, fixture.Admin.CommitAllOffsets(ctx, fixture.Group, ends.Offsets()))
			_, err = fixture.Pool.Exec(ctx, `CREATE TABLE projection (value text); INSERT INTO projection VALUES ('old')`)
			require.NoError(t, err)
			_, err = fixture.Pool.Exec(ctx, `INSERT INTO handled_events (consumer_name, event_id) VALUES ($1, $2)`, fixture.Group, uuid.New())
			require.NoError(t, err)
			service, consumer, found := strings.Cut(fixture.Group, ".")
			require.True(t, found)
			var preparedState string
			var preparedGeneration uuid.UUID
			err = runWithReset([]string{
				"--service", service, "--consumer", consumer,
				"--topics", fixture.Topic, "--database-url", fixture.DatabaseURL,
				"--brokers", strings.Join(fixture.Seeds, ","), "--projection-tables", "projection",
			}, func(resetCtx context.Context, admin *kadm.Client, group string, offsets kadm.Offsets) error {
				readErr := fixture.Pool.QueryRow(resetCtx, `SELECT state, projection_generation FROM consumer_readiness WHERE consumer_name = $1`, group).
					Scan(&preparedState, &preparedGeneration)
				require.NoError(t, readErr)
				if scenario.success {
					return admin.CommitAllOffsets(resetCtx, group, offsets)
				}
				if scenario.partial {
					partial := kadm.Offsets{fixture.Topic: {0: offsets[fixture.Topic][0]}}
					require.NoError(t, admin.CommitAllOffsets(resetCtx, group, partial))
				}
				if scenario.reset {
					require.NoError(t, admin.CommitAllOffsets(resetCtx, group, offsets))
				}
				return errTestResetInterrupted
			})
			if scenario.success {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, errTestResetInterrupted)
			}
			var state string
			var generation uuid.UUID
			require.NoError(t, fixture.Pool.QueryRow(ctx, `SELECT state, projection_generation FROM consumer_readiness WHERE consumer_name = $1`, fixture.Group).Scan(&state, &generation))
			require.Equal(t, preparedGeneration, generation)
			require.Equal(t, "uncertified", preparedState, "the cleared generation must refuse certification before resetting offsets")
			committed, err := fixture.Admin.FetchOffsets(ctx, fixture.Group)
			require.NoError(t, err)
			for partition := range int32(2) {
				offset, ok := committed.Lookup(fixture.Topic, partition)
				require.True(t, ok)
				want := int64(1)
				if scenario.success || scenario.reset || (scenario.partial && partition == 0) {
					want = 0
				}
				require.Equal(t, want, offset.At)
			}
			if scenario.success {
				require.Equal(t, "replaying", state)
			} else {
				require.Equal(t, "uncertified", state)
			}
		})
	}
}

func TestNewReplaySnapshotRequiresEveryPartition(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		name   string
		starts kadm.ListedOffsets
		ends   kadm.ListedOffsets
	}{
		{name: "missing earliest offset", ends: kadm.ListedOffsets{"facts": {0: {Offset: 2}}}},
		{name: "missing end offset", starts: kadm.ListedOffsets{"facts": {0: {Offset: 0}}}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			_, err := newReplaySnapshot([]string{"facts"}, kadm.TopicDetails{
				"facts": {Partitions: kadm.PartitionDetails{0: {Partition: 0}}},
			}, scenario.starts, scenario.ends)
			require.ErrorIs(t, err, errOffsetMissing)
		})
	}
}
