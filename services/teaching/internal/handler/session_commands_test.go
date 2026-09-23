//nolint:testpackage // These integration tests inspect retained attendance, receipts, and session rows.
package handler

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"
	"github.com/stretchr/testify/require"
)

// covers: AC-8, AC-9, AC-11
func TestSessionCommands_ConcurrentMoveAndRestoreChooseOneWinner(t *testing.T) {
	t.Parallel()

	pool := teachingTestPool(t)
	tutorID := uuid.Must(uuid.NewV7())
	cleanTeachingTutor(t, pool, tutorID)
	work := newTeachingHandler(t, pool, time.Date(2026, time.September, 20, 0, 0, 0, 0, time.UTC))
	ctx := vermouth.WithRequestID(t.Context(), "request-session-race")
	moveClass, _, err := work.CreateClass(ctx, tutorID, "UTC", "race-move-class", CreateClassInput{
		Name: "Move contender", RateAmount: 150_000,
		FirstSession: FirstSessionInput{LocalDate: "2026-09-20", StartTime: "10:00", EndTime: "11:00"},
	})
	require.NoError(t, err)
	restoreClass, _, err := work.CreateClass(ctx, tutorID, "UTC", "race-restore-class", CreateClassInput{
		Name: "Restore contender", RateAmount: 150_000,
		FirstSession: FirstSessionInput{LocalDate: "2026-09-20", StartTime: "14:00", EndTime: "15:00"},
	})
	require.NoError(t, err)
	_, _, err = work.CancelSession(
		ctx, tutorID, restoreClass.FirstSession.SessionID, "UTC", "race-cancel",
		SessionVersionInput{ExpectedVersion: 1},
	)
	require.NoError(t, err)
	var baselineOutbox int
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT count(*) FROM outbox WHERE tutor_id = $1`, tutorID,
	).Scan(&baselineOutbox))

	type raceResult struct {
		operation string
		status    int
		err       error
	}
	start := make(chan struct{})
	results := make(chan raceResult, 2)
	var group sync.WaitGroup
	group.Go(func() {
		<-start
		_, status, moveErr := work.MoveSession(
			ctx, tutorID, moveClass.FirstSession.SessionID, "UTC", "race-move",
			MoveSessionInput{
				ExpectedVersion: 1, LocalDate: "2026-09-20", StartTime: "14:00", EndTime: "15:00",
			},
		)
		results <- raceResult{operation: operationMoveSession, status: status, err: moveErr}
	})
	group.Go(func() {
		<-start
		_, status, restoreErr := work.RestoreSession(
			ctx, tutorID, restoreClass.FirstSession.SessionID, "UTC", "race-restore",
			SessionVersionInput{ExpectedVersion: 2},
		)
		results <- raceResult{operation: operationRestoreSession, status: status, err: restoreErr}
	})
	close(start)
	group.Wait()
	close(results)

	successes := 0
	conflicts := 0
	for result := range results {
		if result.err == nil {
			require.Equal(t, 200, result.status, result.operation)
			successes++
			continue
		}
		var conflict *ConflictError
		require.ErrorAs(t, result.err, &conflict, result.operation)
		require.Equal(t, "session_overlap", conflict.Code, result.operation)
		details, ok := conflict.Details.(map[string]any)
		require.True(t, ok)
		require.NotNil(t, details["session_id"])
		conflicts++
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, conflicts)

	var activeAtTarget, raceReceipts, outboxAfter int
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT count(*) FROM sessions
		WHERE tutor_id = $1 AND starts_at = '2026-09-20T14:00:00Z'
		  AND ends_at = '2026-09-20T15:00:00Z'
		  AND cancelled_at IS NULL AND superseded_at IS NULL`, tutorID,
	).Scan(&activeAtTarget))
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT count(*) FROM command_receipts
		WHERE tutor_id = $1 AND idempotency_key IN ('race-move', 'race-restore')`, tutorID,
	).Scan(&raceReceipts))
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT count(*) FROM outbox WHERE tutor_id = $1`, tutorID,
	).Scan(&outboxAfter))
	require.Equal(t, 1, activeAtTarget)
	require.Equal(t, 1, raceReceipts)
	require.Equal(t, baselineOutbox+1, outboxAfter)
}

