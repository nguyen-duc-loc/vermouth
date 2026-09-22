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
	require.Equal(t, uuid.Version(7), created.Rule.ScheduleRuleID.Version())

	rows, err := pool.Query(t.Context(), `
		SELECT key_kind, key_value, occurred_at
		FROM outbox
		WHERE tutor_id = $1 AND event_name = $2
		ORDER BY key_value`, tutorID, vermouth.EventSessionScheduled)
	require.NoError(t, err)
	t.Cleanup(rows.Close)
	eventSessionIDs := make([]uuid.UUID, 0, 3)
	for rows.Next() {
		var keyKind string
		var sessionID uuid.UUID
		var occurredAt time.Time
		require.NoError(t, rows.Scan(&keyKind, &sessionID, &occurredAt))
		require.Equal(t, string(vermouth.KeySessionID), keyKind)
		require.True(t, occurredAt.Equal(commandTime))
		require.Equal(t, uuid.Version(7), sessionID.Version())
		eventSessionIDs = append(eventSessionIDs, sessionID)
	}
	require.NoError(t, rows.Err())

	schedule, err := work.ReadSchedule(
		t.Context(), tutorID, "Asia/Ho_Chi_Minh", "2026-08-30", "2026-09-06",
		[]uuid.UUID{classResult.Class.ClassID}, false, 50, "",
	)
	require.NoError(t, err)
	require.Len(t, schedule.Sessions, 3)
	storedSessionIDs := make([]uuid.UUID, 0, len(schedule.Sessions))
	for _, session := range schedule.Sessions {
		require.Equal(t, uuid.Version(7), session.SessionID.Version())
		require.NotNil(t, session.ScheduleRuleID)
		require.Equal(t, created.Rule.ScheduleRuleID, *session.ScheduleRuleID)
		require.NotNil(t, session.SourceTimeZone)
		require.Equal(t, "Asia/Ho_Chi_Minh", *session.SourceTimeZone)
		require.Equal(t, session.OriginLocalDate, session.LocalDate)
		require.Equal(t, session.DisplayDate, session.LocalDate)
		require.Equal(t, "+07:00", session.StartUTCOffset)
		require.Equal(t, "+07:00", session.EndUTCOffset)
		storedSessionIDs = append(storedSessionIDs, session.SessionID)
	}
	require.ElementsMatch(t, storedSessionIDs, eventSessionIDs)

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

