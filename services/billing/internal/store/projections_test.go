package store_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/nguyen-duc-loc/vermouth/services/billing/internal/store"
	"github.com/nguyen-duc-loc/vermouth/services/billing/internal/store/sqlcgen"
)

// billableFor is the month end run's one read of the projections, over one whole
// month. It doubles as this package's snapshot of every projection at once: a
// billable row is the product of sessions, attendance, students, classes, roster
// periods and class rates agreeing, so comparing two of these compares all six.
func billableFor(t *testing.T, q *sqlcgen.Queries, tutorID uuid.UUID, month time.Month) []sqlcgen.ListBillableSessionsRow {
	t.Helper()
	rows, err := q.ListBillableSessions(t.Context(), sqlcgen.ListBillableSessionsParams{
		TutorID:     tutorID,
		PeriodStart: day(month, 1),
		PeriodEnd:   day(month, 28),
	})
	require.NoError(t, err)
	return rows
}

func TestProjectionBookkeepingUsesTheTransactionClock(t *testing.T) {
	t.Parallel()
	q := queries(t)
	ctx := t.Context()
	tutorID := newTutor(t)
	classID := newID(t)
	firstDate := day(time.September, 1)
	secondDate := day(time.September, 15)

	require.NoError(t, q.UpsertClassRate(ctx, sqlcgen.UpsertClassRateParams{
		ClassID: classID, EffectiveFrom: firstDate, TutorID: tutorID,
		RateAmount: 250_000, Currency: "VND",
	}))
	firstInsert, err := q.GetClassRateBookkeeping(ctx, sqlcgen.GetClassRateBookkeepingParams{
		TutorID: tutorID, ClassID: classID, EffectiveFrom: firstDate,
	})
	require.NoError(t, err)
	require.Equal(t, firstInsert.RecordedAt, firstInsert.UpdatedAt,
		"an insert records both bookkeeping timestamps from one transaction clock")

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(teardownContext()) })
	txQueries := store.Queries(tx)
	require.NoError(t, txQueries.UpsertClassRate(ctx, sqlcgen.UpsertClassRateParams{
		ClassID: classID, EffectiveFrom: firstDate, TutorID: tutorID,
		RateAmount: 275_000, Currency: "VND",
	}))
	require.NoError(t, txQueries.UpsertClassRate(ctx, sqlcgen.UpsertClassRateParams{
		ClassID: classID, EffectiveFrom: secondDate, TutorID: tutorID,
		RateAmount: 300_000, Currency: "VND",
	}))

	firstUpdate, err := txQueries.GetClassRateBookkeeping(ctx, sqlcgen.GetClassRateBookkeepingParams{
		TutorID: tutorID, ClassID: classID, EffectiveFrom: firstDate,
	})
	require.NoError(t, err)
	secondInsert, err := txQueries.GetClassRateBookkeeping(ctx, sqlcgen.GetClassRateBookkeepingParams{
		TutorID: tutorID, ClassID: classID, EffectiveFrom: secondDate,
	})
	require.NoError(t, err)
	require.Equal(t, firstInsert.RecordedAt, firstUpdate.RecordedAt,
		"an upsert keeps the first insert time")
	require.NotEqual(t, firstInsert.UpdatedAt, firstUpdate.UpdatedAt,
		"an applied upsert advances updated_at")
	require.Equal(t, firstUpdate.UpdatedAt, secondInsert.RecordedAt,
		"every write in one consumer transaction uses the same clock")
	require.Equal(t, secondInsert.RecordedAt, secondInsert.UpdatedAt)
	require.NoError(t, tx.Commit(ctx))
}

