package store_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/nguyen-duc-loc/vermouth/services/teaching/internal/store/sqlcgen"
)

// TestAttendanceKeepsOneRowPerSessionAndStudent holds INV-6 in teaching, where the
// mark is made: a correction updates the row in place, so the last mark is the last
// one billing sees rather than one of two opinions.
func TestAttendanceKeepsOneRowPerSessionAndStudent(t *testing.T) {
	t.Parallel()
	q := queries(t)
	ctx := t.Context()
	tutorID := newTutor(t)
	_, studentID, sessionID := seedClassStudentAndSession(t, q, tutorID, 14)
	marked := time.Date(2026, time.September, 14, 4, 0, 0, 0, time.UTC)

	first, err := q.UpsertAttendance(ctx, sqlcgen.UpsertAttendanceParams{
		SessionID: sessionID, StudentID: studentID, TutorID: tutorID, State: "Present", MarkedAt: marked,
	})
	require.NoError(t, err)
	require.Equal(t, "Present", first.State)

	corrected, err := q.UpsertAttendance(ctx, sqlcgen.UpsertAttendanceParams{
		SessionID: sessionID, StudentID: studentID, TutorID: tutorID, State: "Absent", MarkedAt: marked.Add(time.Minute),
	})
	require.NoError(t, err)
	require.Equal(t, "Absent", corrected.State)
	require.Equal(t, first.CreatedAt, corrected.CreatedAt, "a correction is the same row, not a new one")

	stored, err := q.GetAttendance(ctx, sqlcgen.GetAttendanceParams{
		TutorID: tutorID, SessionID: sessionID, StudentID: studentID,
	})
	require.NoError(t, err)
	require.Equal(t, "Absent", stored.State)
}

// TestRejoinOpensASecondPeriodAndOnlyOneStaysOpen is AC-11 on the publisher's side.
// Leaving closes the one open row, rejoining opens a new one, and the partial
// unique index is what makes "the one open row" a fact rather than a hope, which is
// what lets teaching.roster.left carry no effective_from at all.
func TestRejoinOpensASecondPeriodAndOnlyOneStaysOpen(t *testing.T) {
	t.Parallel()
	q := queries(t)
	ctx := t.Context()
	tutorID := newTutor(t)
	classID, studentID, _ := seedClassStudentAndSession(t, q, tutorID, 14)

	require.NoError(t, q.OpenRosterPeriod(ctx, sqlcgen.OpenRosterPeriodParams{
		ClassID: classID, StudentID: studentID, EffectiveFrom: day(time.September, 1), TutorID: tutorID,
	}))
	open, err := q.FindOpenRosterPeriod(ctx, sqlcgen.FindOpenRosterPeriodParams{
		TutorID: tutorID, ClassID: classID, StudentID: studentID,
	})
	require.NoError(t, err)
	require.Equal(t, day(time.September, 1).Time, open.EffectiveFrom.Time)
	require.False(t, open.EffectiveTo.Valid, "an open period has no end")

	require.NoError(t, q.CloseRosterPeriod(ctx, sqlcgen.CloseRosterPeriodParams{
		TutorID: tutorID, ClassID: classID, StudentID: studentID, EffectiveTo: day(time.September, 7),
	}))
	_, err = q.FindOpenRosterPeriod(ctx, sqlcgen.FindOpenRosterPeriodParams{
		TutorID: tutorID, ClassID: classID, StudentID: studentID,
	})
	require.Error(t, err, "after leaving there is no open period to find")

	require.NoError(t, q.OpenRosterPeriod(ctx, sqlcgen.OpenRosterPeriodParams{
		ClassID: classID, StudentID: studentID, EffectiveFrom: day(time.September, 15), TutorID: tutorID,
	}))
	periods, err := q.ListRosterPeriods(ctx, sqlcgen.ListRosterPeriodsParams{
		TutorID: tutorID, ClassID: classID, StudentID: studentID,
	})
	require.NoError(t, err)
	require.Len(t, periods, 2, "rejoining opens a second period rather than reopening the closed one")
	require.True(t, periods[0].EffectiveTo.Valid)
	require.False(t, periods[1].EffectiveTo.Valid)

	// A second open period for the same pair is what the partial unique index is
	// there to refuse.
	require.NoError(t, q.OpenRosterPeriod(ctx, sqlcgen.OpenRosterPeriodParams{
		ClassID: classID, StudentID: studentID, EffectiveFrom: day(time.September, 15), TutorID: tutorID,
	}), "joining again on the same day changes nothing")
	_, err = pool.Exec(ctx, `
		INSERT INTO roster_periods (class_id, student_id, effective_from, tutor_id)
		VALUES ($1, $2, $3, $4)`, classID, studentID, day(time.September, 20), tutorID)
	require.Error(t, err, "two open periods for one pair must be impossible, not merely unlikely (AC-11)")
}

// TestSessionsForADayAreTheTutorsOwn is the permission case (AC-4). It also states
// what the home screen reads: the sessions of one tutor on one local date, which
// this service computed in their timezone and stored as a day.
func TestSessionsForADayAreTheTutorsOwn(t *testing.T) {
	t.Parallel()
	q := queries(t)
	ctx := t.Context()
	tutorID, stranger := newTutor(t), newTutor(t)
	_, _, sessionID := seedClassStudentAndSession(t, q, tutorID, 8)

	mine, err := q.ListSessionsForLocalDate(ctx, sqlcgen.ListSessionsForLocalDateParams{
		TutorID: tutorID, LocalDate: day(time.September, 8),
	})
	require.NoError(t, err)
	require.Len(t, mine, 1)
	require.Equal(t, sessionID, mine[0].SessionID)

	theirs, err := q.ListSessionsForLocalDate(ctx, sqlcgen.ListSessionsForLocalDateParams{
		TutorID: stranger, LocalDate: day(time.September, 8),
	})
	require.NoError(t, err)
	require.Empty(t, theirs, "another tutor's id must return nothing, not somebody else's day (AC-4, INV-8)")
}

// TestAttendanceNeedsASessionAndAStudent shows the foreign keys doing their job.
// Inside one context, between two authoritative tables, a foreign key is right:
// there is no arrival order to tolerate here, because teaching is where these facts
// are made.
func TestAttendanceNeedsASessionAndAStudent(t *testing.T) {
	t.Parallel()
	q := queries(t)
	ctx := t.Context()
	tutorID := newTutor(t)
	_, studentID, _ := seedClassStudentAndSession(t, q, tutorID, 9)

	_, err := q.UpsertAttendance(ctx, sqlcgen.UpsertAttendanceParams{
		SessionID: newID(t), StudentID: studentID, TutorID: tutorID,
		State: "Present", MarkedAt: time.Now().UTC(),
	})
	require.Error(t, err, "attendance for a session that does not exist must be refused")
}
