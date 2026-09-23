//nolint:noinlineerr,gocritic // Aggregate transactions keep each error check beside the operation it guards.
package handler

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
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

const (
	operationChangeRoster = "change_roster"
	maxRosterChanges      = 200
	maxRosterStudents     = 500
)

// ClassSummary is the stable identity shown with a roster.
type ClassSummary struct {
	ClassID uuid.UUID `json:"class_id"`
	Name    string    `json:"name"`
	Color   string    `json:"color"`
}

// RosterStudent is one student covered by a class on the resolved date.
type RosterStudent struct {
	StudentID     uuid.UUID `json:"student_id"`
	Name          string    `json:"name"`
	Phone         *string   `json:"phone"`
	Archived      bool      `json:"archived"`
	EffectiveFrom string    `json:"effective_from"`
	EffectiveTo   *string   `json:"effective_to"`
}

// ClassRoster is the canonical dated roster response.
type ClassRoster struct {
	Class        ClassSummary    `json:"class"`
	ResolvedDate string          `json:"resolved_date"`
	Students     []RosterStudent `json:"students"`
}

// ChangeRosterInput stages one atomic dated addition and removal set.
type ChangeRosterInput struct {
	ChangeDate string      `json:"change_date"`
	Additions  []uuid.UUID `json:"additions"`
	Removals   []uuid.UUID `json:"removals"`
}

// RosterConflictStudent excludes phone from conflict recovery data.
type RosterConflictStudent struct {
	StudentID     uuid.UUID `json:"student_id"`
	Name          string    `json:"name"`
	Archived      bool      `json:"archived"`
	EffectiveFrom string    `json:"effective_from"`
	EffectiveTo   *string   `json:"effective_to"`
}

// RosterConflictDetails is the phone free current roster returned on conflict.
type RosterConflictDetails struct {
	Class        ClassSummary            `json:"class"`
	ResolvedDate string                  `json:"resolved_date"`
	Students     []RosterConflictStudent `json:"students"`
}

type rosterLeftFields struct {
	ClassID     uuid.UUID `json:"class_id"`
	StudentID   uuid.UUID `json:"student_id"`
	TutorID     uuid.UUID `json:"tutor_id"`
	EffectiveTo string    `json:"effective_to"`
}

type validatedRosterChange struct {
	changeDate time.Time
	additions  []uuid.UUID
	removals   []uuid.UUID
	allIDs     []uuid.UUID
}

type rosterEventChange struct {
	studentID uuid.UUID
	name      string
	date      string
}

// ReadClassRoster returns one owned class roster from a repeatable snapshot.
func (h *Handler) ReadClassRoster(
	ctx context.Context,
	tutorID uuid.UUID,
	classID uuid.UUID,
	timezone string,
	requestedDate string,
) (ClassRoster, error) {
	today, err := currentLocalDate(h.now().UTC(), timezone)
	if err != nil {
		return ClassRoster{}, err
	}
	resolvedDate := today
	if requestedDate != "" {
		resolvedDate, err = parseDate("date", requestedDate)
		if err != nil {
			return ClassRoster{}, err
		}
	}
	if resolvedDate.After(today) {
		return ClassRoster{}, &ValidationError{Field: "date", Message: "must be today or earlier"}
	}
	tx, err := h.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return ClassRoster{}, fmt.Errorf("begin class roster read: %w", err)
	}
	defer h.rollback(ctx, tx)
	queries := store.Queries(tx)
	classRow, err := queries.GetOwnedClass(ctx, sqlcgen.GetOwnedClassParams{
		TutorID: tutorID,
		ClassID: classID,
	})
	if err != nil || classRow.ArchivedAt.Valid {
		if err == nil {
			err = pgx.ErrNoRows
		}
		return ClassRoster{}, ownedReadError("read roster class", err)
	}
	rows, err := queries.ListClassRosterAtDate(ctx, sqlcgen.ListClassRosterAtDateParams{
		TutorID:      tutorID,
		ClassID:      classID,
		ResolvedDate: pgtype.Date{Time: resolvedDate, Valid: true},
	})
	if err != nil {
		return ClassRoster{}, fmt.Errorf("list class roster: %w", err)
	}
	result := classRosterFromRows(
		ClassSummary{ClassID: classRow.ClassID, Name: classRow.Name, Color: classRow.Color},
		resolvedDate,
		today,
		rows,
	)
	if err := tx.Commit(ctx); err != nil {
		return ClassRoster{}, fmt.Errorf("commit class roster read: %w", err)
	}
	return result, nil
}

