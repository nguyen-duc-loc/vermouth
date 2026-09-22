// Command consumerfailures gives an operator a safe view of parked records
// and the narrow recovery actions defined by spec 0013.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
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

const commandTimeout = time.Minute

var (
	errUsage              = errors.New("use list, acknowledge, or certify")
	errDatabaseFlags      = errors.New("service, consumer, and database-url are required")
	errBrokerFlags        = errors.New("brokers are required")
	errAcknowledgeFlags   = errors.New("topic, nonnegative partition and offset, and repair-reference are required")
	errResolutionCode     = errors.New("resolution-code must be source_repaired or projection_restored")
	errNoMatchingFailure  = errors.New("no matching unresolved decode_failed record")
	errUnresolvedFailures = errors.New("unresolved consumer failure records remain")
	errProjectionState    = errors.New("projection is not replaying")
	errRetentionGap       = errors.New("retained history has a gap")
	errTopicMetadata      = errors.New("live topic metadata is unavailable")
	errTopicIdentity      = errors.New("topic identity changed")
	errPartitionSet       = errors.New("partition set changed")
	errMissingOffset      = errors.New("committed offset is missing")
	errReplayIncomplete   = errors.New("replay has not reached the captured end")
	errIncompleteOffsets  = errors.New("replay offset map is incomplete")
)

type commonFlags struct {
	service     string
	consumer    string
	databaseURL string
	brokers     string
}

type failureView struct {
	Consumer        string     `json:"consumer"`
	SourceTopic     string     `json:"source_topic"`
	SourcePartition int32      `json:"source_partition"`
	SourceOffset    int64      `json:"source_offset"`
	EventID         *uuid.UUID `json:"event_id,omitempty"`
	TutorID         *uuid.UUID `json:"tutor_id,omitempty"`
	Category        string     `json:"failure_category"`
	FailedAt        time.Time  `json:"failed_at"`
}

func main() {
	err := run(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "consumerfailures:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errUsage
	}
	switch args[0] {
	case "list":
		return runList(args[1:])
	case "acknowledge":
		return runAcknowledge(args[1:])
	case "certify":
		return runCertify(args[1:])
	default:
		return errUsage
	}
}

func addCommonFlags(flags *flag.FlagSet, values *commonFlags) {
	flags.StringVar(&values.service, "service", "", "the service that owns the consumer")
	flags.StringVar(&values.consumer, "consumer", "", "the consumer name inside the service")
	flags.StringVar(
		&values.databaseURL,
		"database-url",
		os.Getenv("CONSUMER_FAILURES_DATABASE_URL"),
		"the consuming service database",
	)
}

func addBrokerFlag(flags *flag.FlagSet, values *commonFlags) {
	flags.StringVar(
		&values.brokers,
		"brokers",
		os.Getenv("BROKER_SEEDS"),
		"comma separated broker addresses",
	)
}

func (f commonFlags) validateDatabase() error {
	if f.service == "" || f.consumer == "" || f.databaseURL == "" {
		return errDatabaseFlags
	}
	return nil
}

func (f commonFlags) validateBroker() error {
	err := f.validateDatabase()
	if err != nil {
		return err
	}
	if strings.TrimSpace(f.brokers) == "" {
		return errBrokerFlags
	}
	return nil
}

func (f commonFlags) group() string { return f.service + "." + f.consumer }

//nolint:funlen // The safe list command keeps flag parsing, query, scan, and JSON output together.
func runList(args []string) error {
	flags := flag.NewFlagSet("list", flag.ContinueOnError)
	var common commonFlags
	addCommonFlags(flags, &common)
	err := flags.Parse(args)
	if err != nil {
		return fmt.Errorf("read list flags: %w", err)
	}
	err = common.validateDatabase()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	conn, err := pgx.Connect(ctx, common.databaseURL)
	if err != nil {
		return fmt.Errorf("connect to %s database: %w", common.service, err)
	}
	defer func() { _ = conn.Close(ctx) }()

	rows, err := conn.Query(ctx, `
		SELECT consumer_name,
			source_topic,
			source_partition,
			source_offset,
			event_id,
			tutor_id,
			failure_category,
			failed_at
		FROM consumer_failures
		WHERE consumer_name = $1 AND resolved_at IS NULL
		ORDER BY source_topic, source_partition, source_offset
	`, common.group())
	if err != nil {
		return fmt.Errorf("list unresolved failures: %w", err)
	}
	defer rows.Close()

	views := make([]failureView, 0)
	for rows.Next() {
		var view failureView
		err = rows.Scan(
			&view.Consumer,
			&view.SourceTopic,
			&view.SourcePartition,
			&view.SourceOffset,
			&view.EventID,
			&view.TutorID,
			&view.Category,
			&view.FailedAt,
		)
		if err != nil {
			return fmt.Errorf("read unresolved failure: %w", err)
		}
		views = append(views, view)
	}
	err = rows.Err()
	if err != nil {
		return fmt.Errorf("iterate unresolved failures: %w", err)
	}
	return json.NewEncoder(os.Stdout).Encode(views)
}

