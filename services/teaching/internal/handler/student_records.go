//nolint:noinlineerr // Receipt transactions keep each error check beside the operation it guards.
package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"

	"github.com/nguyen-duc-loc/vermouth/services/teaching/internal/store"
	"github.com/nguyen-duc-loc/vermouth/services/teaching/internal/store/sqlcgen"
)

const (
	operationUpdateStudent = "update_student"
	operationRemoveStudent = "remove_student"
	studentPageSize        = 50
	studentPageProofSize   = studentPageSize + 1
	studentPatchFieldCount = 3
)

var errInvalidStudentPatch = errors.New("invalid student patch")

// StudentSummary is one active student row in stable search order.
type StudentSummary struct {
	StudentID        uuid.UUID `json:"student_id"`
	Name             string    `json:"name"`
	Phone            *string   `json:"phone"`
	ActiveClassCount int64     `json:"active_class_count"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// StudentPage is one cursor page of active students.
type StudentPage struct {
	Students   []StudentSummary `json:"students"`
	NextCursor *string          `json:"next_cursor"`
}

// StudentMembership is one retained dated class membership.
type StudentMembership struct {
	ClassID       uuid.UUID `json:"class_id"`
	ClassName     string    `json:"class_name"`
	ClassColor    string    `json:"class_color"`
	EffectiveFrom string    `json:"effective_from"`
	EffectiveTo   *string   `json:"effective_to"`
	Active        bool      `json:"active"`
}

// StudentDetail combines an active record with all retained memberships.
type StudentDetail struct {
	Student     Student             `json:"student"`
	Memberships []StudentMembership `json:"memberships"`
}

// PatchStudentInput preserves whether each editable field was present.
type PatchStudentInput struct {
	ExpectedUpdatedAt string  `json:"expected_updated_at"`
	Name              *string `json:"name,omitempty"`
	Phone             *string `json:"phone,omitempty"`
	NameSet           bool    `json:"-"`
	PhoneSet          bool    `json:"-"`
}

// UnmarshalJSON rejects unknown and repeated fields while preserving an
// explicit null phone separately from an omitted phone.
//
//nolint:gocognit // A token loop is required to reject repeated fields before decoding values.
func (input *PatchStudentInput) UnmarshalJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return fmt.Errorf("%w: must be an object", errInvalidStudentPatch)
	}
	seen := make(map[string]struct{}, studentPatchFieldCount)
	for decoder.More() {
		token, tokenErr := decoder.Token()
		if tokenErr != nil {
			return fmt.Errorf("read student patch field: %w", tokenErr)
		}
		name, ok := token.(string)
		if !ok {
			return fmt.Errorf("%w: field name is invalid", errInvalidStudentPatch)
		}
		if _, exists := seen[name]; exists {
			return fmt.Errorf("%w: field %q is repeated", errInvalidStudentPatch, name)
		}
		seen[name] = struct{}{}
		switch name {
		case "expected_updated_at":
			if err := decoder.Decode(&input.ExpectedUpdatedAt); err != nil {
				return fmt.Errorf("decode expected_updated_at: %w", err)
			}
		case "name":
			var value string
			if err := decoder.Decode(&value); err != nil {
				return fmt.Errorf("decode name: %w", err)
			}
			input.Name = &value
			input.NameSet = true
		case validationFieldPhone:
			var value *string
			if err := decoder.Decode(&value); err != nil {
				return fmt.Errorf("decode phone: %w", err)
			}
			input.Phone = value
			input.PhoneSet = true
		default:
			return fmt.Errorf("%w: field %q is unknown", errInvalidStudentPatch, name)
		}
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim('}') {
		return fmt.Errorf("%w: object is incomplete", errInvalidStudentPatch)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: must contain one object", errInvalidStudentPatch)
	}
	return nil
}

// StudentChangedDetails is the private safe recovery value for a stale edit.
type StudentChangedDetails struct {
	StudentID uuid.UUID `json:"student_id"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ClassReference is an owned class identifier and label safe for conflicts.
type ClassReference struct {
	ClassID uuid.UUID `json:"class_id"`
	Name    string    `json:"name"`
}

// ActiveMembershipDetails lists the classes that currently block archival.
type ActiveMembershipDetails struct {
	Classes []ClassReference `json:"classes"`
}

type studentCursor struct {
	Query     string    `json:"query"`
	LowerName string    `json:"lower_name"`
	Name      string    `json:"name"`
	StudentID uuid.UUID `json:"student_id"`
}

type validatedStudentPatch struct {
	expectedUpdatedAt time.Time
	nameSet           bool
	name              string
	phoneSet          bool
	phone             *string
}

type studentChangedFields struct {
	StudentID uuid.UUID `json:"student_id"`
	TutorID   uuid.UUID `json:"tutor_id"`
	Name      string    `json:"name"`
}

type studentRemovedFields struct {
	StudentID uuid.UUID `json:"student_id"`
	TutorID   uuid.UUID `json:"tutor_id"`
}

// ListStudents returns one stable page for the tutor's current local date.
func (h *Handler) ListStudents(
	ctx context.Context,
	tutorID uuid.UUID,
	timezone string,
	query string,
	rawCursor string,
) (StudentPage, error) {
	query = strings.TrimSpace(query)
	if utf8.RuneCountInString(query) > maxStudentNameRunes {
		return StudentPage{}, &ValidationError{Field: "q", Message: "must contain at most 160 characters"}
	}
	localDate, err := currentLocalDate(h.now().UTC(), timezone)
	if err != nil {
		return StudentPage{}, err
	}
	cursor, hasCursor, err := decodeStudentCursor(rawCursor, query)
	if err != nil {
		return StudentPage{}, err
	}
	rows, err := store.Queries(h.pool).ListStudents(ctx, sqlcgen.ListStudentsParams{
		LocalDate:       pgtype.Date{Time: localDate, Valid: true},
		TutorID:         tutorID,
		SearchQuery:     query,
		HasCursor:       hasCursor,
		CursorLowerName: cursor.LowerName,
		CursorName:      cursor.Name,
		CursorStudentID: cursor.StudentID,
		PageSize:        studentPageProofSize,
	})
	if err != nil {
		return StudentPage{}, fmt.Errorf("list students: %w", err)
	}
	var nextCursor *string
	if len(rows) > studentPageSize {
		rows = rows[:studentPageSize]
		encoded, encodeErr := encodeStudentCursor(query, rows[len(rows)-1])
		if encodeErr != nil {
			return StudentPage{}, encodeErr
		}
		nextCursor = &encoded
	}
	students := make([]StudentSummary, 0, len(rows))
	for _, row := range rows {
		students = append(students, StudentSummary{
			StudentID:        row.StudentID,
			Name:             row.Name,
			Phone:            textPointer(row.Phone),
			ActiveClassCount: row.ActiveClassCount,
			UpdatedAt:        row.UpdatedAt.UTC(),
		})
	}
	h.logger.InfoContext(ctx, "Students listed",
		slog.String("request_id", vermouth.RequestID(ctx)),
		slog.Int("count", len(students)),
	)
	return StudentPage{Students: students, NextCursor: nextCursor}, nil
}

// ReadStudent returns an active student and membership history from one
// repeatable database snapshot.
func (h *Handler) ReadStudent(
	ctx context.Context,
	tutorID uuid.UUID,
	studentID uuid.UUID,
	timezone string,
) (StudentDetail, error) {
	localDate, err := currentLocalDate(h.now().UTC(), timezone)
	if err != nil {
		return StudentDetail{}, err
	}
	tx, err := h.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return StudentDetail{}, fmt.Errorf("begin student detail read: %w", err)
	}
	defer h.rollback(ctx, tx)
	queries := store.Queries(tx)
	studentRow, err := queries.GetActiveOwnedStudent(ctx, sqlcgen.GetActiveOwnedStudentParams{
		TutorID:   tutorID,
		StudentID: studentID,
	})
	if err != nil {
		return StudentDetail{}, ownedReadError("read student", err)
	}
	membershipRows, err := queries.ListStudentMemberships(ctx, sqlcgen.ListStudentMembershipsParams{
		LocalDate: pgtype.Date{Time: localDate, Valid: true},
		TutorID:   tutorID,
		StudentID: studentID,
	})
	if err != nil {
		return StudentDetail{}, fmt.Errorf("list student memberships: %w", err)
	}
	memberships := make([]StudentMembership, 0, len(membershipRows))
	for _, row := range membershipRows {
		memberships = append(memberships, studentMembershipFromStored(row))
	}
	if err := tx.Commit(ctx); err != nil {
		return StudentDetail{}, fmt.Errorf("commit student detail read: %w", err)
	}
	return StudentDetail{Student: studentFromStored(studentRow), Memberships: memberships}, nil
}

