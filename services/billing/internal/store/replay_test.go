package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/nguyen-duc-loc/vermouth/services/billing/internal/store"
	"github.com/nguyen-duc-loc/vermouth/services/billing/internal/store/sqlcgen"
)

// How long the broker is given, generously, on a two core machine.
const (
	drainTimeout = 60 * time.Second
	drainPoll    = 250 * time.Millisecond
)

// teachingFacts is the tolerant reader's view of teaching's events: the fields
// billing needs and no others, so a field added later is ignored rather than
// demanded (INV-12). Calendar days arrive as days, not instants.
type teachingFacts struct {
	TutorID           uuid.UUID `json:"tutor_id"`
	ClassID           uuid.UUID `json:"class_id"`
	StudentID         uuid.UUID `json:"student_id"`
	SessionID         uuid.UUID `json:"session_id"`
	Name              string    `json:"name"`
	RateAmount        int64     `json:"rate_amount"`
	Currency          string    `json:"currency"`
	RateEffectiveFrom string    `json:"rate_effective_from"`
	EffectiveFrom     string    `json:"effective_from"`
	StartsAt          time.Time `json:"starts_at"`
	EndsAt            time.Time `json:"ends_at"`
	LocalDate         string    `json:"local_date"`
	State             string    `json:"state"`
	MarkedAt          time.Time `json:"marked_at"`
}

// handleTeachingEvent is the projection write for each teaching event, and the
// only thing these tests bring of their own to the consumer path: everything
// around it (the group, handled_events, the transaction, the retries, the park) is
// pkg/vermouth's real machinery, and every statement is billing's own generated
// query.
//
// It is the shape feature 13's internal/consumer will take. It lives in the test
// because feature 4 owns the tables and their idempotency, not the wiring that
// starts a consumer: teaching publishes none of these events yet.
func handleTeachingEvent(ctx context.Context, tx pgx.Tx, env vermouth.Envelope) error {
	var facts teachingFacts
	err := vermouth.DecodeInto(env, []int{1}, &facts)
	if err != nil {
		return err
	}
	tutorID, err := vermouth.TrustedTutorID(env, facts.TutorID)
	if err != nil {
		return err
	}
	q := store.Queries(tx)

	switch env.EventName {
	case vermouth.EventClassCreated:
		err = q.UpsertClass(ctx, sqlcgen.UpsertClassParams{
			ClassID: facts.ClassID, TutorID: tutorID, Name: facts.Name,
		})
		if err != nil {
			return err
		}
		// class.created seeds the first rate row from rate_effective_from, and
		// class.rate.changed appends from effective_from: two event field names
		// writing the one column.
		from, err := parseDay(facts.RateEffectiveFrom)
		if err != nil {
			return err
		}
		return q.UpsertClassRate(ctx, sqlcgen.UpsertClassRateParams{
			ClassID: facts.ClassID, EffectiveFrom: from, TutorID: tutorID,
			RateAmount: facts.RateAmount, Currency: facts.Currency,
		})

	case vermouth.EventClassChanged:
		return q.UpsertClass(ctx, sqlcgen.UpsertClassParams{
			ClassID: facts.ClassID, TutorID: tutorID, Name: facts.Name,
		})

	case vermouth.EventClassRateChanged:
		from, err := parseDay(facts.EffectiveFrom)
		if err != nil {
			return err
		}
		return q.UpsertClassRate(ctx, sqlcgen.UpsertClassRateParams{
			ClassID: facts.ClassID, EffectiveFrom: from, TutorID: tutorID,
			RateAmount: facts.RateAmount, Currency: facts.Currency,
		})

	case vermouth.EventStudentRegistered, vermouth.EventStudentChanged:
		return q.UpsertStudent(ctx, sqlcgen.UpsertStudentParams{
			StudentID: facts.StudentID, TutorID: tutorID, Name: facts.Name,
		})

	case vermouth.EventStudentRemoved:
		// The event carries no timestamp of its own, so the end is the envelope's
		// occurred_at (INV-4).
		return q.MarkStudentRemoved(ctx, sqlcgen.MarkStudentRemovedParams{
			TutorID: tutorID, StudentID: facts.StudentID, RemovedAt: at(env.OccurredAt),
		})

	case vermouth.EventSessionScheduled, vermouth.EventSessionMoved:
		localDate, err := parseDay(facts.LocalDate)
		if err != nil {
			return err
		}
		return q.UpsertSession(ctx, sqlcgen.UpsertSessionParams{
			SessionID: facts.SessionID, ClassID: facts.ClassID, TutorID: tutorID,
			StartsAt: facts.StartsAt, EndsAt: facts.EndsAt, LocalDate: localDate,
		})

	case vermouth.EventSessionCancelled:
		return q.MarkSessionCancelled(ctx, sqlcgen.MarkSessionCancelledParams{
			TutorID: tutorID, SessionID: facts.SessionID, CancelledAt: at(env.OccurredAt),
		})

	case vermouth.EventAttendanceMarked:
		return q.UpsertAttendance(ctx, sqlcgen.UpsertAttendanceParams{
			SessionID: facts.SessionID, StudentID: facts.StudentID, TutorID: tutorID,
			State: facts.State, MarkedAt: facts.MarkedAt,
		})

	case vermouth.EventRosterJoined:
		from, err := parseDay(facts.EffectiveFrom)
		if err != nil {
			return err
		}
		return q.OpenRosterPeriod(ctx, sqlcgen.OpenRosterPeriodParams{
			ClassID: facts.ClassID, StudentID: facts.StudentID, EffectiveFrom: from, TutorID: tutorID,
		})

	case vermouth.EventRosterLeft:
		// roster.left carries no effective_from, so the one open row for the pair
		// is the only well defined target, which the partial unique index makes
		// true rather than hoped for.
		return q.CloseRosterPeriod(ctx, sqlcgen.CloseRosterPeriodParams{
			TutorID: tutorID, ClassID: facts.ClassID, StudentID: facts.StudentID,
			EffectiveTo: day(env.OccurredAt.Month(), env.OccurredAt.Day()),
		})

	default:
		// The topic carries every event of the publishing service (STK-11), so an
		// event this consumer has no interest in is not a failure.
		return nil
	}
}

