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
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

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
)

func main() {
	err := run(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "replay:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	// Its own flag set rather than the global one: the parse then reports an
	// error instead of exiting from inside a function that is not main.
	flags := flag.NewFlagSet("replay", flag.ContinueOnError)
	service := flags.String("service", "", "the consuming service, for example notifications")
	consumer := flags.String("consumer", "", "the consumer name, for example recipients")
	topics := flags.String("topics", "", "comma separated topics the consumer reads")
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
	err = admin.CommitAllOffsets(ctx, group, starts.Offsets())
	if err != nil {
		return fmt.Errorf("reset group %s to the earliest offset: %w", group, err)
	}

	conn, err := pgx.Connect(ctx, *databaseURL)
	if err != nil {
		return fmt.Errorf("connect to %s database: %w", *service, err)
	}
	defer func() { _ = conn.Close(ctx) }()

	tag, err := conn.Exec(ctx, `DELETE FROM handled_events WHERE consumer_name = $1`, group)
	if err != nil {
		return fmt.Errorf("clear handled events for %s: %w", group, err)
	}

	fmt.Printf("replay ready: group %s reset to the earliest offset on %s, %d handled event rows cleared\n",
		group, strings.Join(topicList, ", "), tag.RowsAffected())
	return nil
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
