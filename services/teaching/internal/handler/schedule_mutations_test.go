//nolint:testpackage // These integration tests inject the handler clock and inspect its transaction boundary.
package handler

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"
	"github.com/stretchr/testify/require"
)

// covers: AC-4, AC-5, AC-6, AC-7, AC-11, AC-16
func TestScheduleMutations_PreserveExceptionsAndReplayCanonicalResponses(t *testing.T) {
	t.Parallel()

	pool := teachingTestPool(t)
	tutorID := uuid.Must(uuid.NewV7())
	cleanTeachingTutor(t, pool, tutorID)
	commandTime := time.Date(2026, time.September, 1, 2, 0, 0, 0, time.UTC)
	work := newTeachingHandler(t, pool, commandTime)
	ctx := vermouth.WithRequestID(t.Context(), "request-schedule-mutations")
	created, _, err := work.CreateClass(ctx, tutorID, "UTC", "recurring-class", CreateClassInput{
		Name: "English B2", RateAmount: 300_000,
		Schedule: &WeeklyScheduleInput{
			ValidFrom: "2026-09-01", ValidThrough: "2026-09-30",
			Slots: []WeeklyScheduleSlotInput{
				{Weekday: 1, StartTime: "10:00", EndTime: "11:00"},
				{Weekday: 4, StartTime: "10:00", EndTime: "11:00"},
			},
		},
	})
	require.NoError(t, err)

	var movedSessionID, cancelledSessionID uuid.UUID
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT session_id FROM sessions
		WHERE tutor_id = $1 AND class_id = $2 AND origin_local_date = '2026-09-07'`,
		tutorID, created.Class.ClassID,
	).Scan(&movedSessionID))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT session_id FROM sessions
		WHERE tutor_id = $1 AND class_id = $2 AND origin_local_date = '2026-09-10'`,
		tutorID, created.Class.ClassID,
	).Scan(&cancelledSessionID))

	moved, status, err := work.MoveSession(
		ctx, tutorID, movedSessionID, "UTC", "move-exception",
		MoveSessionInput{
			ExpectedVersion: 1, LocalDate: "2026-09-08", StartTime: "12:00", EndTime: "13:00",
		},
	)
	require.NoError(t, err)
	require.Equal(t, 200, status)
	require.Equal(t, int64(2), moved.Version)
	require.Equal(t, "2026-09-08", moved.LocalDate)
	require.NotNil(t, moved.MovedAt)

	cancelled, _, err := work.CancelSession(
		ctx, tutorID, cancelledSessionID, "UTC", "cancel-exception",
		SessionVersionInput{ExpectedVersion: 1},
	)
	require.NoError(t, err)
	require.Equal(t, "cancelled", cancelled.State)

	replaced, status, err := work.PutSchedule(
		ctx, tutorID, created.Class.ClassID, "UTC", "replace-rule", PutScheduleInput{
			ExpectedRevision: 1, EffectiveFrom: "2026-09-07", ValidThrough: "2026-10-31",
			Slots: []WeeklyScheduleSlotInput{
				{Weekday: 1, StartTime: "10:00", EndTime: "11:00"},
				{Weekday: 4, StartTime: "10:00", EndTime: "11:00"},
			},
		},
	)
	require.NoError(t, err)
	require.Equal(t, 200, status)
	require.Equal(t, int64(2), replaced.Class.ScheduleRevision)
	require.Equal(t, 2, replaced.PreservedCount)
	require.Positive(t, replaced.SupersededCount)

	work.now = func() time.Time { return commandTime.AddDate(1, 0, 0) }
	replayed, replayStatus, err := work.PutSchedule(
		ctx, tutorID, created.Class.ClassID, "Asia/Ho_Chi_Minh", "replace-rule", PutScheduleInput{
			ExpectedRevision: 1, EffectiveFrom: "2026-09-07", ValidThrough: "2026-10-31",
			Slots: []WeeklyScheduleSlotInput{
				{Weekday: 4, StartTime: "10:00", EndTime: "11:00"},
				{Weekday: 1, StartTime: "10:00", EndTime: "11:00"},
			},
		},
	)
	require.NoError(t, err)
	require.Equal(t, status, replayStatus)
	require.Equal(t, replaced, replayed)

	restored, _, err := work.RestoreSession(
		ctx, tutorID, cancelledSessionID, "UTC", "restore-exception",
		SessionVersionInput{ExpectedVersion: cancelled.Version},
	)
	require.NoError(t, err)
	require.Equal(t, "active", restored.State)
	require.Nil(t, restored.CancelledAt)

	work.now = func() time.Time { return commandTime }
	ended, _, err := work.EndSchedule(
		ctx, tutorID, created.Class.ClassID, "end-rule",
		EndScheduleInput{ExpectedRevision: 2, LastDate: "2026-09-21"},
	)
	require.NoError(t, err)
	require.Equal(t, int64(3), ended.Class.ScheduleRevision)
	require.Equal(t, "ended", ended.Rule.State)
	require.Positive(t, ended.SupersededCount)

	var movedEvents, cancelledEvents int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT count(*) FROM outbox WHERE tutor_id = $1 AND event_name = $2`,
		tutorID, vermouth.EventSessionMoved,
	).Scan(&movedEvents))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT count(*) FROM outbox WHERE tutor_id = $1 AND event_name = $2`,
		tutorID, vermouth.EventSessionCancelled,
	).Scan(&cancelledEvents))
	require.Equal(t, 1, movedEvents)
	require.Greater(t, cancelledEvents, 1)
}
