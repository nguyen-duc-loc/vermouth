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

func validationTestNow() time.Time {
	return time.Date(2026, time.August, 30, 2, 0, 0, 0, time.UTC)
}

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

		validated, err := validateClassInput(input, "Asia/Ho_Chi_Minh", validationTestNow())

		require.NoError(t, err)
		require.Equal(t, "Maths 9A", validated.name)
		require.Equal(t, rate, validated.rateAmount)
		require.Equal(t, time.Date(2026, time.August, 30, 3, 0, 0, 0, time.UTC), validated.first.startsAt)
		require.Equal(t, time.Date(2026, time.August, 30, 4, 0, 0, 0, time.UTC), validated.first.endsAt)
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

			_, err := validateClassInput(input, "Asia/Ho_Chi_Minh", validationTestNow())

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

			_, err := validateClassInput(input, "America/New_York", validationTestNow())

			var validation *ValidationError
			require.ErrorAs(t, err, &validation)
			require.Equal(t, "first_session.start_time", validation.Field)
			require.Contains(t, validation.Message, test.wantText)
		})
	}
}

// covers: AC-1, AC-10
func TestValidateWeeklySchedule_AcceptsSevenSortedSlotsAndClampedTwoYearBound(t *testing.T) {
	t.Parallel()

	location, err := time.LoadLocation("Asia/Ho_Chi_Minh")
	require.NoError(t, err)
	slots := []WeeklyScheduleSlotInput{
		{Weekday: 7, StartTime: "09:00", EndTime: "10:00"},
		{Weekday: 1, StartTime: "17:30", EndTime: "19:00"},
		{Weekday: 6, StartTime: "08:00", EndTime: "09:00"},
		{Weekday: 2, StartTime: "17:30", EndTime: "19:00"},
		{Weekday: 5, StartTime: "17:30", EndTime: "19:00"},
		{Weekday: 3, StartTime: "17:30", EndTime: "19:00"},
		{Weekday: 4, StartTime: "17:30", EndTime: "19:00"},
	}
	schedule, err := validateWeeklySchedule(WeeklyScheduleInput{
		ValidFrom: "2024-02-29", ValidThrough: "2026-02-28", Slots: slots,
	}, location, time.Date(2024, time.February, 29, 2, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.Len(t, schedule.slots, 7)
	require.Equal(t, int16(1), schedule.slots[0].Weekday)
	require.Equal(t, int16(7), schedule.slots[6].Weekday)
	require.Len(t, schedule.occurrences, 731)

	_, err = validateWeeklySchedule(WeeklyScheduleInput{
		ValidFrom: "2024-02-29", ValidThrough: "2026-03-01", Slots: slots[:1],
	}, location, time.Date(2024, time.February, 29, 2, 0, 0, 0, time.UTC))
	var validation *ValidationError
	require.ErrorAs(t, err, &validation)
	require.Equal(t, "schedule.valid_through", validation.Field)
}

// covers: AC-1, AC-9
func TestValidateWeeklySchedule_RejectsDuplicateWeekdays(t *testing.T) {
	t.Parallel()

	location, err := time.LoadLocation("UTC")
	require.NoError(t, err)
	tests := []struct {
		name      string
		slots     []WeeklyScheduleSlotInput
		wantField string
	}{
		{name: "zero slots", wantField: "schedule.slots"},
		{
			name: "eight slots",
			slots: []WeeklyScheduleSlotInput{
				{Weekday: 1, StartTime: "09:00", EndTime: "10:00"},
				{Weekday: 2, StartTime: "09:00", EndTime: "10:00"},
				{Weekday: 3, StartTime: "09:00", EndTime: "10:00"},
				{Weekday: 4, StartTime: "09:00", EndTime: "10:00"},
				{Weekday: 5, StartTime: "09:00", EndTime: "10:00"},
				{Weekday: 6, StartTime: "09:00", EndTime: "10:00"},
				{Weekday: 7, StartTime: "09:00", EndTime: "10:00"},
				{Weekday: 1, StartTime: "11:00", EndTime: "12:00"},
			},
			wantField: "schedule.slots",
		},
		{
			name: "duplicate weekday",
			slots: []WeeklyScheduleSlotInput{
				{Weekday: 1, StartTime: "09:00", EndTime: "10:00"},
				{Weekday: 1, StartTime: "11:00", EndTime: "12:00"},
			},
			wantField: "schedule.slots[1].weekday",
		},
		{
			name:      "equal clocks",
			slots:     []WeeklyScheduleSlotInput{{Weekday: 7, StartTime: "09:00", EndTime: "09:00"}},
			wantField: "schedule.slots[0].end_time",
		},
		{
			name:      "reversed clocks",
			slots:     []WeeklyScheduleSlotInput{{Weekday: 7, StartTime: "10:00", EndTime: "09:00"}},
			wantField: "schedule.slots[0].end_time",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := validateWeeklySchedule(WeeklyScheduleInput{
				ValidFrom: "2026-08-30", ValidThrough: "2026-09-06", Slots: test.slots,
			}, location, validationTestNow())
			var validation *ValidationError
			require.ErrorAs(t, err, &validation)
			require.Equal(t, test.wantField, validation.Field)
		})
	}
}

// covers: AC-10
func TestValidateWeeklySchedule_ResolvesGapForwardAndAmbiguityEarlier(t *testing.T) {
	t.Parallel()

	location, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	spring, err := validateWeeklySchedule(WeeklyScheduleInput{
		ValidFrom:    "2026-03-08",
		ValidThrough: "2026-03-08",
		Slots:        []WeeklyScheduleSlotInput{{Weekday: 7, StartTime: "02:00", EndTime: "03:30"}},
	}, location, time.Date(2026, time.March, 8, 5, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.Len(t, spring.occurrences, 1)
	require.Equal(t, time.Date(2026, time.March, 8, 7, 0, 0, 0, time.UTC), spring.occurrences[0].startsAt)
	require.Equal(t, time.Date(2026, time.March, 8, 7, 30, 0, 0, time.UTC), spring.occurrences[0].endsAt)

	autumn, err := validateWeeklySchedule(WeeklyScheduleInput{
		ValidFrom:    "2026-11-01",
		ValidThrough: "2026-11-01",
		Slots:        []WeeklyScheduleSlotInput{{Weekday: 7, StartTime: "01:30", EndTime: "02:30"}},
	}, location, time.Date(2026, time.November, 1, 4, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.Len(t, autumn.occurrences, 1)
	require.Equal(t, time.Date(2026, time.November, 1, 5, 30, 0, 0, time.UTC), autumn.occurrences[0].startsAt)
	require.Equal(t, time.Date(2026, time.November, 1, 7, 30, 0, 0, time.UTC), autumn.occurrences[0].endsAt)
}

// covers: AC-1, AC-10
func TestValidateWeeklySchedule_EnforcesPastBoundAndRejectsWholeDayGap(t *testing.T) {
	t.Parallel()

	utc := time.UTC
	now := time.Date(2026, time.September, 20, 12, 0, 0, 0, time.UTC)
	accepted, err := validateWeeklySchedule(WeeklyScheduleInput{
		ValidFrom: "2026-08-21", ValidThrough: "2026-08-21",
		Slots: []WeeklyScheduleSlotInput{{Weekday: 5, StartTime: "09:00", EndTime: "10:00"}},
	}, utc, now)
	require.NoError(t, err)
	require.Len(t, accepted.occurrences, 1)

	_, err = validateWeeklySchedule(WeeklyScheduleInput{
		ValidFrom: "2026-08-20", ValidThrough: "2026-08-20",
		Slots: []WeeklyScheduleSlotInput{{Weekday: 4, StartTime: "09:00", EndTime: "10:00"}},
	}, utc, now)
	var tooOld *ValidationError
	require.ErrorAs(t, err, &tooOld)
	require.Equal(t, "schedule.valid_from", tooOld.Field)

	apia, err := time.LoadLocation("Pacific/Apia")
	require.NoError(t, err)
	_, err = validateWeeklySchedule(WeeklyScheduleInput{
		ValidFrom: "2011-12-30", ValidThrough: "2011-12-30",
		Slots: []WeeklyScheduleSlotInput{{Weekday: 5, StartTime: "09:00", EndTime: "10:00"}},
	}, apia, time.Date(2011, time.December, 29, 12, 0, 0, 0, time.UTC))
	var skippedDay *ValidationError
	require.ErrorAs(t, err, &skippedDay)
	require.Contains(t, skippedDay.Message, "outside the schedule date range")
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
	require.Equal(t, "090 123", *validated.phone)

	tooLong := strings.Repeat("1", maxPhoneRunes+1)
	_, err = validateStudentInput(CreateStudentInput{Name: "Mai", Phone: &tooLong})
	var validation *ValidationError
	require.ErrorAs(t, err, &validation)
	require.Equal(t, "phone", validation.Field)
}

// covers: AC-10
func TestCommandHashes_NormalizeExplicitInputWithoutTokenTimezone(t *testing.T) {
	t.Parallel()

	first := validClassInput()
	sameInput := validClassInput()
	sameInput.Name = "Maths 9A"

	firstHash, err := classRequestHash(first)
	require.NoError(t, err)
	sameHash, err := classRequestHash(sameInput)
	require.NoError(t, err)

	require.Equal(t, firstHash, sameHash)

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