// covers: AC-8
func TestRecoverSessionConstraintOverlap_RetriesOneVanishedConflict(t *testing.T) {
	t.Parallel()

	pool := teachingTestPool(t)
	tutorID := uuid.Must(uuid.NewV7())
	cleanTeachingTutor(t, pool, tutorID)
	work := newTeachingHandler(t, pool, time.Date(2026, time.September, 20, 0, 0, 0, 0, time.UTC))
	startsAt := time.Date(2026, time.September, 20, 10, 0, 0, 0, time.UTC)
	endsAt := startsAt.Add(time.Hour)

	tx, err := pool.Begin(t.Context())
	require.NoError(t, err)
	retry, err := work.recoverSessionConstraintOverlap(
		t.Context(), tx, tutorID, uuid.Nil, startsAt, endsAt, "UTC", 1,
	)
	require.NoError(t, err)
	require.True(t, retry)

	tx, err = pool.Begin(t.Context())
	require.NoError(t, err)
	retry, err = work.recoverSessionConstraintOverlap(
		t.Context(), tx, tutorID, uuid.Nil, startsAt, endsAt, "UTC", 0,
	)
	require.False(t, retry)
	var generic *ConflictError
	require.ErrorAs(t, err, &generic)
	require.Equal(t, "session_overlap", generic.Code)
	details, ok := generic.Details.(map[string]any)
	require.True(t, ok)
	require.Nil(t, details["session_id"])

	created, _, err := work.CreateClass(
		t.Context(), tutorID, "UTC", "constraint-conflict-class",
		CreateClassInput{
			Name: "Conflict class", RateAmount: 150_000,
			FirstSession: FirstSessionInput{
				LocalDate: "2026-09-20", StartTime: "10:00", EndTime: "11:00",
			},
		},
	)
	require.NoError(t, err)
	tx, err = pool.Begin(t.Context())
	require.NoError(t, err)
	retry, err = work.recoverSessionConstraintOverlap(
		t.Context(), tx, tutorID, uuid.Nil, startsAt, endsAt, "UTC", 1,
	)
	require.False(t, retry)
	var conflict *ConflictError
	require.ErrorAs(t, err, &conflict)
	require.Equal(t, "session_overlap", conflict.Code)
	details, ok = conflict.Details.(map[string]any)
	require.True(t, ok)
	require.Equal(t, created.FirstSession.SessionID, details["session_id"])

	_, err = tx.Exec(t.Context(), "SELECT 1")
	require.ErrorIs(t, err, pgx.ErrTxClosed)
}

