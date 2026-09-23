package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
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

const operationPutSchedule = "put_schedule"

var errIncompletePutScheduleReceipt = errors.New("put schedule receipt is incomplete")

type commandContext struct {
	CommandTime            time.Time `json:"command_time"`
	GoverningTimeZone      string    `json:"governing_time_zone"`
	RequestDisplayTimeZone string    `json:"request_display_time_zone"`
}

func jsonSnapshot(value any) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode command snapshot: %w", err)
	}
	return encoded, nil
}

// PutScheduleInput creates the first retained weekly rule for an owned class.
type PutScheduleInput struct {
	ExpectedRevision int64                     `json:"expected_revision"`
	EffectiveFrom    string                    `json:"effective_from"`
	ValidThrough     string                    `json:"valid_through"`
	Slots            []WeeklyScheduleSlotInput `json:"slots"`
}

// PutScheduleResult is the committed rule and its authoritative occurrence counts.
type PutScheduleResult struct {
	Class           Class               `json:"class"`
	Rule            ScheduleRuleSummary `json:"rule"`
	FirstSession    *Session            `json:"first_session"`
	CandidateCount  int                 `json:"candidate_count"`
	CreatedCount    int                 `json:"created_count"`
	AdoptedCount    int                 `json:"adopted_count"`
	SupersededCount int                 `json:"superseded_count"`
	PreservedCount  int                 `json:"preserved_count"`
}