//nolint:funlen // The guarded acknowledgement keeps its exact coordinate and audit update together.
func runAcknowledge(args []string) error {
	flags := flag.NewFlagSet("acknowledge", flag.ContinueOnError)
	var common commonFlags
	addCommonFlags(flags, &common)
	topic := flags.String("topic", "", "the source topic")
	partition := flags.Int("partition", -1, "the source partition")
	offset := flags.Int64("offset", -1, "the source offset")
	resolutionCode := flags.String("resolution-code", "", "source_repaired or projection_restored")
	repairReference := flags.String("repair-reference", "", "the constrained repair reference")
	err := flags.Parse(args)
	if err != nil {
		return fmt.Errorf("read acknowledge flags: %w", err)
	}
	err = common.validateDatabase()
	if err != nil {
		return err
	}
	if *topic == "" || *partition < 0 || *partition > math.MaxInt32 || *offset < 0 || *repairReference == "" {
		return errAcknowledgeFlags
	}
	if *resolutionCode != "source_repaired" && *resolutionCode != "projection_restored" {
		return errResolutionCode
	}

	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	conn, err := pgx.Connect(ctx, common.databaseURL)
	if err != nil {
		return fmt.Errorf("connect to %s database: %w", common.service, err)
	}
	defer func() { _ = conn.Close(ctx) }()

	tag, err := conn.Exec(ctx, `
		UPDATE consumer_failures
		SET resolved_at = transaction_timestamp(),
			resolution = 'acknowledged',
			resolution_code = $5,
			resolved_by = $6,
			repair_reference = $7
		WHERE consumer_name = $1
			AND source_topic = $2
			AND source_partition = $3
			AND source_offset = $4
			AND failure_category = 'decode_failed'
			AND resolved_at IS NULL
	`,
		common.group(),
		*topic,
		int32(*partition),
		*offset,
		*resolutionCode,
		currentOperator(),
		*repairReference,
	)
	if err != nil {
		return fmt.Errorf("acknowledge failure: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return errNoMatchingFailure
	}
	fmt.Printf("acknowledged %s %s partition %d offset %d\n", common.group(), *topic, *partition, *offset)
	return nil
}

type manifestEvidence struct {
	generation       uuid.UUID
	sourceTopics     string
	topicIdentity    map[string]string
	partitionSet     map[string][]int32
	earliestOffsets  map[string]map[string]int64
	capturedEnd      map[string]map[string]int64
	completedOffsets map[string]map[string]int64
}

//nolint:funlen,gocognit // Certification keeps one lock, broker proof, and database commit in a visible sequence.
func runCertify(args []string) error {
	flags := flag.NewFlagSet("certify", flag.ContinueOnError)
	var common commonFlags
	addCommonFlags(flags, &common)
	addBrokerFlag(flags, &common)
	err := flags.Parse(args)
	if err != nil {
		return fmt.Errorf("read certify flags: %w", err)
	}
	err = common.validateBroker()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	conn, err := pgx.Connect(ctx, common.databaseURL)
	if err != nil {
		return fmt.Errorf("connect to %s database: %w", common.service, err)
	}
	defer func() { _ = conn.Close(ctx) }()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("start certification transaction: %w", err)
	}
	defer func() {
		rollbackErr := tx.Rollback(ctx)
		if rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			fmt.Fprintln(os.Stderr, "consumerfailures: roll back certification:", rollbackErr)
		}
	}()

	evidence, err := loadManifestForCertification(ctx, tx, common.group())
	if err != nil {
		return err
	}
	err = validateRetainedHistory(evidence.earliestOffsets)
	if err != nil {
		return err
	}

	client, err := kgo.NewClient(
		kgo.SeedBrokers(split(common.brokers)...),
		kgo.ClientID("vermouth.consumerfailures"),
	)
	if err != nil {
		return fmt.Errorf("open broker client: %w", err)
	}
	defer client.Close()
	admin := kadm.NewClient(client)
	topics := split(evidence.sourceTopics)
	err = validateLiveMetadata(ctx, admin, topics, evidence)
	if err != nil {
		return err
	}
	committed, err := admin.FetchOffsets(ctx, common.group())
	if err != nil {
		return fmt.Errorf("read committed offsets for %s: %w", common.group(), err)
	}
	err = committed.Error()
	if err != nil {
		return fmt.Errorf("read committed offset: %w", err)
	}
	evidence.completedOffsets, err = validateCompletedOffsets(committed, evidence.capturedEnd)
	if err != nil {
		return err
	}
	// Check retention after observing completed progress, so a prefix lost
	// while the consumer was catching up cannot hide behind an earlier read.
	err = validateLiveRetainedHistory(ctx, admin, topics, evidence.partitionSet)
	if err != nil {
		return err
	}

	var unresolved int
	err = tx.QueryRow(ctx, `
		SELECT count(*)
		FROM consumer_failures
		WHERE consumer_name = $1 AND resolved_at IS NULL
	`, common.group()).Scan(&unresolved)
	if err != nil {
		return fmt.Errorf("check unresolved failures: %w", err)
	}
	if unresolved != 0 {
		return fmt.Errorf("%w: %s has %d", errUnresolvedFailures, common.group(), unresolved)
	}

	completedJSON, err := json.Marshal(evidence.completedOffsets) //nolint:errchkjson // The nested maps contain only strings and integers.
	if err != nil {
		return fmt.Errorf("encode completed offsets: %w", err)
	}
	_, err = tx.Exec(ctx, `
		UPDATE consumer_replay_manifests
		SET completed_offsets = $3,
			completed_at = transaction_timestamp()
		WHERE consumer_name = $1 AND projection_generation = $2
	`, common.group(), evidence.generation, completedJSON)
	if err != nil {
		return fmt.Errorf("complete replay manifest: %w", err)
	}
	_, err = tx.Exec(ctx, `
		UPDATE consumer_readiness
		SET state = 'certified', updated_at = transaction_timestamp()
		WHERE consumer_name = $1 AND projection_generation = $2
	`, common.group(), evidence.generation)
	if err != nil {
		return fmt.Errorf("certify projection: %w", err)
	}
	err = tx.Commit(ctx)
	if err != nil {
		return fmt.Errorf("commit certification: %w", err)
	}
	fmt.Printf("certified %s generation %s\n", common.group(), evidence.generation)
	return nil
}