// covers: AC-6, AC-7, AC-10, AC-11, AC-13
func TestSessionCommands_PreserveAttendanceBoundsAndImmutableRetries(t *testing.T) {
	t.Parallel()

	pool := teachingTestPool(t)
	tutorID := uuid.Must(uuid.NewV7())
	cleanTeachingTutor(t, pool, tutorID)
	commandTime := time.Date(2024, time.February, 29, 15, 0, 0, 0, time.UTC)
	work := newTeachingHandler(t, pool, commandTime)
	ctx := vermouth.WithRequestID(t.Context(), "request-session-contract")
	created, _, err := work.CreateClass(ctx, tutorID, "America/New_York", "session-contract-class", CreateClassInput{
		Name: "Session contract", RateAmount: 150_000,
		Schedule: &WeeklyScheduleInput{
			ValidFrom: "2024-02-29", ValidThrough: "2024-02-29",
			Slots: []WeeklyScheduleSlotInput{{Weekday: 4, StartTime: "10:00", EndTime: "11:00"}},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, created.FirstSession)
	sessionID := created.FirstSession.SessionID
	student, _, err := work.CreateStudent(
		ctx, tutorID, "session-contract-student", CreateStudentInput{Name: "Mai"},
	)
	require.NoError(t, err)
	_, err = work.ChangeClassRoster(
		ctx, tutorID, created.Class.ClassID, "America/New_York", "session-contract-roster",
		ChangeRosterInput{
			ChangeDate: "2024-02-29", Additions: []uuid.UUID{student.StudentID}, Removals: []uuid.UUID{},
		},
	)
	require.NoError(t, err)
	sheet, err := work.ReadAttendance(ctx, tutorID, sessionID)
	require.NoError(t, err)
	_, err = work.SaveAttendance(
		ctx, tutorID, sessionID, "session-contract-attendance",
		SaveAttendanceInput{
			Revision: sheet.Revision,
			Marks:    []AttendanceMarkInput{{StudentID: student.StudentID, State: "Present"}},
		},
	)
	require.NoError(t, err)

	firstMove, status, err := work.MoveSession(
		ctx, tutorID, sessionID, "Asia/Tokyo", "session-first-move",
		MoveSessionInput{
			ExpectedVersion: 1, LocalDate: "2026-02-28", StartTime: "09:00", EndTime: "10:00",
		},
	)
	require.NoError(t, err)
	require.Equal(t, 200, status)
	require.Equal(t, int64(2), firstMove.Version)
	require.Equal(t, "2024-02-29", firstMove.OriginLocalDate)
	require.Equal(t, "2026-02-28", firstMove.LocalDate)
	require.NotNil(t, firstMove.SourceTimeZone)
	require.Equal(t, "America/New_York", *firstMove.SourceTimeZone)
	require.Equal(t, "Asia/Tokyo", firstMove.DisplayTimeZone)

	work.now = func() time.Time { return commandTime.Add(time.Hour) }
	secondMove, _, err := work.MoveSession(
		ctx, tutorID, sessionID, "UTC", "session-second-move",
		MoveSessionInput{
			ExpectedVersion: 2, LocalDate: "2024-03-10", StartTime: "03:00", EndTime: "04:00",
		},
	)
	require.NoError(t, err)
	require.Equal(t, int64(3), secondMove.Version)
	require.Equal(t, firstMove.SessionID, secondMove.SessionID)

	var attendanceCount int
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT count(*) FROM attendance
		WHERE tutor_id = $1 AND session_id = $2 AND student_id = $3 AND state = 'Present'`,
		tutorID, sessionID, student.StudentID,
	).Scan(&attendanceCount))
	require.Equal(t, 1, attendanceCount)

	_, _, err = work.MoveSession(
		ctx, tutorID, sessionID, "UTC", "session-outside-bound",
		MoveSessionInput{
			ExpectedVersion: 3, LocalDate: "2026-03-01", StartTime: "09:00", EndTime: "10:00",
		},
	)
	var validation *ValidationError
	require.ErrorAs(t, err, &validation)
	require.Equal(t, "local_date", validation.Field)

	replayedMove, replayStatus, err := work.MoveSession(
		ctx, tutorID, sessionID, "UTC", "session-first-move",
		MoveSessionInput{
			ExpectedVersion: 1, LocalDate: "2026-02-28", StartTime: "09:00", EndTime: "10:00",
		},
	)
	require.NoError(t, err)
	require.Equal(t, status, replayStatus)
	requireSameWireResponse(t, firstMove, replayedMove)

	_, _, err = work.MoveSession(
		ctx, tutorID, sessionID, "UTC", "session-first-move",
		MoveSessionInput{
			ExpectedVersion: 1, LocalDate: "2026-02-28", StartTime: "09:00", EndTime: "10:30",
		},
	)
	var moveConflict *ConflictError
	require.ErrorAs(t, err, &moveConflict)
	require.Equal(t, "idempotency_conflict", moveConflict.Code)

	_, _, err = work.MoveSession(
		ctx, tutorID, sessionID, "UTC", "session-stale-move",
		MoveSessionInput{
			ExpectedVersion: 2, LocalDate: "2024-03-11", StartTime: "09:00", EndTime: "10:00",
		},
	)
	var stale *ConflictError
	require.ErrorAs(t, err, &stale)
	require.Equal(t, "stale_session", stale.Code)

	cancelled, _, err := work.CancelSession(
		ctx, tutorID, sessionID, "UTC", "session-cancel",
		SessionVersionInput{ExpectedVersion: 3},
	)
	require.NoError(t, err)
	require.Equal(t, int64(4), cancelled.Version)
	require.Equal(t, "cancelled", cancelled.State)
	require.NotNil(t, cancelled.MovedAt)
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT count(*) FROM attendance WHERE tutor_id = $1 AND session_id = $2`,
		tutorID, sessionID,
	).Scan(&attendanceCount))
	require.Equal(t, 1, attendanceCount)

	restored, _, err := work.RestoreSession(
		ctx, tutorID, sessionID, "UTC", "session-restore",
		SessionVersionInput{ExpectedVersion: 4},
	)
	require.NoError(t, err)
	require.Equal(t, int64(5), restored.Version)
	require.Equal(t, "active", restored.State)
	require.NotNil(t, restored.MovedAt)
	require.Equal(t, cancelled.StartsAt, restored.StartsAt)

	replayedCancel, _, err := work.CancelSession(
		ctx, tutorID, sessionID, "Asia/Tokyo", "session-cancel",
		SessionVersionInput{ExpectedVersion: 3},
	)
	require.NoError(t, err)
	requireSameWireResponse(t, cancelled, replayedCancel)
	replayedRestore, _, err := work.RestoreSession(
		ctx, tutorID, sessionID, "Asia/Tokyo", "session-restore",
		SessionVersionInput{ExpectedVersion: 4},
	)
	require.NoError(t, err)
	requireSameWireResponse(t, restored, replayedRestore)

	_, _, err = work.CancelSession(
		ctx, tutorID, sessionID, "UTC", "session-cancel",
		SessionVersionInput{ExpectedVersion: 4},
	)
	var cancelConflict *ConflictError
	require.ErrorAs(t, err, &cancelConflict)
	require.Equal(t, "idempotency_conflict", cancelConflict.Code)

	cancelledAgain, _, err := work.CancelSession(
		ctx, tutorID, sessionID, "UTC", "session-cancel-again",
		SessionVersionInput{ExpectedVersion: 5},
	)
	require.NoError(t, err)
	_, _, err = work.CancelSession(
		ctx, tutorID, sessionID, "UTC", "session-invalid-cancel",
		SessionVersionInput{ExpectedVersion: cancelledAgain.Version},
	)
	var invalid *ConflictError
	require.ErrorAs(t, err, &invalid)
	require.Equal(t, "invalid_session_state", invalid.Code)
}

