package store_test

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/nguyen-duc-loc/vermouth/services/billing/internal/handler"
	"github.com/nguyen-duc-loc/vermouth/services/billing/internal/store/sqlcgen"
)

// TestTheMissingFieldListAgreesWithTheGate is AC-12. The month end refusal and the
// profile screen must never disagree about whether a profile is complete, so the
// decision lives once in the schema as a generated column and the Go side only
// names which fields are still empty. This walks the interesting rows and asserts
// the two answers are the same one: an empty missing list exactly when is_complete
// is true. A null and an empty string are both missing, which is what the
// generated column says too.
// one saving over the last, so they have to run in order.
//
//nolint:paralleltest,tparallel // The cases share one profile row on purpose, each
func TestTheMissingFieldListAgreesWithTheGate(t *testing.T) {
	t.Parallel()
	q := queries(t)
	ctx := t.Context()
	tutorID := newTutor(t)

	full := sqlcgen.SaveInvoiceProfileParams{
		TutorID:           tutorID,
		LegalName:         words("Nguyen Thi Lan"),
		ContactLine:       words("lan@example.com"),
		BankName:          words("Vietcombank"),
		BankAccountNumber: words("0123456789"),
		BankAccountHolder: words("NGUYEN THI LAN"),
	}

	cases := []struct {
		name    string
		profile sqlcgen.SaveInvoiceProfileParams
		missing []string
	}{
		{
			name:    "the seeded row, where the tutor has typed nothing",
			profile: sqlcgen.SaveInvoiceProfileParams{TutorID: tutorID},
			missing: []string{
				handler.FieldLegalName, handler.FieldContactLine, handler.FieldBankName,
				handler.FieldBankAccountNumber, handler.FieldBankAccountHolder,
			},
		},
		{
			name:    "every field filled in",
			profile: full,
			missing: nil,
		},
		{
			name:    "the contact line left out, which the run still refuses on",
			profile: withField(full, func(p *sqlcgen.SaveInvoiceProfileParams) { p.ContactLine = pgtype.Text{} }),
			missing: []string{handler.FieldContactLine},
		},
		{
			name:    "an empty bank account number, which is missing rather than filled",
			profile: withField(full, func(p *sqlcgen.SaveInvoiceProfileParams) { p.BankAccountNumber = words("") }),
			missing: []string{handler.FieldBankAccountNumber},
		},
		{
			name: "only the bank block, which happens part way through typing",
			profile: withField(full, func(p *sqlcgen.SaveInvoiceProfileParams) {
				p.LegalName = pgtype.Text{}
				p.ContactLine = words("")
			}),
			missing: []string{handler.FieldLegalName, handler.FieldContactLine},
		},
	}

	for _, one := range cases {
		t.Run(one.name, func(t *testing.T) {
			stored, err := q.SaveInvoiceProfile(ctx, one.profile)
			require.NoError(t, err)

			missing := handler.MissingProfileFields(stored)
			require.Equal(t, one.missing, nilWhenEmpty(missing))
			require.Equal(t, len(missing) == 0, stored.IsComplete,
				"the Go missing field list and is_complete must be the same answer (AC-12)")

			read, err := q.GetInvoiceProfile(ctx, tutorID)
			require.NoError(t, err)
			require.Equal(t, stored.IsComplete, read.IsComplete,
				"the gate reads the same on the way out as on the way in")
		})
	}
}

// withField copies the full profile and changes one thing, so each case reads as
// the one difference it is testing.
func withField(base sqlcgen.SaveInvoiceProfileParams, change func(*sqlcgen.SaveInvoiceProfileParams)) sqlcgen.SaveInvoiceProfileParams {
	changed := base
	change(&changed)
	return changed
}

// nilWhenEmpty lets a case say "nothing missing" as nil rather than as an empty
// slice, which reads better in the table above.
func nilWhenEmpty(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	return values
}
