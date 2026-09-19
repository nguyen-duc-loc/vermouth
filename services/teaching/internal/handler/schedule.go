package handler

import (
	"context"
	"fmt"
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
)

// ReadSchedule returns one bounded calendar window in the verified token zone.
//
//nolint:funlen // The bounded read keeps validation and its four owned query sets in one visible flow.
func (h *Handler) ReadSchedule(
	ctx context.Context,
	tutorID uuid.UUID,
	timezone string,
	fromValue string,
	throughValue string,
	classIDs []uuid.UUID,
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
	displayStart := row.StartsAt.In(location)
	displayEnd := row.EndsAt.In(location)
	state := "active"
	if row.CancelledAt.Valid {
		state = "cancelled"
	}
	var ruleID *uuid.UUID
	if row.ScheduleRuleID.Valid {
		value := uuid.UUID(row.ScheduleRuleID.Bytes)
		ruleID = &value
	}
	var sourceZone *string
	if row.SourceTimeZone.Valid {
		value := row.SourceTimeZone.String
		sourceZone = &value
	}
	return ScheduleSession{
		SessionID:       row.SessionID,
		ClassID:         row.ClassID,
		ClassName:       row.ClassName,
		ClassColor:      row.ClassColor,
		ClassArchived:   row.ArchivedAt.Valid,
		StartsAt:        row.StartsAt,
		EndsAt:          row.EndsAt,
		DisplayDate:     displayStart.Format(dateLayout),
		DisplayStart:    displayStart.Format(localTimeLayout),
		DisplayEnd:      displayEnd.Format(localTimeLayout),
		StartUTCOffset:  numericOffset(displayStart),
		EndUTCOffset:    numericOffset(displayEnd),
		LocalDate:       row.LocalDate.Time.Format(dateLayout),
		OriginLocalDate: row.OriginLocalDate.Time.Format(dateLayout),
		ScheduleRuleID:  ruleID,
		SourceTimeZone:  sourceZone,
		Version:         row.Version,
		State:           state,
		MovedAt:         timestampPointer(row.MovedAt),
		CancelledAt:     timestampPointer(row.CancelledAt),
		SupersededAt:    timestampPointer(row.SupersededAt),
		UpdatedAt:       row.UpdatedAt,
	}
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
		return "replaced"
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