// errGroupNotEmpty is the one way the replay refuses: a consumer that still holds
// its group cannot have its offsets reset under it.
var errGroupNotEmpty = errors.New("the consumer group still has a member")

func TestTeachingConsumerRejectsAConflictingPayloadTenant(t *testing.T) {
	t.Parallel()

	envelopeTutorID := newID(t)
	payloadTutorID := newID(t)
	env := newEvent(
		t,
		vermouth.EventStudentRegistered,
		envelopeTutorID,
		vermouth.Key{Kind: vermouth.KeyStudentID, Value: newID(t)},
		teachingFacts{
			TutorID:   payloadTutorID,
			StudentID: newID(t),
			Name:      "Mai",
		},
	)

	var tx pgx.Tx
	err := handleTeachingEvent(t.Context(), tx, env)
	require.ErrorAs(t, err, new(*vermouth.TutorIDConflictError))
}

// parseDay reads the calendar day an event carries. A day travels as a day, so no
// timezone is involved in reading it back.
func parseDay(value string) (pgtype.Date, error) {
	moment, err := time.Parse(time.DateOnly, value)
	if err != nil {
		return pgtype.Date{}, fmt.Errorf("read the day %q: %w", value, err)
	}
	return pgtype.Date{Time: moment, Valid: true}, nil
}

