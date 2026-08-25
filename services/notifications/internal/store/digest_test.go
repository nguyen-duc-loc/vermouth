package store_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/nguyen-duc-loc/vermouth/services/notifications/internal/store/sqlcgen"
)

// TestTheRosterIsCountedAtReadTime is AC-9, and the answer to spec 0001's follow
// up about the roster count. There is no stored integer to drift: the digest counts
// the membership rows whose period covers its day, so a redelivery cannot double
// count and a replay cannot multiply the number.
func TestTheRosterIsCountedAtReadTime(t *testing.T) {
	t.Parallel()
	q := queries(t)
	ctx := t.Context()
	tutorID := newTutor(t)
	classID, sessionID := newID(t), newID(t)
	theDay := day(time.September, 10)
	starts := time.Date(2026, time.September, 10, 3, 0, 0, 0, time.UTC)

	require.NoError(t, q.UpsertClass(ctx, sqlcgen.UpsertClassParams{ClassID: classID, TutorID: tutorID, Name: "Maths 9A"}))
	require.NoError(t, q.UpsertSession(ctx, sqlcgen.UpsertSessionParams{
		SessionID: sessionID, ClassID: classID, TutorID: tutorID,
		StartsAt: starts, EndsAt: starts.Add(90 * time.Minute), LocalDate: theDay,
	}))

	// Three students: one still a member, one who left the day before, and one who
	// leaves on the day itself. The one leaving on the day still counts, because a
	// period covers both its ends.
	staying, gone, leavingToday := newID(t), newID(t), newID(t)
	for _, studentID := range []uuid.UUID{staying, gone, leavingToday} {
		require.NoError(t, q.OpenRosterPeriod(ctx, sqlcgen.OpenRosterPeriodParams{
			ClassID: classID, StudentID: studentID, EffectiveFrom: day(time.September, 1), TutorID: tutorID,
		}))
	}
	require.NoError(t, q.CloseRosterPeriod(ctx, sqlcgen.CloseRosterPeriodParams{
		TutorID: tutorID, ClassID: classID, StudentID: gone, EffectiveTo: day(time.September, 9),
	}))
	require.NoError(t, q.CloseRosterPeriod(ctx, sqlcgen.CloseRosterPeriodParams{
		TutorID: tutorID, ClassID: classID, StudentID: leavingToday, EffectiveTo: theDay,
	}))

	sessions, err := q.ListDigestSessions(ctx, sqlcgen.ListDigestSessionsParams{TutorID: tutorID, LocalDate: theDay})
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	require.Equal(t, "Maths 9A", sessions[0].ClassName)
	require.Equal(t, int64(2), sessions[0].RosterSize,
		"the student who left yesterday is out, the one leaving today is still in (AC-9, AC-11)")
}

// TestProjectionUpsertsAreIdempotent is AC-7 for notifications: the same events
// again leave the same rows, because every table is keyed by what its events carry.
func TestProjectionUpsertsAreIdempotent(t *testing.T) {
	t.Parallel()
	q := queries(t)
	ctx := t.Context()
	tutorID := newTutor(t)
	classID, studentID, sessionID := newID(t), newID(t), newID(t)
	theDay := day(time.September, 11)
	starts := time.Date(2026, time.September, 11, 3, 0, 0, 0, time.UTC)

	apply := func() {
		require.NoError(t, q.UpsertClass(ctx, sqlcgen.UpsertClassParams{ClassID: classID, TutorID: tutorID, Name: "Maths 9A"}))
		require.NoError(t, q.UpsertSession(ctx, sqlcgen.UpsertSessionParams{
			SessionID: sessionID, ClassID: classID, TutorID: tutorID,
			StartsAt: starts, EndsAt: starts.Add(90 * time.Minute), LocalDate: theDay,
		}))
		require.NoError(t, q.OpenRosterPeriod(ctx, sqlcgen.OpenRosterPeriodParams{
			ClassID: classID, StudentID: studentID, EffectiveFrom: day(time.September, 1), TutorID: tutorID,
		}))
	}

	apply()
	once, err := q.ListDigestSessions(ctx, sqlcgen.ListDigestSessionsParams{TutorID: tutorID, LocalDate: theDay})
	require.NoError(t, err)
	require.Len(t, once, 1)
	require.Equal(t, int64(1), once[0].RosterSize)

	apply()
	twice, err := q.ListDigestSessions(ctx, sqlcgen.ListDigestSessionsParams{TutorID: tutorID, LocalDate: theDay})
	require.NoError(t, err)
	require.Equal(t, once, twice, "the same delivery again must change nothing, count included (AC-7, AC-9)")
}

