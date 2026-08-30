package handler_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"
	"github.com/stretchr/testify/require"

	"github.com/nguyen-duc-loc/vermouth/services/billing/internal/consumer"
	"github.com/nguyen-duc-loc/vermouth/services/billing/internal/handler"
)

func billingProjectionPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	databaseURL := os.Getenv("BILLING_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("BILLING_DATABASE_URL is unset, you may run task infra:up and task migrate:up")
	}
	pool, err := vermouth.OpenPool(t.Context(), databaseURL)
	if err != nil {
		t.Skipf("the billing database did not answer, you may run task infra:up: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func cleanBillingProjectionTutor(t *testing.T, pool *pgxpool.Pool, tutorID uuid.UUID) {
	t.Helper()

	t.Cleanup(func() {
		ctx := context.Background() //nolint:usetesting // Cleanup runs after t.Context is canceled.
		for _, statement := range []string{
			`DELETE FROM attendance WHERE tutor_id = $1`,
			`DELETE FROM roster_periods WHERE tutor_id = $1`,
			`DELETE FROM sessions WHERE tutor_id = $1`,
			`DELETE FROM students WHERE tutor_id = $1`,
			`DELETE FROM class_rates WHERE tutor_id = $1`,
			`DELETE FROM classes WHERE tutor_id = $1`,
		} {
			_, err := pool.Exec(ctx, statement, tutorID)
			require.NoError(t, err)
		}
	})
}

func projectionEvent(
	t *testing.T,
	name string,
	tutorID uuid.UUID,
	key vermouth.Key,
	fields map[string]any,
) vermouth.Envelope {
	t.Helper()

	envelope, err := vermouth.NewEnvelope(t.Context(), name, 1, tutorID, key, fields)
	require.NoError(t, err)
	return envelope
}

func applyProjectionEvent(t *testing.T, pool *pgxpool.Pool, envelope vermouth.Envelope) {
	t.Helper()

	tx, err := pool.Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(t.Context()) }()
	require.NoError(t, consumer.Teaching().Handle(t.Context(), tx, envelope))
	require.NoError(t, tx.Commit(t.Context()))
}

// covers: AC-8, AC-9, AC-12
func TestTeachingStatus_ProductionConsumerConvergesFromOutOfOrderFacts(t *testing.T) {
	t.Parallel()

	pool := billingProjectionPool(t)
	tutorID := uuid.Must(uuid.NewV7())
	otherTutorID := uuid.Must(uuid.NewV7())
	classID := uuid.Must(uuid.NewV7())
	sessionID := uuid.Must(uuid.NewV7())
	studentID := uuid.Must(uuid.NewV7())
	cleanBillingProjectionTutor(t, pool, tutorID)
	reader := handler.NewProjectionReader(pool)
	startsAt := time.Date(2026, time.August, 30, 3, 0, 0, 0, time.UTC)
	events := []vermouth.Envelope{
		projectionEvent(t, vermouth.EventSessionScheduled, tutorID,
			vermouth.Key{Kind: vermouth.KeySessionID, Value: sessionID}, map[string]any{
				"tutor_id": tutorID, "session_id": sessionID, "class_id": classID,
				"starts_at": startsAt, "ends_at": startsAt.Add(time.Hour), "local_date": "2026-08-30",
			}),
		projectionEvent(t, vermouth.EventClassCreated, tutorID,
			vermouth.Key{Kind: vermouth.KeyClassID, Value: classID}, map[string]any{
				"tutor_id": tutorID, "class_id": classID, "name": "Maths 9A",
				"rate_amount": int64(250_000), "currency": "VND", "rate_effective_from": "2026-08-30",
				"future_field": "ignored",
			}),
		projectionEvent(t, vermouth.EventStudentRegistered, tutorID,
			vermouth.Key{Kind: vermouth.KeyStudentID, Value: studentID}, map[string]any{
				"tutor_id": tutorID, "student_id": studentID, "name": "Mai", "phone": "must be ignored",
			}),
		projectionEvent(t, vermouth.EventRosterJoined, tutorID,
			vermouth.Key{Kind: vermouth.KeyClassID, Value: classID}, map[string]any{
				"tutor_id": tutorID, "class_id": classID, "student_id": studentID,
				"effective_from": "2026-08-30",
			}),
		projectionEvent(t, vermouth.EventAttendanceMarked, tutorID,
			vermouth.Key{Kind: vermouth.KeySessionID, Value: sessionID}, map[string]any{
				"tutor_id": tutorID, "session_id": sessionID, "student_id": studentID,
				"state": "Present", "marked_at": startsAt,
			}),
	}

	initial, err := reader.TeachingStatus(t.Context(), tutorID)
	require.NoError(t, err)
	require.Equal(t, "waiting", initial.State)
	require.Nil(t, initial.LatestUpdatedAt)

	for _, event := range events[:len(events)-1] {
		applyProjectionEvent(t, pool, event)
	}
	waiting, err := reader.TeachingStatus(t.Context(), tutorID)
	require.NoError(t, err)
	require.Equal(t, "waiting", waiting.State)
	require.EqualValues(t, 1, waiting.ClassCount)
	require.EqualValues(t, 1, waiting.SessionCount)
	require.EqualValues(t, 1, waiting.StudentCount)
	require.EqualValues(t, 1, waiting.OpenRosterCount)
	require.Zero(t, waiting.AttendanceCount)
	require.NotNil(t, waiting.LatestUpdatedAt)

	applyProjectionEvent(t, pool, events[len(events)-1])
	active, err := reader.TeachingStatus(t.Context(), tutorID)
	require.NoError(t, err)
	require.Equal(t, "active", active.State)
	require.EqualValues(t, 1, active.AttendanceCount)

	for _, event := range events {
		applyProjectionEvent(t, pool, event)
	}
	replayed, err := reader.TeachingStatus(t.Context(), tutorID)
	require.NoError(t, err)
	require.Equal(t, active.ClassCount, replayed.ClassCount)
	require.Equal(t, active.SessionCount, replayed.SessionCount)
	require.Equal(t, active.StudentCount, replayed.StudentCount)
	require.Equal(t, active.OpenRosterCount, replayed.OpenRosterCount)
	require.Equal(t, active.AttendanceCount, replayed.AttendanceCount)

	isolated, err := reader.TeachingStatus(t.Context(), otherTutorID)
	require.NoError(t, err)
	require.Equal(t, "waiting", isolated.State)
	require.Zero(t, isolated.ClassCount)
	require.Nil(t, isolated.LatestUpdatedAt)
}