// ChangeClassRoster applies one atomic dated delta with immutable replay.
//
//nolint:funlen,gocognit,maintidx // The command keeps lock order, validation, events, receipt, and commit visible together.
func (h *Handler) ChangeClassRoster(
	ctx context.Context,
	tutorID uuid.UUID,
	classID uuid.UUID,
	timezone string,
	idempotencyKey string,
	input ChangeRosterInput,
) (ClassRoster, error) {
	if err := validateIdempotencyKey(idempotencyKey); err != nil {
		return ClassRoster{}, err
	}
	today, err := currentLocalDate(h.now().UTC(), timezone)
	if err != nil {
		return ClassRoster{}, err
	}
	validated, err := validateRosterChange(input, today)
	if err != nil {
		return ClassRoster{}, err
	}
	requestHash, err := rosterChangeRequestHash(classID, validated)
	if err != nil {
		return ClassRoster{}, err
	}
	if replay, found, replayErr := h.replayRosterSnapshot(
		ctx, tutorID, idempotencyKey, requestHash[:],
	); found || replayErr != nil {
		return replay, replayErr
	}
	commandTime := h.now().UTC()
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return ClassRoster{}, fmt.Errorf("begin change roster: %w", err)
	}
	defer h.rollback(ctx, tx)
	queries := store.Queries(tx)
	_, err = queries.InsertCommandReceipt(ctx, sqlcgen.InsertCommandReceiptParams{
		TutorID:           tutorID,
		Operation:         operationChangeRoster,
		IdempotencyKey:    idempotencyKey,
		RequestHash:       requestHash[:],
		PrimaryResourceID: classID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		h.rollback(ctx, tx)
		result, _, replayErr := h.replayRosterSnapshot(
			ctx, tutorID, idempotencyKey, requestHash[:],
		)
		return result, replayErr
	}
	if err != nil {
		return ClassRoster{}, fmt.Errorf("claim roster receipt: %w", err)
	}
	classRow, err := queries.LockOwnedClass(ctx, sqlcgen.LockOwnedClassParams{
		TutorID: tutorID,
		ClassID: classID,
	})
	if err != nil || classRow.ArchivedAt.Valid {
		if err == nil {
			err = pgx.ErrNoRows
		}
		return ClassRoster{}, ownedReadError("lock roster class", err)
	}
	students, err := queries.LockRosterStudents(ctx, sqlcgen.LockRosterStudentsParams{
		TutorID:    tutorID,
		StudentIds: validated.allIDs,
	})
	if err != nil {
		return ClassRoster{}, fmt.Errorf("lock roster students: %w", err)
	}
	if len(students) != len(validated.allIDs) {
		return ClassRoster{}, ErrNotFound
	}
	studentsByID := make(map[uuid.UUID]sqlcgen.Student, len(students))
	for _, student := range students {
		studentsByID[student.StudentID] = student
	}
	periods, err := queries.ListRosterPeriodsForStudents(
		ctx,
		sqlcgen.ListRosterPeriodsForStudentsParams{
			TutorID:    tutorID,
			ClassID:    classID,
			StudentIds: validated.allIDs,
		},
	)
	if err != nil {
		return ClassRoster{}, fmt.Errorf("list roster periods for change: %w", err)
	}
	periodsByStudent := make(map[uuid.UUID][]sqlcgen.RosterPeriod, len(students))
	for _, period := range periods {
		periodsByStudent[period.StudentID] = append(periodsByStudent[period.StudentID], period)
	}
	for _, studentID := range validated.additions {
		student := studentsByID[studentID]
		if student.RemovedAt.Valid || rosterAdditionConflicts(periodsByStudent[studentID], validated.changeDate) {
			return ClassRoster{}, rosterConflict(
				ctx, queries, tutorID,
				ClassSummary{ClassID: classRow.ClassID, Name: classRow.Name, Color: classRow.Color},
				validated.changeDate,
			)
		}
	}
	removalTargets := make(map[uuid.UUID]sqlcgen.RosterPeriod, len(validated.removals))
	for _, studentID := range validated.removals {
		period, found := coveredRosterPeriod(periodsByStudent[studentID], validated.changeDate)
		if !found || !period.EffectiveFrom.Time.Before(validated.changeDate) {
			return ClassRoster{}, rosterConflict(
				ctx, queries, tutorID,
				ClassSummary{ClassID: classRow.ClassID, Name: classRow.Name, Color: classRow.Color},
				validated.changeDate,
			)
		}
		removalTargets[studentID] = period
	}
	coveredCount, err := queries.CountClassRosterAtDate(ctx, sqlcgen.CountClassRosterAtDateParams{
		TutorID:      tutorID,
		ClassID:      classID,
		ResolvedDate: pgtype.Date{Time: validated.changeDate, Valid: true},
	})
	if err != nil {
		return ClassRoster{}, fmt.Errorf("count roster coverage: %w", err)
	}
	if coveredCount+int64(len(validated.additions))-int64(len(validated.removals)) > maxRosterStudents {
		return ClassRoster{}, rosterConflict(
			ctx, queries, tutorID,
			ClassSummary{ClassID: classRow.ClassID, Name: classRow.Name, Color: classRow.Color},
			validated.changeDate,
		)
	}
	joined := make([]rosterEventChange, 0, len(validated.additions))
	for _, studentID := range validated.additions {
		_, err := queries.InsertRosterPeriod(ctx, sqlcgen.InsertRosterPeriodParams{
			ClassID:       classID,
			StudentID:     studentID,
			EffectiveFrom: pgtype.Date{Time: validated.changeDate, Valid: true},
			TutorID:       tutorID,
		})
		if err != nil {
			if constraintConflict(err) || errors.Is(err, pgx.ErrNoRows) {
				return ClassRoster{}, rosterConflict(
					ctx, queries, tutorID,
					ClassSummary{ClassID: classRow.ClassID, Name: classRow.Name, Color: classRow.Color},
					validated.changeDate,
				)
			}
			return ClassRoster{}, fmt.Errorf("add roster student: %w", err)
		}
		joined = append(joined, rosterEventChange{
			studentID: studentID,
			name:      vermouth.EventRosterJoined,
			date:      validated.changeDate.Format(dateLayout),
		})
	}
	left := make([]rosterEventChange, 0, len(validated.removals))
	for _, studentID := range validated.removals {
		target := removalTargets[studentID]
		effectiveTo := validated.changeDate.AddDate(0, 0, -1)
		_, err := queries.CloseRosterPeriodAt(ctx, sqlcgen.CloseRosterPeriodAtParams{
			EffectiveTo:   pgtype.Date{Time: effectiveTo, Valid: true},
			UpdatedAt:     commandTime,
			TutorID:       tutorID,
			ClassID:       classID,
			StudentID:     studentID,
			EffectiveFrom: target.EffectiveFrom,
		})
		if err != nil {
			return ClassRoster{}, fmt.Errorf("remove roster student: %w", err)
		}
		left = append(left, rosterEventChange{
			studentID: studentID,
			name:      vermouth.EventRosterLeft,
			date:      effectiveTo.Format(dateLayout),
		})
	}
	events := append(joined, left...)
	slices.SortFunc(events, func(leftEvent, rightEvent rosterEventChange) int {
		return cmp.Compare(leftEvent.studentID.String(), rightEvent.studentID.String())
	})
	for _, change := range events {
		var fields any = rosterJoinedFields{
			ClassID:       classID,
			StudentID:     change.studentID,
			TutorID:       tutorID,
			EffectiveFrom: change.date,
		}
		if change.name == vermouth.EventRosterLeft {
			fields = rosterLeftFields{
				ClassID:     classID,
				StudentID:   change.studentID,
				TutorID:     tutorID,
				EffectiveTo: change.date,
			}
		}
		if err := h.writeEvent(ctx, tx, teachingEvent{
			name:       change.name,
			tutorID:    tutorID,
			key:        vermouth.Key{Kind: vermouth.KeyClassID, Value: classID},
			fields:     fields,
			occurredAt: commandTime,
		}); err != nil {
			return ClassRoster{}, err
		}
	}
	rows, err := queries.ListClassRosterAtDate(ctx, sqlcgen.ListClassRosterAtDateParams{
		TutorID:      tutorID,
		ClassID:      classID,
		ResolvedDate: pgtype.Date{Time: validated.changeDate, Valid: true},
	})
	if err != nil {
		return ClassRoster{}, fmt.Errorf("read changed roster: %w", err)
	}
	result := classRosterFromRows(
		ClassSummary{ClassID: classRow.ClassID, Name: classRow.Name, Color: classRow.Color},
		validated.changeDate,
		today,
		rows,
	)
	if err := completeRosterReceipt(ctx, queries, tutorID, idempotencyKey, result); err != nil {
		return ClassRoster{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ClassRoster{}, fmt.Errorf("commit roster change: %w", err)
	}
	h.logger.InfoContext(ctx, "Class roster changed",
		slog.String("request_id", vermouth.RequestID(ctx)),
		slog.String("command", operationChangeRoster),
		slog.String("class_id", classID.String()),
		slog.Int("addition_count", len(validated.additions)),
		slog.Int("removal_count", len(validated.removals)),
	)
	return result, nil
}

func validateRosterChange(input ChangeRosterInput, today time.Time) (validatedRosterChange, error) {
	changeDate, err := parseDate("change_date", input.ChangeDate)
	if err != nil {
		return validatedRosterChange{}, err
	}
	if changeDate.After(today) {
		return validatedRosterChange{}, &ValidationError{
			Field:   "change_date",
			Message: "must be today or earlier",
		}
	}
	if len(input.Additions)+len(input.Removals) == 0 {
		return validatedRosterChange{}, &ValidationError{
			Field: validationFieldBody, Message: "must contain a roster change",
		}
	}
	if len(input.Additions)+len(input.Removals) > maxRosterChanges {
		return validatedRosterChange{}, &ValidationError{
			Field: validationFieldBody, Message: "must change at most 200 students",
		}
	}
	additions := slices.Clone(input.Additions)
	removals := slices.Clone(input.Removals)
	sortUUIDs(additions)
	sortUUIDs(removals)
	if duplicateUUID(additions) || duplicateUUID(removals) {
		return validatedRosterChange{}, &ValidationError{
			Field: validationFieldBody, Message: "student identifiers must be unique",
		}
	}
	additionSet := make(map[uuid.UUID]struct{}, len(additions))
	for _, studentID := range additions {
		additionSet[studentID] = struct{}{}
	}
	for _, studentID := range removals {
		if _, exists := additionSet[studentID]; exists {
			return validatedRosterChange{}, &ValidationError{
				Field:   validationFieldBody,
				Message: "addition and removal identifiers must be disjoint",
			}
		}
	}
	allIDs := append(slices.Clone(additions), removals...)
	sortUUIDs(allIDs)
	return validatedRosterChange{
		changeDate: changeDate,
		additions:  additions,
		removals:   removals,
		allIDs:     allIDs,
	}, nil
}

func rosterAdditionConflicts(periods []sqlcgen.RosterPeriod, changeDate time.Time) bool {
	for _, period := range periods {
		if !period.EffectiveTo.Valid || !period.EffectiveTo.Time.Before(changeDate) {
			return true
		}
	}
	return false
}

func coveredRosterPeriod(
	periods []sqlcgen.RosterPeriod,
	changeDate time.Time,
) (sqlcgen.RosterPeriod, bool) {
	for _, period := range periods {
		if period.EffectiveFrom.Time.After(changeDate) {
			continue
		}
		if !period.EffectiveTo.Valid || !changeDate.After(period.EffectiveTo.Time) {
			return period, true
		}
	}
	return sqlcgen.RosterPeriod{}, false
}

func rosterConflict(
	ctx context.Context,
	queries *sqlcgen.Queries,
	tutorID uuid.UUID,
	classSummary ClassSummary,
	resolvedDate time.Time,
) error {
	rows, err := queries.ListClassRosterAtDate(ctx, sqlcgen.ListClassRosterAtDateParams{
		TutorID:      tutorID,
		ClassID:      classSummary.ClassID,
		ResolvedDate: pgtype.Date{Time: resolvedDate, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("read roster conflict recovery: %w", err)
	}
	students := make([]RosterConflictStudent, 0, len(rows))
	for _, row := range rows {
		student := rosterStudentFromStored(row, false)
		students = append(students, RosterConflictStudent{
			StudentID:     student.StudentID,
			Name:          student.Name,
			Archived:      student.Archived,
			EffectiveFrom: student.EffectiveFrom,
			EffectiveTo:   student.EffectiveTo,
		})
	}
	return &ConflictError{
		Code:    "roster_conflict",
		Message: "the roster changed or the requested dates conflict with retained history",
		Details: RosterConflictDetails{
			Class:        classSummary,
			ResolvedDate: resolvedDate.Format(dateLayout),
			Students:     students,
		},
	}
}

func classRosterFromRows(
	classSummary ClassSummary,
	resolvedDate time.Time,
	today time.Time,
	rows []sqlcgen.ListClassRosterAtDateRow,
) ClassRoster {
	current := resolvedDate.Equal(today)
	students := make([]RosterStudent, 0, len(rows))
	for _, row := range rows {
		students = append(students, rosterStudentFromStored(row, current))
	}
	return ClassRoster{
		Class:        classSummary,
		ResolvedDate: resolvedDate.Format(dateLayout),
		Students:     students,
	}
}

//nolint:revive // The disclosure flag is the privacy rule this mapper owns.
func rosterStudentFromStored(row sqlcgen.ListClassRosterAtDateRow, current bool) RosterStudent {
	var effectiveTo *string
	if row.EffectiveTo.Valid {
		value := row.EffectiveTo.Time.Format(dateLayout)
		effectiveTo = &value
	}
	archived := row.RemovedAt.Valid
	var phone *string
	if current && !archived {
		phone = textPointer(row.Phone)
	}
	return RosterStudent{
		StudentID:     row.StudentID,
		Name:          row.Name,
		Phone:         phone,
		Archived:      archived,
		EffectiveFrom: row.EffectiveFrom.Time.Format(dateLayout),
		EffectiveTo:   effectiveTo,
	}
}

func rosterChangeRequestHash(
	classID uuid.UUID,
	value validatedRosterChange,
) ([sha256.Size]byte, error) {
	encoded, err := json.Marshal(struct {
		Operation  string      `json:"operation"`
		ClassID    uuid.UUID   `json:"class_id"`
		ChangeDate string      `json:"change_date"`
		Additions  []uuid.UUID `json:"additions"`
		Removals   []uuid.UUID `json:"removals"`
	}{
		Operation:  operationChangeRoster,
		ClassID:    classID,
		ChangeDate: value.changeDate.Format(dateLayout),
		Additions:  value.additions,
		Removals:   value.removals,
	})
	if err != nil {
		return [sha256.Size]byte{}, fmt.Errorf("encode roster command hash: %w", err)
	}
	return sha256.Sum256(encoded), nil
}

func (h *Handler) replayRosterSnapshot(
	ctx context.Context,
	tutorID uuid.UUID,
	idempotencyKey string,
	requestHash []byte,
) (ClassRoster, bool, error) {
	receipt, err := store.Queries(h.pool).GetCommandReceipt(ctx, sqlcgen.GetCommandReceiptParams{
		TutorID:        tutorID,
		Operation:      operationChangeRoster,
		IdempotencyKey: idempotencyKey,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ClassRoster{}, false, nil
	}
	if err != nil {
		return ClassRoster{}, false, fmt.Errorf("read roster receipt: %w", err)
	}
	if !bytes.Equal(receipt.RequestHash, requestHash) {
		return ClassRoster{}, true, ErrIdempotencyConflict
	}
	var result ClassRoster
	if err := json.Unmarshal(receipt.ResponseSnapshot, &result); err != nil {
		return ClassRoster{}, true, fmt.Errorf("decode roster receipt: %w", err)
	}
	return result, true, nil
}

func completeRosterReceipt(
	ctx context.Context,
	queries *sqlcgen.Queries,
	tutorID uuid.UUID,
	idempotencyKey string,
	result ClassRoster,
) error {
	snapshot, err := jsonSnapshot(result)
	if err != nil {
		return err
	}
	_, err = queries.CompleteCommandReceipt(ctx, sqlcgen.CompleteCommandReceiptParams{
		TutorID:          tutorID,
		Operation:        operationChangeRoster,
		IdempotencyKey:   idempotencyKey,
		ResponseSnapshot: snapshot,
		ResponseStatus:   pgtype.Int4{Int32: http.StatusOK, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("complete roster receipt: %w", err)
	}
	return nil
}

func sortUUIDs(values []uuid.UUID) {
	slices.SortFunc(values, func(left, right uuid.UUID) int {
		return cmp.Compare(left.String(), right.String())
	})
}

func duplicateUUID(values []uuid.UUID) bool {
	for index := 1; index < len(values); index++ {
		if values[index] == values[index-1] {
			return true
		}
	}
	return false
}
