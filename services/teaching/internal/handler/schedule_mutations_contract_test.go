//nolint:testpackage // These integration tests inspect retained rule history and command receipts.
package handler

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"
	"github.com/stretchr/testify/require"
)

func createScheduledClass(
	t *testing.T,
	work *Handler,
	tutorID uuid.UUID,
	key string,
	validThrough string,
	slots []WeeklyScheduleSlotInput,
) CreateClassResult {
	t.Helper()

	result, created, err := work.CreateClass(
		vermouth.WithRequestID(t.Context(), "request-"+key), tutorID, "UTC", key,
		CreateClassInput{
			Name: "Class " + key, RateAmount: 150_000,
			Schedule: &WeeklyScheduleInput{
				ValidFrom: "2026-09-01", ValidThrough: validThrough, Slots: slots,
			},
		},
	)
	require.NoError(t, err)
	require.True(t, created)
	require.NotNil(t, result.Rule)
	return result
}

func readRuleState(
	t *testing.T,
	pool *pgxpool.Pool,
	tutorID uuid.UUID,
	classID uuid.UUID,
	revision int64,
) (validThrough string, replaced bool, ended bool, retired bool) {
	t.Helper()

	err := pool.QueryRow(t.Context(), `
		SELECT to_char(valid_through, 'YYYY-MM-DD'),
		       replaced_at IS NOT NULL, ended_at IS NOT NULL, retired_at IS NOT NULL
		FROM schedule_rules
		WHERE tutor_id = $1 AND class_id = $2 AND revision = $3`,
		tutorID, classID, revision,
	).Scan(&validThrough, &replaced, &ended, &retired)
	require.NoError(t, err)
	return validThrough, replaced, ended, retired
}

// covers: AC-4, AC-9, AC-10, AC-11
func TestPutSchedule_RetiresPlannedRulesAndKeepsCompletedGaps(t *testing.T) {
	t.Parallel()

	t.Run("retire before and at the planned start", func(t *testing.T) {
		t.Parallel()

		pool := teachingTestPool(t)
		tutorID := uuid.Must(uuid.NewV7())
		cleanTeachingTutor(t, pool, tutorID)
		work := newTeachingHandler(t, pool, time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC))
		created := createScheduledClass(t, work, tutorID, "planned-replacement", "2026-09-30", []WeeklyScheduleSlotInput{{
			Weekday: 2, StartTime: "10:00", EndTime: "11:00",
		}})

		firstReplacement, _, err := work.PutSchedule(
			t.Context(), tutorID, created.Class.ClassID, "Asia/Ho_Chi_Minh", "planned-first",
			PutScheduleInput{
				ExpectedRevision: 1, EffectiveFrom: "2026-09-15", ValidThrough: "2026-10-31",
				Slots: []WeeklyScheduleSlotInput{{Weekday: 2, StartTime: "12:00", EndTime: "13:00"}},
			},
		)
		require.NoError(t, err)
		require.Equal(t, int64(2), firstReplacement.Class.ScheduleRevision)
		require.Equal(t, "Asia/Ho_Chi_Minh", firstReplacement.Rule.TimeZone)

		beforeStart, _, err := work.PutSchedule(
			t.Context(), tutorID, created.Class.ClassID, "America/New_York", "planned-before-start",
			PutScheduleInput{
				ExpectedRevision: 2, EffectiveFrom: "2026-09-10", ValidThrough: "2026-10-31",
				Slots: []WeeklyScheduleSlotInput{{Weekday: 4, StartTime: "09:00", EndTime: "10:00"}},
			},
		)
		require.NoError(t, err)
		require.Equal(t, int64(3), beforeStart.Class.ScheduleRevision)
		require.Equal(t, "America/New_York", beforeStart.Rule.TimeZone)
		oldThrough, oldReplaced, _, _ := readRuleState(t, pool, tutorID, created.Class.ClassID, 1)
		require.Equal(t, "2026-09-09", oldThrough)
		require.True(t, oldReplaced)
		_, _, _, plannedRetired := readRuleState(t, pool, tutorID, created.Class.ClassID, 2)
		require.True(t, plannedRetired)

		atStart, _, err := work.PutSchedule(
			t.Context(), tutorID, created.Class.ClassID, "UTC", "planned-at-start",
			PutScheduleInput{
				ExpectedRevision: 3, EffectiveFrom: "2026-09-10", ValidThrough: "2026-11-30",
				Slots: []WeeklyScheduleSlotInput{{Weekday: 5, StartTime: "14:00", EndTime: "15:00"}},
			},
		)
		require.NoError(t, err)
		require.Equal(t, int64(4), atStart.Class.ScheduleRevision)
		_, _, _, sameStartRetired := readRuleState(t, pool, tutorID, created.Class.ClassID, 3)
		require.True(t, sameStartRetired)
	})

	t.Run("replacement after completion leaves an intentional gap", func(t *testing.T) {
		t.Parallel()

		pool := teachingTestPool(t)
		tutorID := uuid.Must(uuid.NewV7())
		cleanTeachingTutor(t, pool, tutorID)
		work := newTeachingHandler(t, pool, time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC))
		created := createScheduledClass(t, work, tutorID, "completed-gap", "2026-09-07", []WeeklyScheduleSlotInput{{
			Weekday: 2, StartTime: "10:00", EndTime: "11:00",
		}})
		work.now = func() time.Time { return time.Date(2026, time.September, 20, 0, 0, 0, 0, time.UTC) }

		result, _, err := work.PutSchedule(
			t.Context(), tutorID, created.Class.ClassID, "UTC", "completed-gap-replacement",
			PutScheduleInput{
				ExpectedRevision: 1, EffectiveFrom: "2026-09-20", ValidThrough: "2026-10-20",
				Slots: []WeeklyScheduleSlotInput{{Weekday: 7, StartTime: "10:00", EndTime: "11:00"}},
			},
		)
		require.NoError(t, err)
		require.Zero(t, result.SupersededCount)
		oldThrough, oldReplaced, oldEnded, oldRetired := readRuleState(
			t, pool, tutorID, created.Class.ClassID, 1,
		)
		require.Equal(t, "2026-09-07", oldThrough)
		require.False(t, oldReplaced)
		require.False(t, oldEnded)
		require.False(t, oldRetired)
	})
}