// TestReplayRebuildsProjectionsAndNeverTouchesAnInvoice is the heart of AC-7 and
// AC-8, and the reason a replay is safe to run without reading a handler first.
//
// It delivers one event twice, drains the topic, issues an invoice from what
// landed, then replays the topic from the earliest offset exactly the way
// `task replay:` does: wait for the group to be empty, commit the earliest
// offsets, and delete that consumer's handled_events rows. Afterwards every
// projection reads the same, and the invoice, its lines, its number, its run and
// the profile fields are untouched.
// beside another copy of itself would prove nothing and cost a rebalance.
//
//nolint:paralleltest // One broker, one topic and one consumer group: running this
func TestReplayRebuildsProjectionsAndNeverTouchesAnInvoice(t *testing.T) {
	q := queries(t)
	ctx := t.Context()
	seeds := brokerSeeds(t)
	tutorID := newTutor(t)

	// A topic of this run's own, so the test never reads or writes the real
	// teaching.events log, and the same properties are proven either way.
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	topic := vermouth.TopicTeaching + ".test." + suffix
	require.NoError(t, vermouth.EnsureTopics(ctx, seeds, 3, topic))
	admin := newAdmin(t, seeds)
	t.Cleanup(func() {
		_, err := admin.DeleteTopics(teardownContext(), topic, vermouth.DLQTopic(topic))
		require.NoError(t, err)
	})

	consumer := vermouth.Consumer{
		Name:    "projections-test-" + suffix,
		Topics:  []string{topic},
		Accepts: []int{1},
		Handle:  handleTeachingEvent,
	}
	group := vermouth.GroupName("billing", consumer.Name)
	t.Cleanup(func() {
		done := teardownContext()
		_, err := pool.Exec(done, `DELETE FROM handled_events WHERE consumer_name = $1`, group)
		require.NoError(t, err)
		_, err = admin.DeleteGroups(done, group)
		require.NoError(t, err)
	})

	classID, studentID, sessionID := newID(t), newID(t), newID(t)
	starts := time.Date(2026, time.September, 14, 3, 0, 0, 0, time.UTC)
	envelopes := make([]vermouth.Envelope, 0, 6)
	envelopes = append(envelopes,
		newEvent(t, vermouth.EventClassCreated, tutorID, vermouth.Key{Kind: vermouth.KeyClassID, Value: classID},
			teachingFacts{
				TutorID: tutorID, ClassID: classID, Name: "Maths 9A",
				RateAmount: 250_000, Currency: "VND", RateEffectiveFrom: "2026-09-01",
			}),
		newEvent(t, vermouth.EventStudentRegistered, tutorID, vermouth.Key{Kind: vermouth.KeyStudentID, Value: studentID},
			teachingFacts{TutorID: tutorID, StudentID: studentID, Name: "Mai"}),
		newEvent(t, vermouth.EventRosterJoined, tutorID, vermouth.Key{Kind: vermouth.KeyClassID, Value: classID},
			teachingFacts{TutorID: tutorID, ClassID: classID, StudentID: studentID, EffectiveFrom: "2026-09-01"}),
		newEvent(t, vermouth.EventSessionScheduled, tutorID, vermouth.Key{Kind: vermouth.KeySessionID, Value: sessionID},
			teachingFacts{
				TutorID: tutorID, ClassID: classID, SessionID: sessionID,
				StartsAt: starts, EndsAt: starts.Add(90 * time.Minute), LocalDate: "2026-09-14",
			}),
		newEvent(t, vermouth.EventAttendanceMarked, tutorID, vermouth.Key{Kind: vermouth.KeySessionID, Value: sessionID},
			teachingFacts{
				TutorID: tutorID, ClassID: classID, SessionID: sessionID, StudentID: studentID,
				State: "Present", MarkedAt: starts.Add(time.Hour),
			}),
	)

	// The session is delivered twice, the same event_id and all: a redelivery is
	// what a broker does when a commit did not land, and it must change nothing.
	produce(t, seeds, topic, append(envelopes, envelopes[3]))

	cfg := vermouth.Config{
		Service:         "billing",
		BrokerSeeds:     seeds,
		TopicPartitions: 3,
		RetryMax:        3,
		RetryBaseDelay:  50 * time.Millisecond,
	}
	drain(t, cfg, consumer, group, len(envelopes))

	billable := billableFor(t, q, tutorID, time.September)
	require.Len(t, billable, 1, "one session, one student, one billable row, delivered twice or not")
	require.Equal(t, int64(250_000), billable[0].RateAmount)
	require.Equal(t, "Mai", billable[0].StudentName)
	require.Equal(t, "Maths 9A", billable[0].ClassName)

	// Now the authoritative records the replay must not touch.
	profile := saveCompleteProfile(t, q, tutorID, "0123456789")
	invoice, line := issueOneInvoice(t, q, tutorID, profile)
	counterBefore, err := q.GetInvoiceNumberCounter(ctx, sqlcgen.GetInvoiceNumberCounterParams{TutorID: tutorID, PeriodYear: 2026})
	require.NoError(t, err)
	runBefore, err := q.GetBillingRun(ctx, sqlcgen.GetBillingRunParams{TutorID: tutorID, BillingRunID: invoice.BillingRunID})
	require.NoError(t, err)

	// The replay itself: both halves, because either alone is a silent no
	// operation (STK-22).
	require.NoError(t, waitForEmptyGroup(ctx, admin, group))
	starts0, err := admin.ListStartOffsets(ctx, topic)
	require.NoError(t, err)
	require.NoError(t, admin.CommitAllOffsets(ctx, group, starts0.Offsets()))
	_, err = pool.Exec(ctx, `DELETE FROM handled_events WHERE consumer_name = $1`, group)
	require.NoError(t, err)

	drain(t, cfg, consumer, group, len(envelopes))

	require.Equal(t, billable, billableFor(t, q, tutorID, time.September),
		"a full replay must leave every projection business field as it was (AC-7, INV-11)")

	invoiceAfter, err := q.GetInvoice(ctx, sqlcgen.GetInvoiceParams{TutorID: tutorID, InvoiceID: invoice.InvoiceID})
	require.NoError(t, err)
	require.Equal(t, invoice, invoiceAfter, "no replay may rewrite an invoice (AC-8)")

	linesAfter, err := q.ListInvoiceLines(ctx, sqlcgen.ListInvoiceLinesParams{TutorID: tutorID, InvoiceID: invoice.InvoiceID})
	require.NoError(t, err)
	require.Equal(t, []sqlcgen.InvoiceLine{line}, linesAfter, "nor an invoice line (AC-8)")

	counterAfter, err := q.GetInvoiceNumberCounter(ctx, sqlcgen.GetInvoiceNumberCounterParams{TutorID: tutorID, PeriodYear: 2026})
	require.NoError(t, err)
	require.Equal(t, counterBefore, counterAfter, "nor the invoice number counter (AC-8)")

	runAfter, err := q.GetBillingRun(ctx, sqlcgen.GetBillingRunParams{TutorID: tutorID, BillingRunID: invoice.BillingRunID})
	require.NoError(t, err)
	require.Equal(t, runBefore, runAfter, "nor the month end run (AC-8)")

	profileAfter, err := q.GetInvoiceProfile(ctx, tutorID)
	require.NoError(t, err)
	require.Equal(t, profile, profileAfter, "nor a single field of the invoice profile (AC-8)")
}

