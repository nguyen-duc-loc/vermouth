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

	"github.com/nguyen-duc-loc/vermouth/services/notifications/internal/store"
	"github.com/nguyen-duc-loc/vermouth/services/notifications/internal/store/sqlcgen"
)

// These tests run against the real Postgres of test/compose.test.yaml (STK-15).
// Isolation is tutor_id, the same isolation the product has, so each test invents
// a tutor and can never see another test's rows.

var pool *pgxpool.Pool

func TestMain(m *testing.M) {
	url := os.Getenv("NOTIFICATIONS_DATABASE_URL")
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
		t.Skip("NOTIFICATIONS_DATABASE_URL is unset or the database did not answer: run task infra:up and task migrate:up")
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

// forgetStatements empty one tutor out of notifications. This is the only SQL
// these tests write of their own, and it exists because the model has no delete to
// borrow (AC-11).
var forgetStatements = []string{
	`DELETE FROM digest_runs WHERE tutor_id = $1`,
	`DELETE FROM roster_periods WHERE tutor_id = $1`,
	`DELETE FROM sessions WHERE tutor_id = $1`,
	`DELETE FROM classes WHERE tutor_id = $1`,
	`DELETE FROM recipients WHERE tutor_id = $1`,
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

func at(moment time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: moment.UTC(), Valid: true}
}
