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

// ConflictError carries one stable conflict code and only owned recovery data.
type ConflictError struct {
	Code    string
	Message string
	Details any
}

func (e *ConflictError) Error() string { return e.Message }

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
	Name         string               `json:"name"`
	Color        *string              `json:"color"`
	RateAmount   int64                `json:"rate_amount"`
	FirstSession FirstSessionInput    `json:"first_session"`
	Schedule     *WeeklyScheduleInput `json:"schedule"`
}

// FirstSessionInput carries local clock values that resolve through the token
// timezone before any write starts.
type FirstSessionInput struct {
	LocalDate string `json:"local_date"`
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time"`
}

// WeeklyScheduleInput is one bounded weekly rule captured in the verified
// tutor timezone.
type WeeklyScheduleInput struct {
	ValidFrom    string                    `json:"valid_from"`
	ValidThrough string                    `json:"valid_through"`
	Slots        []WeeklyScheduleSlotInput `json:"slots"`
}

// WeeklyScheduleSlotInput gives one ISO weekday its own local clock range.
type WeeklyScheduleSlotInput struct {
	Weekday   int16  `json:"weekday"`
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
	ScheduleRevision  int64     `json:"schedule_revision"`
}

// Session is teaching's boundary view of one concrete session.
type Session struct {
	SessionID uuid.UUID `json:"session_id"`
	ClassID   uuid.UUID `json:"class_id"`
	StartsAt  time.Time `json:"starts_at"`
	EndsAt    time.Time `json:"ends_at"`
	LocalDate string    `json:"local_date"`
}

// CanonicalSession is one owned session after a mutation, including its
// optimistic version and rule origin.
type CanonicalSession struct {
	SessionID       uuid.UUID  `json:"session_id"`
	ClassID         uuid.UUID  `json:"class_id"`
	StartsAt        time.Time  `json:"starts_at"`
	EndsAt          time.Time  `json:"ends_at"`
	LocalDate       string     `json:"local_date"`
	OriginLocalDate string     `json:"origin_local_date"`
	ScheduleRuleID  *uuid.UUID `json:"schedule_rule_id"`
	SourceTimeZone  *string    `json:"source_time_zone"`
	DisplayTimeZone string     `json:"display_time_zone"`
	StartUTCOffset  string     `json:"start_utc_offset"`
	EndUTCOffset    string     `json:"end_utc_offset"`
	Version         int64      `json:"version"`
	State           string     `json:"state"`
	MovedAt         *time.Time `json:"moved_at"`
	CancelledAt     *time.Time `json:"cancelled_at"`
	SupersededAt    *time.Time `json:"superseded_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// CreateClassResult keeps the first session beside the aggregate that created
// it, including on an idempotent replay.
type CreateClassResult struct {
	Class          Class                `json:"class"`
	FirstSession   *Session             `json:"first_session"`
	Rule           *ScheduleRuleSummary `json:"rule"`
	CandidateCount int                  `json:"candidate_count"`
	CreatedCount   int                  `json:"created_count"`
	AdoptedCount   int                  `json:"adopted_count"`
}

// ScheduleRuleSummary is the canonical rule shape returned after a write.
type ScheduleRuleSummary struct {
	ScheduleRuleID uuid.UUID                 `json:"schedule_rule_id"`
	ClassID        uuid.UUID                 `json:"class_id"`
	Revision       int64                     `json:"revision"`
	ValidFrom      string                    `json:"valid_from"`
	ValidThrough   string                    `json:"valid_through"`
	TimeZone       string                    `json:"time_zone"`
	State          string                    `json:"state"`
	Slots          []WeeklyScheduleSlotInput `json:"slots"`
	ReplacedAt     *time.Time                `json:"replaced_at"`
	EndedAt        *time.Time                `json:"ended_at"`
	RetiredAt      *time.Time                `json:"retired_at"`
}

// ScheduleClass is one active class available to the calendar filter.
type ScheduleClass struct {
	ClassID          uuid.UUID `json:"class_id"`
	Name             string    `json:"name"`
	Color            string    `json:"color"`
	ScheduleRevision int64     `json:"schedule_revision"`
}

// ScheduleSession is one concrete calendar fact formatted in the request zone.
type ScheduleSession struct {
	SessionID       uuid.UUID  `json:"session_id"`
	ClassID         uuid.UUID  `json:"class_id"`
	ClassName       string     `json:"class_name"`
	ClassColor      string     `json:"class_color"`
	ClassArchived   bool       `json:"class_archived"`
	StartsAt        time.Time  `json:"starts_at"`
	EndsAt          time.Time  `json:"ends_at"`
	DisplayDate     string     `json:"display_date"`
	DisplayStart    string     `json:"display_start"`
	DisplayEnd      string     `json:"display_end"`
	StartUTCOffset  string     `json:"start_utc_offset"`
	EndUTCOffset    string     `json:"end_utc_offset"`
	LocalDate       string     `json:"local_date"`
	OriginLocalDate string     `json:"origin_local_date"`
	ScheduleRuleID  *uuid.UUID `json:"schedule_rule_id"`
	SourceTimeZone  *string    `json:"source_time_zone"`
	Version         int64      `json:"version"`
	State           string     `json:"state"`
	MovedAt         *time.Time `json:"moved_at"`
	CancelledAt     *time.Time `json:"cancelled_at"`
	SupersededAt    *time.Time `json:"superseded_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// Schedule is the bounded calendar page returned from teaching.
type Schedule struct {
	TutorID           uuid.UUID             `json:"tutor_id"`
	RequestTimeZone   string                `json:"request_time_zone"`
	From              string                `json:"from"`
	Through           string                `json:"through"`
	Classes           []ScheduleClass       `json:"classes"`
	Rules             []ScheduleRuleSummary `json:"rules"`
	Sessions          []ScheduleSession     `json:"sessions"`
	ReplacedHistory   []ScheduleSession     `json:"replaced_history"`
	NextHistoryCursor *string               `json:"next_history_cursor"`
}

// CreateStudentInput contains the only student contact data this slice stores.
type CreateStudentInput struct {
	Name  string  `json:"name"`
	Phone *string `json:"phone"`
}

// Student is the active owned record returned by student commands and reads.
// Phone never enters an event, history response, or attendance response.
type Student struct {
	StudentID uuid.UUID `json:"student_id"`
	Name      string    `json:"name"`
	Phone     *string   `json:"phone"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
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