// TestSeedingAProfileTwiceKeepsWhatTheTutorTyped is the one place a consumer
// writes an authoritative table, and the reason it is safe: the seed writes no
// field, so a replay recreates a missing row and can never overwrite bank details.
func TestSeedingAProfileTwiceKeepsWhatTheTutorTyped(t *testing.T) {
	t.Parallel()
	q := queries(t)
	ctx := t.Context()
	tutorID := newTutor(t)

	require.NoError(t, q.SeedInvoiceProfile(ctx, tutorID))
	empty, err := q.GetInvoiceProfile(ctx, tutorID)
	require.NoError(t, err)
	require.False(t, empty.IsComplete, "a seeded row is empty, so the month end run refuses until the tutor fills it")

	typed := saveCompleteProfile(t, q, tutorID, "0123456789")
	require.NoError(t, q.SeedInvoiceProfile(ctx, tutorID))
	after, err := q.GetInvoiceProfile(ctx, tutorID)
	require.NoError(t, err)
	require.Equal(t, typed, after, "seeding again must change nothing the tutor typed (AC-8)")
}

// brokerSeeds reads the one broker of test/compose.test.yaml, skipping when it is
// not up.
func brokerSeeds(t *testing.T) []string {
	t.Helper()
	raw := os.Getenv("BROKER_SEEDS")
	if raw == "" {
		t.Skip("BROKER_SEEDS is unset: run task infra:up, then task test")
	}
	return strings.Split(raw, ",")
}

