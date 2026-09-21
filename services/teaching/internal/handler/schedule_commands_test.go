//nolint:testpackage // These integration tests inspect the complete command transaction.
package handler

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"
	"github.com/stretchr/testify/require"
)

type aggregateCounts struct {
	rules    int
	slots    int
	sessions int
	receipts int
	outbox   int
}

func readAggregateCounts(t *testing.T, pool *pgxpool.Pool, tutorID uuid.UUID) aggregateCounts {
	t.Helper()

	var counts aggregateCounts
	err := pool.QueryRow(t.Context(), `
		SELECT
			(SELECT count(*) FROM schedule_rules WHERE tutor_id = $1),
			(SELECT count(*) FROM schedule_slots WHERE tutor_id = $1),
			(SELECT count(*) FROM sessions WHERE tutor_id = $1),
			(SELECT count(*) FROM command_receipts WHERE tutor_id = $1),
			(SELECT count(*) FROM outbox WHERE tutor_id = $1)`, tutorID,
	).Scan(&counts.rules, &counts.slots, &counts.sessions, &counts.receipts, &counts.outbox)
	require.NoError(t, err)
	return counts
}

// covers: AC-1, AC-8, AC-9, AC-11
func TestPutSchedule_InvalidInputAndOverlapWriteNothing(t *testing.T) {
	t.Parallel()

	pool := teachingTestPool(t)
	tutorID := uuid.Must(uuid.NewV7())
	cleanTeachingTutor(t, pool, tutorID)
	commandTime := time.Date(2026, time.September, 20, 0, 0, 0, 0, time.UTC)
	work := newTeachingHandler(t, pool, commandTime)
	ctx := vermouth.WithRequestID(t.Context(), "request-schedule-rollback")
	classResult, _, err := work.CreateClass(ctx, tutorID, "UTC", "rollback-class", CreateClassInput{
		Name: "Rollback class", RateAmount: 150_000,
		FirstSession: FirstSessionInput{LocalDate: "2026-09-20", StartTime: "09:00", EndTime: "10:00"},
	})
	require.NoError(t, err)
	_, _, err = work.CreateClass(ctx, tutorID, "UTC", "blocking-class", CreateClassInput{
		Name: "Blocking class", RateAmount: 150_000,
		FirstSession: FirstSessionInput{LocalDate: "2026-09-20", StartTime: "12:30", EndTime: "13:30"},
	})
	require.NoError(t, err)
	baseline := readAggregateCounts(t, pool, tutorID)

	invalidInputs := []struct {
		name  string
		slots []WeeklyScheduleSlotInput
	}{
		{name: "zero slots"},
		{name: "eight slots", slots: []WeeklyScheduleSlotInput{
			{Weekday: 1, StartTime: "09:00", EndTime: "10:00"},
			{Weekday: 2, StartTime: "09:00", EndTime: "10:00"},
			{Weekday: 3, StartTime: "09:00", EndTime: "10:00"},
			{Weekday: 4, StartTime: "09:00", EndTime: "10:00"},
			{Weekday: 5, StartTime: "09:00", EndTime: "10:00"},
			{Weekday: 6, StartTime: "09:00", EndTime: "10:00"},
			{Weekday: 7, StartTime: "09:00", EndTime: "10:00"},
			{Weekday: 1, StartTime: "11:00", EndTime: "12:00"},
		}},
		{name: "equal clocks", slots: []WeeklyScheduleSlotInput{{
			Weekday: 7, StartTime: "12:00", EndTime: "12:00",
		}}},
	}
	for index, test := range invalidInputs {
		_, _, err = work.PutSchedule(
			ctx, tutorID, classResult.Class.ClassID, "UTC", "invalid-schedule-"+test.name,
			PutScheduleInput{
				ExpectedRevision: 0, EffectiveFrom: "2026-09-20",
				ValidThrough: "2026-09-20", Slots: test.slots,
			},
		)
		var validation *ValidationError
		require.ErrorAs(t, err, &validation, "case %d: %s", index, test.name)
		require.Equal(t, baseline, readAggregateCounts(t, pool, tutorID))
	}

	_, _, err = work.PutSchedule(
		ctx, tutorID, classResult.Class.ClassID, "UTC", "overlapping-schedule",
		PutScheduleInput{
			ExpectedRevision: 0, EffectiveFrom: "2026-09-20", ValidThrough: "2026-09-20",
			Slots: []WeeklyScheduleSlotInput{{Weekday: 7, StartTime: "12:00", EndTime: "13:00"}},
		},
	)
	var conflict *ConflictError
	require.ErrorAs(t, err, &conflict)
	require.Equal(t, "session_overlap", conflict.Code)
	require.Equal(t, baseline, readAggregateCounts(t, pool, tutorID))
}