func TestProjectionReplayPreservesDigestRuns(t *testing.T) {
	t.Parallel()
	q := queries(t)
	ctx := t.Context()
	tutorID := newTutor(t)
	classID, studentID, sessionID := newID(t), newID(t), newID(t)
	theDay := day(time.September, 18)
	starts := time.Date(2026, time.September, 18, 3, 0, 0, 0, time.UTC)

	applyProjections := func() {
		require.NoError(t, q.UpsertRecipient(ctx, sqlcgen.UpsertRecipientParams{
			TutorID: tutorID, Email: "tutor@example.com", DisplayName: "Tutor",
			Timezone: "Asia/Ho_Chi_Minh", Language: "vi",
		}))
		require.NoError(t, q.UpsertClass(ctx, sqlcgen.UpsertClassParams{
			ClassID: classID, TutorID: tutorID, Name: "Maths 9A",
		}))
		require.NoError(t, q.UpsertSession(ctx, sqlcgen.UpsertSessionParams{
			SessionID: sessionID, ClassID: classID, TutorID: tutorID,
			StartsAt: starts, EndsAt: starts.Add(90 * time.Minute), LocalDate: theDay,
		}))
		require.NoError(t, q.OpenRosterPeriod(ctx, sqlcgen.OpenRosterPeriodParams{
			ClassID: classID, StudentID: studentID,
			EffectiveFrom: day(time.September, 1), TutorID: tutorID,
		}))
	}

	applyProjections()
	runBefore, err := q.InsertDigestRun(ctx, sqlcgen.InsertDigestRunParams{
		TutorID: tutorID, LocalDate: theDay, State: "Pending",
	})
	require.NoError(t, err)
	businessBefore, err := q.ListDigestSessions(ctx, sqlcgen.ListDigestSessionsParams{
		TutorID: tutorID, LocalDate: theDay,
	})
	require.NoError(t, err)

	applyProjections()

	runAfter, err := q.GetDigestRun(ctx, sqlcgen.GetDigestRunParams{
		TutorID: tutorID, LocalDate: theDay,
	})
	require.NoError(t, err)
	require.Equal(t, runBefore, runAfter, "a projection replay cannot touch a digest run (AC-8)")
	businessAfter, err := q.ListDigestSessions(ctx, sqlcgen.ListDigestSessionsParams{
		TutorID: tutorID, LocalDate: theDay,
	})
	require.NoError(t, err)
	require.Equal(t, businessBefore, businessAfter,
		"replay equality covers projection keys and business fields, not bookkeeping timestamps")
}

func TestProjectionJoinsCannotCrossTutors(t *testing.T) {
	t.Parallel()
	q := queries(t)
	ctx := t.Context()
	tutorA, tutorB := newTutor(t), newTutor(t)
	classID, studentID, sessionID := newID(t), newID(t), newID(t)
	theDay := day(time.September, 19)
	starts := time.Date(2026, time.September, 19, 3, 0, 0, 0, time.UTC)

	require.NoError(t, q.UpsertClass(ctx, sqlcgen.UpsertClassParams{
		ClassID: classID, TutorID: tutorA, Name: "Tutor A class",
	}))
	require.NoError(t, q.UpsertSession(ctx, sqlcgen.UpsertSessionParams{
		SessionID: sessionID, ClassID: classID, TutorID: tutorB,
		StartsAt: starts, EndsAt: starts.Add(90 * time.Minute), LocalDate: theDay,
	}))
	require.NoError(t, q.OpenRosterPeriod(ctx, sqlcgen.OpenRosterPeriodParams{
		ClassID: classID, StudentID: studentID,
		EffectiveFrom: day(time.September, 1), TutorID: tutorB,
	}))

	sessions, err := q.ListDigestSessions(ctx, sqlcgen.ListDigestSessionsParams{
		TutorID: tutorB, LocalDate: theDay,
	})
	require.NoError(t, err)
	require.Empty(t, sessions,
		"a projection join cannot borrow another tutor's class label (AC-4)")
}