func TestProjectionJoinsCannotCrossTutors(t *testing.T) {
	t.Parallel()
	q := queries(t)
	ctx := t.Context()
	tutorA, tutorB := newTutor(t), newTutor(t)
	classID, studentID, sessionID := newID(t), newID(t), newID(t)
	starts := time.Date(2026, time.September, 19, 3, 0, 0, 0, time.UTC)

	require.NoError(t, q.UpsertClass(ctx, sqlcgen.UpsertClassParams{
		ClassID: classID, TutorID: tutorA, Name: "Tutor A class",
	}))
	require.NoError(t, q.UpsertStudent(ctx, sqlcgen.UpsertStudentParams{
		StudentID: studentID, TutorID: tutorB, Name: "Mai",
	}))
	require.NoError(t, q.UpsertClassRate(ctx, sqlcgen.UpsertClassRateParams{
		ClassID: classID, EffectiveFrom: day(time.September, 1), TutorID: tutorB,
		RateAmount: 250_000, Currency: "VND",
	}))
	require.NoError(t, q.UpsertSession(ctx, sqlcgen.UpsertSessionParams{
		SessionID: sessionID, ClassID: classID, TutorID: tutorB,
		StartsAt: starts, EndsAt: starts.Add(90 * time.Minute), LocalDate: day(time.September, 19),
	}))
	require.NoError(t, q.UpsertAttendance(ctx, sqlcgen.UpsertAttendanceParams{
		SessionID: sessionID, StudentID: studentID, TutorID: tutorB,
		State: "Present", MarkedAt: starts,
	}))
	require.NoError(t, q.OpenRosterPeriod(ctx, sqlcgen.OpenRosterPeriodParams{
		ClassID: classID, StudentID: studentID,
		EffectiveFrom: day(time.September, 1), TutorID: tutorB,
	}))

	require.Empty(t, billableFor(t, q, tutorB, time.September),
		"a projection join cannot borrow another tutor's class label (AC-4)")
}

// TestProjectionUpsertsAreIdempotent is AC-7 at its plainest: apply the same set
// of events twice and the projections hold what one delivery would have left.
// This is the property that lets a consumer take no care at all about a
// redelivery, because every projection is keyed by what its events carry.
func TestProjectionUpsertsAreIdempotent(t *testing.T) {
	t.Parallel()
	q := queries(t)
	ctx := t.Context()
	tutorID := newTutor(t)
	classID, studentID, sessionID := newID(t), newID(t), newID(t)
	marked := time.Date(2026, time.September, 14, 3, 0, 0, 0, time.UTC)

	apply := func() {
		require.NoError(t, q.UpsertClass(ctx, sqlcgen.UpsertClassParams{
			ClassID: classID, TutorID: tutorID, Name: "Maths 9A",
		}))
		require.NoError(t, q.UpsertClassRate(ctx, sqlcgen.UpsertClassRateParams{
			ClassID: classID, EffectiveFrom: day(time.September, 1), TutorID: tutorID,
			RateAmount: 250_000, Currency: "VND",
		}))
		require.NoError(t, q.UpsertStudent(ctx, sqlcgen.UpsertStudentParams{
			StudentID: studentID, TutorID: tutorID, Name: "Mai",
		}))
		require.NoError(t, q.UpsertSession(ctx, sqlcgen.UpsertSessionParams{
			SessionID: sessionID, ClassID: classID, TutorID: tutorID,
			StartsAt: marked, EndsAt: marked.Add(90 * time.Minute), LocalDate: day(time.September, 14),
		}))
		require.NoError(t, q.OpenRosterPeriod(ctx, sqlcgen.OpenRosterPeriodParams{
			ClassID: classID, StudentID: studentID, EffectiveFrom: day(time.September, 1), TutorID: tutorID,
		}))
		require.NoError(t, q.UpsertAttendance(ctx, sqlcgen.UpsertAttendanceParams{
			SessionID: sessionID, StudentID: studentID, TutorID: tutorID,
			State: "Present", MarkedAt: marked,
		}))
	}

	apply()
	once := billableFor(t, q, tutorID, time.September)
	require.Len(t, once, 1, "one present session for one student is one billable row")

	apply()
	require.Equal(t, once, billableFor(t, q, tutorID, time.September),
		"the same delivery again must change nothing (AC-7, INV-5)")
}

