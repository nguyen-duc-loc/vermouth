//nolint:testpackage // These integration tests inject the handler clock and inspect its transaction boundary.
package handler

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"
	"github.com/stretchr/testify/require"
)

func teachingTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	databaseURL := os.Getenv("TEACHING_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEACHING_DATABASE_URL is unset, you may run task infra:up and task migrate:up")
	}
	pool, err := vermouth.OpenPool(t.Context(), databaseURL)
	if err != nil {
		t.Skipf("the teaching database did not answer, you may run task infra:up: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func cleanTeachingTutor(t *testing.T, pool *pgxpool.Pool, tutorID uuid.UUID) {
	t.Helper()

	t.Cleanup(func() {
		ctx := context.Background() //nolint:usetesting // Cleanup runs after t.Context is canceled.
		for _, statement := range []string{
			`DELETE FROM outbox WHERE tutor_id = $1`,
			`DELETE FROM attendance WHERE tutor_id = $1`,
			`DELETE FROM roster_periods WHERE tutor_id = $1`,
			`DELETE FROM sessions WHERE tutor_id = $1`,
			`DELETE FROM schedule_slots WHERE tutor_id = $1`,
			`DELETE FROM schedule_rules WHERE tutor_id = $1`,
			`DELETE FROM students WHERE tutor_id = $1`,
			`DELETE FROM classes WHERE tutor_id = $1`,
			`DELETE FROM command_receipts WHERE tutor_id = $1`,
		} {
			_, err := pool.Exec(ctx, statement, tutorID)
			require.NoError(t, err)
		}
	})
}

// covers: AC-1, AC-3, AC-9, AC-10, AC-11
func TestPutSchedule_AdoptsExactStandaloneAndReplaysImmutableResponse(t *testing.T) {
	t.Parallel()

	pool := teachingTestPool(t)
	tutorID := uuid.Must(uuid.NewV7())
	cleanTeachingTutor(t, pool, tutorID)
	commandTime := time.Date(2026, time.August, 30, 2, 0, 0, 0, time.UTC)
	work := newTeachingHandler(t, pool, commandTime)
	ctx := vermouth.WithRequestID(t.Context(), "request-put-schedule")
	classResult, _, err := work.CreateClass(
		ctx, tutorID, "Asia/Ho_Chi_Minh", "standalone-class", testClassInput(),
	)
	require.NoError(t, err)
	standaloneID := classResult.FirstSession.SessionID
	input := PutScheduleInput{
		ExpectedRevision: 0,
		EffectiveFrom:    "2026-08-30",
		ValidThrough:     "2026-09-06",
		Slots: []WeeklyScheduleSlotInput{
			{Weekday: 7, StartTime: "10:00", EndTime: "11:00"},
			{Weekday: 1, StartTime: "17:30", EndTime: "19:00"},
		},
	}

	created, status, err := work.PutSchedule(
		ctx, tutorID, classResult.Class.ClassID, "Asia/Ho_Chi_Minh", "put-schedule", input,
	)
	require.NoError(t, err)
	require.Equal(t, 201, status)
	require.Equal(t, int64(1), created.Class.ScheduleRevision)
	require.Equal(t, 3, created.CandidateCount)
	require.Equal(t, 2, created.CreatedCount)
	require.Equal(t, 1, created.AdoptedCount)
	require.NotNil(t, created.FirstSession)
	require.Equal(t, standaloneID, created.FirstSession.SessionID)
	require.Equal(t, "Asia/Ho_Chi_Minh", created.Rule.TimeZone)
	require.Equal(t, int16(1), created.Rule.Slots[0].Weekday)
	require.Equal(t, int16(7), created.Rule.Slots[1].Weekday)

	work.now = func() time.Time { return commandTime.AddDate(1, 0, 0) }
	reordered := input
	reordered.Slots = []WeeklyScheduleSlotInput{input.Slots[1], input.Slots[0]}
	replayed, replayStatus, err := work.PutSchedule(
		ctx, tutorID, classResult.Class.ClassID, "UTC", "put-schedule", reordered,
	)
	require.NoError(t, err)
	require.Equal(t, status, replayStatus)
	require.Equal(t, created, replayed)

	var sessionCount, scheduledEventCount int
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT count(*) FROM sessions WHERE tutor_id = $1 AND class_id = $2`,
		tutorID, classResult.Class.ClassID,
	).Scan(&sessionCount))
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT count(*) FROM outbox
		WHERE tutor_id = $1 AND event_name = $2`,
		tutorID, vermouth.EventSessionScheduled,
	).Scan(&scheduledEventCount))
	require.Equal(t, 3, sessionCount)
	require.Equal(t, 3, scheduledEventCount)

	var version int64
	var scheduleRuleID uuid.UUID
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT version, schedule_rule_id
		FROM sessions
		WHERE tutor_id = $1 AND session_id = $2`,
		tutorID, standaloneID,
	).Scan(&version, &scheduleRuleID))
	require.Equal(t, int64(2), version)
	require.Equal(t, created.Rule.ScheduleRuleID, scheduleRuleID)

	var contextTime string
	var contextZone string
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT context_snapshot->>'command_time', context_snapshot->>'governing_time_zone'
		FROM command_receipts
		WHERE tutor_id = $1 AND operation = 'put_schedule' AND idempotency_key = 'put-schedule'`,
		tutorID,
	).Scan(&contextTime, &contextZone))
	require.Equal(t, commandTime.Format(time.RFC3339), contextTime)
	require.Equal(t, "Asia/Ho_Chi_Minh", contextZone)
}

