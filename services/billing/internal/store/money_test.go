package store_test

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/nguyen-duc-loc/vermouth/services/billing/internal/store/sqlcgen"
)

// uniqueViolation is Postgres' own code for a unique constraint, which is what
// decides both races below rather than any lock this service takes.
const uniqueViolation = "23505"

// TestConcurrentRunsLeaveExactlyOneAndTheLoserReadsIt is the failure case spec
// 0001's flow 1 step 3 fixes and spec 0003 keys for (AC-1): two month end runs
// for the same tutor, period and generation, started at the same moment. Exactly
// one commits; the loser is refused by the unique constraint, reads the winner's
// run, and answers with the winner's invoices. Nothing here takes a lock, which
// is the point: the constraint is the decision.
func TestConcurrentRunsLeaveExactlyOneAndTheLoserReadsIt(t *testing.T) {
	t.Parallel()
	q := queries(t)
	ctx := t.Context()
	tutorID := newTutor(t)

	// Each attempt reports what happened and the test itself judges it: an
	// assertion inside a goroutine could not stop the test anyway.
	const attempts = 4
	type attempt struct {
		run sqlcgen.BillingRun
		err error
	}
	results := make(chan attempt, attempts)
	var start sync.WaitGroup
	var running sync.WaitGroup
	start.Add(1)

	for range attempts {
		running.Go(func() {
			start.Wait()
			run, err := q.InsertBillingRun(ctx, sqlcgen.InsertBillingRunParams{
				BillingRunID: uuid.Must(uuid.NewV7()), TutorID: tutorID,
				PeriodYear: testYear, PeriodMonth: 9, Generation: 1,
			})
			results <- attempt{run: run, err: err}
		})
	}
	start.Done()
	running.Wait()
	close(results)

	winners := make([]sqlcgen.BillingRun, 0, attempts)
	losers := 0
	for one := range results {
		if one.err != nil {
			var pgErr *pgconn.PgError
			require.True(t, errors.As(one.err, &pgErr) && pgErr.Code == uniqueViolation,
				"the only allowed failure is the unique constraint, got %v", one.err)
			losers++
			continue
		}
		winners = append(winners, one.run)
	}

	require.Len(t, winners, 1, "exactly one run may commit for a period and generation")
	require.Equal(t, attempts-1, losers, "every other press is refused by the constraint")

	current, err := q.CurrentBillingRunGeneration(ctx, sqlcgen.CurrentBillingRunGenerationParams{
		TutorID: tutorID, PeriodYear: testYear, PeriodMonth: 9,
	})
	require.NoError(t, err)
	require.Equal(t, winners[0].BillingRunID, current.BillingRunID,
		"a loser reads the winner's run rather than starting its own")
	require.Equal(t, int32(1), current.Generation)
	require.False(t, current.Superseded, "a live run is not superseded until a void says so")
}

// TestConcurrentInvoiceNumbersNeverRepeat is the other half of AC-1. The counter
// is one row bumped by one statement, so concurrent takes serialise on it: two
// invoices can never carry the same number, which UNIQUE (tutor_id,
// invoice_number) would otherwise have to catch after the fact.
func TestConcurrentInvoiceNumbersNeverRepeat(t *testing.T) {
	t.Parallel()
	q := queries(t)
	ctx := t.Context()
	tutorID := newTutor(t)

	const takes = 8
	type take struct {
		sequence int32
		err      error
	}
	results := make(chan take, takes)
	var start sync.WaitGroup
	var running sync.WaitGroup
	start.Add(1)

	for range takes {
		running.Go(func() {
			start.Wait()
			sequence, err := q.TakeNextInvoiceNumber(ctx, sqlcgen.TakeNextInvoiceNumberParams{
				TutorID: tutorID, PeriodYear: testYear,
			})
			results <- take{sequence: sequence, err: err}
		})
	}
	start.Done()
	running.Wait()
	close(results)

	taken := make(map[int32]bool, takes)
	for one := range results {
		require.NoError(t, one.err)
		require.False(t, taken[one.sequence], "sequence %d was handed out twice", one.sequence)
		taken[one.sequence] = true
	}

	require.Len(t, taken, takes)
	counter, err := q.GetInvoiceNumberCounter(ctx, sqlcgen.GetInvoiceNumberCounterParams{
		TutorID: tutorID, PeriodYear: testYear,
	})
	require.NoError(t, err)
	require.Equal(t, int32(takes), counter.LastSequence, "the counter counts every number spent")
}

