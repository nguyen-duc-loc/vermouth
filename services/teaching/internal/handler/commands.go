package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"

	"github.com/nguyen-duc-loc/vermouth/services/teaching/internal/store"
	"github.com/nguyen-duc-loc/vermouth/services/teaching/internal/store/sqlcgen"
)

const (
	operationCreateClass   = "create_class"
	operationCreateStudent = "create_student"
	currencyVND            = "VND"
)

var errClassReceiptMissingSession = errors.New("create class receipt has no session id")

// Handler owns teaching transactions and reads. HTTP only validates transport
// shape and maps these outcomes onto the public boundary.
type Handler struct {
	pool         *pgxpool.Pool
	logger       *slog.Logger
	publishTopic string
	now          func() time.Time
}

// New builds teaching work over its own database and outbox topic.
func New(pool *pgxpool.Pool, logger *slog.Logger, publishTopic string) *Handler {
	return &Handler{
		pool:         pool,
		logger:       logger,
		publishTopic: publishTopic,
		now:          time.Now,
	}
}

type teachingEvent struct {
	name    string
	tutorID uuid.UUID
	key     vermouth.Key
	fields  any
}

type classCreatedFields struct {
	ClassID           uuid.UUID `json:"class_id"`
	TutorID           uuid.UUID `json:"tutor_id"`
	Name              string    `json:"name"`
	RateAmount        int64     `json:"rate_amount"`
	Currency          string    `json:"currency"`
	RateEffectiveFrom string    `json:"rate_effective_from"`
}

type sessionScheduledFields struct {
	SessionID uuid.UUID `json:"session_id"`
	ClassID   uuid.UUID `json:"class_id"`
	TutorID   uuid.UUID `json:"tutor_id"`
	StartsAt  time.Time `json:"starts_at"`
	EndsAt    time.Time `json:"ends_at"`
	LocalDate string    `json:"local_date"`
}

type studentRegisteredFields struct {
	StudentID uuid.UUID `json:"student_id"`
	TutorID   uuid.UUID `json:"tutor_id"`
	Name      string    `json:"name"`
}

type rosterJoinedFields struct {
	ClassID       uuid.UUID `json:"class_id"`
	StudentID     uuid.UUID `json:"student_id"`
	TutorID       uuid.UUID `json:"tutor_id"`
	EffectiveFrom string    `json:"effective_from"`
}

type attendanceMarkedFields struct {
	SessionID uuid.UUID `json:"session_id"`
	ClassID   uuid.UUID `json:"class_id"`
	StudentID uuid.UUID `json:"student_id"`
	TutorID   uuid.UUID `json:"tutor_id"`
	State     string    `json:"state"`
	MarkedAt  time.Time `json:"marked_at"`
}

