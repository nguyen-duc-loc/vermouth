package handler

import (
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nguyen-duc-loc/vermouth/services/teaching/internal/store"
	"github.com/nguyen-duc-loc/vermouth/services/teaching/internal/store/sqlcgen"
)

const (
	maxScheduleWindowDays = 42
	hoursPerDay           = 24
	secondsPerHour        = 60 * 60
	secondsPerMinute      = 60
	historyCursorField    = "history_cursor"
	invalidHistoryCursor  = "is invalid for this schedule query"
	stateReplaced         = "replaced"
)

// ReadSchedule returns one bounded calendar window in the verified token zone.
//
//nolint:funlen,gocognit,nestif,revive // The bounded read keeps validation and its owned query sets in one visible flow.
func (h *Handler) ReadSchedule(
	ctx context.Context,
	tutorID uuid.UUID,
	timezone string,
	fromValue string,
	throughValue string,
	classIDs []uuid.UUID,
	includeReplaced bool,
	historyLimit int,
	historyCursor string,
) (Schedule, error) {
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return Schedule{}, fmt.Errorf("load schedule display timezone: %w", err)
	}
	fromDate, err := parseDate("from", fromValue)
	if err != nil {
		return Schedule{}, err
	}
	throughDate, err := parseDate("through", throughValue)
	if err != nil {
		return Schedule{}, err
	}
	days := int(throughDate.Sub(fromDate).Hours()/hoursPerDay) + 1
	if days < 1 || days > maxScheduleWindowDays {
		return Schedule{}, &ValidationError{
			Field: "through", Message: "must end on or after from within 42 inclusive dates",
		}
	}
	fromInstant := localMidnight(fromDate, location)
	throughInstant := localMidnight(throughDate.AddDate(0, 0, 1), location)
	if historyLimit == 0 {
		historyLimit = 50
	}
	if historyLimit < 1 || historyLimit > 50 {
		return Schedule{}, &ValidationError{Field: "history_limit", Message: "must be from 1 through 50"}
	}
	slices.SortFunc(classIDs, func(left, right uuid.UUID) int {
		return cmp.Compare(left.String(), right.String())
	})
	cursor, err := decodeScheduleHistoryCursor(
		historyCursor, tutorID, fromValue, throughValue, classIDs, historyLimit,
	)
	if err != nil {
		return Schedule{}, err
	}
	queries := store.Queries(h.pool)

	classRows, err := queries.ListScheduleClasses(ctx, tutorID)
	if err != nil {
		return Schedule{}, fmt.Errorf("list schedule classes: %w", err)
	}
	ruleRows, err := queries.ListScheduleRules(ctx, sqlcgen.ListScheduleRulesParams{
		TutorID:        tutorID,
		ThroughDate:    pgtype.Date{Time: throughDate, Valid: true},
		FromDate:       pgtype.Date{Time: fromDate, Valid: true},
		HasClassFilter: len(classIDs) > 0,
		ClassIds:       classIDs,
	})
	if err != nil {
		return Schedule{}, fmt.Errorf("list schedule rules: %w", err)
	}
	ruleIDs := make([]uuid.UUID, 0, len(ruleRows))
	for index := range ruleRows {
		ruleIDs = append(ruleIDs, ruleRows[index].ScheduleRuleID)
	}
	slotsByRule := make(map[uuid.UUID][]sqlcgen.ListScheduleSlotsRow, len(ruleIDs))
	if len(ruleIDs) > 0 {
		slotRows, slotErr := queries.ListScheduleSlots(ctx, sqlcgen.ListScheduleSlotsParams{
			TutorID: tutorID, ScheduleRuleIds: ruleIDs,
		})
		if slotErr != nil {
			return Schedule{}, fmt.Errorf("list schedule slots: %w", slotErr)
		}
		for _, row := range slotRows {
			slotsByRule[row.ScheduleRuleID] = append(slotsByRule[row.ScheduleRuleID], row)
		}
	}
	sessionRows, err := queries.ListScheduleSessions(ctx, sqlcgen.ListScheduleSessionsParams{
		TutorID:        tutorID,
		FromInstant:    fromInstant,
		ThroughInstant: throughInstant,
		HasClassFilter: len(classIDs) > 0,
		ClassIds:       classIDs,
	})
	if err != nil {
		return Schedule{}, fmt.Errorf("list schedule sessions: %w", err)
	}

	result := Schedule{
		TutorID:         tutorID,
		RequestTimeZone: timezone,
		From:            fromDate.Format(dateLayout),
		Through:         throughDate.Format(dateLayout),
		Classes:         make([]ScheduleClass, 0, len(classRows)),
		Rules:           make([]ScheduleRuleSummary, 0, len(ruleRows)),
		Sessions:        make([]ScheduleSession, 0, len(sessionRows)),
		ReplacedHistory: make([]ScheduleSession, 0),
	}
	for _, row := range classRows {
		result.Classes = append(result.Classes, ScheduleClass{
			ClassID: row.ClassID, Name: row.Name, Color: row.Color,
			ScheduleRevision: row.ScheduleRevision,
		})
	}
	requestTime := h.now().UTC()
	for index := range ruleRows {
		row := &ruleRows[index]
		result.Rules = append(
			result.Rules,
			scheduleRuleSummaryFromRead(*row, slotsByRule[row.ScheduleRuleID], requestTime),
		)
	}
	for index := range sessionRows {
		result.Sessions = append(result.Sessions, scheduleSessionFromRow(sessionRows[index], location))
	}
	if includeReplaced {
		historyRows, historyErr := queries.ListReplacedScheduleSessions(
			ctx, sqlcgen.ListReplacedScheduleSessionsParams{
				TutorID:        tutorID,
				FromDate:       pgtype.Date{Time: fromDate, Valid: true},
				ThroughDate:    pgtype.Date{Time: throughDate, Valid: true},
				HasClassFilter: len(classIDs) > 0, ClassIds: classIDs,
				HasCursor:        cursor != nil,
				CursorOriginDate: pgtype.Date{Time: cursorOriginDate(cursor), Valid: cursor != nil},
				CursorSessionID:  cursorSessionID(cursor), PageSize: int32(historyLimit + 1),
			},
		)
		if historyErr != nil {
			return Schedule{}, fmt.Errorf("list replaced schedule sessions: %w", historyErr)
		}
		hasNext := len(historyRows) > historyLimit
		if hasNext {
			historyRows = historyRows[:historyLimit]
		}
		for index := range historyRows {
			row := &historyRows[index]
			result.ReplacedHistory = append(result.ReplacedHistory, scheduleSessionFromValues(
				row.SessionID, row.ClassID, row.ClassName, row.ClassColor, row.ArchivedAt,
				row.StartsAt, row.EndsAt, row.LocalDate, row.OriginLocalDate,
				row.ScheduleRuleID, row.SourceTimeZone, row.Version, row.MovedAt,
				row.CancelledAt, row.SupersededAt, row.UpdatedAt, location,
			))
		}
		if hasNext && len(historyRows) > 0 {
			last := historyRows[len(historyRows)-1]
			encoded, encodeErr := encodeScheduleHistoryCursor(scheduleHistoryCursor{
				TutorID: tutorID, From: fromValue, Through: throughValue,
				ClassIDs: classIDs, Limit: historyLimit, OriginDate: last.OriginLocalDate.Time.Format(dateLayout),
				SessionID: last.SessionID, Order: 1,
			})
			if encodeErr != nil {
				return Schedule{}, encodeErr
			}
			result.NextHistoryCursor = &encoded
		}
	}
	return result, nil
}

