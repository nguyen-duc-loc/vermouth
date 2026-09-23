//nolint:noinlineerr,gocritic // Aggregate transactions keep each error check beside the operation it guards.
package handler

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"

	"github.com/nguyen-duc-loc/vermouth/services/teaching/internal/store"
	"github.com/nguyen-duc-loc/vermouth/services/teaching/internal/store/sqlcgen"
)

const operationSaveAttendance = "save_attendance"

// SessionSummary identifies the concrete session behind an attendance sheet.
type SessionSummary struct {
	SessionID  uuid.UUID `json:"session_id"`
	ClassID    uuid.UUID `json:"class_id"`
	ClassName  string    `json:"class_name"`
	ClassColor string    `json:"class_color"`
	StartsAt   time.Time `json:"starts_at"`
	EndsAt     time.Time `json:"ends_at"`
	LocalDate  string    `json:"local_date"`
	State      string    `json:"state"`
}

// AttendanceStudent is one roster label with nullable saved truth.
type AttendanceStudent struct {
	StudentID uuid.UUID  `json:"student_id"`
	Name      string     `json:"name"`
	Archived  bool       `json:"archived"`
	State     *string    `json:"state"`
	MarkedAt  *time.Time `json:"marked_at"`
}

// AttendanceSheet is one coherent session, roster, and attendance snapshot.
type AttendanceSheet struct {
	Session          SessionSummary      `json:"session"`
	Eligible         bool                `json:"eligible"`
	IneligibleReason *string             `json:"ineligible_reason"`
	Revision         string              `json:"revision"`
	Students         []AttendanceStudent `json:"students"`
}

// AttendanceMarkInput is one required roster decision in a complete save.
type AttendanceMarkInput struct {
	StudentID uuid.UUID `json:"student_id"`
	State     string    `json:"state"`
}

// SaveAttendanceInput carries the read revision and complete mark set.
type SaveAttendanceInput struct {
	Revision string                `json:"revision"`
	Marks    []AttendanceMarkInput `json:"marks"`
}

// SavedAttendanceMark is one canonical mark from an accepted pass.
type SavedAttendanceMark struct {
	StudentID uuid.UUID `json:"student_id"`
	State     string    `json:"state"`
	MarkedAt  time.Time `json:"marked_at"`
}

// AttendanceSave is the complete response from one accepted pass.
type AttendanceSave struct {
	SessionID uuid.UUID             `json:"session_id"`
	MarkedAt  time.Time             `json:"marked_at"`
	Marks     []SavedAttendanceMark `json:"marks"`
}

// AttendanceChangedDetails gives the browser the fresh coherent sheet.
type AttendanceChangedDetails struct {
	Sheet AttendanceSheet `json:"sheet"`
}

// SessionNotEligibleDetails gives one exhaustive refusal reason.
type SessionNotEligibleDetails struct {
	Reason string `json:"reason"`
}

type attendanceRevisionStudent struct {
	StudentID uuid.UUID  `json:"student_id"`
	Name      string     `json:"name"`
	Archived  bool       `json:"archived"`
	State     *string    `json:"state"`
	UpdatedAt *time.Time `json:"updated_at"`
}

// ReadAttendance returns one coherent whole roster attendance sheet.
func (h *Handler) ReadAttendance(
	ctx context.Context,
	tutorID uuid.UUID,
	sessionID uuid.UUID,
) (AttendanceSheet, error) {
	tx, err := h.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return AttendanceSheet{}, fmt.Errorf("begin attendance read: %w", err)
	}
	defer h.rollback(ctx, tx)
	queries := store.Queries(tx)
	sheet, err := buildAttendanceSheet(ctx, queries, tutorID, sessionID, h.now().UTC())
	if err != nil {
		return AttendanceSheet{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AttendanceSheet{}, fmt.Errorf("commit attendance read: %w", err)
	}
	return sheet, nil
}