func newAdmin(t *testing.T, seeds []string) *kadm.Client {
	t.Helper()
	client, err := kgo.NewClient(kgo.SeedBrokers(seeds...), kgo.ClientID("billing.test.admin"))
	require.NoError(t, err)
	t.Cleanup(client.Close)
	return kadm.NewClient(client)
}

// newEvent builds an envelope the way a publisher does, so the test exercises the
// same encode and decode path production does.
func newEvent(t *testing.T, name string, tutorID uuid.UUID, key vermouth.Key, facts teachingFacts) vermouth.Envelope {
	t.Helper()
	env, err := vermouth.NewEnvelope(t.Context(), name, 1, tutorID, key, facts)
	require.NoError(t, err)
	return env
}

func produce(t *testing.T, seeds []string, topic string, envelopes []vermouth.Envelope) {
	t.Helper()
	producer, err := vermouth.NewProducer(seeds, "billing.test")
	require.NoError(t, err)
	defer producer.Close()

	records := make([]*kgo.Record, 0, len(envelopes))
	for _, env := range envelopes {
		value, err := json.Marshal(env)
		require.NoError(t, err)
		records = append(records, &kgo.Record{Topic: topic, Key: env.Key.Bytes(), Value: value})
	}
	require.NoError(t, producer.ProduceSync(t.Context(), records...).FirstErr())
}

// drain runs the real consumer until it has handled the number of distinct events
// expected, then stops it and waits for it to leave its group, which is what makes
// the next reset allowed.
func drain(t *testing.T, cfg vermouth.Config, consumer vermouth.Consumer, group string, want int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), drainTimeout)
	defer cancel()

	stopped := make(chan error, 1)
	go func() {
		stopped <- vermouth.RunConsumer(ctx, cfg, pool, quietLogger(), consumer)
	}()

	deadline := time.Now().Add(drainTimeout - drainPoll)
	for handledCount(t, group) < want && time.Now().Before(deadline) {
		time.Sleep(drainPoll)
	}
	handled := handledCount(t, group)
	cancel()
	require.NoError(t, <-stopped)
	require.Equal(t, want, handled,
		"the consumer must handle each distinct event exactly once, however many times it arrives (AC-7, INV-5)")
}

// handledCount is how many distinct events this consumer has recorded. It reads
// handled_events with pgx directly, because the shared module's own tables are
// never queried through db/queries (STK-3).
func handledCount(t *testing.T, group string) int {
	t.Helper()
	var count int
	err := pool.QueryRow(t.Context(),
		`SELECT count(*) FROM handled_events WHERE consumer_name = $1`, group).Scan(&count)
	require.NoError(t, err)
	return count
}

// waitForEmptyGroup is the same wait `task replay:` does: the coordinator refuses
// an offset commit for a group that still has a member.
func waitForEmptyGroup(ctx context.Context, admin *kadm.Client, group string) error {
	deadline := time.Now().Add(drainTimeout)
	for {
		described, err := admin.DescribeGroups(ctx, group)
		if err != nil {
			return err
		}
		one, found := described[group]
		if !found || one.State == "Empty" || one.State == "Dead" || len(one.Members) == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return errGroupNotEmpty
		}
		time.Sleep(drainPoll)
	}
}

// quietLogger keeps a passing run silent and a failing one readable: warnings and
// errors only, which is where a retry or a park would show up.
func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}