func newTeachingHandler(t *testing.T, pool *pgxpool.Pool, now time.Time) *Handler {
	t.Helper()

	work := New(pool, slog.New(slog.DiscardHandler), vermouth.TopicTeaching)
	work.now = func() time.Time { return now }
	return work
}

func testClassInput() CreateClassInput {
	return CreateClassInput{
		Name:       " Maths 9A ",
		RateAmount: 250_000,
		FirstSession: FirstSessionInput{
			LocalDate: "2026-08-30",
			StartTime: "10:00",
			EndTime:   "11:00",
		},
	}
}

// covers: AC-2, AC-3, AC-10, AC-11, AC-12
func TestCreateClass_ConcurrentRetryReturnsOneCommittedAggregate(t *testing.T) {
	t.Parallel()

	pool := teachingTestPool(t)
	tutorID := uuid.Must(uuid.NewV7())
	cleanTeachingTutor(t, pool, tutorID)
	work := newTeachingHandler(t, pool, time.Date(2026, time.August, 30, 2, 0, 0, 0, time.UTC))
	ctx := vermouth.WithRequestID(t.Context(), "request-class")
	type outcome struct {
		result  CreateClassResult
		created bool
		err     error
	}
	outcomes := make(chan outcome, 2)

	for range 2 {
		go func() {
			result, created, err := work.CreateClass(
				ctx,
				tutorID,
				"Asia/Ho_Chi_Minh",
				"class-command",
				testClassInput(),
			)
			outcomes <- outcome{result: result, created: created, err: err}
		}()
	}
	first, second := <-outcomes, <-outcomes

	require.NoError(t, first.err)
	require.NoError(t, second.err)
	require.NotEqual(t, first.created, second.created)
	require.Equal(t, first.result, second.result)
	require.Equal(t, uuid.Version(7), first.result.Class.ClassID.Version())
	require.Equal(t, uuid.Version(7), first.result.FirstSession.SessionID.Version())
	require.Equal(t, "Maths 9A", first.result.Class.Name)
	require.Equal(t, "VND", first.result.Class.Currency)
	require.True(t, knownClassColor(first.result.Class.Color))
	require.True(t, first.result.FirstSession.StartsAt.Equal(
		time.Date(2026, time.August, 30, 3, 0, 0, 0, time.UTC),
	))

	var classCount, sessionCount, receiptCount, eventCount int
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM classes WHERE tutor_id = $1`, tutorID).Scan(&classCount))
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM sessions WHERE tutor_id = $1`, tutorID).Scan(&sessionCount))
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM command_receipts WHERE tutor_id = $1`, tutorID).Scan(&receiptCount))
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM outbox WHERE tutor_id = $1`, tutorID).Scan(&eventCount))
	require.Equal(t, 1, classCount)
	require.Equal(t, 1, sessionCount)
	require.Equal(t, 1, receiptCount)
	require.Equal(t, 2, eventCount)

	changed := testClassInput()
	changed.Name = "Physics"
	_, _, err := work.CreateClass(ctx, tutorID, "Asia/Ho_Chi_Minh", "class-command", changed)
	require.ErrorIs(t, err, ErrIdempotencyConflict)
	timezoneReplay, replayCreated, err := work.CreateClass(
		ctx, tutorID, "UTC", "class-command", testClassInput(),
	)
	require.NoError(t, err)
	require.False(t, replayCreated)
	require.Equal(t, first.result, timezoneReplay)
}