// TestNothingToBillSpendsNoInvoiceNumber is the nothing to bill case (AC-1,
// AC-8). A student on the roster all month whose sessions are all Absent or
// unmarked produces no billable row, so the run groups over nothing, issues no
// invoice, and leaves the counter for that tutor and year exactly where it was: a
// number is never burnt on an invoice that does not exist.
func TestNothingToBillSpendsNoInvoiceNumber(t *testing.T) {
	t.Parallel()
	q := queries(t)
	ctx := t.Context()
	tutorID := newTutor(t)
	classID, studentID := newID(t), newID(t)
	seedClassAndStudent(t, q, tutorID, classID, studentID)

	for i, state := range []string{"Absent", ""} {
		sessionID := newID(t)
		starts := time.Date(2026, time.September, 2+i, 3, 0, 0, 0, time.UTC)
		require.NoError(t, q.UpsertSession(ctx, sqlcgen.UpsertSessionParams{
			SessionID: sessionID, ClassID: classID, TutorID: tutorID,
			StartsAt: starts, EndsAt: starts.Add(90 * time.Minute), LocalDate: day(time.September, 2+i),
		}))
		if state == "" {
			continue
		}
		require.NoError(t, q.UpsertAttendance(ctx, sqlcgen.UpsertAttendanceParams{
			SessionID: sessionID, StudentID: studentID, TutorID: tutorID, State: state, MarkedAt: starts,
		}))
	}

	require.Empty(t, billableFor(t, q, tutorID, time.September),
		"a month of Absent and unmarked sessions bills nobody")
	_, err := q.GetInvoiceNumberCounter(ctx, sqlcgen.GetInvoiceNumberCounterParams{
		TutorID: tutorID, PeriodYear: testYear,
	})
	require.Error(t, err, "no invoice means no counter row, so no number was spent")
	require.Empty(t, invoicesFor(t, q, tutorID, 9))
}

// TestAnIssuedInvoiceKeepsWhatItWasRenderedWith is INV-9 in the schema: the payee
// block and the student name are frozen onto the invoice at issue, so changing the
// bank account afterwards cannot reach backwards into a file the tutor already
// sent.
func TestAnIssuedInvoiceKeepsWhatItWasRenderedWith(t *testing.T) {
	t.Parallel()
	q := queries(t)
	ctx := t.Context()
	tutorID := newTutor(t)

	profile := saveCompleteProfile(t, q, tutorID, "0123456789")
	require.True(t, profile.IsComplete)

	invoice, _ := issueOneInvoice(t, q, tutorID, profile)
	require.Equal(t, "0123456789", invoice.PayeeBankAccountNumber)

	// The tutor changes bank account later.
	changed := saveCompleteProfile(t, q, tutorID, "9876543210")
	require.Equal(t, "9876543210", changed.BankAccountNumber.String)

	stored, err := q.GetInvoice(ctx, sqlcgen.GetInvoiceParams{TutorID: tutorID, InvoiceID: invoice.InvoiceID})
	require.NoError(t, err)
	require.Equal(t, "0123456789", stored.PayeeBankAccountNumber,
		"an issued invoice keeps the account number it was rendered with (INV-9)")
}

func TestAuthoritativeReferencesRejectCrossTutorChildren(t *testing.T) {
	t.Parallel()
	q := queries(t)
	ctx := t.Context()
	tutorA, tutorB := newTutor(t), newTutor(t)
	invoiceA, _ := issueOneInvoice(t, q, tutorA, saveCompleteProfile(t, q, tutorA, "0123456789"))
	invoiceB, _ := issueOneInvoice(t, q, tutorB, saveCompleteProfile(t, q, tutorB, "9876543210"))

	params := sqlcgen.InsertInvoiceParams{
		InvoiceID: newID(t), TutorID: tutorB, BillingRunID: invoiceA.BillingRunID,
		StudentID: newID(t), InvoiceNumber: "2026-9001",
		PeriodYear: 2026, PeriodMonth: 9, TotalAmount: 250_000, Currency: "VND",
		IssuedAt: time.Now().UTC(), StudentName: "Mai",
		PayeeLegalName: "Nguyen Thi Lan", PayeeContactLine: "lan@example.com",
		PayeeBankName: "Vietcombank", PayeeBankAccountNumber: "9876543210",
		PayeeBankAccountHolder: "NGUYEN THI LAN",
	}
	_, err := q.InsertInvoice(ctx, params)
	require.Error(t, err, "an invoice cannot point at another tutor's billing run")

	params.InvoiceID = newID(t)
	params.BillingRunID = invoiceB.BillingRunID
	params.InvoiceNumber = "2026-9002"
	params.ReplacesInvoiceID = pgtype.UUID{Bytes: invoiceA.InvoiceID, Valid: true}
	_, err = q.InsertInvoice(ctx, params)
	require.Error(t, err, "a replacement cannot point at another tutor's invoice")

	_, err = q.InsertInvoiceLine(ctx, sqlcgen.InsertInvoiceLineParams{
		InvoiceLineID: newID(t), InvoiceID: invoiceA.InvoiceID, TutorID: tutorB,
		SessionID: newID(t), SessionDate: day(time.September, 14),
		ClassName: "Maths 9A", RateAmount: 250_000, Amount: 250_000,
	})
	require.Error(t, err, "an invoice line cannot point at another tutor's invoice")
}

