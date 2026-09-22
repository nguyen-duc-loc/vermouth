// Command replay resets one consumer so it reads its topics again from the
// beginning (STK-22, INV-11).
//
// It does both halves in one invocation, because either half alone is a silent
// no operation: resetting the group without clearing handled_events means every
// replayed event is skipped as already handled, and clearing handled_events
// without resetting the group means nothing is redelivered. The caller stops
// the consumer before and starts it after, which is what `task
// replay:<service>:<consumer>` does.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/user"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

const (
	// emptyGroupTimeout is how long the reset waits for the consumer to leave
	// its group, which is a little over the broker's own session timeout.
	emptyGroupTimeout = 90 * time.Second
	// emptyGroupPoll is how often the group is asked again meanwhile.
	emptyGroupPoll = 2 * time.Second
	// wholeRunTimeout bounds the whole replay, so a broker that never answers
	// fails the command rather than hanging a terminal.
	wholeRunTimeout = time.Minute
)

// The two ways a replay refuses to run, static so the message is one string in
// one place.
var (
	errMissingFlags  = errors.New("service, consumer, topics, database-url and brokers are all required")
	errGroupNotEmpty = errors.New(
		"stop the consumer before replaying it, and give the broker its session timeout to forget a member that was killed rather than stopped",
	)
	errTopicMissing      = errors.New("topic was not returned by broker metadata")
	errOffsetMissing     = errors.New("replay offset is missing")
	errGenerationChanged = errors.New("projection generation changed during offset reset")
)

func main() {
	err := run(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "replay:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	return runWithReset(args, func(ctx context.Context, admin *kadm.Client, group string, offsets kadm.Offsets) error {
		return admin.CommitAllOffsets(ctx, group, offsets)
	})
}