// covers: AC-4, AC-6, AC-10, AC-11
func TestCreateStudent_RetryKeepsPrivatePhoneOutOfTheEvent(t *testing.T) {
	t.Parallel()

	pool := teachingTestPool(t)
	tutorID := uuid.Must(uuid.NewV7())
	cleanTeachingTutor(t, pool, tutorID)
	work := newTeachingHandler(t, pool, time.Date(2026, time.August, 30, 2, 0, 0, 0, time.UTC))
	ctx := vermouth.WithRequestID(t.Context(), "request-student")
	phone := "0901234567"
	input := CreateStudentInput{Name: " Mai ", Phone: &phone}

	createdStudent, created, err := work.CreateStudent(ctx, tutorID, "student-command", input)
	require.NoError(t, err)
	require.True(t, created)
	require.Equal(t, "Mai", createdStudent.Name)
	require.NotNil(t, createdStudent.Phone)
	require.Equal(t, phone, *createdStudent.Phone)

	replayedStudent, created, err := work.CreateStudent(ctx, tutorID, "student-command", input)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, createdStudent, replayedStudent)

	var envelope string
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT envelope::text
		FROM outbox
		WHERE tutor_id = $1 AND event_name = $2`, tutorID, vermouth.EventStudentRegistered).Scan(&envelope))
	require.NotContains(t, envelope, "0901234567")
	require.NotContains(t, envelope, "phone")
	require.Contains(t, envelope, "request-student")

	otherPhone := "0900000000"
	_, _, err = work.CreateStudent(
		ctx,
		tutorID,
		"student-command",
		CreateStudentInput{Name: "Mai", Phone: &otherPhone},
	)
	require.ErrorIs(t, err, ErrIdempotencyConflict)
}

// covers: AC-1, AC-4, AC-5, AC-6, AC-7, AC-10, AC-12
func TestRosterAndAttendance_RetryCorrectionAndReloadUseTeachingTruth(t *testing.T) {
	t.Parallel()

	pool := teachingTestPool(t)
	tutorID := uuid.Must(uuid.NewV7())
	cleanTeachingTutor(t, pool, tutorID)
	markedAt := time.Date(2026, time.August, 30, 3, 30, 0, 0, time.UTC)
	work := newTeachingHandler(t, pool, markedAt)
	ctx := vermouth.WithRequestID(t.Context(), "request-loop")

	classResult, _, err := work.CreateClass(
		ctx,
		tutorID,
		"Asia/Ho_Chi_Minh",
		"class-loop",
		testClassInput(),
	)
	require.NoError(t, err)
	student, _, err := work.CreateStudent(ctx, tutorID, "student-loop", CreateStudentInput{Name: "Mai"})
	require.NoError(t, err)
	joinInput := JoinRosterInput{StudentID: student.StudentID, EffectiveFrom: "2026-08-30"}

	period, created, err := work.JoinRoster(ctx, tutorID, classResult.Class.ClassID, joinInput)
	require.NoError(t, err)
	require.True(t, created)
	replayedPeriod, created, err := work.JoinRoster(ctx, tutorID, classResult.Class.ClassID, joinInput)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, period, replayedPeriod)
	_, _, err = work.JoinRoster(ctx, tutorID, classResult.Class.ClassID, JoinRosterInput{
		StudentID: student.StudentID, EffectiveFrom: "2026-08-31",
	})
	require.ErrorIs(t, err, ErrConflict)

	present, err := work.MarkAttendance(
		ctx,
		tutorID,
		classResult.FirstSession.SessionID,
		student.StudentID,
		MarkAttendanceInput{State: "Present"},
	)
	require.NoError(t, err)
	require.True(t, present.MarkedAt.Equal(markedAt))
	repeated, err := work.MarkAttendance(
		ctx,
		tutorID,
		classResult.FirstSession.SessionID,
		student.StudentID,
		MarkAttendanceInput{State: "Present"},
	)
	require.NoError(t, err)
	require.Equal(t, present, repeated)

	correctedAt := markedAt.Add(time.Minute)
	work.now = func() time.Time { return correctedAt }
	corrected, err := work.MarkAttendance(
		ctx,
		tutorID,
		classResult.FirstSession.SessionID,
		student.StudentID,
		MarkAttendanceInput{State: "Absent"},
	)
	require.NoError(t, err)
	require.Equal(t, "Absent", corrected.State)
	require.True(t, corrected.MarkedAt.Equal(correctedAt))

	strangerID := uuid.Must(uuid.NewV7())
	_, err = work.MarkAttendance(
		ctx,
		strangerID,
		classResult.FirstSession.SessionID,
		student.StudentID,
		MarkAttendanceInput{State: "Present"},
	)
	require.ErrorIs(t, err, ErrNotFound)

	work.now = func() time.Time { return time.Date(2026, time.August, 30, 4, 0, 0, 0, time.UTC) }
	home, err := work.ReadHome(ctx, tutorID, "Asia/Ho_Chi_Minh", "")
	require.NoError(t, err)
	require.Len(t, home.Sessions, 1)
	require.Equal(t, classResult.Class.Name, home.Sessions[0].ClassName)
	require.Equal(t, classResult.Class.Color, home.Sessions[0].ClassColor)
	require.Len(t, home.Sessions[0].Students, 1)
	require.Equal(t, student.Name, home.Sessions[0].Students[0].Name)
	require.NotNil(t, home.Sessions[0].Students[0].AttendanceState)
	require.Equal(t, "Absent", *home.Sessions[0].Students[0].AttendanceState)

	var rosterEvents, attendanceEvents int
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT count(*) FROM outbox WHERE tutor_id = $1 AND event_name = $2`,
		tutorID, vermouth.EventRosterJoined,
	).Scan(&rosterEvents))
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT count(*) FROM outbox WHERE tutor_id = $1 AND event_name = $2`,
		tutorID, vermouth.EventAttendanceMarked,
	).Scan(&attendanceEvents))
	require.Equal(t, 1, rosterEvents)
	require.Equal(t, 2, attendanceEvents)
}