// SaveAttendance writes one complete roster pass with immutable replay.
//
//nolint:funlen,gocognit // The command keeps lock order, revision, complete validation, events, receipt, and commit visible together.
func (h *Handler) SaveAttendance(
	ctx context.Context,
	tutorID uuid.UUID,
	sessionID uuid.UUID,
	idempotencyKey string,
	input SaveAttendanceInput,
) (AttendanceSave, error) {
	if err := validateIdempotencyKey(idempotencyKey); err != nil {
		return AttendanceSave{}, err
	}
	marks, err := validateAttendanceMarks(input)
	if err != nil {
		return AttendanceSave{}, err
	}
	requestHash, err := attendanceRequestHash(sessionID, input.Revision, marks)
	if err != nil {
		return AttendanceSave{}, err
	}
	if replay, found, replayErr := h.replayAttendanceSnapshot(
		ctx, tutorID, idempotencyKey, requestHash[:],
	); found || replayErr != nil {
		return replay, replayErr
	}
	commandTime := h.now().UTC()
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return AttendanceSave{}, fmt.Errorf("begin save attendance: %w", err)
	}
	defer h.rollback(ctx, tx)
	queries := store.Queries(tx)
	_, err = queries.InsertCommandReceipt(ctx, sqlcgen.InsertCommandReceiptParams{
		TutorID:           tutorID,
		Operation:         operationSaveAttendance,
		IdempotencyKey:    idempotencyKey,
		RequestHash:       requestHash[:],
		PrimaryResourceID: sessionID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		h.rollback(ctx, tx)
		result, _, replayErr := h.replayAttendanceSnapshot(
			ctx, tutorID, idempotencyKey, requestHash[:],
		)
		return result, replayErr
	}
	if err != nil {
		return AttendanceSave{}, fmt.Errorf("claim attendance receipt: %w", err)
	}
	initialSession, err := queries.GetAttendanceSheetSession(ctx, sqlcgen.GetAttendanceSheetSessionParams{
		TutorID:   tutorID,
		SessionID: sessionID,
	})
	if err != nil {
		return AttendanceSave{}, ownedReadError("read attendance session", err)
	}
	_, err = queries.LockOwnedClass(ctx, sqlcgen.LockOwnedClassParams{
		TutorID: tutorID,
		ClassID: initialSession.ClassID,
	})
	if err != nil {
		return AttendanceSave{}, ownedReadError("lock attendance class", err)
	}
	lockedSession, err := queries.GetOwnedSessionForUpdate(ctx, sqlcgen.GetOwnedSessionForUpdateParams{
		TutorID:   tutorID,
		SessionID: sessionID,
	})
	if err != nil || lockedSession.ClassID != initialSession.ClassID {
		if err == nil {
			err = pgx.ErrNoRows
		}
		return AttendanceSave{}, ownedReadError("lock attendance session", err)
	}
	studentRows, err := queries.ListAttendanceSheetStudents(
		ctx,
		sqlcgen.ListAttendanceSheetStudentsParams{TutorID: tutorID, SessionID: sessionID},
	)
	if err != nil {
		return AttendanceSave{}, fmt.Errorf("list attendance roster for lock: %w", err)
	}
	studentIDs := make([]uuid.UUID, 0, len(studentRows))
	for _, row := range studentRows {
		studentIDs = append(studentIDs, row.StudentID)
	}
	sortUUIDs(studentIDs)
	if len(studentIDs) > 0 {
		lockedStudents, lockErr := queries.LockRosterStudents(ctx, sqlcgen.LockRosterStudentsParams{
			TutorID:    tutorID,
			StudentIds: studentIDs,
		})
		if lockErr != nil {
			return AttendanceSave{}, fmt.Errorf("lock attendance students: %w", lockErr)
		}
		if len(lockedStudents) != len(studentIDs) {
			return AttendanceSave{}, ErrNotFound
		}
	}
	freshSheet, err := buildAttendanceSheet(ctx, queries, tutorID, sessionID, commandTime)
	if err != nil {
		return AttendanceSave{}, err
	}
	if input.Revision != freshSheet.Revision {
		return AttendanceSave{}, &ConflictError{
			Code:    "attendance_changed",
			Message: "attendance changed after it was read",
			Details: AttendanceChangedDetails{Sheet: freshSheet},
		}
	}
	if !freshSheet.Eligible {
		reason := "not_started"
		if freshSheet.IneligibleReason != nil {
			reason = *freshSheet.IneligibleReason
		}
		return AttendanceSave{}, &ConflictError{
			Code:    "session_not_eligible",
			Message: "the session is not eligible for attendance",
			Details: SessionNotEligibleDetails{Reason: reason},
		}
	}
	if err := requireCompleteAttendanceMarks(freshSheet, marks); err != nil {
		return AttendanceSave{}, err
	}
	marksByID := make(map[uuid.UUID]string, len(marks))
	for _, mark := range marks {
		marksByID[mark.StudentID] = mark.State
	}
	result := AttendanceSave{
		SessionID: sessionID,
		MarkedAt:  commandTime,
		Marks:     make([]SavedAttendanceMark, 0, len(freshSheet.Students)),
	}
	for _, student := range freshSheet.Students {
		state := marksByID[student.StudentID]
		_, err := queries.UpsertAttendanceAt(ctx, sqlcgen.UpsertAttendanceAtParams{
			SessionID: sessionID,
			StudentID: student.StudentID,
			TutorID:   tutorID,
			State:     state,
			MarkedAt:  commandTime,
		})
		if err != nil {
			return AttendanceSave{}, fmt.Errorf("save attendance row: %w", err)
		}
		if err := h.writeEvent(ctx, tx, teachingEvent{
			name:       vermouth.EventAttendanceMarked,
			tutorID:    tutorID,
			key:        vermouth.Key{Kind: vermouth.KeySessionID, Value: sessionID},
			occurredAt: commandTime,
			fields: attendanceMarkedFields{
				SessionID: sessionID,
				ClassID:   freshSheet.Session.ClassID,
				StudentID: student.StudentID,
				TutorID:   tutorID,
				State:     state,
				MarkedAt:  commandTime,
			},
		}); err != nil {
			return AttendanceSave{}, err
		}
		result.Marks = append(result.Marks, SavedAttendanceMark{
			StudentID: student.StudentID,
			State:     state,
			MarkedAt:  commandTime,
		})
	}
	if err := completeAttendanceReceipt(ctx, queries, tutorID, idempotencyKey, result); err != nil {
		return AttendanceSave{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AttendanceSave{}, fmt.Errorf("commit attendance save: %w", err)
	}
	h.logger.InfoContext(ctx, "Attendance saved",
		slog.String("request_id", vermouth.RequestID(ctx)),
		slog.String("command", operationSaveAttendance),
		slog.String("session_id", sessionID.String()),
		slog.Int("student_count", len(result.Marks)),
	)
	return result, nil
}

//nolint:funlen // One mapper keeps revision input and returned sheet visibly identical.
func buildAttendanceSheet(
	ctx context.Context,
	queries *sqlcgen.Queries,
	tutorID uuid.UUID,
	sessionID uuid.UUID,
	now time.Time,
) (AttendanceSheet, error) {
	session, err := queries.GetAttendanceSheetSession(ctx, sqlcgen.GetAttendanceSheetSessionParams{
		TutorID:   tutorID,
		SessionID: sessionID,
	})
	if err != nil {
		return AttendanceSheet{}, ownedReadError("read attendance session", err)
	}
	rows, err := queries.ListAttendanceSheetStudents(ctx, sqlcgen.ListAttendanceSheetStudentsParams{
		TutorID:   tutorID,
		SessionID: sessionID,
	})
	if err != nil {
		return AttendanceSheet{}, fmt.Errorf("list attendance students: %w", err)
	}
	state, eligible, reason := attendanceEligibility(session, now)
	students := make([]AttendanceStudent, 0, len(rows))
	revisionStudents := make([]attendanceRevisionStudent, 0, len(rows))
	for _, row := range rows {
		var markState *string
		if row.State.Valid {
			value := row.State.String
			markState = &value
		}
		var markedAt *time.Time
		if row.MarkedAt.Valid {
			value := row.MarkedAt.Time
			markedAt = &value
		}
		var updatedAt *time.Time
		if row.AttendanceUpdatedAt.Valid {
			value := row.AttendanceUpdatedAt.Time
			updatedAt = &value
		}
		archived := row.RemovedAt.Valid
		students = append(students, AttendanceStudent{
			StudentID: row.StudentID,
			Name:      row.Name,
			Archived:  archived,
			State:     markState,
			MarkedAt:  markedAt,
		})
		revisionStudents = append(revisionStudents, attendanceRevisionStudent{
			StudentID: row.StudentID,
			Name:      row.Name,
			Archived:  archived,
			State:     markState,
			UpdatedAt: updatedAt,
		})
	}
	summary := SessionSummary{
		SessionID:  session.SessionID,
		ClassID:    session.ClassID,
		ClassName:  session.ClassName,
		ClassColor: session.ClassColor,
		StartsAt:   session.StartsAt,
		EndsAt:     session.EndsAt,
		LocalDate:  session.LocalDate.Time.Format(dateLayout),
		State:      state,
	}
	revision, err := attendanceRevision(summary, revisionStudents)
	if err != nil {
		return AttendanceSheet{}, err
	}
	return AttendanceSheet{
		Session:          summary,
		Eligible:         eligible,
		IneligibleReason: reason,
		Revision:         revision,
		Students:         students,
	}, nil
}

func attendanceEligibility(
	session sqlcgen.GetAttendanceSheetSessionRow,
	now time.Time,
) (string, bool, *string) {
	state := stateActive
	var reason string
	switch {
	case session.SupersededAt.Valid:
		state, reason = stateReplaced, stateReplaced
	case session.CancelledAt.Valid:
		state, reason = stateCancelled, stateCancelled
	case session.ClassArchivedAt.Valid:
		state, reason = "class_archived", "class_archived"
	case now.Before(session.StartsAt):
		reason = "not_started"
	default:
		return state, true, nil
	}
	return state, false, &reason
}

func attendanceRevision(
	session SessionSummary,
	students []attendanceRevisionStudent,
) (string, error) {
	encoded, err := json.Marshal(struct {
		Session  SessionSummary              `json:"session"`
		Students []attendanceRevisionStudent `json:"students"`
	}{Session: session, Students: students})
	if err != nil {
		return "", fmt.Errorf("encode attendance revision: %w", err)
	}
	hash := sha256.Sum256(encoded)
	return base64.RawURLEncoding.EncodeToString(hash[:]), nil
}

func validateAttendanceMarks(input SaveAttendanceInput) ([]AttendanceMarkInput, error) {
	if input.Revision == "" {
		return nil, &ValidationError{Field: "revision", Message: "is required"}
	}
	marks := slices.Clone(input.Marks)
	slices.SortFunc(marks, func(left, right AttendanceMarkInput) int {
		return cmp.Compare(left.StudentID.String(), right.StudentID.String())
	})
	for index, mark := range marks {
		if mark.State != "Present" && mark.State != "Absent" {
			return nil, &ValidationError{
				Field: validationFieldMarks, Message: "each state must be Present or Absent",
			}
		}
		if index > 0 && mark.StudentID == marks[index-1].StudentID {
			return nil, &ValidationError{
				Field: validationFieldMarks, Message: "student identifiers must be unique",
			}
		}
	}
	return marks, nil
}

func requireCompleteAttendanceMarks(sheet AttendanceSheet, marks []AttendanceMarkInput) error {
	if len(sheet.Students) != len(marks) {
		return &ValidationError{
			Field: validationFieldMarks, Message: "must contain exactly one mark for every roster student",
		}
	}
	expected := make([]uuid.UUID, 0, len(sheet.Students))
	for _, student := range sheet.Students {
		expected = append(expected, student.StudentID)
	}
	sortUUIDs(expected)
	for index, studentID := range expected {
		if marks[index].StudentID != studentID {
			return &ValidationError{
				Field: validationFieldMarks, Message: "must contain exactly one mark for every roster student",
			}
		}
	}
	return nil
}

func attendanceRequestHash(
	sessionID uuid.UUID,
	revision string,
	marks []AttendanceMarkInput,
) ([sha256.Size]byte, error) {
	encoded, err := json.Marshal(struct {
		Operation string                `json:"operation"`
		SessionID uuid.UUID             `json:"session_id"`
		Revision  string                `json:"revision"`
		Marks     []AttendanceMarkInput `json:"marks"`
	}{Operation: operationSaveAttendance, SessionID: sessionID, Revision: revision, Marks: marks})
	if err != nil {
		return [sha256.Size]byte{}, fmt.Errorf("encode attendance command hash: %w", err)
	}
	return sha256.Sum256(encoded), nil
}

func (h *Handler) replayAttendanceSnapshot(
	ctx context.Context,
	tutorID uuid.UUID,
	idempotencyKey string,
	requestHash []byte,
) (AttendanceSave, bool, error) {
	receipt, err := store.Queries(h.pool).GetCommandReceipt(ctx, sqlcgen.GetCommandReceiptParams{
		TutorID:        tutorID,
		Operation:      operationSaveAttendance,
		IdempotencyKey: idempotencyKey,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return AttendanceSave{}, false, nil
	}
	if err != nil {
		return AttendanceSave{}, false, fmt.Errorf("read attendance receipt: %w", err)
	}
	if !bytes.Equal(receipt.RequestHash, requestHash) {
		return AttendanceSave{}, true, ErrIdempotencyConflict
	}
	var result AttendanceSave
	if err := json.Unmarshal(receipt.ResponseSnapshot, &result); err != nil {
		return AttendanceSave{}, true, fmt.Errorf("decode attendance receipt: %w", err)
	}
	return result, true, nil
}

func completeAttendanceReceipt(
	ctx context.Context,
	queries *sqlcgen.Queries,
	tutorID uuid.UUID,
	idempotencyKey string,
	result AttendanceSave,
) error {
	snapshot, err := jsonSnapshot(result)
	if err != nil {
		return err
	}
	_, err = queries.CompleteCommandReceipt(ctx, sqlcgen.CompleteCommandReceiptParams{
		TutorID:          tutorID,
		Operation:        operationSaveAttendance,
		IdempotencyKey:   idempotencyKey,
		ResponseSnapshot: snapshot,
		ResponseStatus:   pgtype.Int4{Int32: http.StatusOK, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("complete attendance receipt: %w", err)
	}
	return nil
}