// covers: AC-6, AC-10
func TestMoveSession_UsesStandaloneTokenZoneAcrossClockTransitions(t *testing.T) {
	t.Parallel()

	pool := teachingTestPool(t)
	tutorID := uuid.Must(uuid.NewV7())
	cleanTeachingTutor(t, pool, tutorID)
	work := newTeachingHandler(t, pool, time.Date(2026, time.March, 1, 12, 0, 0, 0, time.UTC))
	ctx := vermouth.WithRequestID(t.Context(), "request-standalone-clock")
	created, _, err := work.CreateClass(ctx, tutorID, "UTC", "standalone-clock-class", CreateClassInput{
		Name: "Standalone clock", RateAmount: 150_000,
		FirstSession: FirstSessionInput{LocalDate: "2026-03-01", StartTime: "10:00", EndTime: "11:00"},
	})
	require.NoError(t, err)
	sessionID := created.FirstSession.SessionID

	_, _, err = work.MoveSession(
		ctx, tutorID, sessionID, "America/New_York", "standalone-inverted-gap",
		MoveSessionInput{
			ExpectedVersion: 1, LocalDate: "2026-03-08", StartTime: "02:30", EndTime: "03:00",
		},
	)
	var invalidGap *ValidationError
	require.ErrorAs(t, err, &invalidGap)

	spring, _, err := work.MoveSession(
		ctx, tutorID, sessionID, "America/New_York", "standalone-spring-gap",
		MoveSessionInput{
			ExpectedVersion: 1, LocalDate: "2026-03-08", StartTime: "02:00", EndTime: "03:30",
		},
	)
	require.NoError(t, err)
	require.Nil(t, spring.SourceTimeZone)
	require.Equal(t, "America/New_York", spring.DisplayTimeZone)
	require.Equal(t, "-04:00", spring.StartUTCOffset)
	require.Equal(t, "-04:00", spring.EndUTCOffset)
	require.True(t, spring.StartsAt.Equal(time.Date(2026, time.March, 8, 7, 0, 0, 0, time.UTC)))

	autumn, _, err := work.MoveSession(
		ctx, tutorID, sessionID, "America/New_York", "standalone-autumn-repeat",
		MoveSessionInput{
			ExpectedVersion: 2, LocalDate: "2026-11-01", StartTime: "01:30", EndTime: "02:30",
		},
	)
	require.NoError(t, err)
	require.Equal(t, "-04:00", autumn.StartUTCOffset)
	require.Equal(t, "-05:00", autumn.EndUTCOffset)
	require.True(t, autumn.StartsAt.Equal(time.Date(2026, time.November, 1, 5, 30, 0, 0, time.UTC)))
	require.True(t, autumn.EndsAt.Equal(time.Date(2026, time.November, 1, 7, 30, 0, 0, time.UTC)))
}