// TestAttendanceCorrectionLandsOnTheSameRow holds INV-6 where it decides money: a
// mark changed from Present to Absent must replace the first answer rather than
// sit beside it, or the month end run would bill a session twice.
func TestAttendanceCorrectionLandsOnTheSameRow(t *testing.T) {
	t.Parallel()
	q := queries(t)
	ctx := t.Context()
	tutorID := newTutor(t)
	classID, studentID, sessionID := newID(t), newID(t), newID(t)
	starts := time.Date(2026, time.September, 15, 3, 0, 0, 0, time.UTC)

	seedClassAndStudent(t, q, tutorID, classID, studentID)
	require.NoError(t, q.UpsertSession(ctx, sqlcgen.UpsertSessionParams{
		SessionID: sessionID, ClassID: classID, TutorID: tutorID,
		StartsAt: starts, EndsAt: starts.Add(90 * time.Minute), LocalDate: day(time.September, 15),
	}))
	mark := func(state string) {
		require.NoError(t, q.UpsertAttendance(ctx, sqlcgen.UpsertAttendanceParams{
			SessionID: sessionID, StudentID: studentID, TutorID: tutorID,
			State: state, MarkedAt: starts.Add(2 * time.Hour),
		}))
	}

	mark("Present")
	require.Len(t, billableFor(t, q, tutorID, time.September), 1)
	mark("Absent")
	require.Empty(t, billableFor(t, q, tutorID, time.September),
		"a correction to Absent must leave nothing to bill, not a second row")
}

// TestOnlyPresentBills states the money rule the schema leans on: a missing
// attendance row is unmarked, which for money is the same answer as Absent, and
// only Present bills.
func TestOnlyPresentBills(t *testing.T) {
	t.Parallel()
	q := queries(t)
	ctx := t.Context()
	tutorID := newTutor(t)
	classID, studentID := newID(t), newID(t)
	seedClassAndStudent(t, q, tutorID, classID, studentID)

	for i, state := range []string{"Present", "Absent", ""} {
		sessionID := newID(t)
		starts := time.Date(2026, time.September, 7+i, 3, 0, 0, 0, time.UTC)
		require.NoError(t, q.UpsertSession(ctx, sqlcgen.UpsertSessionParams{
			SessionID: sessionID, ClassID: classID, TutorID: tutorID,
			StartsAt: starts, EndsAt: starts.Add(90 * time.Minute), LocalDate: day(time.September, 7+i),
		}))
		if state == "" {
			// Unmarked: no attendance row at all.
			continue
		}
		require.NoError(t, q.UpsertAttendance(ctx, sqlcgen.UpsertAttendanceParams{
			SessionID: sessionID, StudentID: studentID, TutorID: tutorID,
			State: state, MarkedAt: starts,
		}))
	}
	require.Len(t, billableFor(t, q, tutorID, time.September), 1,
		"of Present, Absent and unmarked, only Present bills")
}

// TestMovedSessionBillsInItsNewPeriod is AC-9 where a stored month column would
// have gone wrong: the period a session belongs to is a range over local_date, so
// moving a session from September into October moves the money with it.
func TestMovedSessionBillsInItsNewPeriod(t *testing.T) {
	t.Parallel()
	q := queries(t)
	ctx := t.Context()
	tutorID := newTutor(t)
	classID, studentID, sessionID := newID(t), newID(t), newID(t)
	seedClassAndStudent(t, q, tutorID, classID, studentID)

	scheduled := time.Date(2026, time.September, 21, 3, 0, 0, 0, time.UTC)
	require.NoError(t, q.UpsertSession(ctx, sqlcgen.UpsertSessionParams{
		SessionID: sessionID, ClassID: classID, TutorID: tutorID,
		StartsAt: scheduled, EndsAt: scheduled.Add(90 * time.Minute), LocalDate: day(time.September, 21),
	}))
	require.NoError(t, q.UpsertAttendance(ctx, sqlcgen.UpsertAttendanceParams{
		SessionID: sessionID, StudentID: studentID, TutorID: tutorID, State: "Present", MarkedAt: scheduled,
	}))
	require.Len(t, billableFor(t, q, tutorID, time.September), 1)

	moved := time.Date(2026, time.October, 5, 3, 0, 0, 0, time.UTC)
	require.NoError(t, q.UpsertSession(ctx, sqlcgen.UpsertSessionParams{
		SessionID: sessionID, ClassID: classID, TutorID: tutorID,
		StartsAt: moved, EndsAt: moved.Add(90 * time.Minute), LocalDate: day(time.October, 5),
	}))
	require.Empty(t, billableFor(t, q, tutorID, time.September),
		"a moved session must stop billing in the month it left")
	require.Len(t, billableFor(t, q, tutorID, time.October), 1,
		"and start billing in the month it moved to (AC-9)")
}