// CreateClass commits the class, optional weekly rule, concrete sessions,
// receipt, and outbox facts together. The bool is true only for the request
// that created them.
//
//nolint:funlen,gocognit,maintidx // One linear function keeps the receipt, aggregate, generated rows, events, and commit auditable as one transaction.
func (h *Handler) CreateClass(
	ctx context.Context,
	tutorID uuid.UUID,
	timezone string,
	idempotencyKey string,
	input CreateClassInput,
) (CreateClassResult, bool, error) {
	err := validateIdempotencyKey(idempotencyKey)
	if err != nil {
		return CreateClassResult{}, false, err
	}
	requestHash, err := classRequestHash(input)
	if err != nil {
		return CreateClassResult{}, false, err
	}
	replayed, found, replayErr := h.replayClassSnapshot(ctx, tutorID, idempotencyKey, requestHash[:])
	if found || replayErr != nil {
		return replayed, false, replayErr
	}
	commandTime := h.now().UTC()
	validated, err := validateClassInput(input, timezone, commandTime)
	if err != nil {
		return CreateClassResult{}, false, err
	}
	classID, err := uuid.NewV7()
	if err != nil {
		return CreateClassResult{}, false, fmt.Errorf("generate class id: %w", err)
	}
	occurrences := make([]validatedOccurrence, 0, 1)
	if validated.first != nil {
		occurrences = append(occurrences, *validated.first)
	} else {
		occurrences = append(occurrences, validated.schedule.occurrences...)
	}
	sessionIDs := make([]uuid.UUID, 0, len(occurrences))
	for range occurrences {
		sessionID, sessionErr := uuid.NewV7()
		if sessionErr != nil {
			return CreateClassResult{}, false, fmt.Errorf("generate session id: %w", sessionErr)
		}
		sessionIDs = append(sessionIDs, sessionID)
	}
	var scheduleRuleID uuid.UUID
	if validated.schedule != nil {
		scheduleRuleID, err = uuid.NewV7()
		if err != nil {
			return CreateClassResult{}, false, fmt.Errorf("generate schedule rule id: %w", err)
		}
	}
	color := suggestedClassColor(classID.String())
	if validated.color != nil {
		color = *validated.color
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return CreateClassResult{}, false, fmt.Errorf("begin create class: %w", err)
	}
	defer h.rollback(ctx, tx)
	queries := store.Queries(tx)
	contextSnapshot, err := jsonSnapshot(commandContext{
		CommandTime: commandTime, GoverningTimeZone: timezone, RequestDisplayTimeZone: timezone,
	})
	if err != nil {
		return CreateClassResult{}, false, err
	}

	_, err = queries.InsertCommandReceipt(ctx, sqlcgen.InsertCommandReceiptParams{
		TutorID:           tutorID,
		Operation:         operationCreateClass,
		IdempotencyKey:    idempotencyKey,
		RequestHash:       requestHash[:],
		PrimaryResourceID: classID,
		RelatedResourceID: pgtype.UUID{Bytes: sessionIDs[0], Valid: true},
		ContextSnapshot:   contextSnapshot,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		h.rollback(ctx, tx)
		return h.replayClass(ctx, tutorID, idempotencyKey, requestHash[:])
	}
	if err != nil {
		return CreateClassResult{}, false, fmt.Errorf("claim create class receipt: %w", err)
	}

	rateEffectiveFrom := occurrences[0].localDate
	scheduleRevision := int64(0)
	if validated.schedule != nil {
		rateEffectiveFrom = validated.schedule.validFrom
		scheduleRevision = 1
	}
	classRow, err := queries.InsertClass(ctx, sqlcgen.InsertClassParams{
		ClassID:           classID,
		TutorID:           tutorID,
		Name:              validated.name,
		Color:             color,
		RateAmount:        validated.rateAmount,
		Currency:          currencyVND,
		RateEffectiveFrom: pgtype.Date{Time: rateEffectiveFrom, Valid: true},
		ScheduleRevision:  scheduleRevision,
	})
	if err != nil {
		return CreateClassResult{}, false, fmt.Errorf("insert class: %w", err)
	}
	var rule *ScheduleRuleSummary
	if validated.schedule != nil {
		_, err = queries.InsertScheduleRule(ctx, sqlcgen.InsertScheduleRuleParams{
			ScheduleRuleID: scheduleRuleID,
			TutorID:        tutorID,
			ClassID:        classID,
			Revision:       scheduleRevision,
			ValidFrom:      pgtype.Date{Time: validated.schedule.validFrom, Valid: true},
			ValidThrough:   pgtype.Date{Time: validated.schedule.validThrough, Valid: true},
			TimeZone:       validated.timezone,
		})
		if err != nil {
			return CreateClassResult{}, false, fmt.Errorf("insert schedule rule: %w", err)
		}
		for _, slot := range validated.schedule.slots {
			_, err = queries.InsertScheduleSlot(ctx, sqlcgen.InsertScheduleSlotParams{
				ScheduleRuleID: scheduleRuleID,
				TutorID:        tutorID,
				Weekday:        slot.Weekday,
				StartTime:      slot.StartTime,
				EndTime:        slot.EndTime,
			})
			if err != nil {
				return CreateClassResult{}, false, fmt.Errorf("insert schedule slot: %w", err)
			}
		}
		rule = &ScheduleRuleSummary{
			ScheduleRuleID: scheduleRuleID, ClassID: classID, Revision: scheduleRevision,
			ValidFrom:    validated.schedule.validFrom.Format(dateLayout),
			ValidThrough: validated.schedule.validThrough.Format(dateLayout),
			TimeZone:     validated.timezone,
			State: ruleState(
				validated.schedule.validFrom,
				validated.schedule.validThrough,
				validated.timezone,
				nil,
				nil,
				nil,
				commandTime,
			),
			Slots: validated.schedule.slots,
		}
	}
	err = h.writeEvent(ctx, tx, teachingEvent{
		name:    vermouth.EventClassCreated,
		tutorID: tutorID,
		key:     vermouth.Key{Kind: vermouth.KeyClassID, Value: classID},
		fields: classCreatedFields{
			ClassID: classID, TutorID: tutorID, Name: validated.name,
			RateAmount: validated.rateAmount, Currency: currencyVND,
			RateEffectiveFrom: rateEffectiveFrom.Format(dateLayout),
		},
	})
	if err != nil {
		return CreateClassResult{}, false, err
	}
	inserted := make([]sqlcgen.InsertSessionRow, 0, len(occurrences))
	for index := range occurrences {
		occurrence := &occurrences[index]
		ruleID := pgtype.UUID{}
		if validated.schedule != nil {
			ruleID = pgtype.UUID{Bytes: scheduleRuleID, Valid: true}
		}
		row, insertErr := queries.InsertSession(ctx, sqlcgen.InsertSessionParams{
			SessionID: sessionIDs[index], ClassID: classID, TutorID: tutorID,
			StartsAt: occurrence.startsAt, EndsAt: occurrence.endsAt,
			LocalDate:       pgtype.Date{Time: occurrence.localDate, Valid: true},
			ScheduleRuleID:  ruleID,
			OriginLocalDate: pgtype.Date{Time: occurrence.originLocalDate, Valid: true},
		})
		if insertErr != nil {
			if constraintConflict(insertErr) {
				return CreateClassResult{}, false, ErrConflict
			}
			return CreateClassResult{}, false, fmt.Errorf("insert class session: %w", insertErr)
		}
		inserted = append(inserted, row)
		date := occurrence.localDate.Format(dateLayout)
		writeErr := h.writeEvent(ctx, tx, teachingEvent{
			name: vermouth.EventSessionScheduled, tutorID: tutorID,
			key: vermouth.Key{Kind: vermouth.KeySessionID, Value: sessionIDs[index]},
			fields: sessionScheduledFields{
				SessionID: sessionIDs[index], ClassID: classID, TutorID: tutorID,
				StartsAt: occurrence.startsAt, EndsAt: occurrence.endsAt, LocalDate: date,
			},
		})
		if writeErr != nil {
			return CreateClassResult{}, false, writeErr
		}
	}
	result := CreateClassResult{
		Class: Class{
			ClassID: classRow.ClassID, Name: classRow.Name, Color: classRow.Color,
			RateAmount: classRow.RateAmount, Currency: classRow.Currency,
			RateEffectiveFrom: classRow.RateEffectiveFrom.Time.Format(dateLayout),
			ScheduleRevision:  classRow.ScheduleRevision,
		},
		Rule: rule, CandidateCount: len(occurrences), CreatedCount: len(inserted),
	}
	if validated.first != nil {
		first := sessionFromValues(
			inserted[0].SessionID,
			inserted[0].ClassID,
			inserted[0].StartsAt,
			inserted[0].EndsAt,
			inserted[0].LocalDate.Time,
		)
		result.FirstSession = &first
	} else {
		result.FirstSession = firstUpcomingInserted(inserted, commandTime)
	}
	responseSnapshot, err := jsonSnapshot(result)
	if err != nil {
		return CreateClassResult{}, false, err
	}
	_, err = queries.CompleteCommandReceipt(ctx, sqlcgen.CompleteCommandReceiptParams{
		TutorID: tutorID, Operation: operationCreateClass, IdempotencyKey: idempotencyKey,
		ResponseSnapshot: responseSnapshot, ResponseStatus: pgtype.Int4{Int32: http.StatusCreated, Valid: true},
	})
	if err != nil {
		return CreateClassResult{}, false, fmt.Errorf("complete create class receipt: %w", err)
	}
	err = tx.Commit(ctx)
	if err != nil {
		return CreateClassResult{}, false, fmt.Errorf("commit create class: %w", err)
	}
	return result, true, nil
}

func (h *Handler) replayClassSnapshot(
	ctx context.Context,
	tutorID uuid.UUID,
	idempotencyKey string,
	requestHash []byte,
) (CreateClassResult, bool, error) {
	receipt, err := store.Queries(h.pool).GetCommandReceipt(ctx, sqlcgen.GetCommandReceiptParams{
		TutorID: tutorID, Operation: operationCreateClass, IdempotencyKey: idempotencyKey,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return CreateClassResult{}, false, nil
	}
	if err != nil {
		return CreateClassResult{}, false, fmt.Errorf("read create class receipt: %w", err)
	}
	if !bytes.Equal(receipt.RequestHash, requestHash) {
		return CreateClassResult{}, true, ErrIdempotencyConflict
	}
	if len(receipt.ResponseSnapshot) == 0 {
		result, _, replayErr := h.replayClass(ctx, tutorID, idempotencyKey, requestHash)
		return result, true, replayErr
	}
	var result CreateClassResult
	err = json.Unmarshal(receipt.ResponseSnapshot, &result)
	if err != nil {
		return CreateClassResult{}, true, fmt.Errorf("decode create class receipt response: %w", err)
	}
	return result, true, nil
}

//nolint:funlen // Rebuilding the legacy and recurring response together keeps one replay contract visible.
func (h *Handler) replayClass(
	ctx context.Context,
	tutorID uuid.UUID,
	idempotencyKey string,
	requestHash []byte,
) (CreateClassResult, bool, error) {
	queries := store.Queries(h.pool)
	receipt, err := queries.GetCommandReceipt(ctx, sqlcgen.GetCommandReceiptParams{
		TutorID: tutorID, Operation: operationCreateClass, IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		return CreateClassResult{}, false, fmt.Errorf("read create class receipt: %w", err)
	}
	if !bytes.Equal(receipt.RequestHash, requestHash) {
		return CreateClassResult{}, false, ErrIdempotencyConflict
	}
	classRow, err := queries.GetOwnedClass(ctx, sqlcgen.GetOwnedClassParams{
		TutorID: tutorID, ClassID: receipt.PrimaryResourceID,
	})
	if err != nil {
		return CreateClassResult{}, false, fmt.Errorf("recover class receipt resource: %w", err)
	}
	sessions, err := queries.ListOwnedClassSessions(ctx, sqlcgen.ListOwnedClassSessionsParams{
		TutorID: tutorID, ClassID: receipt.PrimaryResourceID,
	})
	if err != nil {
		return CreateClassResult{}, false, fmt.Errorf("recover class sessions: %w", err)
	}
	if len(sessions) == 0 || !receipt.RelatedResourceID.Valid {
		return CreateClassResult{}, false, errClassReceiptMissingSession
	}
	result := CreateClassResult{
		Class: Class{
			ClassID: classRow.ClassID, Name: classRow.Name, Color: classRow.Color,
			RateAmount: classRow.RateAmount, Currency: classRow.Currency,
			RateEffectiveFrom: classRow.RateEffectiveFrom.Time.Format(dateLayout),
			ScheduleRevision:  classRow.ScheduleRevision,
		},
		CandidateCount: len(sessions), CreatedCount: len(sessions),
	}
	ruleRow, ruleErr := queries.GetOwnedLatestScheduleRule(ctx, sqlcgen.GetOwnedLatestScheduleRuleParams{
		TutorID: tutorID, ClassID: receipt.PrimaryResourceID,
	})
	if ruleErr == nil {
		slots, slotErr := queries.ListScheduleSlots(ctx, sqlcgen.ListScheduleSlotsParams{
			TutorID: tutorID, ScheduleRuleIds: []uuid.UUID{ruleRow.ScheduleRuleID},
		})
		if slotErr != nil {
			return CreateClassResult{}, false, fmt.Errorf("recover schedule slots: %w", slotErr)
		}
		result.Rule = scheduleRuleSummary(ruleRow, slots, h.now().UTC())
	} else if !errors.Is(ruleErr, pgx.ErrNoRows) {
		return CreateClassResult{}, false, fmt.Errorf("recover class schedule rule: %w", ruleErr)
	}
	if result.Rule == nil {
		first := sessionFromValues(
			sessions[0].SessionID,
			sessions[0].ClassID,
			sessions[0].StartsAt,
			sessions[0].EndsAt,
			sessions[0].LocalDate.Time,
		)
		result.FirstSession = &first
	} else {
		result.FirstSession = firstUpcomingStored(sessions, h.now().UTC())
	}
	return result, false, nil
}

// CreateStudent commits the student, receipt, and phone free event together.
// The bool is true only for the request that created them.
//
//nolint:funlen // Keeping receipt claim, private phone write, event, and commit together makes the transaction boundary auditable.
func (h *Handler) CreateStudent(
	ctx context.Context,
	tutorID uuid.UUID,
	idempotencyKey string,
	input CreateStudentInput,
) (Student, bool, error) {
	err := validateIdempotencyKey(idempotencyKey)
	if err != nil {
		return Student{}, false, err
	}
	validated, err := validateStudentInput(input)
	if err != nil {
		return Student{}, false, err
	}
	requestHash, err := studentRequestHash(validated)
	if err != nil {
		return Student{}, false, err
	}
	studentID, err := uuid.NewV7()
	if err != nil {
		return Student{}, false, fmt.Errorf("generate student id: %w", err)
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return Student{}, false, fmt.Errorf("begin create student: %w", err)
	}
	defer h.rollback(ctx, tx)
	queries := store.Queries(tx)
	_, err = queries.InsertCommandReceipt(ctx, sqlcgen.InsertCommandReceiptParams{
		TutorID: tutorID, Operation: operationCreateStudent, IdempotencyKey: idempotencyKey,
		RequestHash: requestHash[:], PrimaryResourceID: studentID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		h.rollback(ctx, tx)
		return h.replayStudent(ctx, tutorID, idempotencyKey, requestHash[:])
	}
	if err != nil {
		return Student{}, false, fmt.Errorf("claim create student receipt: %w", err)
	}
	phone := pgtype.Text{}
	if validated.phone != nil {
		phone = pgtype.Text{String: *validated.phone, Valid: true}
	}
	row, err := queries.InsertStudent(ctx, sqlcgen.InsertStudentParams{
		StudentID: studentID, TutorID: tutorID, Name: validated.name, Phone: phone,
	})
	if err != nil {
		return Student{}, false, fmt.Errorf("insert student: %w", err)
	}
	err = h.writeEvent(ctx, tx, teachingEvent{
		name:    vermouth.EventStudentRegistered,
		tutorID: tutorID,
		key:     vermouth.Key{Kind: vermouth.KeyStudentID, Value: studentID},
		fields:  studentRegisteredFields{StudentID: studentID, TutorID: tutorID, Name: validated.name},
	})
	if err != nil {
		return Student{}, false, err
	}
	err = tx.Commit(ctx)
	if err != nil {
		return Student{}, false, fmt.Errorf("commit create student: %w", err)
	}
	return Student{StudentID: row.StudentID, Name: row.Name, Phone: textPointer(row.Phone)}, true, nil
}

func (h *Handler) replayStudent(
	ctx context.Context,
	tutorID uuid.UUID,
	idempotencyKey string,
	requestHash []byte,
) (Student, bool, error) {
	queries := store.Queries(h.pool)
	receipt, err := queries.GetCommandReceipt(ctx, sqlcgen.GetCommandReceiptParams{
		TutorID: tutorID, Operation: operationCreateStudent, IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		return Student{}, false, fmt.Errorf("read create student receipt: %w", err)
	}
	if !bytes.Equal(receipt.RequestHash, requestHash) {
		return Student{}, false, ErrIdempotencyConflict
	}
	row, err := queries.GetOwnedStudent(ctx, sqlcgen.GetOwnedStudentParams{
		TutorID: tutorID, StudentID: receipt.PrimaryResourceID,
	})
	if err != nil {
		return Student{}, false, fmt.Errorf("recover student receipt resource: %w", err)
	}
	return Student{StudentID: row.StudentID, Name: row.Name, Phone: textPointer(row.Phone)}, false, nil
}

// JoinRoster opens one period or returns the exact existing open period. A
// different open effective date is a conflict and writes no event.
//
//nolint:funlen // The linear flow makes every ownership, retry, event, and commit decision visible in one transaction.
func (h *Handler) JoinRoster(
	ctx context.Context,
	tutorID uuid.UUID,
	classID uuid.UUID,
	input JoinRosterInput,
) (RosterPeriod, bool, error) {
	effectiveFrom, err := parseDate("effective_from", input.EffectiveFrom)
	if err != nil {
		return RosterPeriod{}, false, err
	}
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return RosterPeriod{}, false, fmt.Errorf("begin join roster: %w", err)
	}
	defer h.rollback(ctx, tx)
	queries := store.Queries(tx)
	_, err = queries.GetOwnedClass(ctx, sqlcgen.GetOwnedClassParams{
		TutorID: tutorID, ClassID: classID,
	})
	if err != nil {
		return RosterPeriod{}, false, ownedReadError("read roster class", err)
	}
	_, err = queries.GetOwnedStudent(ctx, sqlcgen.GetOwnedStudentParams{
		TutorID: tutorID, StudentID: input.StudentID,
	})
	if err != nil {
		return RosterPeriod{}, false, ownedReadError("read roster student", err)
	}

	open, err := queries.FindOpenRosterPeriod(ctx, sqlcgen.FindOpenRosterPeriodParams{
		TutorID: tutorID, ClassID: classID, StudentID: input.StudentID,
	})
	if err == nil {
		if open.EffectiveFrom.Time.Equal(effectiveFrom) {
			return rosterFromOpen(open), false, nil
		}
		return RosterPeriod{}, false, ErrConflict
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return RosterPeriod{}, false, fmt.Errorf("read open roster period: %w", err)
	}

	row, err := queries.InsertRosterPeriod(ctx, sqlcgen.InsertRosterPeriodParams{
		ClassID: classID, StudentID: input.StudentID,
		EffectiveFrom: pgtype.Date{Time: effectiveFrom, Valid: true}, TutorID: tutorID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		recoveredOpen, readErr := queries.FindOpenRosterPeriod(ctx, sqlcgen.FindOpenRosterPeriodParams{
			TutorID: tutorID, ClassID: classID, StudentID: input.StudentID,
		})
		if readErr != nil {
			return RosterPeriod{}, false, fmt.Errorf("recover concurrent roster period: %w", readErr)
		}
		if recoveredOpen.EffectiveFrom.Time.Equal(effectiveFrom) {
			return rosterFromOpen(recoveredOpen), false, nil
		}
		return RosterPeriod{}, false, ErrConflict
	}
	if err != nil {
		if constraintConflict(err) {
			return RosterPeriod{}, false, ErrConflict
		}
		return RosterPeriod{}, false, fmt.Errorf("insert roster period: %w", err)
	}
	err = h.writeEvent(ctx, tx, teachingEvent{
		name:    vermouth.EventRosterJoined,
		tutorID: tutorID,
		key:     vermouth.Key{Kind: vermouth.KeyClassID, Value: classID},
		fields: rosterJoinedFields{
			ClassID: classID, StudentID: input.StudentID, TutorID: tutorID,
			EffectiveFrom: effectiveFrom.Format(dateLayout),
		},
	})
	if err != nil {
		return RosterPeriod{}, false, err
	}
	err = tx.Commit(ctx)
	if err != nil {
		return RosterPeriod{}, false, fmt.Errorf("commit join roster: %w", err)
	}
	return rosterFromStored(row), true, nil
}

// MarkAttendance serialises corrections on the owned session, checks inclusive
// roster coverage, and writes an event only for a real state change.
func (h *Handler) MarkAttendance(
	ctx context.Context,
	tutorID uuid.UUID,
	sessionID uuid.UUID,
	studentID uuid.UUID,
	input MarkAttendanceInput,
) (Attendance, error) {
	if input.State != "Present" && input.State != "Absent" {
		return Attendance{}, &ValidationError{Field: "state", Message: "must be Present or Absent"}
	}
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return Attendance{}, fmt.Errorf("begin mark attendance: %w", err)
	}
	defer h.rollback(ctx, tx)
	queries := store.Queries(tx)
	writeContext, err := queries.GetAttendanceWriteContext(ctx, sqlcgen.GetAttendanceWriteContextParams{
		StudentID: studentID, TutorID: tutorID, SessionID: sessionID,
	})
	if err != nil {
		return Attendance{}, ownedReadError("read attendance context", err)
	}
	if !writeContext.Rostered {
		return Attendance{}, ErrConflict
	}
	existing, err := queries.GetAttendance(ctx, sqlcgen.GetAttendanceParams{
		TutorID: tutorID, SessionID: sessionID, StudentID: studentID,
	})
	if err == nil && existing.State == input.State {
		return attendanceFromStored(existing), nil
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Attendance{}, fmt.Errorf("read attendance: %w", err)
	}
	markedAt := h.now().UTC()
	row, err := queries.UpsertAttendance(ctx, sqlcgen.UpsertAttendanceParams{
		SessionID: sessionID, StudentID: studentID, TutorID: tutorID,
		State: input.State, MarkedAt: markedAt,
	})
	if err != nil {
		return Attendance{}, fmt.Errorf("save attendance: %w", err)
	}
	err = h.writeEvent(ctx, tx, teachingEvent{
		name:    vermouth.EventAttendanceMarked,
		tutorID: tutorID,
		key:     vermouth.Key{Kind: vermouth.KeySessionID, Value: sessionID},
		fields: attendanceMarkedFields{
			SessionID: sessionID, ClassID: writeContext.ClassID, StudentID: studentID,
			TutorID: tutorID, State: input.State, MarkedAt: markedAt,
		},
	})
	if err != nil {
		return Attendance{}, err
	}
	err = tx.Commit(ctx)
	if err != nil {
		return Attendance{}, fmt.Errorf("commit attendance: %w", err)
	}
	return attendanceFromStored(row), nil
}

func (h *Handler) writeEvent(ctx context.Context, tx pgx.Tx, event teachingEvent) error {
	envelope, err := vermouth.NewEnvelope(ctx, event.name, 1, event.tutorID, event.key, event.fields)
	if err != nil {
		return fmt.Errorf("build %s: %w", event.name, err)
	}
	err = vermouth.WriteOutbox(ctx, tx, h.publishTopic, envelope)
	if err != nil {
		return err
	}
	h.logger.InfoContext(ctx, "Event written to the outbox",
		slog.String("request_id", vermouth.RequestID(ctx)),
		slog.String("event_name", event.name),
		slog.String("event_id", envelope.EventID.String()),
	)
	return nil
}

func (h *Handler) rollback(ctx context.Context, tx pgx.Tx) {
	err := tx.Rollback(ctx)
	if err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		h.logger.ErrorContext(ctx, "Roll back teaching transaction", slog.String("error", err.Error()))
	}
}

func ownedReadError(action string, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return fmt.Errorf("%s: %w", action, err)
}

func constraintConflict(err error) bool {
	pgError, ok := errors.AsType[*pgconn.PgError](err)
	return ok && (pgError.Code == "23505" || pgError.Code == "23P01")
}

func firstUpcomingInserted(rows []sqlcgen.InsertSessionRow, commandTime time.Time) *Session {
	for index := range rows {
		row := &rows[index]
		if !row.StartsAt.Before(commandTime) {
			result := sessionFromValues(
				row.SessionID, row.ClassID, row.StartsAt, row.EndsAt, row.LocalDate.Time,
			)
			return &result
		}
	}
	return nil
}

func firstUpcomingStored(rows []sqlcgen.ListOwnedClassSessionsRow, commandTime time.Time) *Session {
	for index := range rows {
		row := &rows[index]
		if !row.StartsAt.Before(commandTime) && !row.CancelledAt.Valid && !row.SupersededAt.Valid {
			result := sessionFromValues(
				row.SessionID, row.ClassID, row.StartsAt, row.EndsAt, row.LocalDate.Time,
			)
			return &result
		}
	}
	return nil
}

func sessionFromValues(
	sessionID uuid.UUID,
	classID uuid.UUID,
	startsAt time.Time,
	endsAt time.Time,
	localDate time.Time,
) Session {
	return Session{
		SessionID: sessionID,
		ClassID:   classID,
		StartsAt:  startsAt,
		EndsAt:    endsAt,
		LocalDate: localDate.Format(dateLayout),
	}
}

func scheduleRuleSummary(
	rule sqlcgen.ScheduleRule,
	slots []sqlcgen.ListScheduleSlotsRow,
	commandTime time.Time,
) *ScheduleRuleSummary {
	result := &ScheduleRuleSummary{
		ScheduleRuleID: rule.ScheduleRuleID,
		ClassID:        rule.ClassID,
		Revision:       rule.Revision,
		ValidFrom:      rule.ValidFrom.Time.Format(dateLayout),
		ValidThrough:   rule.ValidThrough.Time.Format(dateLayout),
		TimeZone:       rule.TimeZone,
		State: ruleState(
			rule.ValidFrom.Time,
			rule.ValidThrough.Time,
			rule.TimeZone,
			timestampPointer(rule.ReplacedAt),
			timestampPointer(rule.EndedAt),
			timestampPointer(rule.RetiredAt),
			commandTime,
		),
		ReplacedAt: timestampPointer(rule.ReplacedAt),
		EndedAt:    timestampPointer(rule.EndedAt),
		RetiredAt:  timestampPointer(rule.RetiredAt),
		Slots:      make([]WeeklyScheduleSlotInput, 0, len(slots)),
	}
	for _, slot := range slots {
		result.Slots = append(result.Slots, WeeklyScheduleSlotInput{
			Weekday: slot.Weekday, StartTime: slot.StartTime, EndTime: slot.EndTime,
		})
	}
	return result
}

func textPointer(value pgtype.Text) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
}

func rosterFromOpen(row sqlcgen.FindOpenRosterPeriodRow) RosterPeriod {
	return RosterPeriod{
		ClassID: row.ClassID, StudentID: row.StudentID,
		EffectiveFrom: row.EffectiveFrom.Time.Format(dateLayout),
	}
}

func rosterFromStored(row sqlcgen.RosterPeriod) RosterPeriod {
	var effectiveTo *string
	if row.EffectiveTo.Valid {
		value := row.EffectiveTo.Time.Format(dateLayout)
		effectiveTo = &value
	}
	return RosterPeriod{
		ClassID: row.ClassID, StudentID: row.StudentID,
		EffectiveFrom: row.EffectiveFrom.Time.Format(dateLayout), EffectiveTo: effectiveTo,
	}
}

func attendanceFromStored(row sqlcgen.Attendance) Attendance {
	return Attendance{
		SessionID: row.SessionID,
		StudentID: row.StudentID,
		State:     row.State,
		MarkedAt:  row.MarkedAt,
	}
}