// covers: AC-5, AC-11, AC-13, AC-16
func TestEndSchedule_RetiresPlannedRuleAndReplaysImmutableResult(t *testing.T) {
	t.Parallel()

	pool := teachingTestPool(t)
	tutorID := uuid.Must(uuid.NewV7())
	cleanTeachingTutor(t, pool, tutorID)
	commandTime := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	work := newTeachingHandler(t, pool, commandTime)
	created := createScheduledClass(t, work, tutorID, "planned-end", "2026-09-30", []WeeklyScheduleSlotInput{{
		Weekday: 2, StartTime: "10:00", EndTime: "11:00",
	}})
	planned, _, err := work.PutSchedule(
		t.Context(), tutorID, created.Class.ClassID, "UTC", "planned-end-future",
		PutScheduleInput{
			ExpectedRevision: 1, EffectiveFrom: "2026-10-10", ValidThrough: "2026-10-31",
			Slots: []WeeklyScheduleSlotInput{{Weekday: 6, StartTime: "12:00", EndTime: "13:00"}},
		},
	)
	require.NoError(t, err)
	require.Equal(t, int64(2), planned.Class.ScheduleRevision)

	baselineCancelledEvents := 0
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT count(*) FROM outbox WHERE tutor_id = $1 AND event_name = $2`,
		tutorID, vermouth.EventSessionCancelled,
	).Scan(&baselineCancelledEvents))
	ended, status, err := work.EndSchedule(
		t.Context(), tutorID, created.Class.ClassID, "planned-end-command",
		EndScheduleInput{ExpectedRevision: 2, LastDate: "2026-09-20"},
	)
	require.NoError(t, err)
	require.Equal(t, 200, status)
	require.Equal(t, int64(3), ended.Class.ScheduleRevision)
	require.Equal(t, "retired", ended.Rule.State)
	predecessorThrough, _, predecessorEnded, _ := readRuleState(t, pool, tutorID, created.Class.ClassID, 1)
	require.Equal(t, "2026-09-20", predecessorThrough)
	require.True(t, predecessorEnded)
	_, _, _, latestRetired := readRuleState(t, pool, tutorID, created.Class.ClassID, 2)
	require.True(t, latestRetired)

	var cancelledEvents int
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT count(*) FROM outbox WHERE tutor_id = $1 AND event_name = $2`,
		tutorID, vermouth.EventSessionCancelled,
	).Scan(&cancelledEvents))
	require.Equal(t, ended.SupersededCount, cancelledEvents-baselineCancelledEvents)

	work.now = func() time.Time { return commandTime.AddDate(1, 0, 0) }
	replayed, replayStatus, err := work.EndSchedule(
		t.Context(), tutorID, created.Class.ClassID, "planned-end-command",
		EndScheduleInput{ExpectedRevision: 2, LastDate: "2026-09-20"},
	)
	require.NoError(t, err)
	require.Equal(t, status, replayStatus)
	require.Equal(t, ended, replayed)
	work.now = func() time.Time { return commandTime }

	_, _, err = work.EndSchedule(
		t.Context(), tutorID, created.Class.ClassID, "planned-end-stale",
		EndScheduleInput{ExpectedRevision: 2, LastDate: "2026-09-20"},
	)
	var stale *ConflictError
	require.ErrorAs(t, err, &stale)
	require.Equal(t, "stale_schedule", stale.Code)

	_, _, err = work.EndSchedule(
		t.Context(), tutorID, created.Class.ClassID, "planned-end-invalid-state",
		EndScheduleInput{ExpectedRevision: 3, LastDate: "2026-09-30"},
	)
	var invalid *ConflictError
	require.ErrorAs(t, err, &invalid)
	require.Equal(t, "invalid_schedule_state", invalid.Code)
}