// TestCancelledSessionIsNeverBillableAgain holds the state transition spec 0003
// writes for a session: cancelled is an end, not a pause.
func TestCancelledSessionIsNeverBillableAgain(t *testing.T) {
	t.Parallel()
	q := queries(t)
	ctx := t.Context()
	tutorID := newTutor(t)
	classID, studentID, sessionID := newID(t), newID(t), newID(t)
	seedClassAndStudent(t, q, tutorID, classID, studentID)
	starts := time.Date(2026, time.September, 22, 3, 0, 0, 0, time.UTC)
	require.NoError(t, q.UpsertSession(ctx, sqlcgen.UpsertSessionParams{
		SessionID: sessionID, ClassID: classID, TutorID: tutorID,
		StartsAt: starts, EndsAt: starts.Add(90 * time.Minute), LocalDate: day(time.September, 22),
	}))
	require.NoError(t, q.UpsertAttendance(ctx, sqlcgen.UpsertAttendanceParams{
		SessionID: sessionID, StudentID: studentID, TutorID: tutorID, State: "Present", MarkedAt: starts,
	}))
	require.NoError(t, q.MarkSessionCancelled(ctx, sqlcgen.MarkSessionCancelledParams{
		TutorID: tutorID, SessionID: sessionID, CancelledAt: at(starts),
	}))
	require.Empty(t, billableFor(t, q, tutorID, time.September))
}

// TestRejoinBillsBothPeriodsAndNotTheGap is the rejoin case (AC-11). Joining,
// leaving and rejoining leaves two roster period rows with exactly one open, and a
// session in the gap bills nobody even though the student is on the roster again
// afterwards.
func TestRejoinBillsBothPeriodsAndNotTheGap(t *testing.T) {
	t.Parallel()
	q := queries(t)
	ctx := t.Context()
	tutorID := newTutor(t)
	classID, studentID := newID(t), newID(t)

	require.NoError(t, q.UpsertClass(ctx, sqlcgen.UpsertClassParams{ClassID: classID, TutorID: tutorID, Name: "Maths 9A"}))
	require.NoError(t, q.UpsertStudent(ctx, sqlcgen.UpsertStudentParams{StudentID: studentID, TutorID: tutorID, Name: "Mai"}))
	require.NoError(t, q.UpsertClassRate(ctx, sqlcgen.UpsertClassRateParams{
		ClassID: classID, EffectiveFrom: day(time.September, 1), TutorID: tutorID,
		RateAmount: 250_000, Currency: "VND",
	}))

	// Member for the first week, gone for the second, back for the third.
	require.NoError(t, q.OpenRosterPeriod(ctx, sqlcgen.OpenRosterPeriodParams{
		ClassID: classID, StudentID: studentID, EffectiveFrom: day(time.September, 1), TutorID: tutorID,
	}))
	require.NoError(t, q.CloseRosterPeriod(ctx, sqlcgen.CloseRosterPeriodParams{
		TutorID: tutorID, ClassID: classID, StudentID: studentID, EffectiveTo: day(time.September, 7),
	}))
	require.NoError(t, q.OpenRosterPeriod(ctx, sqlcgen.OpenRosterPeriodParams{
		ClassID: classID, StudentID: studentID, EffectiveFrom: day(time.September, 15), TutorID: tutorID,
	}))

	// One Present session in each week, including the gap.
	for _, dayOfMonth := range []int{3, 10, 17} {
		sessionID := newID(t)
		starts := time.Date(2026, time.September, dayOfMonth, 3, 0, 0, 0, time.UTC)
		require.NoError(t, q.UpsertSession(ctx, sqlcgen.UpsertSessionParams{
			SessionID: sessionID, ClassID: classID, TutorID: tutorID,
			StartsAt: starts, EndsAt: starts.Add(90 * time.Minute), LocalDate: day(time.September, dayOfMonth),
		}))
		require.NoError(t, q.UpsertAttendance(ctx, sqlcgen.UpsertAttendanceParams{
			SessionID: sessionID, StudentID: studentID, TutorID: tutorID, State: "Present", MarkedAt: starts,
		}))
	}

	billable := billableFor(t, q, tutorID, time.September)
	require.Len(t, billable, 2, "the two sessions inside a membership period bill, the one in the gap does not")
	require.Equal(t, day(time.September, 3).Time, billable[0].LocalDate.Time)
	require.Equal(t, day(time.September, 17).Time, billable[1].LocalDate.Time)

	// A second left closes the open row, and only ever the open one.
	require.NoError(t, q.CloseRosterPeriod(ctx, sqlcgen.CloseRosterPeriodParams{
		TutorID: tutorID, ClassID: classID, StudentID: studentID, EffectiveTo: day(time.September, 20),
	}))
	require.Len(t, billableFor(t, q, tutorID, time.September), 2,
		"closing on the 20th leaves both billed sessions billed")
}