// UpdateStudent applies an optimistic edit with immutable receipt replay.
//
//nolint:funlen,gocognit // The transaction keeps replay, lock, update, event, receipt, and commit visible together.
func (h *Handler) UpdateStudent(
	ctx context.Context,
	tutorID uuid.UUID,
	studentID uuid.UUID,
	idempotencyKey string,
	input PatchStudentInput,
) (Student, error) {
	if err := validateIdempotencyKey(idempotencyKey); err != nil {
		return Student{}, err
	}
	validated, err := validateStudentPatch(input)
	if err != nil {
		return Student{}, err
	}
	requestHash, err := studentPatchRequestHash(studentID, validated)
	if err != nil {
		return Student{}, err
	}
	if replay, found, replayErr := h.replayStudentSnapshot(
		ctx, tutorID, operationUpdateStudent, idempotencyKey, requestHash[:],
	); found || replayErr != nil {
		return replay, replayErr
	}
	commandTime := h.now().UTC()
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return Student{}, fmt.Errorf("begin update student: %w", err)
	}
	defer h.rollback(ctx, tx)
	queries := store.Queries(tx)
	_, err = queries.InsertCommandReceipt(ctx, sqlcgen.InsertCommandReceiptParams{
		TutorID:           tutorID,
		Operation:         operationUpdateStudent,
		IdempotencyKey:    idempotencyKey,
		RequestHash:       requestHash[:],
		PrimaryResourceID: studentID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		h.rollback(ctx, tx)
		result, _, replayErr := h.replayStudentSnapshot(
			ctx, tutorID, operationUpdateStudent, idempotencyKey, requestHash[:],
		)
		return result, replayErr
	}
	if err != nil {
		return Student{}, fmt.Errorf("claim update student receipt: %w", err)
	}
	current, err := queries.LockActiveOwnedStudent(ctx, sqlcgen.LockActiveOwnedStudentParams{
		TutorID:   tutorID,
		StudentID: studentID,
	})
	if err != nil {
		return Student{}, ownedReadError("lock student", err)
	}
	if !current.UpdatedAt.Equal(validated.expectedUpdatedAt) {
		return Student{}, &ConflictError{
			Code:    "student_changed",
			Message: "the student changed after it was read",
			Details: StudentChangedDetails{StudentID: studentID, UpdatedAt: current.UpdatedAt.UTC()},
		}
	}
	name := current.Name
	if validated.nameSet {
		name = validated.name
	}
	phone := textPointer(current.Phone)
	if validated.phoneSet {
		phone = validated.phone
	}
	nameChanged := name != current.Name
	phoneChanged := !equalOptionalText(phone, textPointer(current.Phone))
	if !nameChanged && !phoneChanged {
		result := studentFromStored(current)
		if err := completeStudentReceipt(
			ctx, queries, tutorID, operationUpdateStudent, idempotencyKey, result, http.StatusOK,
		); err != nil {
			return Student{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return Student{}, fmt.Errorf("commit unchanged student: %w", err)
		}
		return result, nil
	}
	updatedAt := commandTime
	if !updatedAt.After(current.UpdatedAt) {
		updatedAt = current.UpdatedAt.Add(time.Microsecond)
	}
	row, err := queries.UpdateStudentRecord(ctx, sqlcgen.UpdateStudentRecordParams{
		TutorID:   tutorID,
		StudentID: studentID,
		Name:      name,
		Phone:     nullableText(phone),
		UpdatedAt: updatedAt,
	})
	if err != nil {
		return Student{}, fmt.Errorf("update student: %w", err)
	}
	if nameChanged {
		err = h.writeEvent(ctx, tx, teachingEvent{
			name:    vermouth.EventStudentChanged,
			tutorID: tutorID,
			key:     vermouth.Key{Kind: vermouth.KeyStudentID, Value: studentID},
			fields:  studentChangedFields{StudentID: studentID, TutorID: tutorID, Name: name},
		})
		if err != nil {
			return Student{}, err
		}
	}
	result := studentFromStored(row)
	if err := completeStudentReceipt(
		ctx, queries, tutorID, operationUpdateStudent, idempotencyKey, result, http.StatusOK,
	); err != nil {
		return Student{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Student{}, fmt.Errorf("commit update student: %w", err)
	}
	h.logger.InfoContext(ctx, "Student updated",
		slog.String("request_id", vermouth.RequestID(ctx)),
		slog.String("command", operationUpdateStudent),
		slog.String("student_id", studentID.String()),
	)
	return result, nil
}

// ArchiveStudent removes an active student only when no class covers today.
//
//nolint:funlen // The transaction keeps replay, lock, guard, event, receipt, and commit visible together.
func (h *Handler) ArchiveStudent(
	ctx context.Context,
	tutorID uuid.UUID,
	studentID uuid.UUID,
	timezone string,
	idempotencyKey string,
) error {
	if err := validateIdempotencyKey(idempotencyKey); err != nil {
		return err
	}
	requestHash, err := archiveStudentRequestHash(studentID)
	if err != nil {
		return err
	}
	if found, replayErr := h.replayEmptySnapshot(
		ctx, tutorID, operationRemoveStudent, idempotencyKey, requestHash[:],
	); found || replayErr != nil {
		return replayErr
	}
	commandTime := h.now().UTC()
	localDate, err := currentLocalDate(commandTime, timezone)
	if err != nil {
		return err
	}
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin archive student: %w", err)
	}
	defer h.rollback(ctx, tx)
	queries := store.Queries(tx)
	_, err = queries.InsertCommandReceipt(ctx, sqlcgen.InsertCommandReceiptParams{
		TutorID:           tutorID,
		Operation:         operationRemoveStudent,
		IdempotencyKey:    idempotencyKey,
		RequestHash:       requestHash[:],
		PrimaryResourceID: studentID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		h.rollback(ctx, tx)
		_, replayErr := h.replayEmptySnapshot(
			ctx, tutorID, operationRemoveStudent, idempotencyKey, requestHash[:],
		)
		return replayErr
	}
	if err != nil {
		return fmt.Errorf("claim archive student receipt: %w", err)
	}
	_, err = queries.LockActiveOwnedStudent(ctx, sqlcgen.LockActiveOwnedStudentParams{
		TutorID:   tutorID,
		StudentID: studentID,
	})
	if err != nil {
		return ownedReadError("lock student", err)
	}
	classRows, err := queries.ListActiveStudentClasses(ctx, sqlcgen.ListActiveStudentClassesParams{
		TutorID:   tutorID,
		StudentID: studentID,
		LocalDate: pgtype.Date{Time: localDate, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("list active student classes: %w", err)
	}
	if len(classRows) > 0 {
		classes := make([]ClassReference, 0, len(classRows))
		for _, row := range classRows {
			classes = append(classes, ClassReference{ClassID: row.ClassID, Name: row.Name})
		}
		return &ConflictError{
			Code:    "active_memberships",
			Message: "the student still belongs to an active class",
			Details: ActiveMembershipDetails{Classes: classes},
		}
	}
	_, err = queries.ArchiveStudentRecord(ctx, sqlcgen.ArchiveStudentRecordParams{
		TutorID:   tutorID,
		StudentID: studentID,
		RemovedAt: pgtype.Timestamptz{Time: commandTime, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("archive student: %w", err)
	}
	err = h.writeEvent(ctx, tx, teachingEvent{
		name:       vermouth.EventStudentRemoved,
		tutorID:    tutorID,
		key:        vermouth.Key{Kind: vermouth.KeyStudentID, Value: studentID},
		fields:     studentRemovedFields{StudentID: studentID, TutorID: tutorID},
		occurredAt: commandTime,
	})
	if err != nil {
		return err
	}
	if err := completeEmptyReceipt(
		ctx, queries, tutorID, operationRemoveStudent, idempotencyKey, http.StatusNoContent,
	); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit archive student: %w", err)
	}
	h.logger.InfoContext(ctx, "Student archived",
		slog.String("request_id", vermouth.RequestID(ctx)),
		slog.String("command", operationRemoveStudent),
		slog.String("student_id", studentID.String()),
	)
	return nil
}

func validateStudentPatch(input PatchStudentInput) (validatedStudentPatch, error) {
	if !input.NameSet && !input.PhoneSet {
		return validatedStudentPatch{}, &ValidationError{
			Field: validationFieldBody, Message: "must change name or phone",
		}
	}
	expected, err := time.Parse(time.RFC3339Nano, input.ExpectedUpdatedAt)
	if err != nil || expected.IsZero() {
		return validatedStudentPatch{}, &ValidationError{
			Field:   "expected_updated_at",
			Message: "must be an RFC 3339 timestamp",
		}
	}
	result := validatedStudentPatch{
		expectedUpdatedAt: expected.UTC(),
		nameSet:           input.NameSet,
		phoneSet:          input.PhoneSet,
	}
	if input.NameSet {
		name, nameErr := validateStudentName(*input.Name)
		if nameErr != nil {
			return validatedStudentPatch{}, nameErr
		}
		result.name = name
	}
	if input.PhoneSet && input.Phone != nil {
		phone := strings.TrimSpace(*input.Phone)
		if utf8.RuneCountInString(phone) > maxPhoneRunes {
			return validatedStudentPatch{}, &ValidationError{
				Field:   validationFieldPhone,
				Message: "must contain at most 40 characters",
			}
		}
		if phone != "" {
			result.phone = &phone
		}
	}
	return result, nil
}

func currentLocalDate(now time.Time, timezone string) (time.Time, error) {
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return time.Time{}, fmt.Errorf("load request timezone: %w", err)
	}
	return localCalendarDate(now, location), nil
}

func decodeStudentCursor(raw, query string) (studentCursor, bool, error) {
	if raw == "" {
		return studentCursor{}, false, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return studentCursor{}, false, &ValidationError{Field: cursorField, Message: cursorMalformed}
	}
	var cursor studentCursor
	if err := json.Unmarshal(decoded, &cursor); err != nil {
		return studentCursor{}, false, &ValidationError{Field: cursorField, Message: cursorMalformed}
	}
	if cursor.Query != query || cursor.LowerName == "" || cursor.Name == "" || cursor.StudentID == uuid.Nil {
		return studentCursor{}, false, &ValidationError{
			Field:   cursorField,
			Message: "does not belong to this student search",
		}
	}
	return cursor, true, nil
}

func encodeStudentCursor(query string, row sqlcgen.ListStudentsRow) (string, error) {
	encoded, err := json.Marshal(studentCursor{
		Query:     query,
		LowerName: row.LowerName,
		Name:      row.Name,
		StudentID: row.StudentID,
	})
	if err != nil {
		return "", fmt.Errorf("encode student cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

func studentMembershipFromStored(row sqlcgen.ListStudentMembershipsRow) StudentMembership {
	var effectiveTo *string
	if row.EffectiveTo.Valid {
		value := row.EffectiveTo.Time.Format(dateLayout)
		effectiveTo = &value
	}
	return StudentMembership{
		ClassID:       row.ClassID,
		ClassName:     row.ClassName,
		ClassColor:    row.ClassColor,
		EffectiveFrom: row.EffectiveFrom.Time.Format(dateLayout),
		EffectiveTo:   effectiveTo,
		Active:        row.Active,
	}
}

func studentPatchRequestHash(
	studentID uuid.UUID,
	value validatedStudentPatch,
) ([sha256.Size]byte, error) {
	canonical := struct {
		Operation         string    `json:"operation"`
		StudentID         uuid.UUID `json:"student_id"`
		ExpectedUpdatedAt string    `json:"expected_updated_at"`
		NameSet           bool      `json:"name_set"`
		Name              string    `json:"name"`
		PhoneSet          bool      `json:"phone_set"`
		Phone             *string   `json:"phone"`
	}{
		Operation:         operationUpdateStudent,
		StudentID:         studentID,
		ExpectedUpdatedAt: value.expectedUpdatedAt.Format(time.RFC3339Nano),
		NameSet:           value.nameSet,
		Name:              value.name,
		PhoneSet:          value.phoneSet,
		Phone:             value.phone,
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return [sha256.Size]byte{}, fmt.Errorf("encode update student command hash: %w", err)
	}
	return sha256.Sum256(encoded), nil
}

func archiveStudentRequestHash(studentID uuid.UUID) ([sha256.Size]byte, error) {
	encoded, err := json.Marshal(struct {
		Operation string    `json:"operation"`
		StudentID uuid.UUID `json:"student_id"`
	}{Operation: operationRemoveStudent, StudentID: studentID})
	if err != nil {
		return [sha256.Size]byte{}, fmt.Errorf("encode archive student command hash: %w", err)
	}
	return sha256.Sum256(encoded), nil
}

func (h *Handler) replayStudentSnapshot(
	ctx context.Context,
	tutorID uuid.UUID,
	operation string,
	idempotencyKey string,
	requestHash []byte,
) (Student, bool, error) {
	receipt, err := store.Queries(h.pool).GetCommandReceipt(ctx, sqlcgen.GetCommandReceiptParams{
		TutorID:        tutorID,
		Operation:      operation,
		IdempotencyKey: idempotencyKey,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Student{}, false, nil
	}
	if err != nil {
		return Student{}, false, fmt.Errorf("read %s receipt: %w", operation, err)
	}
	if !bytes.Equal(receipt.RequestHash, requestHash) {
		return Student{}, true, ErrIdempotencyConflict
	}
	var result Student
	if err := json.Unmarshal(receipt.ResponseSnapshot, &result); err != nil {
		return Student{}, true, fmt.Errorf("decode %s receipt: %w", operation, err)
	}
	return result, true, nil
}

func (h *Handler) replayEmptySnapshot(
	ctx context.Context,
	tutorID uuid.UUID,
	operation string,
	idempotencyKey string,
	requestHash []byte,
) (bool, error) {
	receipt, err := store.Queries(h.pool).GetCommandReceipt(ctx, sqlcgen.GetCommandReceiptParams{
		TutorID:        tutorID,
		Operation:      operation,
		IdempotencyKey: idempotencyKey,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read %s receipt: %w", operation, err)
	}
	if !bytes.Equal(receipt.RequestHash, requestHash) {
		return true, ErrIdempotencyConflict
	}
	return true, nil
}

func completeStudentReceipt(
	ctx context.Context,
	queries *sqlcgen.Queries,
	tutorID uuid.UUID,
	operation string,
	idempotencyKey string,
	result Student,
	status int32,
) error {
	snapshot, err := jsonSnapshot(result)
	if err != nil {
		return err
	}
	_, err = queries.CompleteCommandReceipt(ctx, sqlcgen.CompleteCommandReceiptParams{
		TutorID:          tutorID,
		Operation:        operation,
		IdempotencyKey:   idempotencyKey,
		ResponseSnapshot: snapshot,
		ResponseStatus:   pgtype.Int4{Int32: status, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("complete %s receipt: %w", operation, err)
	}
	return nil
}

func completeEmptyReceipt(
	ctx context.Context,
	queries *sqlcgen.Queries,
	tutorID uuid.UUID,
	operation string,
	idempotencyKey string,
	status int32,
) error {
	_, err := queries.CompleteCommandReceipt(ctx, sqlcgen.CompleteCommandReceiptParams{
		TutorID:          tutorID,
		Operation:        operation,
		IdempotencyKey:   idempotencyKey,
		ResponseSnapshot: []byte("{}"),
		ResponseStatus:   pgtype.Int4{Int32: status, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("complete %s receipt: %w", operation, err)
	}
	return nil
}

func nullableText(value *string) pgtype.Text {
	if value == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *value, Valid: true}
}

func equalOptionalText(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
