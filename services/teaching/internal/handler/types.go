package handler

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ErrNotFound hides whether an identifier was missing or belonged to another tutor.
var ErrNotFound = errors.New("owned teaching resource not found")

// ErrConflict reports a request that cannot coexist with committed teaching state.
var ErrConflict = errors.New("teaching state conflict")

// ErrIdempotencyConflict reports a command key reused for different validated input.
var ErrIdempotencyConflict = errors.New("idempotency key names different input")

// ValidationError identifies one caller controlled field that failed its
// confirmed contract.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// CreateClassInput is the validated source for a class and its first session.
type CreateClassInput struct {
	Name         string            `json:"name"`
	Color        *string           `json:"color"`
	RateAmount   int64             `json:"rate_amount"`
	FirstSession FirstSessionInput `json:"first_session"`
}

// FirstSessionInput carries local clock values that resolve through the token
// timezone before any write starts.
type FirstSessionInput struct {
	LocalDate string `json:"local_date"`
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time"`
}

// Class is teaching's boundary view of its class truth.
type Class struct {
	ClassID           uuid.UUID `json:"class_id"`
	Name              string    `json:"name"`
	Color             string    `json:"color"`
	RateAmount        int64     `json:"rate_amount"`
	Currency          string    `json:"currency"`
	RateEffectiveFrom string    `json:"rate_effective_from"`
}

// Session is teaching's boundary view of one concrete session.
type Session struct {
	SessionID uuid.UUID `json:"session_id"`
	ClassID   uuid.UUID `json:"class_id"`
	StartsAt  time.Time `json:"starts_at"`
	EndsAt    time.Time `json:"ends_at"`
	LocalDate string    `json:"local_date"`
}

// CreateClassResult keeps the first session beside the aggregate that created
// it, including on an idempotent replay.
type CreateClassResult struct {
	Class        Class   `json:"class"`
	FirstSession Session `json:"first_session"`
}

// CreateStudentInput contains the only student contact data this slice stores.
type CreateStudentInput struct {
	Name  string  `json:"name"`
	Phone *string `json:"phone"`
}

// Student is returned only by the immediate create command. Phone never enters
// an event or the home response.
type Student struct {
	StudentID uuid.UUID `json:"student_id"`
	Name      string    `json:"name"`
	Phone     *string   `json:"phone"`
}

// JoinRosterInput opens membership on an inclusive local date.
type JoinRosterInput struct {
	StudentID     uuid.UUID `json:"student_id"`
	EffectiveFrom string    `json:"effective_from"`
}

// RosterPeriod is the canonical open membership returned by a join.
type RosterPeriod struct {
	ClassID       uuid.UUID `json:"class_id"`
	StudentID     uuid.UUID `json:"student_id"`
	EffectiveFrom string    `json:"effective_from"`
	EffectiveTo   *string   `json:"effective_to"`
}

// MarkAttendanceInput is one of the two catalogue states.
type MarkAttendanceInput struct {
	State string `json:"state"`
}

// Attendance is the canonical saved state used after every write and reload.
type Attendance struct {
	SessionID uuid.UUID `json:"session_id"`
	StudentID uuid.UUID `json:"student_id"`
	State     string    `json:"state"`
	MarkedAt  time.Time `json:"marked_at"`
}

// SetupDefaults starts the guided sheet from the tutor's verified local clock.
type SetupDefaults struct {
	LocalDate string `json:"local_date"`
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time"`
}

// HomeStudent is a covered roster member with nullable attendance truth.
type HomeStudent struct {
	StudentID       uuid.UUID  `json:"student_id"`
	Name            string     `json:"name"`
	AttendanceState *string    `json:"attendance_state"`
	MarkedAt        *time.Time `json:"marked_at"`
}

// HomeSession is one active session and its covered students.
type HomeSession struct {
	SessionID  uuid.UUID     `json:"session_id"`
	ClassID    uuid.UUID     `json:"class_id"`
	ClassName  string        `json:"class_name"`
	ClassColor string        `json:"class_color"`
	StartsAt   time.Time     `json:"starts_at"`
	EndsAt     time.Time     `json:"ends_at"`
	LocalDate  string        `json:"local_date"`
	Students   []HomeStudent `json:"students"`
}

// Home is teaching's authoritative contribution to the aggregated screen.
type Home struct {
	RequestTimeZone     string        `json:"request_time_zone"`
	LocalDate           string        `json:"local_date"`
	NextLocalMidnightAt time.Time     `json:"next_local_midnight_at"`
	SetupDefaults       SetupDefaults `json:"setup_defaults"`
	Sessions            []HomeSession `json:"sessions"`
	NextCursor          *string       `json:"next_cursor"`
}
