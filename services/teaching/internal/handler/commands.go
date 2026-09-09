package handler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
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

// CreateClass commits the class, first session, receipt, and two outbox facts
// together. The bool is true only for the request that created them.
//
//nolint:funlen // Keeping the aggregate writes and both outbox facts together makes the transaction boundary auditable.
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
	validated, err := validateClassInput(input, timezone)
	if err != nil {
		return CreateClassResult{}, false, err
	}
	requestHash, err := classRequestHash(validated)
	if err != nil {
		return CreateClassResult{}, false, err
	}
	classID, err := uuid.NewV7()
	if err != nil {
		return CreateClassResult{}, false, fmt.Errorf("generate class id: %w", err)
	}
	sessionID, err := uuid.NewV7()
	if err != nil {
		return CreateClassResult{}, false, fmt.Errorf("generate session id: %w", err)
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

	_, err = queries.InsertCommandReceipt(ctx, sqlcgen.InsertCommandReceiptParams{
		TutorID:           tutorID,
		Operation:         operationCreateClass,
		IdempotencyKey:    idempotencyKey,
		RequestHash:       requestHash[:],
		PrimaryResourceID: classID,
		RelatedResourceID: pgtype.UUID{Bytes: sessionID, Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		h.rollback(ctx, tx)
		return h.replayClass(ctx, tutorID, idempotencyKey, requestHash[:])
	}
	if err != nil {
		return CreateClassResult{}, false, fmt.Errorf("claim create class receipt: %w", err)
	}

	classRow, err := queries.InsertClass(ctx, sqlcgen.InsertClassParams{
		ClassID:           classID,
		TutorID:           tutorID,
		Name:              validated.name,
		Color:             color,
		RateAmount:        validated.rateAmount,
		Currency:          currencyVND,
		RateEffectiveFrom: pgtype.Date{Time: validated.localDate, Valid: true},
	})
	if err != nil {
		return CreateClassResult{}, false, fmt.Errorf("insert class: %w", err)
	}
	sessionRow, err := queries.InsertSession(ctx, sqlcgen.InsertSessionParams{
		SessionID: sessionID,
		ClassID:   classID,
		TutorID:   tutorID,
		StartsAt:  validated.startsAt,
		EndsAt:    validated.endsAt,
		LocalDate: pgtype.Date{Time: validated.localDate, Valid: true},
	})
	if err != nil {
		return CreateClassResult{}, false, fmt.Errorf("insert first session: %w", err)
	}
	date := validated.localDate.Format(dateLayout)
	err = h.writeEvent(ctx, tx, teachingEvent{
		name:    vermouth.EventClassCreated,
		tutorID: tutorID,
		key:     vermouth.Key{Kind: vermouth.KeyClassID, Value: classID},
		fields: classCreatedFields{
			ClassID: classID, TutorID: tutorID, Name: validated.name,
			RateAmount: validated.rateAmount, Currency: currencyVND, RateEffectiveFrom: date,
		},
	})
	if err != nil {
		return CreateClassResult{}, false, err
	}
	err = h.writeEvent(ctx, tx, teachingEvent{
		name:    vermouth.EventSessionScheduled,
		tutorID: tutorID,
		key:     vermouth.Key{Kind: vermouth.KeySessionID, Value: sessionID},
		fields: sessionScheduledFields{
			SessionID: sessionID, ClassID: classID, TutorID: tutorID,
			StartsAt: validated.startsAt, EndsAt: validated.endsAt, LocalDate: date,
		},
	})
	if err != nil {
		return CreateClassResult{}, false, err
	}
	err = tx.Commit(ctx)
	if err != nil {
		return CreateClassResult{}, false, fmt.Errorf("commit create class: %w", err)
	}
	return CreateClassResult{
		Class: Class{
			ClassID: classRow.ClassID, Name: classRow.Name, Color: classRow.Color,
			RateAmount: classRow.RateAmount, Currency: classRow.Currency,
			RateEffectiveFrom: classRow.RateEffectiveFrom.Time.Format(dateLayout),
		},
		FirstSession: sessionFromRow(sessionRow),
	}, true, nil
}

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
	if !receipt.RelatedResourceID.Valid {
		return CreateClassResult{}, false, errClassReceiptMissingSession
	}
	classRow, err := queries.GetOwnedClass(ctx, sqlcgen.GetOwnedClassParams{
		TutorID: tutorID, ClassID: receipt.PrimaryResourceID,
	})
	if err != nil {
		return CreateClassResult{}, false, fmt.Errorf("recover class receipt resource: %w", err)
	}
	sessionID := uuid.UUID(receipt.RelatedResourceID.Bytes)
	sessionRow, err := queries.GetOwnedSession(ctx, sqlcgen.GetOwnedSessionParams{
		TutorID: tutorID, SessionID: sessionID,
	})
	if err != nil {
		return CreateClassResult{}, false, fmt.Errorf("recover class session receipt resource: %w", err)
	}
	return CreateClassResult{
		Class: Class{
			ClassID: classRow.ClassID, Name: classRow.Name, Color: classRow.Color,
			RateAmount: classRow.RateAmount, Currency: classRow.Currency,
			RateEffectiveFrom: classRow.RateEffectiveFrom.Time.Format(dateLayout),
		},
		FirstSession: sessionFromStored(sessionRow),
	}, false, nil
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
	return ok && pgError.Code == "23505"
}

func sessionFromRow(row sqlcgen.Session) Session {
	return sessionFromStored(row)
}

func sessionFromStored(row sqlcgen.Session) Session {
	return Session{
		SessionID: row.SessionID,
		ClassID:   row.ClassID,
		StartsAt:  row.StartsAt,
		EndsAt:    row.EndsAt,
		LocalDate: row.LocalDate.Time.Format(dateLayout),
	}
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
