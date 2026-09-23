//nolint:testpackage // These white box tests lock roster normalization and phone disclosure.
package handler

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/nguyen-duc-loc/vermouth/services/teaching/internal/store/sqlcgen"
)

// covers: AC-7, AC-8
func TestValidateRosterChange_NormalizesOneBoundedDisjointDelta(t *testing.T) {
	t.Parallel()

	today := time.Date(2026, time.September, 20, 0, 0, 0, 0, time.UTC)
	first := uuid.MustParse("018f8f7e-91b0-7cc4-bd8c-f4d9030ca421")
	second := uuid.MustParse("018f8f7e-91b0-7cc4-bd8c-f4d9030ca422")
	third := uuid.MustParse("018f8f7e-91b0-7cc4-bd8c-f4d9030ca423")

	result, err := validateRosterChange(ChangeRosterInput{
		ChangeDate: "2026-09-20",
		Additions:  []uuid.UUID{second, first},
		Removals:   []uuid.UUID{third},
	}, today)

	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{first, second}, result.additions)
	require.Equal(t, []uuid.UUID{third}, result.removals)
	require.Equal(t, []uuid.UUID{first, second, third}, result.allIDs)
}

// covers: AC-7, AC-8
func TestValidateRosterChange_RejectsInvalidAtomicDeltas(t *testing.T) {
	t.Parallel()

	today := time.Date(2026, time.September, 20, 0, 0, 0, 0, time.UTC)
	studentID := uuid.MustParse("018f8f7e-91b0-7cc4-bd8c-f4d9030ca421")
	tooMany := make([]uuid.UUID, maxRosterChanges+1)
	for index := range tooMany {
		tooMany[index] = uuid.Must(uuid.NewV7())
	}
	tests := []struct {
		name  string
		input ChangeRosterInput
		field string
	}{
		{"future date", ChangeRosterInput{ChangeDate: "2026-09-21", Additions: []uuid.UUID{studentID}}, "change_date"},
		{"empty delta", ChangeRosterInput{ChangeDate: "2026-09-20"}, validationFieldBody},
		{"too many students", ChangeRosterInput{ChangeDate: "2026-09-20", Additions: tooMany}, validationFieldBody},
		{"duplicate addition", ChangeRosterInput{ChangeDate: "2026-09-20", Additions: []uuid.UUID{studentID, studentID}}, validationFieldBody},
		{"overlapping sets", ChangeRosterInput{ChangeDate: "2026-09-20", Additions: []uuid.UUID{studentID}, Removals: []uuid.UUID{studentID}}, validationFieldBody},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := validateRosterChange(test.input, today)

			var validation *ValidationError
			require.ErrorAs(t, err, &validation)
			require.Equal(t, test.field, validation.Field)
		})
	}
}

// covers: AC-6, AC-15
func TestRosterStudentFromStored_DisclosesPhoneOnlyForCurrentActiveRows(t *testing.T) {
	t.Parallel()

	phone := "090-123 456"
	row := sqlcgen.ListClassRosterAtDateRow{
		StudentID:     uuid.MustParse("018f8f7e-91b0-7cc4-bd8c-f4d9030ca421"),
		Name:          "Mai",
		Phone:         pgtype.Text{String: phone, Valid: true},
		EffectiveFrom: pgtype.Date{Time: time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC), Valid: true},
	}

	current := rosterStudentFromStored(row, true)
	past := rosterStudentFromStored(row, false)
	row.RemovedAt = pgtype.Timestamptz{Time: time.Now(), Valid: true}
	archived := rosterStudentFromStored(row, true)

	require.Equal(t, &phone, current.Phone)
	require.Nil(t, past.Phone)
	require.Nil(t, archived.Phone)
	require.True(t, archived.Archived)
	require.NotContains(t, strings.TrimSpace(archived.Name), phone)
}