// saveCompleteProfile fills every field the completeness gate asks for, so the run
// is allowed to proceed.
func saveCompleteProfile(t *testing.T, q *sqlcgen.Queries, tutorID uuid.UUID, account string) sqlcgen.InvoiceProfile {
	t.Helper()
	profile, err := q.SaveInvoiceProfile(t.Context(), sqlcgen.SaveInvoiceProfileParams{
		TutorID:           tutorID,
		LegalName:         words("Nguyen Thi Lan"),
		ContactLine:       words("lan@example.com"),
		BankName:          words("Vietcombank"),
		BankAccountNumber: words(account),
		BankAccountHolder: words("NGUYEN THI LAN"),
	})
	require.NoError(t, err)
	return profile
}

// issueOneInvoice does what the month end run does, in the order it does it: take
// the generation, take the number, write the invoice with the render block frozen
// on, then its lines.
func issueOneInvoice(t *testing.T, q *sqlcgen.Queries, tutorID uuid.UUID, profile sqlcgen.InvoiceProfile) (sqlcgen.Invoice, sqlcgen.InvoiceLine) {
	t.Helper()
	ctx := t.Context()
	run, err := q.InsertBillingRun(ctx, sqlcgen.InsertBillingRunParams{
		BillingRunID: newID(t), TutorID: tutorID, PeriodYear: testYear, PeriodMonth: 9, Generation: 1,
	})
	require.NoError(t, err)
	sequence, err := q.TakeNextInvoiceNumber(ctx, sqlcgen.TakeNextInvoiceNumberParams{TutorID: tutorID, PeriodYear: testYear})
	require.NoError(t, err)

	invoice, err := q.InsertInvoice(ctx, sqlcgen.InsertInvoiceParams{
		InvoiceID: newID(t), TutorID: tutorID, BillingRunID: run.BillingRunID, StudentID: newID(t),
		// The year in the number is the period being billed, not the year the run
		// happened, so a December period invoiced in January still reads December.
		InvoiceNumber: invoiceNumber(run.PeriodYear, sequence),
		PeriodYear:    run.PeriodYear, PeriodMonth: run.PeriodMonth,
		TotalAmount: 250_000, Currency: "VND", IssuedAt: time.Now().UTC(),
		ReplacesInvoiceID:      pgtype.UUID{},
		StudentName:            "Mai",
		PayeeLegalName:         profile.LegalName.String,
		PayeeContactLine:       profile.ContactLine.String,
		PayeeBankName:          profile.BankName.String,
		PayeeBankAccountNumber: profile.BankAccountNumber.String,
		PayeeBankAccountHolder: profile.BankAccountHolder.String,
	})
	require.NoError(t, err)

	line, err := q.InsertInvoiceLine(ctx, sqlcgen.InsertInvoiceLineParams{
		InvoiceLineID: newID(t), InvoiceID: invoice.InvoiceID, TutorID: tutorID,
		SessionID: newID(t), SessionDate: day(time.September, 14),
		ClassName: "Maths 9A", RateAmount: 250_000, Amount: 250_000,
	})
	require.NoError(t, err)
	return invoice, line
}

// invoiceNumber is <year>-<4 digit sequence>, formatted in Go from the counter's
// pair the way spec 0003 says.
func invoiceNumber(year, sequence int32) string {
	return fmt.Sprintf("%d-%04d", year, sequence)
}

func invoicesFor(t *testing.T, q *sqlcgen.Queries, tutorID uuid.UUID, month int32) []sqlcgen.ListInvoicesForPeriodRow {
	t.Helper()
	rows, err := q.ListInvoicesForPeriod(t.Context(), sqlcgen.ListInvoicesForPeriodParams{
		TutorID: tutorID, PeriodYear: testYear, PeriodMonth: month,
	})
	require.NoError(t, err)
	return rows
}