// TestRosterBoundaryIsInclusiveAtBothEnds is the boundary case spec 0003 argues
// out loud (AC-11): a student who leaves on the day a session runs, and is marked
// Present for it, is billed for that session, and a one day membership covers
// exactly that one day. If attendance says Present, the session has to bill
// someone.
func TestRosterBoundaryIsInclusiveAtBothEnds(t *testing.T) {
	t.Parallel()
	q := queries(t)
	ctx := t.Context()
	tutorID := newTutor(t)
	classID := newID(t)
	require.NoError(t, q.UpsertClass(ctx, sqlcgen.UpsertClassParams{ClassID: classID, TutorID: tutorID, Name: "Maths 9A"}))
	require.NoError(t, q.UpsertClassRate(ctx, sqlcgen.UpsertClassRateParams{
		ClassID: classID, EffectiveFrom: day(time.September, 1), TutorID: tutorID,
		RateAmount: 250_000, Currency: "VND",
	}))

	leaves, oneDay := newID(t), newID(t)
	require.NoError(t, q.UpsertStudent(ctx, sqlcgen.UpsertStudentParams{StudentID: leaves, TutorID: tutorID, Name: "Mai"}))
	require.NoError(t, q.UpsertStudent(ctx, sqlcgen.UpsertStudentParams{StudentID: oneDay, TutorID: tutorID, Name: "Linh"}))

	// One leaves on the 10th; the other is a member for the 10th only.
	require.NoError(t, q.OpenRosterPeriod(ctx, sqlcgen.OpenRosterPeriodParams{
		ClassID: classID, StudentID: leaves, EffectiveFrom: day(time.September, 1), TutorID: tutorID,
	}))
	require.NoError(t, q.CloseRosterPeriod(ctx, sqlcgen.CloseRosterPeriodParams{
		TutorID: tutorID, ClassID: classID, StudentID: leaves, EffectiveTo: day(time.September, 10),
	}))
	require.NoError(t, q.OpenRosterPeriod(ctx, sqlcgen.OpenRosterPeriodParams{
		ClassID: classID, StudentID: oneDay, EffectiveFrom: day(time.September, 10), TutorID: tutorID,
	}))
	require.NoError(t, q.CloseRosterPeriod(ctx, sqlcgen.CloseRosterPeriodParams{
		TutorID: tutorID, ClassID: classID, StudentID: oneDay, EffectiveTo: day(time.September, 10),
	}))

	sessionID := newID(t)
	starts := time.Date(2026, time.September, 10, 3, 0, 0, 0, time.UTC)
	require.NoError(t, q.UpsertSession(ctx, sqlcgen.UpsertSessionParams{
		SessionID: sessionID, ClassID: classID, TutorID: tutorID,
		StartsAt: starts, EndsAt: starts.Add(90 * time.Minute), LocalDate: day(time.September, 10),
	}))
	for _, studentID := range []uuid.UUID{leaves, oneDay} {
		require.NoError(t, q.UpsertAttendance(ctx, sqlcgen.UpsertAttendanceParams{
			SessionID: sessionID, StudentID: studentID, TutorID: tutorID, State: "Present", MarkedAt: starts,
		}))
	}

	require.Len(t, billableFor(t, q, tutorID, time.September), 2,
		"the leave date bills, and a one day membership covers its one day (AC-11)")

	// The day after the leave date is outside both periods.
	after := newID(t)
	startsAfter := time.Date(2026, time.September, 11, 3, 0, 0, 0, time.UTC)
	require.NoError(t, q.UpsertSession(ctx, sqlcgen.UpsertSessionParams{
		SessionID: after, ClassID: classID, TutorID: tutorID,
		StartsAt: startsAfter, EndsAt: startsAfter.Add(90 * time.Minute), LocalDate: day(time.September, 11),
	}))
	require.NoError(t, q.UpsertAttendance(ctx, sqlcgen.UpsertAttendanceParams{
		SessionID: after, StudentID: leaves, TutorID: tutorID, State: "Present", MarkedAt: startsAfter,
	}))
	require.Len(t, billableFor(t, q, tutorID, time.September), 2,
		"the day after the leave date bills nobody")
}

