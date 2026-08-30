package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nguyen-duc-loc/vermouth/services/teaching/internal/store"
	"github.com/nguyen-duc-loc/vermouth/services/teaching/internal/store/sqlcgen"
)

const (
	homePageSize       = 50
	homePageProofSize  = homePageSize + 1
	halfHourMinutes    = 30
	defaultSpanMinutes = 60
	minutesPerHour     = 60
	minutesPerDay      = 24 * minutesPerHour
	cursorField        = "cursor"
	cursorMalformed    = "is malformed"
)

var errNoInstantForLocalDate = errors.New("no instant exists for local date")

type homeCursor struct {
	LocalDate string    `json:"local_date"`
	StartsAt  time.Time `json:"starts_at"`
	SessionID uuid.UUID `json:"session_id"`
}

// ReadHome returns teaching truth for the verified request timezone. A cursor
// is accepted only for the current local date.
func (h *Handler) ReadHome(
	ctx context.Context,
	tutorID uuid.UUID,
	timezone string,
	rawCursor string,
) (Home, error) {
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return Home{}, fmt.Errorf("load request timezone: %w", err)
	}
	now := h.now().UTC()
	localNow := now.In(location)
	localDate := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, time.UTC)
	localDateText := localDate.Format(dateLayout)
	nextMidnight, err := firstInstantOfDate(localDate.AddDate(0, 0, 1), location)
	if err != nil {
		return Home{}, err
	}
	defaults, err := buildSetupDefaults(localNow, location)
	if err != nil {
		return Home{}, err
	}

	cursor, hasCursor, err := decodeHomeCursor(rawCursor, localDateText)
	if err != nil {
		return Home{}, err
	}
	rows, err := store.Queries(h.pool).ListHomeSessions(ctx, sqlcgen.ListHomeSessionsParams{
		TutorID:         tutorID,
		LocalDate:       pgtype.Date{Time: localDate, Valid: true},
		HasCursor:       hasCursor,
		CursorStartsAt:  cursor.StartsAt,
		CursorSessionID: cursor.SessionID,
		PageSize:        homePageProofSize,
	})
	if err != nil {
		return Home{}, fmt.Errorf("list home sessions: %w", err)
	}

	var nextCursor *string
	if len(rows) > homePageSize {
		rows = rows[:homePageSize]
		encoded, encodeErr := encodeHomeCursor(rows[len(rows)-1])
		if encodeErr != nil {
			return Home{}, encodeErr
		}
		nextCursor = &encoded
	}
	sessions, err := h.homeSessions(ctx, tutorID, rows)
	if err != nil {
		return Home{}, err
	}
	return Home{
		RequestTimeZone:     timezone,
		LocalDate:           localDateText,
		NextLocalMidnightAt: nextMidnight,
		SetupDefaults:       defaults,
		Sessions:            sessions,
		NextCursor:          nextCursor,
	}, nil
}

func (h *Handler) homeSessions(
	ctx context.Context,
	tutorID uuid.UUID,
	rows []sqlcgen.ListHomeSessionsRow,
) ([]HomeSession, error) {
	sessions := make([]HomeSession, 0, len(rows))
	if len(rows) == 0 {
		return sessions, nil
	}
	ids := make([]uuid.UUID, 0, len(rows))
	byID := make(map[uuid.UUID]int, len(rows))
	for index := range rows {
		row := &rows[index]
		byID[row.SessionID] = len(sessions)
		ids = append(ids, row.SessionID)
		sessions = append(sessions, HomeSession{
			SessionID:  row.SessionID,
			ClassID:    row.ClassID,
			ClassName:  row.ClassName,
			ClassColor: row.ClassColor,
			StartsAt:   row.StartsAt,
			EndsAt:     row.EndsAt,
			LocalDate:  row.LocalDate.Time.Format(dateLayout),
			Students:   []HomeStudent{},
		})
	}
	studentRows, err := store.Queries(h.pool).ListHomeStudents(ctx, sqlcgen.ListHomeStudentsParams{
		TutorID:    tutorID,
		SessionIds: ids,
	})
	if err != nil {
		return nil, fmt.Errorf("list home roster students: %w", err)
	}
	for _, row := range studentRows {
		index, exists := byID[row.SessionID]
		if !exists {
			continue
		}
		var state *string
		if row.AttendanceState.Valid {
			value := row.AttendanceState.String
			state = &value
		}
		var markedAt *time.Time
		if row.MarkedAt.Valid {
			value := row.MarkedAt.Time
			markedAt = &value
		}
		sessions[index].Students = append(sessions[index].Students, HomeStudent{
			StudentID:       row.StudentID,
			Name:            row.Name,
			AttendanceState: state,
			MarkedAt:        markedAt,
		})
	}
	return sessions, nil
}