func loadManifestForCertification(
	ctx context.Context,
	tx pgx.Tx,
	group string,
) (manifestEvidence, error) {
	var evidence manifestEvidence
	var state string
	var topicIdentityJSON, partitionSetJSON, earliestJSON, capturedEndJSON []byte
	err := tx.QueryRow(ctx, `
		SELECT r.projection_generation,
			r.state,
			m.source_topic,
			m.topic_identity,
			m.partition_set,
			m.earliest_offsets,
			m.captured_end_offsets
		FROM consumer_readiness r
		JOIN consumer_replay_manifests m
			ON m.consumer_name = r.consumer_name
			AND m.projection_generation = r.projection_generation
		WHERE r.consumer_name = $1
		FOR UPDATE OF r
	`, group).Scan(
		&evidence.generation,
		&state,
		&evidence.sourceTopics,
		&topicIdentityJSON,
		&partitionSetJSON,
		&earliestJSON,
		&capturedEndJSON,
	)
	if err != nil {
		return manifestEvidence{}, fmt.Errorf("load current replay manifest: %w", err)
	}
	if state != "replaying" {
		return manifestEvidence{}, fmt.Errorf("%w: got %s", errProjectionState, state)
	}
	err = json.Unmarshal(topicIdentityJSON, &evidence.topicIdentity)
	if err != nil {
		return manifestEvidence{}, fmt.Errorf("decode replay topic identities: %w", err)
	}
	err = json.Unmarshal(partitionSetJSON, &evidence.partitionSet)
	if err != nil {
		return manifestEvidence{}, fmt.Errorf("decode replay partition set: %w", err)
	}
	err = json.Unmarshal(earliestJSON, &evidence.earliestOffsets)
	if err != nil {
		return manifestEvidence{}, fmt.Errorf("decode replay earliest offsets: %w", err)
	}
	err = json.Unmarshal(capturedEndJSON, &evidence.capturedEnd)
	if err != nil {
		return manifestEvidence{}, fmt.Errorf("decode replay captured ends: %w", err)
	}
	for _, offsets := range []map[string]map[string]int64{evidence.earliestOffsets, evidence.capturedEnd} {
		err = validateOffsetMap(offsets, evidence.partitionSet)
		if err != nil {
			return manifestEvidence{}, err
		}
	}
	return evidence, nil
}