// TestRateInForceIsTheNewestOnOrBeforeTheDay holds what the class_rates key is
// for: the rate that applies is the newest row on or before the session's day, so
// a rate change part way through a month does not reprice the sessions before it.
func TestRateInForceIsTheNewestOnOrBeforeTheDay(t *testing.T) {
	t.Parallel()
	q := queries(t)
	ctx := t.Context()
	tutorID := newTutor(t)
	classID, studentID := newID(t), newID(t)
	seedClassAndStudent(t, q, tutorID, classID, studentID)
	require.NoError(t, q.UpsertClassRate(ctx, sqlcgen.UpsertClassRateParams{
		ClassID: classID, EffectiveFrom: day(time.September, 15), TutorID: tutorID,
		RateAmount: 300_000, Currency: "VND",
	}))

	for _, dayOfMonth := range []int{14, 16} {
		sessionID := newID(t)
		starts := time.Date(2026, time.September, dayOfMonth, 3, 0, 0, 0, time.UTC)
		require.NoError(t, q.UpsertSession(ctx, sqlcgen.UpsertSessionParams{
			SessionID: sessionID, ClassID: classID, TutorID: tutorID,
			StartsAt: starts, EndsAt: starts.Add(90 * time.Minute), LocalDate: day(time.September, dayOfMonth),
		}))
		require.NoError(t, q.UpsertAttendance(ctx, sqlcgen.UpsertAttendanceParams{
			SessionID: sessionID, StudentID: studentID, TutorID: tutorID, State: "Present", MarkedAt: starts,
		}))
	}

	billable := billableFor(t, q, tutorID, time.September)
	require.Len(t, billable, 2)
	require.True(t, billable[0].RateKnown)
	require.Equal(t, int64(250_000), billable[0].RateAmount, "the session before the change keeps the old rate")
	require.Equal(t, int64(300_000), billable[1].RateAmount, "the session after it takes the new one")

	inForce, err := q.RateInForceOn(ctx, sqlcgen.RateInForceOnParams{
		TutorID: tutorID, ClassID: classID, EffectiveFrom: day(time.September, 30),
	})
	require.NoError(t, err)
	require.Equal(t, int64(300_000), inForce.RateAmount)
}

// TestAnotherTutorSeesNothing is the permission case (AC-4): the same query run
// with another tutor_id returns nothing rather than someone else's rows. There is
// no cross tutor read path because there is no statement without the filter.
func TestAnotherTutorSeesNothing(t *testing.T) {
	t.Parallel()
	q := queries(t)
	ctx := t.Context()
	tutorID, stranger := newTutor(t), newTutor(t)
	classID, studentID, sessionID := newID(t), newID(t), newID(t)
	seedClassAndStudent(t, q, tutorID, classID, studentID)
	starts := time.Date(2026, time.September, 8, 3, 0, 0, 0, time.UTC)
	require.NoError(t, q.UpsertSession(ctx, sqlcgen.UpsertSessionParams{
		SessionID: sessionID, ClassID: classID, TutorID: tutorID,
		StartsAt: starts, EndsAt: starts.Add(90 * time.Minute), LocalDate: day(time.September, 8),
	}))
	require.NoError(t, q.UpsertAttendance(ctx, sqlcgen.UpsertAttendanceParams{
		SessionID: sessionID, StudentID: studentID, TutorID: tutorID, State: "Present", MarkedAt: starts,
	}))

	require.Len(t, billableFor(t, q, tutorID, time.September), 1)
	require.Empty(t, billableFor(t, q, stranger, time.September),
		"another tutor's id must return nothing, not somebody else's session (AC-4, INV-8)")

	_, err := q.RateInForceOn(ctx, sqlcgen.RateInForceOnParams{
		TutorID: stranger, ClassID: classID, EffectiveFrom: day(time.September, 30),
	})
	require.Error(t, err, "a rate is not readable by a tutor who does not own the class")
}

