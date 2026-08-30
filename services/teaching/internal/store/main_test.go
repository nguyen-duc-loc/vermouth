package store_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"
	"github.com/stretchr/testify/require"

	"github.com/nguyen-duc-loc/vermouth/services/teaching/internal/store"
	"github.com/nguyen-duc-loc/vermouth/services/teaching/internal/store/sqlcgen"
)

// These tests run against the real Postgres of test/compose.test.yaml (STK-15).
// Isolation is tutor_id, the same isolation the product has.

var pool *pgxpool.Pool

func TestMain(m *testing.M) {
	url := os.Getenv("TEACHING_DATABASE_URL")
	if url != "" {
		opened, err := vermouth.OpenPool(context.Background(), url)
		if err == nil {
			pool = opened
		}
	}
	m.Run()
	if pool != nil {
		pool.Close()
	}
}

func queries(t *testing.T) *sqlcgen.Queries {
	t.Helper()
	if pool == nil {
		t.Skip("TEACHING_DATABASE_URL is unset or the database did not answer: run task infra:up and task migrate:up")
	}
	return store.Queries(pool)
}

func newTutor(t *testing.T) uuid.UUID {
	t.Helper()
	tutorID := newID(t)
	t.Cleanup(func() { forget(t, tutorID) })
	return tutorID
}

func newID(t *testing.T) uuid.UUID {
	t.Helper()
	id, err := uuid.NewV7()
	require.NoError(t, err)
	return id
}

// forgetStatements empty one tutor out of teaching, children before parents
// because these tables do carry foreign keys: they are all authoritative and all
// in one context, which is exactly where a foreign key belongs. This is the only
// SQL these tests write of their own, and it exists because the model has no
// delete to borrow (AC-11).
var forgetStatements = []string{
	`DELETE FROM attendance WHERE tutor_id = $1`,
	`DELETE FROM roster_periods WHERE tutor_id = $1`,
	`DELETE FROM sessions WHERE tutor_id = $1`,
	`DELETE FROM students WHERE tutor_id = $1`,
	`DELETE FROM classes WHERE tutor_id = $1`,
}

func forget(t *testing.T, tutorID uuid.UUID) {
	t.Helper()
	for _, statement := range forgetStatements {
		_, err := pool.Exec(teardownContext(), statement, tutorID)
		require.NoError(t, err)
	}
}

// teardownContext outlives a test's own context, which Go cancels before it runs
// the Cleanup functions. It takes no *testing.T on purpose: a teardown cannot use
// the context the test just lost.
func teardownContext() context.Context { return context.Background() }

// testYear is the one made up year everything below happens in, so a call reads
// as the month and day it is about.
const testYear = 2026

// day is a calendar day, which is a date and never an instant.
//
// stays a parameter so a call reads as the day it is about.
//
//nolint:unparam // Every test here happens to sit in one September; the month
func day(month time.Month, dayOfMonth int) pgtype.Date {
	return pgtype.Date{Time: time.Date(testYear, month, dayOfMonth, 0, 0, 0, 0, time.UTC), Valid: true}
}

// seedClassStudentAndSession is the shape every test needs: one class, one
// student, one session on a day.
func seedClassStudentAndSession(t *testing.T, q *sqlcgen.Queries, tutorID uuid.UUID, dayOfMonth int) (classID, studentID, sessionID uuid.UUID) {
	t.Helper()
	ctx := t.Context()
	classID, studentID, sessionID = newID(t), newID(t), newID(t)

	_, err := q.InsertClass(ctx, sqlcgen.InsertClassParams{
		ClassID: classID, TutorID: tutorID, Name: "Maths 9A", Color: "blue",
		RateAmount: 250_000, Currency: "VND", RateEffectiveFrom: day(time.September, 1),
	})
	require.NoError(t, err)
	_, err = q.InsertStudent(ctx, sqlcgen.InsertStudentParams{
		StudentID: studentID, TutorID: tutorID, Name: "Mai", Phone: pgtype.Text{String: "0901234567", Valid: true},
	})
	require.NoError(t, err)
	starts := time.Date(2026, time.September, dayOfMonth, 3, 0, 0, 0, time.UTC)
	_, err = q.InsertSession(ctx, sqlcgen.InsertSessionParams{
		SessionID: sessionID, ClassID: classID, TutorID: tutorID,
		StartsAt: starts, EndsAt: starts.Add(90 * time.Minute), LocalDate: day(time.September, dayOfMonth),
	})
	require.NoError(t, err)
	return classID, studentID, sessionID
}
