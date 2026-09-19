package handler

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	dateLayout                = time.DateOnly
	localTimeLayout           = "15:04"
	maxNameRunes              = 120
	maxPhoneRunes             = 40
	maxRateAmount             = int64(1_000_000_000)
	fnvOffsetBasis            = uint32(0x811c9dc5)
	fnvPrime                  = uint32(0x01000193)
	classColorCount           = uint32(7)
	offsetScanHours           = 48
	offsetSampleStep          = 30 * time.Minute
	maxRuleYears              = 2
	weeklyOccurrencesCapacity = 106
	daysPerWeek               = 7
)

const (
	classColorRed uint32 = iota
	classColorRose
	classColorOrange
	classColorGreen
	classColorBlue
	classColorYellow
)

type validatedClass struct {
	name       string
	color      *string
	rateAmount int64
	timezone   string
	first      *validatedOccurrence
	schedule   *validatedSchedule
}

type validatedOccurrence struct {
	localDate time.Time
	startTime string
	endTime   string
	startsAt  time.Time
	endsAt    time.Time
}

type validatedSchedule struct {
	validFrom    time.Time
	validThrough time.Time
	slot         WeeklyScheduleSlotInput
	occurrences  []validatedOccurrence
}

type validatedStudent struct {
	name  string
	phone *string
}

func validateName(field, raw string) (string, error) {
	name := strings.TrimSpace(raw)
	count := utf8.RuneCountInString(name)
	if count < 1 || count > maxNameRunes {
		return "", &ValidationError{Field: field, Message: "must contain 1 through 120 characters"}
	}
	return name, nil
}

func validateClassInput(input CreateClassInput, timezone string, now time.Time) (validatedClass, error) {
	name, err := validateName("name", input.Name)
	if err != nil {
		return validatedClass{}, err
	}
	if input.RateAmount < 0 || input.RateAmount > maxRateAmount {
		return validatedClass{}, &ValidationError{
			Field:   "rate_amount",
			Message: "must be an integer from 0 through 1000000000 dong",
		}
	}
	if input.Color != nil && !knownClassColor(*input.Color) {
		return validatedClass{}, &ValidationError{Field: "color", Message: "is not a known class color"}
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return validatedClass{}, fmt.Errorf("load request timezone: %w", err)
	}
	hasFirstSession := input.FirstSession.LocalDate != "" || input.FirstSession.StartTime != "" ||
		input.FirstSession.EndTime != ""
	if input.Schedule != nil && hasFirstSession {
		return validatedClass{}, &ValidationError{
			Field: "schedule", Message: "must be used instead of first_session, not beside it",
		}
	}
	if input.Schedule == nil && !hasFirstSession {
		return validatedClass{}, &ValidationError{
			Field: "first_session", Message: "or schedule is required",
		}
	}
	validated := validatedClass{
		name: name, color: input.Color, rateAmount: input.RateAmount, timezone: timezone,
	}
	if input.Schedule != nil {
		schedule, scheduleErr := validateWeeklySchedule(*input.Schedule, location, now)
		if scheduleErr != nil {
			return validatedClass{}, scheduleErr
		}
		validated.schedule = &schedule
		return validated, nil
	}
	first, err := validateOccurrence("first_session", input.FirstSession, location)
	if err != nil {
		return validatedClass{}, err
	}
	validated.first = &first
	return validated, nil
}

func validateOccurrence(
	prefix string,
	input FirstSessionInput,
	location *time.Location,
) (validatedOccurrence, error) {
	localDate, err := parseDate(prefix+".local_date", input.LocalDate)
	if err != nil {
		return validatedOccurrence{}, err
	}
	startsAt, err := resolveUniqueLocal(
		prefix+".start_time",
		localDate,
		input.StartTime,
		location,
	)
	if err != nil {
		return validatedOccurrence{}, err
	}
	endsAt, err := resolveUniqueLocal(
		prefix+".end_time",
		localDate,
		input.EndTime,
		location,
	)
	if err != nil {
		return validatedOccurrence{}, err
	}
	if !endsAt.After(startsAt) {
		return validatedOccurrence{}, &ValidationError{
			Field:   prefix + ".end_time",
			Message: "must be later than start_time on the same local date",
		}
	}
	return validatedOccurrence{
		localDate: localDate,
		startTime: input.StartTime,
		endTime:   input.EndTime,
		startsAt:  startsAt.UTC(),
		endsAt:    endsAt.UTC(),
	}, nil
}

