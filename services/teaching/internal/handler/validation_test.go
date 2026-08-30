//nolint:testpackage // These white box tests lock the validation and hashing contract.
package handler

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

var errValidationTest = errors.New("validation test failure")

func validClassInput() CreateClassInput {
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

// covers: AC-2, AC-11
func TestValidateClassInput_AcceptsTrimmedNamesRateBoundariesAndLocalTime(t *testing.T) {
	t.Parallel()

	for _, rate := range []int64{0, maxRateAmount} {
		input := validClassInput()
		input.RateAmount = rate

		validated, err := validateClassInput(input, "Asia/Ho_Chi_Minh")

		require.NoError(t, err)
		require.Equal(t, "Maths 9A", validated.name)
		require.Equal(t, rate, validated.rateAmount)
		require.Equal(t, time.Date(2026, time.August, 30, 3, 0, 0, 0, time.UTC), validated.startsAt)
		require.Equal(t, time.Date(2026, time.August, 30, 4, 0, 0, 0, time.UTC), validated.endsAt)
	}
}

// covers: AC-2, AC-3, AC-11
func TestValidateClassInput_RejectsInvalidCallerValues(t *testing.T) {
	t.Parallel()

	unknownColor := "chartreuse"
	tests := []struct {
		name      string
		change    func(*CreateClassInput)
		wantField string
	}{
		{"empty name", func(input *CreateClassInput) { input.Name = "  " }, "name"},
		{"long Unicode name", func(input *CreateClassInput) { input.Name = strings.Repeat("ớ", 121) }, "name"},
		{"negative rate", func(input *CreateClassInput) { input.RateAmount = -1 }, "rate_amount"},
		{"rate above maximum", func(input *CreateClassInput) { input.RateAmount = maxRateAmount + 1 }, "rate_amount"},
		{"unknown color", func(input *CreateClassInput) { input.Color = &unknownColor }, "color"},
		{"invalid date", func(input *CreateClassInput) { input.FirstSession.LocalDate = "2026-8-30" }, "first_session.local_date"},
		{"invalid start", func(input *CreateClassInput) { input.FirstSession.StartTime = "9:30" }, "first_session.start_time"},
		{"equal times", func(input *CreateClassInput) { input.FirstSession.EndTime = "10:00" }, "first_session.end_time"},
		{"reversed times", func(input *CreateClassInput) { input.FirstSession.EndTime = "09:30" }, "first_session.end_time"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := validClassInput()
			test.change(&input)

			_, err := validateClassInput(input, "Asia/Ho_Chi_Minh")

			var validation *ValidationError
			require.ErrorAs(t, err, &validation)
			require.Equal(t, test.wantField, validation.Field)
		})
	}
}

// covers: AC-2, AC-11
func TestValidateClassInput_RejectsMissingAndRepeatedClockReadings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		date      string
		startTime string
		wantText  string
	}{
		{"spring clock gap", "2026-03-08", "02:30", "does not exist"},
		{"autumn repeated clock", "2026-11-01", "01:30", "is repeated"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := validClassInput()
			input.FirstSession.LocalDate = test.date
			input.FirstSession.StartTime = test.startTime
			input.FirstSession.EndTime = "04:00"

			_, err := validateClassInput(input, "America/New_York")

			var validation *ValidationError
			require.ErrorAs(t, err, &validation)
			require.Equal(t, "first_session.start_time", validation.Field)
			require.Contains(t, validation.Message, test.wantText)
		})
	}
}

// covers: AC-3, AC-11
func TestKnownClassColor_AcceptsOnlyTheContractPalette(t *testing.T) {
	t.Parallel()

	for _, color := range []string{"red", "rose", "orange", "green", "blue", "yellow", "violet"} {
		require.True(t, knownClassColor(color))
	}
	for _, color := range []string{"", "purple", "Blue", "blue "} {
		require.False(t, knownClassColor(color))
	}
}

// covers: AC-3
func TestSuggestedClassColor_MatchesTheBrowserFNVVectors(t *testing.T) {
	t.Parallel()

	require.Equal(t, "yellow", suggestedClassColor("a"))
	require.Equal(t, "red", suggestedClassColor("foobar"))
}

// covers: AC-4, AC-6, AC-11
func TestValidateStudentInput_NormalizesOnlyTheDocumentedFields(t *testing.T) {
	t.Parallel()

	emptyPhone := ""
	validated, err := validateStudentInput(CreateStudentInput{Name: " Mai ", Phone: &emptyPhone})
	require.NoError(t, err)
	require.Equal(t, "Mai", validated.name)
	require.Nil(t, validated.phone)

	phone := " 090 123 "
	validated, err = validateStudentInput(CreateStudentInput{Name: "Mai", Phone: &phone})
	require.NoError(t, err)
	require.NotNil(t, validated.phone)
	require.Equal(t, phone, *validated.phone)

	tooLong := strings.Repeat("1", maxPhoneRunes+1)
	_, err = validateStudentInput(CreateStudentInput{Name: "Mai", Phone: &tooLong})
	var validation *ValidationError
	require.ErrorAs(t, err, &validation)
	require.Equal(t, "phone", validation.Field)
}

// covers: AC-10
func TestCommandHashes_NameTheValidatedCommandAndTimezone(t *testing.T) {
	t.Parallel()

	first, err := validateClassInput(validClassInput(), "Asia/Ho_Chi_Minh")
	require.NoError(t, err)
	sameInput := validClassInput()
	sameInput.Name = "Maths 9A"
	same, err := validateClassInput(sameInput, "Asia/Ho_Chi_Minh")
	require.NoError(t, err)
	otherTimezone, err := validateClassInput(validClassInput(), "UTC")
	require.NoError(t, err)

	firstHash, err := classRequestHash(first)
	require.NoError(t, err)
	sameHash, err := classRequestHash(same)
	require.NoError(t, err)
	timezoneHash, err := classRequestHash(otherTimezone)
	require.NoError(t, err)

	require.Equal(t, firstHash, sameHash)
	require.NotEqual(t, firstHash, timezoneHash)

	emptyPhone := ""
	studentWithEmpty, err := validateStudentInput(CreateStudentInput{Name: " Mai ", Phone: &emptyPhone})
	require.NoError(t, err)
	studentWithNil, err := validateStudentInput(CreateStudentInput{Name: "Mai"})
	require.NoError(t, err)
	emptyHash, err := studentRequestHash(studentWithEmpty)
	require.NoError(t, err)
	nilHash, err := studentRequestHash(studentWithNil)
	require.NoError(t, err)
	require.Equal(t, emptyHash, nilHash)
}

// covers: AC-10, AC-11
func TestValidateIdempotencyKey_CountsUnicodeCharacters(t *testing.T) {
	t.Parallel()

	require.NoError(t, validateIdempotencyKey(strings.Repeat("ớ", 128)))
	for _, key := range []string{"", strings.Repeat("ớ", 129)} {
		var validation *ValidationError
		require.ErrorAs(t, validateIdempotencyKey(key), &validation)
		require.Equal(t, "Idempotency-Key", validation.Field)
	}
}

func TestOwnedReadError_HidesMissingAndCrossTutorResources(t *testing.T) {
	t.Parallel()

	require.ErrorIs(t, ownedReadError("read class", pgx.ErrNoRows), ErrNotFound)
	require.ErrorIs(t, ownedReadError("read class", errValidationTest), errValidationTest)
}
