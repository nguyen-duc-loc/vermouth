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

	"github.com/nguyen-duc-loc/vermouth/services/billing/internal/store"
	"github.com/nguyen-duc-loc/vermouth/services/billing/internal/store/sqlcgen"
)

// These tests run against the real Postgres and Redpanda of
// test/compose.test.yaml (STK-15), because what they are here to catch (a
// projection that is not idempotent, a replay that touches an invoice, two
// concurrent runs both committing) only appears against a real database and a
// real broker. One stack, started once: nothing here starts a container.
//
// Isolation is tutor_id and nothing else, which is the same isolation the
// product has. Every table is scoped by it (INV-8), so each test invents a tutor
// and can never see another test's rows.

// pool is opened once for the whole package rather than per test, so a run costs
// one set of connections on a two core machine.
var pool *pgxpool.Pool

func TestMain(m *testing.M) {
	url := os.Getenv("BILLING_DATABASE_URL")
	if url != "" {
		ctx := context.Background()
		opened, err := vermouth.OpenPool(ctx, url)
		if err == nil {
			pool = opened
		}
	}
	m.Run()
	if pool != nil {
		pool.Close()
	}
}

// queries binds billing's own statements to the shared pool, skipping the test
// when the stack is not up. A skip says what to run; a failure here would only
// say the machine is quiet.
func queries(t *testing.T) *sqlcgen.Queries {
	t.Helper()
	if pool == nil {
		t.Skip("BILLING_DATABASE_URL is unset or billing's database did not answer: run task infra:up and task migrate:up")
	}
	return store.Queries(pool)
}

// newTutor invents a tutor for one test and forgets it afterwards.
func newTutor(t *testing.T) uuid.UUID {
	t.Helper()
	tutorID := newID(t)
	t.Cleanup(func() { forget(t, tutorID) })
	return tutorID
}

// newID is a UUIDv7, the way every identifier in this system is made.
func newID(t *testing.T) uuid.UUID {
	t.Helper()
	id, err := uuid.NewV7()
	require.NoError(t, err)
	return id
}

// forgetStatements empty one tutor out of billing, children first. This is the
// only place these tests write SQL of their own, and it exists because the model
// deliberately has no delete to borrow (AC-11): nothing in db/queries removes a
// row, so teardown cannot go through it.
var forgetStatements = []string{
	`DELETE FROM invoice_lines WHERE tutor_id = $1`,
	`DELETE FROM invoices WHERE tutor_id = $1`,
	`DELETE FROM billing_runs WHERE tutor_id = $1`,
	`DELETE FROM invoice_number_counters WHERE tutor_id = $1`,
	`DELETE FROM invoice_profiles WHERE tutor_id = $1`,
	`DELETE FROM attendance WHERE tutor_id = $1`,
	`DELETE FROM roster_periods WHERE tutor_id = $1`,
	`DELETE FROM class_rates WHERE tutor_id = $1`,
	`DELETE FROM sessions WHERE tutor_id = $1`,
	`DELETE FROM students WHERE tutor_id = $1`,
	`DELETE FROM classes WHERE tutor_id = $1`,
}

func forget(t *testing.T, tutorID uuid.UUID) {
	t.Helper()
	ctx := teardownContext()
	for _, statement := range forgetStatements {
		_, err := pool.Exec(ctx, statement, tutorID)
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

// day is a calendar day, which is a date and never an instant: the whole point
// of local_date is that it does not depend on a timezone at read time.
func day(month time.Month, dayOfMonth int) pgtype.Date {
	return pgtype.Date{Time: time.Date(testYear, month, dayOfMonth, 0, 0, 0, 0, time.UTC), Valid: true}
}

// at is an instant in UTC.
func at(moment time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: moment.UTC(), Valid: true}
}

// words is a filled in text column. A column the tutor has not typed yet is the
// zero pgtype.Text, which is null.
func words(value string) pgtype.Text { return pgtype.Text{String: value, Valid: true} }