//nolint:gocognit,funlen // Replay keeps the broker cut and database reset in one fail closed sequence.
func runWithReset(args []string, reset func(context.Context, *kadm.Client, string, kadm.Offsets) error) error {
	// Its own flag set rather than the global one: the parse then reports an
	// error instead of exiting from inside a function that is not main.
	flags := flag.NewFlagSet("replay", flag.ContinueOnError)
	service := flags.String("service", "", "the consuming service, for example notifications")
	consumer := flags.String("consumer", "", "the consumer name, for example recipients")
	topics := flags.String("topics", "", "comma separated topics the consumer reads")
	projectionTables := flags.String(
		"projection-tables",
		"",
		"comma separated rebuildable tables to clear before the offset reset",
	)
	databaseURL := flags.String("database-url", os.Getenv("REPLAY_DATABASE_URL"), "the consuming service's database")
	seeds := flags.String("brokers", os.Getenv("BROKER_SEEDS"), "comma separated broker addresses")
	err := flags.Parse(args)
	if err != nil {
		return fmt.Errorf("read the flags: %w", err)
	}

	if *service == "" || *consumer == "" || *topics == "" || *databaseURL == "" || *seeds == "" {
		return errMissingFlags
	}

	group := *service + "." + *consumer
	topicList := split(*topics)
	ctx, cancel := context.WithTimeout(context.Background(), wholeRunTimeout)
	defer cancel()

	client, err := kgo.NewClient(kgo.SeedBrokers(split(*seeds)...), kgo.ClientID("vermouth.replay"))
	if err != nil {
		return fmt.Errorf("open broker client: %w", err)
	}
	defer client.Close()

	admin := kadm.NewClient(client)

	// The consumer must have left the group first: the coordinator refuses an
	// offset commit for a group that still has a member, and a consumer that is
	// still reading would carry on past the reset anyway. Killing the process
	// starts the leave; the coordinator can take a moment to notice.
	err = waitForEmptyGroup(ctx, admin, group)
	if err != nil {
		return err
	}

	starts, err := admin.ListStartOffsets(ctx, topicList...)
	if err != nil {
		return fmt.Errorf("list earliest offsets: %w", err)
	}
	err = starts.Error()
	if err != nil {
		return fmt.Errorf("read earliest offsets: %w", err)
	}
	ends, err := admin.ListEndOffsets(ctx, topicList...)
	if err != nil {
		return fmt.Errorf("list captured end offsets: %w", err)
	}
	err = ends.Error()
	if err != nil {
		return fmt.Errorf("read captured end offsets: %w", err)
	}
	topicDetails, err := admin.ListTopics(ctx, topicList...)
	if err != nil {
		return fmt.Errorf("read topic identity: %w", err)
	}
	snapshot, err := newReplaySnapshot(topicList, topicDetails, starts, ends)
	if err != nil {
		return err
	}

	conn, err := pgx.Connect(ctx, *databaseURL)
	if err != nil {
		return fmt.Errorf("connect to %s database: %w", *service, err)
	}
	defer func() { _ = conn.Close(ctx) }()

	generation, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("create projection generation: %w", err)
	}
	operator := currentOperator()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("start replay transaction: %w", err)
	}
	defer func() {
		rollbackErr := tx.Rollback(ctx)
		if rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			fmt.Fprintln(os.Stderr, "replay: roll back preparation:", rollbackErr)
		}
	}()

	_, err = tx.Exec(ctx, `
		INSERT INTO consumer_readiness (consumer_name, projection_generation, state)
		VALUES ($1, $2, 'uncertified')
		ON CONFLICT (consumer_name) DO NOTHING
	`, group, generation)
	if err != nil {
		return fmt.Errorf("seed readiness for %s: %w", group, err)
	}
	var lockedGeneration uuid.UUID
	err = tx.QueryRow(ctx, `
		SELECT projection_generation
		FROM consumer_readiness
		WHERE consumer_name = $1
		FOR UPDATE
	`, group).Scan(&lockedGeneration)
	if err != nil {
		return fmt.Errorf("lock readiness for %s: %w", group, err)
	}
	_, err = tx.Exec(ctx, `
		UPDATE consumer_readiness
		SET projection_generation = $2,
			state = 'uncertified',
			updated_at = transaction_timestamp()
		WHERE consumer_name = $1
	`, group, generation)
	if err != nil {
		return fmt.Errorf("clear certification for %s: %w", group, err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO consumer_replay_manifests (
			consumer_name,
			projection_generation,
			source_topic,
			topic_identity,
			partition_set,
			earliest_offsets,
			captured_end_offsets,
			started_at,
			operator_identity
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, transaction_timestamp(), $8)
	`,
		group,
		generation,
		snapshot.sourceTopics,
		snapshot.topicIdentity,
		snapshot.partitionSet,
		snapshot.earliestOffsets,
		snapshot.capturedEnds,
		operator,
	)
	if err != nil {
		return fmt.Errorf("record replay manifest for %s: %w", group, err)
	}
	tag, err := tx.Exec(ctx, `DELETE FROM handled_events WHERE consumer_name = $1`, group)
	if err != nil {
		return fmt.Errorf("clear handled events for %s: %w", group, err)
	}
	var clearedProjectionRows int64
	for _, table := range split(*projectionTables) {
		projectionTag, deleteErr := tx.Exec(ctx, "DELETE FROM "+pgx.Identifier{table}.Sanitize())
		if deleteErr != nil {
			return fmt.Errorf("clear projection table %s: %w", table, deleteErr)
		}
		clearedProjectionRows += projectionTag.RowsAffected()
	}
	err = tx.Commit(ctx)
	if err != nil {
		return fmt.Errorf("commit replay preparation for %s: %w", group, err)
	}

	err = reset(ctx, admin, group, starts.Offsets())
	if err != nil {
		return fmt.Errorf("reset group %s to the earliest offset: %w", group, err)
	}
	// A cleared projection cannot be certified until every broker reset has
	// succeeded. A crash or partial reset leaves this generation uncertified.
	resetTag, err := conn.Exec(ctx, `
		UPDATE consumer_readiness
		SET state = 'replaying', updated_at = transaction_timestamp()
		WHERE consumer_name = $1 AND projection_generation = $2 AND state = 'uncertified'
	`, group, generation)
	if err != nil {
		return fmt.Errorf("record completed offset reset for %s: %w", group, err)
	}
	if resetTag.RowsAffected() != 1 {
		return errGenerationChanged
	}

	fmt.Printf(
		"replay ready: group %s generation %s reset on %s, %d handled event rows and %d projection rows cleared, certification required\n",
		group,
		generation,
		strings.Join(topicList, ", "),
		tag.RowsAffected(),
		clearedProjectionRows,
	)
	return nil
}

type replaySnapshot struct {
	sourceTopics    string
	topicIdentity   []byte
	partitionSet    []byte
	earliestOffsets []byte
	capturedEnds    []byte
}

func newReplaySnapshot(
	topics []string,
	details kadm.TopicDetails,
	starts kadm.ListedOffsets,
	ends kadm.ListedOffsets,
) (replaySnapshot, error) {
	identities := make(map[string]string, len(topics))
	partitions := make(map[string][]int32, len(topics))
	for _, topic := range topics {
		detail, ok := details[topic]
		if !ok {
			return replaySnapshot{}, fmt.Errorf("%w: %s", errTopicMissing, topic)
		}
		if detail.Err != nil {
			return replaySnapshot{}, fmt.Errorf("read topic %s metadata: %w", topic, detail.Err)
		}
		identities[topic] = detail.ID.String()
		for partition := range detail.Partitions {
			start, hasStart := starts[topic][partition]
			end, hasEnd := ends[topic][partition]
			if !hasStart || !hasEnd {
				return replaySnapshot{}, fmt.Errorf("%w: %s partition %d", errOffsetMissing, topic, partition)
			}
			partitions[topic] = append(partitions[topic], partition)
			if start.Offset < 0 || end.Offset < start.Offset {
				return replaySnapshot{}, fmt.Errorf("%w: %s partition %d has invalid bounds", errOffsetMissing, topic, partition)
			}
		}
		slices.Sort(partitions[topic])
	}

	identityJSON, err := json.Marshal(identities) //nolint:errchkjson // The map contains only strings.
	if err != nil {
		return replaySnapshot{}, fmt.Errorf("encode topic identities: %w", err)
	}
	partitionJSON, err := json.Marshal(partitions) //nolint:errchkjson // The map contains only integer slices.
	if err != nil {
		return replaySnapshot{}, fmt.Errorf("encode partition set: %w", err)
	}
	earliestJSON, err := marshalOffsets(starts)
	if err != nil {
		return replaySnapshot{}, fmt.Errorf("encode earliest offsets: %w", err)
	}
	endJSON, err := marshalOffsets(ends)
	if err != nil {
		return replaySnapshot{}, fmt.Errorf("encode captured end offsets: %w", err)
	}
	return replaySnapshot{
		sourceTopics:    strings.Join(topics, ","),
		topicIdentity:   identityJSON,
		partitionSet:    partitionJSON,
		earliestOffsets: earliestJSON,
		capturedEnds:    endJSON,
	}, nil
}

func marshalOffsets(offsets kadm.ListedOffsets) ([]byte, error) {
	encoded := make(map[string]map[string]int64, len(offsets))
	for topic, topicOffsets := range offsets {
		encoded[topic] = make(map[string]int64, len(topicOffsets))
		for partition, offset := range topicOffsets {
			if offset.Err != nil {
				return nil, fmt.Errorf("%s partition %d: %w", topic, partition, offset.Err)
			}
			encoded[topic][strconv.FormatInt(int64(partition), 10)] = offset.Offset
		}
	}
	return json.Marshal(encoded)
}

func currentOperator() string {
	current, err := user.Current()
	if err == nil && current.Username != "" {
		return current.Username
	}
	if name := strings.TrimSpace(os.Getenv("USER")); name != "" {
		return name
	}
	return "unknown"
}

// waitForEmptyGroup blocks until the group has no members, which is what makes
// the reset below allowed. A group that does not exist yet is already empty.
func waitForEmptyGroup(ctx context.Context, admin *kadm.Client, group string) error {
	deadline := time.Now().Add(emptyGroupTimeout)
	for {
		described, err := admin.DescribeGroups(ctx, group)
		if err != nil {
			return fmt.Errorf("describe group %s: %w", group, err)
		}
		one, found := described[group]
		if !found || one.State == "Empty" || one.State == "Dead" || len(one.Members) == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("group %s still has %d member(s) after %s: %w",
				group, len(one.Members), emptyGroupTimeout, errGroupNotEmpty)
		}
		fmt.Printf("waiting for %s to leave its group, state %s\n", group, one.State)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(emptyGroupPoll):
		}
	}
}

func split(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
