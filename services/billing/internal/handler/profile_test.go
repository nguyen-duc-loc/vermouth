package handler_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"
	"github.com/stretchr/testify/require"

	"github.com/nguyen-duc-loc/vermouth/services/billing/internal/consumer"
	"github.com/nguyen-duc-loc/vermouth/services/billing/internal/handler"
	"github.com/nguyen-duc-loc/vermouth/services/billing/internal/store"
	"github.com/nguyen-duc-loc/vermouth/services/billing/internal/store/sqlcgen"
)

// covers: AC-2, AC-3, AC-4, AC-6, AC-7, AC-8, AC-9
func TestProfileService_NormalizesRetriesConflictsAndCompletes(t *testing.T) {
	t.Parallel()

	pool := billingProjectionPool(t)
	tutorID := uuid.Must(uuid.NewV7())
	otherTutorID := uuid.Must(uuid.NewV7())
	cleanInvoiceProfiles(t, pool, tutorID, otherTutorID)
	profiles := handler.NewProfileService(pool)

	empty, err := profiles.Read(t.Context(), tutorID)
	require.NoError(t, err)
	require.Zero(t, empty.Revision)
	require.False(t, empty.IsComplete)
	require.Equal(t, []string{
		handler.FieldLegalName,
		handler.FieldContactLine,
		handler.FieldBankCode,
		handler.FieldBankAccountNumber,
		handler.FieldBankAccountHolder,
	}, empty.MissingFields)

	partialInput := handler.ProfileInput{ExpectedRevision: 0, LegalName: new("  Nguyễn   An  ")}
	partial, err := profiles.Save(t.Context(), tutorID, partialInput)
	require.NoError(t, err)
	require.Equal(t, new("Nguyễn An"), partial.LegalName)
	require.Equal(t, int64(1), partial.Revision)

	retried, err := profiles.Save(t.Context(), tutorID, partialInput)
	require.NoError(t, err)
	require.Equal(t, partial, retried, "an identical stale retry must not increase revision")

	_, err = profiles.Save(t.Context(), tutorID, handler.ProfileInput{
		ExpectedRevision: 0,
		LegalName:        new("A different name"),
	})
	var conflict *handler.ProfileConflictError
	require.ErrorAs(t, err, &conflict)
	require.Equal(t, int64(1), conflict.CurrentRevision)

	complete, err := profiles.Save(t.Context(), tutorID, handler.ProfileInput{
		ExpectedRevision:  partial.Revision,
		LegalName:         partial.LegalName,
		ContactLine:       new("  invoices@example.com "),
		BankCode:          new("970436"),
		BankAccountNumber: new(" ab123 "),
		BankAccountHolder: new(" Nguyễn   An "),
	})
	require.NoError(t, err)
	require.True(t, complete.IsComplete)
	require.Empty(t, complete.MissingFields)
	require.Equal(t, "active", complete.BankStatus)
	require.Equal(t, new("Vietcombank"), complete.BankName)
	require.Equal(t, new("AB123"), complete.BankAccountNumber)
	require.Equal(t, int64(2), complete.Revision)

	envelope, err := vermouth.NewEnvelope(
		t.Context(), vermouth.EventTutorRegistered, 1, tutorID,
		vermouth.Key{Kind: vermouth.KeyTutorID, Value: tutorID},
		map[string]any{"tutor_id": tutorID, "future_field": "ignored"},
	)
	require.NoError(t, err)
	tx, err := pool.Begin(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) }) //nolint:usetesting // Cleanup outlives the test context.
	require.NoError(t, consumer.Identity().Handle(t.Context(), tx, envelope))
	require.NoError(t, tx.Commit(t.Context()))
	afterReplay, err := profiles.Read(t.Context(), tutorID)
	require.NoError(t, err)
	require.Equal(t, complete, afterReplay, "identity replay must not change any profile value or revision")

	other, err := profiles.Read(t.Context(), otherTutorID)
	require.NoError(t, err)
	require.Nil(t, other.LegalName, "one tutor must never read another tutor's profile")
}

// covers: AC-6, AC-7
func TestProfileService_RejectsInvalidTextThatNormalizesToTheCurrentValue(t *testing.T) {
	t.Parallel()

	pool := billingProjectionPool(t)
	tutorID := uuid.Must(uuid.NewV7())
	cleanInvoiceProfiles(t, pool, tutorID)
	profiles := handler.NewProfileService(pool)

	current, err := profiles.Save(t.Context(), tutorID, handler.ProfileInput{
		ExpectedRevision: 0,
		LegalName:        new("Nguyễn An"),
	})
	require.NoError(t, err)

	_, err = profiles.Save(t.Context(), tutorID, handler.ProfileInput{
		ExpectedRevision: current.Revision,
		LegalName:        new("Nguyễn\nAn"),
	})
	var validation *handler.ProfileValidationError
	require.ErrorAs(t, err, &validation)
	require.Equal(t, []handler.ProfileFieldError{
		{Field: handler.FieldLegalName, Reason: "invalid_format"},
	}, validation.Fields)

	after, err := profiles.Read(t.Context(), tutorID)
	require.NoError(t, err)
	require.Equal(t, current, after)
}