func validateOffsetMap(offsets map[string]map[string]int64, partitionSet map[string][]int32) error {
	if len(partitionSet) == 0 || len(offsets) != len(partitionSet) {
		return errIncompleteOffsets
	}
	for topic, partitions := range partitionSet {
		if len(partitions) == 0 || len(offsets[topic]) != len(partitions) {
			return fmt.Errorf("%w: %s", errIncompleteOffsets, topic)
		}
		for _, partition := range partitions {
			offset, ok := offsets[topic][strconv.FormatInt(int64(partition), 10)]
			if !ok || offset < 0 {
				return fmt.Errorf("%w: %s partition %d", errIncompleteOffsets, topic, partition)
			}
		}
	}
	return nil
}

func validateLiveRetainedHistory(
	ctx context.Context,
	admin *kadm.Client,
	topics []string,
	partitionSet map[string][]int32,
) error {
	starts, err := admin.ListStartOffsets(ctx, topics...)
	if err != nil {
		return fmt.Errorf("read live earliest offsets: %w", err)
	}
	err = starts.Error()
	if err != nil {
		return fmt.Errorf("read live earliest offset: %w", err)
	}
	live := make(map[string]map[string]int64, len(starts))
	for topic, partitions := range starts {
		live[topic] = make(map[string]int64, len(partitions))
		for partition, offset := range partitions {
			live[topic][strconv.FormatInt(int64(partition), 10)] = offset.Offset
		}
	}
	err = validateOffsetMap(live, partitionSet)
	if err != nil {
		return err
	}
	return validateRetainedHistory(live)
}

func validateRetainedHistory(offsets map[string]map[string]int64) error {
	for topic, partitions := range offsets {
		for partition, offset := range partitions {
			if offset != 0 {
				return fmt.Errorf(
					"%w: %s partition %s starts at %d",
					errRetentionGap,
					topic,
					partition,
					offset,
				)
			}
		}
	}
	return nil
}

func validateLiveMetadata(
	ctx context.Context,
	admin *kadm.Client,
	topics []string,
	evidence manifestEvidence,
) error {
	details, err := admin.ListTopics(ctx, topics...)
	if err != nil {
		return fmt.Errorf("read live topic metadata: %w", err)
	}
	if len(topics) == 0 || len(topics) != len(evidence.partitionSet) {
		return errPartitionSet
	}
	for _, topic := range topics {
		detail, ok := details[topic]
		if !ok || detail.Err != nil {
			return fmt.Errorf("%w: %s", errTopicMetadata, topic)
		}
		if evidence.topicIdentity[topic] != detail.ID.String() {
			return fmt.Errorf("%w: %s", errTopicIdentity, topic)
		}
		livePartitions := make([]int32, 0, len(detail.Partitions))
		for partition := range detail.Partitions {
			livePartitions = append(livePartitions, partition)
		}
		slices.Sort(livePartitions)
		storedPartitions := evidence.partitionSet[topic]
		if len(livePartitions) != len(storedPartitions) {
			return fmt.Errorf("%w: %s", errPartitionSet, topic)
		}
		for index := range livePartitions {
			if livePartitions[index] != storedPartitions[index] {
				return fmt.Errorf("%w: %s", errPartitionSet, topic)
			}
		}
	}
	return nil
}

func validateCompletedOffsets(
	committed kadm.OffsetResponses,
	captured map[string]map[string]int64,
) (map[string]map[string]int64, error) {
	completed := make(map[string]map[string]int64, len(captured))
	for topic, partitions := range captured {
		completed[topic] = make(map[string]int64, len(partitions))
		for rawPartition, capturedEnd := range partitions {
			partition, err := strconv.ParseInt(rawPartition, 10, 32)
			if err != nil {
				return nil, fmt.Errorf("invalid stored partition %s: %w", rawPartition, err)
			}
			partitionID := int32(partition)
			offset, ok := committed.Lookup(topic, partitionID)
			if !ok {
				return nil, fmt.Errorf("%w: %s partition %d", errMissingOffset, topic, partition)
			}
			if offset.At < capturedEnd {
				return nil, fmt.Errorf(
					"%w: %s partition %d committed %d, need %d",
					errReplayIncomplete,
					topic,
					partition,
					offset.At,
					capturedEnd,
				)
			}
			completed[topic][rawPartition] = offset.At
		}
	}
	return completed, nil
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