func validateWeeklySchedule(
	input WeeklyScheduleInput,
	location *time.Location,
	now time.Time,
) (validatedSchedule, error) {
	validFrom, err := parseDate("schedule.valid_from", input.ValidFrom)
	if err != nil {
		return validatedSchedule{}, err
	}
	validThrough, err := parseDate("schedule.valid_through", input.ValidThrough)
	if err != nil {
		return validatedSchedule{}, err
	}
	today := localCalendarDate(now, location)
	if validFrom.Before(today.AddDate(0, 0, -30)) {
		return validatedSchedule{}, &ValidationError{
			Field: "schedule.valid_from", Message: "must be no more than 30 local dates before today",
		}
	}
	if validThrough.Before(validFrom) || validThrough.After(validFrom.AddDate(maxRuleYears, 0, 0)) {
		return validatedSchedule{}, &ValidationError{
			Field: "schedule.valid_through", Message: "must be on or after valid_from and no more than two years later",
		}
	}
	if len(input.Slots) != 1 {
		return validatedSchedule{}, &ValidationError{
			Field: "schedule.slots", Message: "must contain exactly one weekday in this tracer slice",
		}
	}
	slot := input.Slots[0]
	if slot.Weekday < 1 || slot.Weekday > 7 {
		return validatedSchedule{}, &ValidationError{
			Field: "schedule.slots[0].weekday", Message: "must be an ISO weekday from 1 through 7",
		}
	}
	occurrences := make([]validatedOccurrence, 0, weeklyOccurrencesCapacity)
	for day := validFrom; !day.After(validThrough); day = day.AddDate(0, 0, 1) {
		if int(day.Weekday()) != int(slot.Weekday%daysPerWeek) {
			continue
		}
		occurrence, occurrenceErr := validateOccurrence(
			"schedule.slots[0]",
			FirstSessionInput{
				LocalDate: day.Format(dateLayout), StartTime: slot.StartTime, EndTime: slot.EndTime,
			},
			location,
		)
		if occurrenceErr != nil {
			return validatedSchedule{}, occurrenceErr
		}
		occurrences = append(occurrences, occurrence)
	}
	if len(occurrences) == 0 {
		return validatedSchedule{}, &ValidationError{
			Field: "schedule", Message: "must produce at least one occurrence",
		}
	}
	return validatedSchedule{
		validFrom: validFrom, validThrough: validThrough, slot: slot, occurrences: occurrences,
	}, nil
}

func localCalendarDate(now time.Time, location *time.Location) time.Time {
	local := now.In(location)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
}

func validateStudentInput(input CreateStudentInput) (validatedStudent, error) {
	name, err := validateName("name", input.Name)
	if err != nil {
		return validatedStudent{}, err
	}
	phone := input.Phone
	if phone != nil && utf8.RuneCountInString(*phone) > maxPhoneRunes {
		return validatedStudent{}, &ValidationError{Field: "phone", Message: "must contain at most 40 characters"}
	}
	if phone != nil && *phone == "" {
		phone = nil
	}
	return validatedStudent{name: name, phone: phone}, nil
}

func validateIdempotencyKey(key string) error {
	length := utf8.RuneCountInString(key)
	if length < 1 || length > 128 {
		return &ValidationError{Field: "Idempotency-Key", Message: "must contain 1 through 128 characters"}
	}
	return nil
}

func knownClassColor(color string) bool {
	switch color {
	case "red", "rose", "orange", "green", "blue", "yellow", "violet":
		return true
	default:
		return false
	}
}

func suggestedClassColor(classID string) string {
	hash := fnvOffsetBasis
	for _, value := range []byte(classID) {
		hash ^= uint32(value)
		hash *= fnvPrime
	}
	switch hash % classColorCount {
	case classColorRed:
		return "red"
	case classColorRose:
		return "rose"
	case classColorOrange:
		return "orange"
	case classColorGreen:
		return "green"
	case classColorBlue:
		return "blue"
	case classColorYellow:
		return "yellow"
	default:
		return "violet"
	}
}

func parseDate(field, value string) (time.Time, error) {
	parsed, err := time.Parse(dateLayout, value)
	if err != nil || parsed.Format(dateLayout) != value {
		return time.Time{}, &ValidationError{Field: field, Message: "must use YYYY-MM-DD"}
	}
	return parsed, nil
}