// TestACancelledSessionLeavesTheDigest holds the session transition: cancelled is
// an end, and the morning email must not mention it.
func TestACancelledSessionLeavesTheDigest(t *testing.T) {
	t.Parallel()
	q := queries(t)
	ctx := t.Context()
	tutorID := newTutor(t)
	classID, sessionID := newID(t), newID(t)
	theDay := day(time.September, 12)
	starts := time.Date(2026, time.September, 12, 3, 0, 0, 0, time.UTC)

	require.NoError(t, q.UpsertClass(ctx, sqlcgen.UpsertClassParams{ClassID: classID, TutorID: tutorID, Name: "Maths 9A"}))
	require.NoError(t, q.UpsertSession(ctx, sqlcgen.UpsertSessionParams{
		SessionID: sessionID, ClassID: classID, TutorID: tutorID,
		StartsAt: starts, EndsAt: starts.Add(90 * time.Minute), LocalDate: theDay,
	}))
	require.NoError(t, q.MarkSessionCancelled(ctx, sqlcgen.MarkSessionCancelledParams{
		TutorID: tutorID, SessionID: sessionID, CancelledAt: at(starts),
	}))

	sessions, err := q.ListDigestSessions(ctx, sqlcgen.ListDigestSessionsParams{TutorID: tutorID, LocalDate: theDay})
	require.NoError(t, err)
	require.Empty(t, sessions)
}

// TestOneDigestPerTutorPerDay is what the digest_runs key is for: the scheduler is
// an in process ticker in a single replica, so a repeated tick has to be a unique
// violation rather than a second email (STK-23).
func TestOneDigestPerTutorPerDay(t *testing.T) {
	t.Parallel()
	q := queries(t)
	ctx := t.Context()
	tutorID := newTutor(t)
	theDay := day(time.September, 13)

	run, err := q.InsertDigestRun(ctx, sqlcgen.InsertDigestRunParams{
		TutorID: tutorID, LocalDate: theDay, State: "Pending",
	})
	require.NoError(t, err)
	require.Equal(t, "Pending", run.State)
	require.Zero(t, run.Attempts)
	require.Empty(t, run.LastError, "a fresh run has no error to report yet")

	_, err = q.InsertDigestRun(ctx, sqlcgen.InsertDigestRunParams{
		TutorID: tutorID, LocalDate: theDay, State: "Pending",
	})
	require.Error(t, err, "a second tick on the same morning must be refused, not send a second digest")

	// Another day is another digest, and another tutor is unaffected either way.
	_, err = q.InsertDigestRun(ctx, sqlcgen.InsertDigestRunParams{
		TutorID: tutorID, LocalDate: day(time.September, 14), State: "Pending",
	})
	require.NoError(t, err)
}

// TestAnotherTutorSeesNoSessions is the permission case (AC-4): the same read with
// another tutor_id returns nothing rather than someone else's morning.
func TestAnotherTutorSeesNoSessions(t *testing.T) {
	t.Parallel()
	q := queries(t)
	ctx := t.Context()
	tutorID, stranger := newTutor(t), newTutor(t)
	classID, sessionID := newID(t), newID(t)
	theDay := day(time.September, 16)
	starts := time.Date(2026, time.September, 16, 3, 0, 0, 0, time.UTC)

	require.NoError(t, q.UpsertClass(ctx, sqlcgen.UpsertClassParams{ClassID: classID, TutorID: tutorID, Name: "Maths 9A"}))
	require.NoError(t, q.UpsertSession(ctx, sqlcgen.UpsertSessionParams{
		SessionID: sessionID, ClassID: classID, TutorID: tutorID,
		StartsAt: starts, EndsAt: starts.Add(90 * time.Minute), LocalDate: theDay,
	}))

	mine, err := q.ListDigestSessions(ctx, sqlcgen.ListDigestSessionsParams{TutorID: tutorID, LocalDate: theDay})
	require.NoError(t, err)
	require.Len(t, mine, 1)

	theirs, err := q.ListDigestSessions(ctx, sqlcgen.ListDigestSessionsParams{TutorID: stranger, LocalDate: theDay})
	require.NoError(t, err)
	require.Empty(t, theirs, "another tutor's id must return nothing (AC-4, INV-8)")
}