// covers: AC-6, AC-8
func TestProfileService_RejectsInvalidAndInactiveBankValuesWithoutChangingTheRow(t *testing.T) {
	t.Parallel()

	pool := billingProjectionPool(t)
	tutorID := uuid.Must(uuid.NewV7())
	cleanInvoiceProfiles(t, pool, tutorID)
	profiles := handler.NewProfileService(pool)
	before, err := profiles.Read(t.Context(), tutorID)
	require.NoError(t, err)

	_, err = profiles.Save(t.Context(), tutorID, handler.ProfileInput{
		ExpectedRevision:  0,
		LegalName:         new("Name\nwith break"),
		BankCode:          new("971005"),
		BankAccountNumber: new("12-34"),
	})
	var validation *handler.ProfileValidationError
	require.ErrorAs(t, err, &validation)
	require.ElementsMatch(t, []handler.ProfileFieldError{
		{Field: handler.FieldLegalName, Reason: "invalid_format"},
		{Field: handler.FieldBankAccountNumber, Reason: "invalid_format"},
		{Field: handler.FieldBankCode, Reason: "inactive_bank"},
	}, validation.Fields)

	after, err := profiles.Read(t.Context(), tutorID)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

// covers: AC-8
func TestProfileService_KeepsSavedInactiveBankReadableAndBlocksChangedSaves(t *testing.T) {
	t.Parallel()

	pool := billingProjectionPool(t)
	tutorID := uuid.Must(uuid.NewV7())
	cleanInvoiceProfiles(t, pool, tutorID)
	profiles := handler.NewProfileService(pool)
	_, err := profiles.Read(t.Context(), tutorID)
	require.NoError(t, err)
	stored, err := store.Queries(pool).UpdateInvoiceProfile(
		t.Context(),
		sqlcgen.UpdateInvoiceProfileParams{
			OwnerTutorID:      tutorID,
			LegalName:         pgtype.Text{String: "Nguyễn An", Valid: true},
			ContactLine:       pgtype.Text{String: "invoices@example.com", Valid: true},
			BankCode:          pgtype.Text{String: "971005", Valid: true},
			BankName:          pgtype.Text{String: "ViettelMoney", Valid: true},
			BankAccountNumber: pgtype.Text{String: "AB123", Valid: true},
			BankAccountHolder: pgtype.Text{String: "NGUYỄN AN", Valid: true},
		},
	)
	require.NoError(t, err)

	inactive, err := profiles.Read(t.Context(), tutorID)
	require.NoError(t, err)
	require.Equal(t, "inactive", inactive.BankStatus)
	require.True(t, inactive.IsComplete, "structural completeness is separate from bank activity")
	input := handler.ProfileInput{
		ExpectedRevision:  0,
		LegalName:         inactive.LegalName,
		ContactLine:       inactive.ContactLine,
		BankCode:          inactive.BankCode,
		BankAccountNumber: inactive.BankAccountNumber,
		BankAccountHolder: inactive.BankAccountHolder,
	}
	retried, err := profiles.Save(t.Context(), tutorID, input)
	require.NoError(t, err)
	require.Equal(t, stored.Revision, retried.Revision)

	input.ExpectedRevision = stored.Revision
	input.ContactLine = new("changed@example.com")
	_, err = profiles.Save(t.Context(), tutorID, input)
	var validation *handler.ProfileValidationError
	require.ErrorAs(t, err, &validation)
	require.Contains(t, validation.Fields, handler.ProfileFieldError{
		Field: handler.FieldBankCode, Reason: "inactive_bank",
	})
}

// covers: AC-7
func TestProfileService_ConcurrentIdenticalSavesAdvanceRevisionOnce(t *testing.T) {
	t.Parallel()

	pool := billingProjectionPool(t)
	tutorID := uuid.Must(uuid.NewV7())
	cleanInvoiceProfiles(t, pool, tutorID)
	profiles := handler.NewProfileService(pool)
	_, err := profiles.Read(t.Context(), tutorID)
	require.NoError(t, err)
	input := handler.ProfileInput{ExpectedRevision: 0, LegalName: new("Nguyễn An")}

	results := make(chan handler.InvoiceProfile, 2)
	errorsFound := make(chan error, 2)
	for range 2 {
		go func() {
			profile, saveErr := profiles.Save(t.Context(), tutorID, input)
			results <- profile
			errorsFound <- saveErr
		}()
	}
	for range 2 {
		require.NoError(t, <-errorsFound)
		require.Equal(t, int64(1), (<-results).Revision)
	}
}

func cleanInvoiceProfiles(t *testing.T, pool *pgxpool.Pool, tutorIDs ...uuid.UUID) {
	t.Helper()

	t.Cleanup(func() {
		ctx := context.Background() //nolint:usetesting // Cleanup runs after the test context is canceled.
		for _, tutorID := range tutorIDs {
			_, err := pool.Exec(ctx, `DELETE FROM invoice_profiles WHERE tutor_id = $1`, tutorID)
			require.NoError(t, err)
		}
	})
}