func parseLocalTime(field, value string) (time.Time, error) {
	parsed, err := time.Parse(localTimeLayout, value)
	if err != nil || parsed.Format(localTimeLayout) != value {
		return time.Time{}, &ValidationError{Field: field, Message: "must use 24 hour HH:mm"}
	}
	return parsed, nil
}

func resolveUniqueLocal(field string, date time.Time, clock string, location *time.Location) (time.Time, error) {
	parsedClock, err := parseLocalTime(field, clock)
	if err != nil {
		return time.Time{}, err
	}
	candidates := localCandidates(
		date.Year(),
		date.Month(),
		date.Day(),
		parsedClock.Hour(),
		parsedClock.Minute(),
		location,
	)
	if len(candidates) == 0 {
		return time.Time{}, &ValidationError{Field: field, Message: "does not exist in the request timezone"}
	}
	if len(candidates) > 1 {
		return time.Time{}, &ValidationError{Field: field, Message: "is repeated in the request timezone"}
	}
	return candidates[0], nil
}

func localCandidates(
	year int,
	month time.Month,
	day int,
	hour int,
	minute int,
	location *time.Location,
) []time.Time {
	wall := time.Date(year, month, day, hour, minute, 0, 0, time.UTC)
	offsets := make(map[int]struct{})
	for sample := wall.Add(-offsetScanHours * time.Hour); !sample.After(wall.Add(offsetScanHours * time.Hour)); sample = sample.Add(offsetSampleStep) {
		_, offset := sample.In(location).Zone()
		offsets[offset] = struct{}{}
	}
	candidates := make([]time.Time, 0, len(offsets))
	seen := make(map[int64]struct{})
	for offset := range offsets {
		candidate := wall.Add(-time.Duration(offset) * time.Second)
		local := candidate.In(location)
		matches := local.Year() == year && local.Month() == month && local.Day() == day &&
			local.Hour() == hour && local.Minute() == minute
		if !matches {
			continue
		}
		if _, exists := seen[candidate.Unix()]; exists {
			continue
		}
		seen[candidate.Unix()] = struct{}{}
		candidates = append(candidates, candidate)
	}
	slices.SortFunc(candidates, func(left, right time.Time) int { return left.Compare(right) })
	return candidates
}

func classRequestHash(value validatedClass) ([sha256.Size]byte, error) {
	type canonicalOccurrence struct {
		LocalDate string `json:"local_date"`
		StartTime string `json:"start_time"`
		EndTime   string `json:"end_time"`
	}
	type canonicalSchedule struct {
		ValidFrom    string                    `json:"valid_from"`
		ValidThrough string                    `json:"valid_through"`
		Slots        []WeeklyScheduleSlotInput `json:"slots"`
	}
	canonical := struct {
		Name         string               `json:"name"`
		Color        *string              `json:"color"`
		RateAmount   int64                `json:"rate_amount"`
		FirstSession *canonicalOccurrence `json:"first_session"`
		Schedule     *canonicalSchedule   `json:"schedule"`
		Timezone     string               `json:"timezone"`
	}{
		Name: value.name, Color: value.color, RateAmount: value.rateAmount, Timezone: value.timezone,
	}
	if value.first != nil {
		canonical.FirstSession = &canonicalOccurrence{
			LocalDate: value.first.localDate.Format(dateLayout),
			StartTime: value.first.startTime,
			EndTime:   value.first.endTime,
		}
	}
	if value.schedule != nil {
		canonical.Schedule = &canonicalSchedule{
			ValidFrom:    value.schedule.validFrom.Format(dateLayout),
			ValidThrough: value.schedule.validThrough.Format(dateLayout),
			Slots:        []WeeklyScheduleSlotInput{value.schedule.slot},
		}
	}
	//nolint:errchkjson // This fixed struct contains only JSON primitive values, but the checked error keeps the boundary explicit.
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return [sha256.Size]byte{}, fmt.Errorf("encode class command hash: %w", err)
	}
	return sha256.Sum256(encoded), nil
}

func studentRequestHash(value validatedStudent) ([sha256.Size]byte, error) {
	canonical := struct {
		Name  string  `json:"name"`
		Phone *string `json:"phone"`
	}{Name: value.name, Phone: value.phone}
	//nolint:errchkjson // This fixed struct contains only JSON primitive values, but the checked error keeps the boundary explicit.
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return [sha256.Size]byte{}, fmt.Errorf("encode student command hash: %w", err)
	}
	return sha256.Sum256(encoded), nil
}