func localMidnight(date time.Time, location *time.Location) time.Time {
	return time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, location).UTC()
}

func scheduleRuleSummaryFromRead(
	row sqlcgen.ListScheduleRulesRow,
	slots []sqlcgen.ListScheduleSlotsRow,
	requestTime time.Time,
) ScheduleRuleSummary {
	result := ScheduleRuleSummary{
		ScheduleRuleID: row.ScheduleRuleID,
		ClassID:        row.ClassID,
		Revision:       row.Revision,
		ValidFrom:      row.ValidFrom.Time.Format(dateLayout),
		ValidThrough:   row.ValidThrough.Time.Format(dateLayout),
		TimeZone:       row.TimeZone,
		State: ruleState(
			row.ValidFrom.Time,
			row.ValidThrough.Time,
			row.TimeZone,
			timestampPointer(row.ReplacedAt),
			timestampPointer(row.EndedAt),
			timestampPointer(row.RetiredAt),
			requestTime,
		),
		Slots:      make([]WeeklyScheduleSlotInput, 0, len(slots)),
		ReplacedAt: timestampPointer(row.ReplacedAt),
		EndedAt:    timestampPointer(row.EndedAt),
		RetiredAt:  timestampPointer(row.RetiredAt),
	}
	for _, slot := range slots {
		result.Slots = append(result.Slots, WeeklyScheduleSlotInput{
			Weekday: slot.Weekday, StartTime: slot.StartTime, EndTime: slot.EndTime,
		})
	}
	return result
}

func scheduleSessionFromRow(
	row sqlcgen.ListScheduleSessionsRow,
	location *time.Location,
) ScheduleSession {
	return scheduleSessionFromValues(
		row.SessionID, row.ClassID, row.ClassName, row.ClassColor, row.ArchivedAt,
		row.StartsAt, row.EndsAt, row.LocalDate, row.OriginLocalDate,
		row.ScheduleRuleID, row.SourceTimeZone, row.Version, row.MovedAt,
		row.CancelledAt, row.SupersededAt, row.UpdatedAt, location,
	)
}