// covers: AC-1, AC-2, AC-3, AC-19, AC-23
func TestPutClassRate_RevisesEveryCommandAndReplaysOnlyTheReceipt(t *testing.T) {
	t.Parallel()

	pool := teachingTestPool(t)
	tutorID := uuid.Must(uuid.NewV7())
	cleanTeachingTutor(t, pool, tutorID)
	commandTime := time.Date(2026, time.September, 5, 2, 0, 0, 0, time.UTC)
	work := newTeachingHandler(t, pool, commandTime)
	ctx := vermouth.WithRequestID(t.Context(), "request-class-rate")
	created, _, err := work.CreateClass(
		ctx, tutorID, "Asia/Ho_Chi_Minh", "class-with-rate", testClassInput(),
	)
	require.NoError(t, err)
	require.Equal(t, int64(1), created.Class.RateRevision)

	first, status, err := work.PutClassRate(
		ctx,
		tutorID,
		created.Class.ClassID,
		"Asia/Ho_Chi_Minh",
		"rate-september",
		"2026-09-01",
		PutClassRateInput{RateAmount: 300_000},
	)
	require.NoError(t, err)
	require.Equal(t, 200, status)
	require.Equal(t, int64(2), first.RateRevision)
	require.Equal(t, "2026-09-01", first.Current.EffectiveFrom)
	require.True(t, first.IssuedInvoicesUnchanged)

	sameAmount, sameAmountStatus, err := work.PutClassRate(
		ctx,
		tutorID,
		created.Class.ClassID,
		"Asia/Ho_Chi_Minh",
		"rate-september-again",
		"2026-09-01",
		PutClassRateInput{RateAmount: 300_000},
	)
	require.NoError(t, err)
	require.Equal(t, 200, sameAmountStatus)
	require.Equal(t, int64(3), sameAmount.RateRevision)

	replayed, replayStatus, err := work.PutClassRate(
		ctx,
		tutorID,
		created.Class.ClassID,
		"UTC",
		"rate-september",
		"2026-09-01",
		PutClassRateInput{RateAmount: 300_000},
	)
	require.NoError(t, err)
	require.Equal(t, status, replayStatus)
	require.Equal(t, first, replayed)
	_, _, err = work.PutClassRate(
		ctx,
		tutorID,
		created.Class.ClassID,
		"Asia/Ho_Chi_Minh",
		"rate-september",
		"2026-09-01",
		PutClassRateInput{RateAmount: 301_000},
	)
	require.ErrorIs(t, err, ErrIdempotencyConflict)

	backdated, _, err := work.PutClassRate(
		ctx,
		tutorID,
		created.Class.ClassID,
		"Asia/Ho_Chi_Minh",
		"rate-august-correction",
		"2026-08-30",
		PutClassRateInput{RateAmount: 275_000},
	)
	require.NoError(t, err)
	require.Equal(t, int64(4), backdated.RateRevision)
	require.Equal(t, "2026-09-01", backdated.Current.EffectiveFrom)
	require.Equal(t, int64(300_000), backdated.Current.RateAmount)

	strangerID := uuid.Must(uuid.NewV7())
	_, err = work.ReadClassRateState(
		ctx,
		strangerID,
		created.Class.ClassID,
		"Asia/Ho_Chi_Minh",
	)
	require.ErrorIs(t, err, ErrNotFound)

	var eventCount int
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT count(*)
		FROM outbox
		WHERE tutor_id = $1 AND event_name = $2
	`, tutorID, vermouth.EventClassRateChanged).Scan(&eventCount))
	require.Equal(t, 3, eventCount, "receipt replay must not write a second event")

	_, err = pool.Exec(t.Context(), `
		DELETE FROM sessions WHERE tutor_id = $1 AND class_id = $2
	`, tutorID, created.Class.ClassID)
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), `
		UPDATE classes SET archived_at = $3 WHERE tutor_id = $1 AND class_id = $2
	`, tutorID, created.Class.ClassID, commandTime)
	require.NoError(t, err)
	_, _, err = work.PutClassRate(
		ctx,
		tutorID,
		created.Class.ClassID,
		"Asia/Ho_Chi_Minh",
		"archived-no-session",
		"2026-09-01",
		PutClassRateInput{RateAmount: 300_000},
	)
	var conflict *ConflictError
	require.ErrorAs(t, err, &conflict)
	require.Equal(t, "archived_class_has_no_sessions", conflict.Code)
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
	rosterInput := ChangeRosterInput{
		ChangeDate: "2026-08-30", Additions: []uuid.UUID{student.StudentID}, Removals: []uuid.UUID{},
	}
	roster, err := work.ChangeClassRoster(
		ctx, tutorID, classResult.Class.ClassID, "Asia/Ho_Chi_Minh", "roster-loop", rosterInput,
	)
	require.NoError(t, err)
	replayedRoster, err := work.ChangeClassRoster(
		ctx, tutorID, classResult.Class.ClassID, "Asia/Ho_Chi_Minh", "roster-loop", rosterInput,
	)
	require.NoError(t, err)
	require.Equal(t, roster, replayedRoster)
	_, err = work.ChangeClassRoster(
		ctx, tutorID, classResult.Class.ClassID, "Asia/Ho_Chi_Minh", "roster-loop",
		ChangeRosterInput{
			ChangeDate: "2026-08-30", Additions: []uuid.UUID{}, Removals: []uuid.UUID{student.StudentID},
		},
	)
	require.ErrorIs(t, err, ErrIdempotencyConflict)

	sheet, err := work.ReadAttendance(ctx, tutorID, classResult.FirstSession.SessionID)
	require.NoError(t, err)
	present, err := work.SaveAttendance(
		ctx, tutorID, classResult.FirstSession.SessionID, "attendance-present",
		SaveAttendanceInput{
			Revision: sheet.Revision,
			Marks:    []AttendanceMarkInput{{StudentID: student.StudentID, State: "Present"}},
		},
	)
	require.NoError(t, err)
	require.True(t, present.MarkedAt.Equal(markedAt))
	repeated, err := work.SaveAttendance(
		ctx, tutorID, classResult.FirstSession.SessionID, "attendance-present",
		SaveAttendanceInput{
			Revision: sheet.Revision,
			Marks:    []AttendanceMarkInput{{StudentID: student.StudentID, State: "Present"}},
		},
	)
	require.NoError(t, err)
	require.Equal(t, present, repeated)

	correctedAt := markedAt.Add(time.Minute)
	work.now = func() time.Time { return correctedAt }
	freshSheet, err := work.ReadAttendance(ctx, tutorID, classResult.FirstSession.SessionID)
	require.NoError(t, err)
	corrected, err := work.SaveAttendance(
		ctx, tutorID, classResult.FirstSession.SessionID, "attendance-absent",
		SaveAttendanceInput{
			Revision: freshSheet.Revision,
			Marks:    []AttendanceMarkInput{{StudentID: student.StudentID, State: "Absent"}},
		},
	)
	require.NoError(t, err)
	require.Equal(t, "Absent", corrected.Marks[0].State)
	require.True(t, corrected.MarkedAt.Equal(correctedAt))

	strangerID := uuid.Must(uuid.NewV7())
	_, err = work.ReadAttendance(ctx, strangerID, classResult.FirstSession.SessionID)
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