// seedClassAndStudent is the shape most tests need: one class at one rate with one
// student on its roster from the first of September.
func seedClassAndStudent(t *testing.T, q *sqlcgen.Queries, tutorID, classID, studentID uuid.UUID) {
	t.Helper()
	ctx := t.Context()
	require.NoError(t, q.UpsertClass(ctx, sqlcgen.UpsertClassParams{ClassID: classID, TutorID: tutorID, Name: "Maths 9A"}))
	require.NoError(t, q.UpsertStudent(ctx, sqlcgen.UpsertStudentParams{StudentID: studentID, TutorID: tutorID, Name: "Mai"}))
	require.NoError(t, q.UpsertClassRate(ctx, sqlcgen.UpsertClassRateParams{
		ClassID: classID, EffectiveFrom: day(time.September, 1), TutorID: tutorID,
		RateAmount: 250_000, Currency: "VND",
	}))
	require.NoError(t, q.OpenRosterPeriod(ctx, sqlcgen.OpenRosterPeriodParams{
		ClassID: classID, StudentID: studentID, EffectiveFrom: day(time.September, 1), TutorID: tutorID,
	}))
}

// TestAMissingRateIsNotAFreeLesson keeps the two answers apart. A rate of zero is
// legal, so a session whose class has no rate on or before its day must come back
// as rate_known false rather than as zero money, which is what lets the month end
// run refuse with rate_missing instead of issuing an invoice that is quietly
// wrong.
func TestAMissingRateIsNotAFreeLesson(t *testing.T) {
	t.Parallel()
	q := queries(t)
	ctx := t.Context()
	tutorID := newTutor(t)
	classID, studentID, sessionID := newID(t), newID(t), newID(t)

	require.NoError(t, q.UpsertClass(ctx, sqlcgen.UpsertClassParams{ClassID: classID, TutorID: tutorID, Name: "Maths 9A"}))
	require.NoError(t, q.UpsertStudent(ctx, sqlcgen.UpsertStudentParams{StudentID: studentID, TutorID: tutorID, Name: "Mai"}))
	require.NoError(t, q.OpenRosterPeriod(ctx, sqlcgen.OpenRosterPeriodParams{
		ClassID: classID, StudentID: studentID, EffectiveFrom: day(time.September, 1), TutorID: tutorID,
	}))
	starts := time.Date(2026, time.September, 9, 3, 0, 0, 0, time.UTC)
	require.NoError(t, q.UpsertSession(ctx, sqlcgen.UpsertSessionParams{
		SessionID: sessionID, ClassID: classID, TutorID: tutorID,
		StartsAt: starts, EndsAt: starts.Add(90 * time.Minute), LocalDate: day(time.September, 9),
	}))
	require.NoError(t, q.UpsertAttendance(ctx, sqlcgen.UpsertAttendanceParams{
		SessionID: sessionID, StudentID: studentID, TutorID: tutorID, State: "Present", MarkedAt: starts,
	}))

	// No class_rates row at all: the session is still returned, so the run can see
	// it and refuse, rather than being dropped from the read.
	billable := billableFor(t, q, tutorID, time.September)
	require.Len(t, billable, 1)
	require.False(t, billable[0].RateKnown, "a session with no rate must say so")

	// A rate of zero is a different answer, and a legal one.
	require.NoError(t, q.UpsertClassRate(ctx, sqlcgen.UpsertClassRateParams{
		ClassID: classID, EffectiveFrom: day(time.September, 1), TutorID: tutorID,
		RateAmount: 0, Currency: "VND",
	}))
	billable = billableFor(t, q, tutorID, time.September)
	require.Len(t, billable, 1)
	require.True(t, billable[0].RateKnown, "a free lesson has a rate, and it is zero")
	require.Zero(t, billable[0].RateAmount)
}