func scheduleSessionFromValues(
	sessionID uuid.UUID,
	classID uuid.UUID,
	className string,
	classColor string,
	archivedAt pgtype.Timestamptz,
	startsAt time.Time,
	endsAt time.Time,
	localDate pgtype.Date,
	originLocalDate pgtype.Date,
	ruleValue pgtype.UUID,
	sourceTimeZone pgtype.Text,
	version int64,
	movedAt pgtype.Timestamptz,
	cancelledAt pgtype.Timestamptz,
	supersededAt pgtype.Timestamptz,
	updatedAt time.Time,
	location *time.Location,
) ScheduleSession {
	displayStart := startsAt.In(location)
	displayEnd := endsAt.In(location)
	state := sessionState(cancelledAt, supersededAt)
	var ruleID *uuid.UUID
	if ruleValue.Valid {
		value := uuid.UUID(ruleValue.Bytes)
		ruleID = &value
	}
	var sourceZone *string
	if sourceTimeZone.Valid {
		value := sourceTimeZone.String
		sourceZone = &value
	}
	return ScheduleSession{
		SessionID:       sessionID,
		ClassID:         classID,
		ClassName:       className,
		ClassColor:      classColor,
		ClassArchived:   archivedAt.Valid,
		StartsAt:        startsAt,
		EndsAt:          endsAt,
		DisplayDate:     displayStart.Format(dateLayout),
		DisplayStart:    displayStart.Format(localTimeLayout),
		DisplayEnd:      displayEnd.Format(localTimeLayout),
		StartUTCOffset:  numericOffset(displayStart),
		EndUTCOffset:    numericOffset(displayEnd),
		LocalDate:       localDate.Time.Format(dateLayout),
		OriginLocalDate: originLocalDate.Time.Format(dateLayout),
		ScheduleRuleID:  ruleID,
		SourceTimeZone:  sourceZone,
		Version:         version,
		State:           state,
		MovedAt:         timestampPointer(movedAt),
		CancelledAt:     timestampPointer(cancelledAt),
		SupersededAt:    timestampPointer(supersededAt),
		UpdatedAt:       updatedAt,
	}
}

type scheduleHistoryCursor struct {
	TutorID    uuid.UUID   `json:"tutor_id"`
	From       string      `json:"from"`
	Through    string      `json:"through"`
	ClassIDs   []uuid.UUID `json:"class_ids"`
	Limit      int         `json:"limit"`
	OriginDate string      `json:"origin_date"`
	SessionID  uuid.UUID   `json:"session_id"`
	Order      int         `json:"order"`
}

func encodeScheduleHistoryCursor(cursor scheduleHistoryCursor) (string, error) {
	value, err := json.Marshal(cursor)
	if err != nil {
		return "", fmt.Errorf("encode schedule history cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

//nolint:nilnil // An absent cursor is a valid optional input and is distinct from an invalid cursor.
func decodeScheduleHistoryCursor(
	value string,
	tutorID uuid.UUID,
	from string,
	through string,
	classIDs []uuid.UUID,
	limit int,
) (*scheduleHistoryCursor, error) {
	if value == "" {
		return nil, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, &ValidationError{Field: historyCursorField, Message: invalidHistoryCursor}
	}
	var cursor scheduleHistoryCursor
	err = json.Unmarshal(decoded, &cursor)
	if err != nil || cursor.TutorID != tutorID ||
		cursor.From != from || cursor.Through != through || cursor.Limit != limit || cursor.Order != 1 ||
		!slices.Equal(cursor.ClassIDs, classIDs) {
		return nil, &ValidationError{Field: historyCursorField, Message: invalidHistoryCursor}
	}
	_, err = parseDate(historyCursorField, cursor.OriginDate)
	if err != nil || cursor.SessionID == uuid.Nil {
		return nil, &ValidationError{Field: historyCursorField, Message: invalidHistoryCursor}
	}
	return &cursor, nil
}

func cursorOriginDate(cursor *scheduleHistoryCursor) time.Time {
	if cursor == nil {
		return time.Time{}
	}
	value, _ := time.Parse(dateLayout, cursor.OriginDate)
	return value
}

func cursorSessionID(cursor *scheduleHistoryCursor) uuid.UUID {
	if cursor == nil {
		return uuid.Nil
	}
	return cursor.SessionID
}

func numericOffset(value time.Time) string {
	_, seconds := value.Zone()
	sign := "+"
	if seconds < 0 {
		sign = "-"
		seconds = -seconds
	}
	return fmt.Sprintf(
		"%s%02d:%02d",
		sign,
		seconds/secondsPerHour,
		seconds%secondsPerHour/secondsPerMinute,
	)
}

func ruleState(
	validFrom time.Time,
	validThrough time.Time,
	timezone string,
	replacedAt *time.Time,
	endedAt *time.Time,
	retiredAt *time.Time,
	requestTime time.Time,
) string {
	if retiredAt != nil {
		return "retired"
	}
	if endedAt != nil {
		return "ended"
	}
	if replacedAt != nil {
		return stateReplaced
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return "invalid"
	}
	today := localCalendarDate(requestTime, location)
	if today.Before(validFrom) {
		return "planned"
	}
	if today.After(validThrough) {
		return "completed"
	}
	return "active"
}

func timestampPointer(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time
	return &result
}