// PutSchedule locks the class and creates its first weekly rule in one transaction.
// It adopts an exact active standalone session without publishing a duplicate fact.
//
//nolint:funlen,gocognit,maintidx // One linear transaction keeps the lock, receipt, rows, events, and counts auditable.
func (h *Handler) PutSchedule(
	ctx context.Context,
	tutorID uuid.UUID,
	classID uuid.UUID,
	timezone string,
	idempotencyKey string,
	input PutScheduleInput,
) (PutScheduleResult, int, error) {
	err := validateIdempotencyKey(idempotencyKey)
	if err != nil {
		return PutScheduleResult{}, 0, err
	}
	requestHash, err := putScheduleRequestHash(classID, input)
	if err != nil {
		return PutScheduleResult{}, 0, err
	}
	result, status, found, replayErr := h.replayPutSchedule(
		ctx, tutorID, idempotencyKey, requestHash[:],
	)
	if found || replayErr != nil {
		return result, status, replayErr
	}
	if input.ExpectedRevision > 0 {
		return h.replaceSchedule(
			ctx, tutorID, classID, timezone, idempotencyKey, input, requestHash[:],
		)
	}
	commandTime := h.now().UTC()
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return PutScheduleResult{}, 0, fmt.Errorf("load request timezone: %w", err)
	}
	validated, err := validateWeeklySchedule(WeeklyScheduleInput{
		ValidFrom: input.EffectiveFrom, ValidThrough: input.ValidThrough, Slots: input.Slots,
	}, location, commandTime)
	if err != nil {
		return PutScheduleResult{}, 0, err
	}
	if input.ExpectedRevision < 0 {
		return PutScheduleResult{}, 0, &ValidationError{
			Field: "expected_revision", Message: "must be zero or greater",
		}
	}
	ruleID, err := uuid.NewV7()
	if err != nil {
		return PutScheduleResult{}, 0, fmt.Errorf("generate schedule rule id: %w", err)
	}
	sessionIDs := make([]uuid.UUID, len(validated.occurrences))
	for index := range sessionIDs {
		sessionIDs[index], err = uuid.NewV7()
		if err != nil {
			return PutScheduleResult{}, 0, fmt.Errorf("generate session id: %w", err)
		}
	}
	contextSnapshot, err := jsonSnapshot(commandContext{
		CommandTime: commandTime, GoverningTimeZone: timezone, RequestDisplayTimeZone: timezone,
	})
	if err != nil {
		return PutScheduleResult{}, 0, err
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return PutScheduleResult{}, 0, fmt.Errorf("begin put schedule: %w", err)
	}
	defer h.rollback(ctx, tx)
	queries := store.Queries(tx)
	classRow, err := queries.LockOwnedClass(ctx, sqlcgen.LockOwnedClassParams{
		TutorID: tutorID, ClassID: classID,
	})
	if err != nil {
		return PutScheduleResult{}, 0, ownedReadError("lock schedule class", err)
	}
	_, err = queries.InsertCommandReceipt(ctx, sqlcgen.InsertCommandReceiptParams{
		TutorID: tutorID, Operation: operationPutSchedule, IdempotencyKey: idempotencyKey,
		RequestHash: requestHash[:], PrimaryResourceID: classID,
		RelatedResourceID: pgtype.UUID{Bytes: ruleID, Valid: true}, ContextSnapshot: contextSnapshot,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		h.rollback(ctx, tx)
		result, status, _, replayErr = h.replayPutSchedule(
			ctx, tutorID, idempotencyKey, requestHash[:],
		)
		return result, status, replayErr
	}
	if err != nil {
		return PutScheduleResult{}, 0, fmt.Errorf("claim put schedule receipt: %w", err)
	}
	if classRow.ScheduleRevision != input.ExpectedRevision || classRow.ScheduleRevision != 0 {
		return PutScheduleResult{}, 0, staleScheduleError(classID, classRow.ScheduleRevision, nil)
	}
	nextRevision := classRow.ScheduleRevision + 1
	_, err = queries.InsertScheduleRule(ctx, sqlcgen.InsertScheduleRuleParams{
		ScheduleRuleID: ruleID, TutorID: tutorID, ClassID: classID, Revision: nextRevision,
		ValidFrom:    pgtype.Date{Time: validated.validFrom, Valid: true},
		ValidThrough: pgtype.Date{Time: validated.validThrough, Valid: true}, TimeZone: timezone,
	})
	if err != nil {
		return PutScheduleResult{}, 0, fmt.Errorf("insert first schedule rule: %w", err)
	}
	for _, slot := range validated.slots {
		_, err = queries.InsertScheduleSlot(ctx, sqlcgen.InsertScheduleSlotParams{
			ScheduleRuleID: ruleID, TutorID: tutorID, Weekday: slot.Weekday,
			StartTime: slot.StartTime, EndTime: slot.EndTime,
		})
		if err != nil {
			return PutScheduleResult{}, 0, fmt.Errorf("insert first schedule slot: %w", err)
		}
	}
	createdCount := 0
	adoptedCount := 0
	for index := range validated.occurrences {
		occurrence := &validated.occurrences[index]
		match, matchErr := queries.FindAdoptableStandaloneSession(ctx, sqlcgen.FindAdoptableStandaloneSessionParams{
			TutorID: tutorID, ClassID: classID, StartsAt: occurrence.startsAt, EndsAt: occurrence.endsAt,
			LocalDate: pgtype.Date{Time: occurrence.localDate, Valid: true},
		})
		if matchErr == nil {
			_, err = queries.AdoptStandaloneSession(ctx, sqlcgen.AdoptStandaloneSessionParams{
				ScheduleRuleID:  pgtype.UUID{Bytes: ruleID, Valid: true},
				OriginLocalDate: pgtype.Date{Time: occurrence.originLocalDate, Valid: true},
				UpdatedAt:       commandTime, TutorID: tutorID, ClassID: classID, SessionID: match.SessionID,
			})
			if err != nil {
				return PutScheduleResult{}, 0, fmt.Errorf("adopt standalone session: %w", err)
			}
			adoptedCount++
			continue
		}
		if !errors.Is(matchErr, pgx.ErrNoRows) {
			return PutScheduleResult{}, 0, fmt.Errorf("find adoptable standalone session: %w", matchErr)
		}
		row, insertErr := queries.InsertSession(ctx, sqlcgen.InsertSessionParams{
			SessionID: sessionIDs[index], ClassID: classID, TutorID: tutorID,
			StartsAt: occurrence.startsAt, EndsAt: occurrence.endsAt,
			LocalDate:       pgtype.Date{Time: occurrence.localDate, Valid: true},
			ScheduleRuleID:  pgtype.UUID{Bytes: ruleID, Valid: true},
			OriginLocalDate: pgtype.Date{Time: occurrence.originLocalDate, Valid: true},
		})
		if insertErr != nil {
			if constraintConflict(insertErr) {
				h.rollback(ctx, tx)
				return PutScheduleResult{}, 0, h.committedOverlapError(
					ctx, tutorID, uuid.Nil, occurrence.startsAt, occurrence.endsAt, timezone,
				)
			}
			return PutScheduleResult{}, 0, fmt.Errorf("insert scheduled session: %w", insertErr)
		}
		createdCount++
		writeErr := h.writeEvent(ctx, tx, teachingEvent{
			name:       vermouth.EventSessionScheduled,
			tutorID:    tutorID,
			key:        vermouth.Key{Kind: vermouth.KeySessionID, Value: row.SessionID},
			occurredAt: commandTime,
			fields: sessionScheduledFields{
				SessionID: row.SessionID, ClassID: classID, TutorID: tutorID,
				StartsAt: row.StartsAt, EndsAt: row.EndsAt,
				LocalDate: row.LocalDate.Time.Format(dateLayout),
			},
		})
		if writeErr != nil {
			return PutScheduleResult{}, 0, writeErr
		}
	}
	updatedClass, err := queries.SetClassScheduleRevision(ctx, sqlcgen.SetClassScheduleRevisionParams{
		TutorID: tutorID, ClassID: classID, ScheduleRevision: nextRevision, UpdatedAt: commandTime,
	})
	if err != nil {
		return PutScheduleResult{}, 0, fmt.Errorf("increment class schedule revision: %w", err)
	}
	result = PutScheduleResult{
		Class: Class{
			ClassID: updatedClass.ClassID, Name: updatedClass.Name, Color: updatedClass.Color,
			RateAmount: updatedClass.RateAmount, Currency: updatedClass.Currency,
			RateEffectiveFrom: updatedClass.RateEffectiveFrom.Time.Format(dateLayout),
			ScheduleRevision:  updatedClass.ScheduleRevision,
			RateRevision:      updatedClass.RateRevision,
		},
		Rule: ScheduleRuleSummary{
			ScheduleRuleID: ruleID, ClassID: classID, Revision: nextRevision,
			ValidFrom:    validated.validFrom.Format(dateLayout),
			ValidThrough: validated.validThrough.Format(dateLayout), TimeZone: timezone,
			State: ruleState(validated.validFrom, validated.validThrough, timezone, nil, nil, nil, commandTime),
			Slots: validated.slots,
		},
		CandidateCount: len(validated.occurrences), CreatedCount: createdCount,
		AdoptedCount: adoptedCount,
	}
	first, firstErr := queries.GetFirstUpcomingClassSession(ctx, sqlcgen.GetFirstUpcomingClassSessionParams{
		TutorID: tutorID, ClassID: classID, CommandTime: commandTime,
	})
	if firstErr == nil {
		value := sessionFromValues(
			first.SessionID, first.ClassID, first.StartsAt, first.EndsAt, first.LocalDate.Time,
		)
		result.FirstSession = &value
	} else if !errors.Is(firstErr, pgx.ErrNoRows) {
		return PutScheduleResult{}, 0, fmt.Errorf("read first upcoming class session: %w", firstErr)
	}
	responseSnapshot, err := jsonSnapshot(result)
	if err != nil {
		return PutScheduleResult{}, 0, err
	}
	_, err = queries.CompleteCommandReceipt(ctx, sqlcgen.CompleteCommandReceiptParams{
		TutorID: tutorID, Operation: operationPutSchedule, IdempotencyKey: idempotencyKey,
		ResponseSnapshot: responseSnapshot, ResponseStatus: pgtype.Int4{Int32: http.StatusCreated, Valid: true},
	})
	if err != nil {
		return PutScheduleResult{}, 0, fmt.Errorf("complete put schedule receipt: %w", err)
	}
	err = tx.Commit(ctx)
	if err != nil {
		return PutScheduleResult{}, 0, fmt.Errorf("commit put schedule: %w", err)
	}
	return result, http.StatusCreated, nil
}