func decodeHomeCursor(raw, currentDate string) (homeCursor, bool, error) {
	if raw == "" {
		return homeCursor{}, false, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return homeCursor{}, false, &ValidationError{Field: cursorField, Message: cursorMalformed}
	}
	var cursor homeCursor
	err = json.Unmarshal(decoded, &cursor)
	if err != nil {
		return homeCursor{}, false, &ValidationError{Field: cursorField, Message: cursorMalformed}
	}
	valid := cursor.LocalDate == currentDate && !cursor.StartsAt.IsZero() && cursor.SessionID != uuid.Nil
	if !valid {
		return homeCursor{}, false, &ValidationError{
			Field:   cursorField,
			Message: "does not belong to the current local date",
		}
	}
	return cursor, true, nil
}

func encodeHomeCursor(row sqlcgen.ListHomeSessionsRow) (string, error) {
	encoded, err := json.Marshal(homeCursor{
		LocalDate: row.LocalDate.Time.Format(dateLayout),
		StartsAt:  row.StartsAt,
		SessionID: row.SessionID,
	})
	if err != nil {
		return "", fmt.Errorf("encode home cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

func buildSetupDefaults(localNow time.Time, location *time.Location) (SetupDefaults, error) {
	date := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, time.UTC)
	minute := localNow.Hour()*minutesPerHour + localNow.Minute()
	startMinute := ((minute / halfHourMinutes) + 1) * halfHourMinutes
	if defaults, ok := findDefaultSlot(date, startMinute, location); ok {
		return defaults, nil
	}
	nextDate := date.AddDate(0, 0, 1)
	if defaults, ok := findDefaultSlot(nextDate, 0, location); ok {
		return defaults, nil
	}
	return SetupDefaults{}, errorsForLocalDate(nextDate)
}

func findDefaultSlot(date time.Time, firstMinute int, location *time.Location) (SetupDefaults, bool) {
	for startMinute := firstMinute; startMinute+defaultSpanMinutes < minutesPerDay; startMinute += halfHourMinutes {
		endMinute := startMinute + defaultSpanMinutes
		startCandidates := localCandidates(
			date.Year(), date.Month(), date.Day(), startMinute/minutesPerHour, startMinute%minutesPerHour, location,
		)
		endCandidates := localCandidates(
			date.Year(), date.Month(), date.Day(), endMinute/minutesPerHour, endMinute%minutesPerHour, location,
		)
		if len(startCandidates) != 1 || len(endCandidates) != 1 ||
			!endCandidates[0].After(startCandidates[0]) {
			continue
		}
		return SetupDefaults{
			LocalDate: date.Format(dateLayout),
			StartTime: fmt.Sprintf("%02d:%02d", startMinute/minutesPerHour, startMinute%minutesPerHour),
			EndTime:   fmt.Sprintf("%02d:%02d", endMinute/minutesPerHour, endMinute%minutesPerHour),
		}, true
	}
	return SetupDefaults{}, false
}

func firstInstantOfDate(date time.Time, location *time.Location) (time.Time, error) {
	for minute := range minutesPerDay {
		candidates := localCandidates(
			date.Year(), date.Month(), date.Day(), minute/minutesPerHour, minute%minutesPerHour, location,
		)
		if len(candidates) > 0 {
			return candidates[0].UTC(), nil
		}
	}
	return time.Time{}, errorsForLocalDate(date)
}

func errorsForLocalDate(date time.Time) error {
	return fmt.Errorf("%w: %s", errNoInstantForLocalDate, date.Format(dateLayout))
}