func (h *Handler) replayPutSchedule(
	ctx context.Context,
	tutorID uuid.UUID,
	idempotencyKey string,
	requestHash []byte,
) (PutScheduleResult, int, bool, error) {
	receipt, err := store.Queries(h.pool).GetCommandReceipt(ctx, sqlcgen.GetCommandReceiptParams{
		TutorID: tutorID, Operation: operationPutSchedule, IdempotencyKey: idempotencyKey,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return PutScheduleResult{}, 0, false, nil
	}
	if err != nil {
		return PutScheduleResult{}, 0, false, fmt.Errorf("read put schedule receipt: %w", err)
	}
	if !bytes.Equal(receipt.RequestHash, requestHash) {
		return PutScheduleResult{}, 0, true, ErrIdempotencyConflict
	}
	if len(receipt.ResponseSnapshot) == 0 || !receipt.ResponseStatus.Valid {
		return PutScheduleResult{}, 0, true, errIncompletePutScheduleReceipt
	}
	var result PutScheduleResult
	err = json.Unmarshal(receipt.ResponseSnapshot, &result)
	if err != nil {
		return PutScheduleResult{}, 0, true, fmt.Errorf("decode put schedule receipt response: %w", err)
	}
	return result, int(receipt.ResponseStatus.Int32), true, nil
}

func putScheduleRequestHash(classID uuid.UUID, input PutScheduleInput) ([sha256.Size]byte, error) {
	slots := slices.Clone(input.Slots)
	slices.SortFunc(slots, func(left, right WeeklyScheduleSlotInput) int {
		return int(left.Weekday - right.Weekday)
	})
	canonical := struct {
		ClassID          uuid.UUID                 `json:"class_id"`
		ExpectedRevision int64                     `json:"expected_revision"`
		EffectiveFrom    string                    `json:"effective_from"`
		ValidThrough     string                    `json:"valid_through"`
		Slots            []WeeklyScheduleSlotInput `json:"slots"`
	}{
		ClassID: classID, ExpectedRevision: input.ExpectedRevision,
		EffectiveFrom: input.EffectiveFrom, ValidThrough: input.ValidThrough, Slots: slots,
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return [sha256.Size]byte{}, fmt.Errorf("encode put schedule command hash: %w", err)
	}
	return sha256.Sum256(encoded), nil
}
