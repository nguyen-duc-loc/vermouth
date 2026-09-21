package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"

	"github.com/nguyen-duc-loc/vermouth/services/teaching/internal/store"
	"github.com/nguyen-duc-loc/vermouth/services/teaching/internal/store/sqlcgen"
)

const operationEndSchedule = "end_schedule"

var errIncompleteCommandReceipt = errors.New("command receipt is incomplete")

// EndScheduleInput closes or retires the latest retained weekly rule.
type EndScheduleInput struct {
	ExpectedRevision int64  `json:"expected_revision"`
	LastDate         string `json:"last_date"`
}

// EndScheduleResult reports the retained rule and exact affected session counts.
type EndScheduleResult struct {
	Class           Class               `json:"class"`
	Rule            ScheduleRuleSummary `json:"rule"`
	SupersededCount int                 `json:"superseded_count"`
	PreservedCount  int                 `json:"preserved_count"`
}

func staleScheduleError(classID uuid.UUID, revision int64, latest *ScheduleRuleSummary) error {
	return &ConflictError{
		Code: "stale_schedule", Message: "the class schedule changed; refresh before trying again",
		Details: map[string]any{
			"class_id": classID, "schedule_revision": revision, "latest_rule": latest,
		},
	}
}

func invalidScheduleStateError(classID uuid.UUID, revision int64, state string) error {
	return &ConflictError{
		Code: "invalid_schedule_state", Message: "the schedule cannot make that transition",
		Details: map[string]any{
			"class_id": classID, "schedule_revision": revision, "latest_rule_state": state,
			"allowed_actions": []string{"replace", "end"},
		},
	}
}

func (h *Handler) replayCommand(
	ctx context.Context,
	tutorID uuid.UUID,
	operation string,
	idempotencyKey string,
	requestHash []byte,
	target any,
) (int, bool, error) {
	receipt, err := store.Queries(h.pool).GetCommandReceipt(ctx, sqlcgen.GetCommandReceiptParams{
		TutorID: tutorID, Operation: operation, IdempotencyKey: idempotencyKey,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("read %s receipt: %w", operation, err)
	}
	if !bytes.Equal(receipt.RequestHash, requestHash) {
		return 0, true, &ConflictError{
			Code: "idempotency_conflict", Message: "the idempotency key names different input",
			Details: map[string]any{"operation": operation, "resource_id": receipt.PrimaryResourceID},
		}
	}
	if len(receipt.ResponseSnapshot) == 0 || !receipt.ResponseStatus.Valid {
		return 0, true, fmt.Errorf("%s: %w", operation, errIncompleteCommandReceipt)
	}
	err = json.Unmarshal(receipt.ResponseSnapshot, target)
	if err != nil {
		return 0, true, fmt.Errorf("decode %s receipt response: %w", operation, err)
	}
	return int(receipt.ResponseStatus.Int32), true, nil
}

func commandHash(resourceID uuid.UUID, input any) ([sha256.Size]byte, error) {
	canonical := struct {
		ResourceID uuid.UUID `json:"resource_id"`
		Input      any       `json:"input"`
	}{ResourceID: resourceID, Input: input}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return [sha256.Size]byte{}, fmt.Errorf("encode command hash: %w", err)
	}
	return sha256.Sum256(encoded), nil
}

func classFromLocked(row sqlcgen.LockOwnedClassRow) Class {
	return Class{
		ClassID: row.ClassID, Name: row.Name, Color: row.Color, RateAmount: row.RateAmount,
		Currency: row.Currency, RateEffectiveFrom: row.RateEffectiveFrom.Time.Format(dateLayout),
		ScheduleRevision: row.ScheduleRevision,
	}
}

func ruleSummaryFromStored(
	rule sqlcgen.ScheduleRule,
	slots []WeeklyScheduleSlotInput,
	commandTime time.Time,
) ScheduleRuleSummary {
	return ScheduleRuleSummary{
		ScheduleRuleID: rule.ScheduleRuleID, ClassID: rule.ClassID, Revision: rule.Revision,
		ValidFrom:    rule.ValidFrom.Time.Format(dateLayout),
		ValidThrough: rule.ValidThrough.Time.Format(dateLayout), TimeZone: rule.TimeZone,
		State: ruleState(
			rule.ValidFrom.Time, rule.ValidThrough.Time, rule.TimeZone,
			timestampPointer(rule.ReplacedAt), timestampPointer(rule.EndedAt),
			timestampPointer(rule.RetiredAt), commandTime,
		),
		Slots: slots, ReplacedAt: timestampPointer(rule.ReplacedAt),
		EndedAt: timestampPointer(rule.EndedAt), RetiredAt: timestampPointer(rule.RetiredAt),
	}
}

//nolint:funlen,gocognit,maintidx,nestif,gocritic,noinlineerr // The complete replacement transaction is intentionally visible in one place.
func (h *Handler) replaceSchedule(
	ctx context.Context,
	tutorID uuid.UUID,
	classID uuid.UUID,
	timezone string,
	idempotencyKey string,
	input PutScheduleInput,
	requestHash []byte,
) (PutScheduleResult, int, error) {
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
	if validated.validFrom.Before(localCalendarDate(commandTime, location)) {
		return PutScheduleResult{}, 0, &ValidationError{
			Field: "effective_from", Message: "must be today or a future local date",
		}
	}
	ruleID, err := uuid.NewV7()
	if err != nil {
		return PutScheduleResult{}, 0, fmt.Errorf("generate replacement rule id: %w", err)
	}
	contextSnapshot, err := jsonSnapshot(commandContext{
		CommandTime: commandTime, GoverningTimeZone: timezone, RequestDisplayTimeZone: timezone,
	})
	if err != nil {
		return PutScheduleResult{}, 0, err
	}
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return PutScheduleResult{}, 0, fmt.Errorf("begin replace schedule: %w", err)
	}
	defer h.rollback(ctx, tx)
	queries := store.Queries(tx)
	classRow, err := queries.LockOwnedClass(ctx, sqlcgen.LockOwnedClassParams{
		TutorID: tutorID, ClassID: classID,
	})
	if err != nil {
		return PutScheduleResult{}, 0, ownedReadError("lock schedule class", err)
	}
	rules, err := queries.ListOwnedScheduleRulesForUpdate(
		ctx, sqlcgen.ListOwnedScheduleRulesForUpdateParams{TutorID: tutorID, ClassID: classID},
	)
	if err != nil {
		return PutScheduleResult{}, 0, fmt.Errorf("lock schedule rules: %w", err)
	}
	if classRow.ScheduleRevision != input.ExpectedRevision {
		return PutScheduleResult{}, 0, staleScheduleError(classID, classRow.ScheduleRevision, nil)
	}
	if len(rules) == 0 {
		return PutScheduleResult{}, 0, invalidScheduleStateError(classID, classRow.ScheduleRevision, "absent")
	}
	_, err = queries.InsertCommandReceipt(ctx, sqlcgen.InsertCommandReceiptParams{
		TutorID: tutorID, Operation: operationPutSchedule, IdempotencyKey: idempotencyKey,
		RequestHash: requestHash, PrimaryResourceID: classID,
		RelatedResourceID: pgtype.UUID{Bytes: ruleID, Valid: true}, ContextSnapshot: contextSnapshot,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		h.rollback(ctx, tx)
		var replay PutScheduleResult
		status, found, replayErr := h.replayCommand(
			ctx, tutorID, operationPutSchedule, idempotencyKey, requestHash, &replay,
		)
		if found || replayErr != nil {
			return replay, status, replayErr
		}
	}
	if err != nil {
		return PutScheduleResult{}, 0, fmt.Errorf("claim replace schedule receipt: %w", err)
	}

	latest := rules[0]
	affected := false
	if !latest.RetiredAt.Valid && !validated.validFrom.After(latest.ValidThrough.Time) {
		affected = true
		if !validated.validFrom.After(latest.ValidFrom.Time) {
			_, err = queries.RetireScheduleRule(ctx, sqlcgen.RetireScheduleRuleParams{
				TutorID: tutorID, ClassID: classID, ScheduleRuleID: latest.ScheduleRuleID,
				RetiredAt: pgtype.Timestamptz{Time: commandTime, Valid: true},
			})
			if err != nil {
				return PutScheduleResult{}, 0, fmt.Errorf("retire future schedule rule: %w", err)
			}
			for index := 1; index < len(rules); index++ {
				predecessor := &rules[index]
				if predecessor.RetiredAt.Valid || predecessor.ValidThrough.Time.Before(validated.validFrom) {
					continue
				}
				_, err = queries.ReplaceScheduleRule(ctx, sqlcgen.ReplaceScheduleRuleParams{
					TutorID: tutorID, ClassID: classID, ScheduleRuleID: predecessor.ScheduleRuleID,
					ValidThrough: pgtype.Date{Time: validated.validFrom.AddDate(0, 0, -1), Valid: true},
					ReplacedAt:   pgtype.Timestamptz{Time: commandTime, Valid: true},
				})
				if err != nil {
					return PutScheduleResult{}, 0, fmt.Errorf("close predecessor schedule rule: %w", err)
				}
				break
			}
		} else {
			_, err = queries.ReplaceScheduleRule(ctx, sqlcgen.ReplaceScheduleRuleParams{
				TutorID: tutorID, ClassID: classID, ScheduleRuleID: latest.ScheduleRuleID,
				ValidThrough: pgtype.Date{Time: validated.validFrom.AddDate(0, 0, -1), Valid: true},
				ReplacedAt:   pgtype.Timestamptz{Time: commandTime, Valid: true},
			})
			if err != nil {
				return PutScheduleResult{}, 0, fmt.Errorf("close replaced schedule rule: %w", err)
			}
		}
	}

	exceptions, err := queries.ListRetainedScheduleExceptions(ctx, sqlcgen.ListRetainedScheduleExceptionsParams{
		TutorID: tutorID, ClassID: classID,
		OriginLocalDate: pgtype.Date{Time: validated.validFrom, Valid: true},
	})
	if err != nil {
		return PutScheduleResult{}, 0, fmt.Errorf("read retained schedule exceptions: %w", err)
	}
	suppressed := make(map[string]struct{}, len(exceptions))
	for _, exception := range exceptions {
		suppressed[exception.OriginLocalDate.Time.Format(dateLayout)] = struct{}{}
	}
	var superseded []sqlcgen.SupersedeUntouchedScheduleSessionsRow
	if affected {
		superseded, err = queries.SupersedeUntouchedScheduleSessions(
			ctx, sqlcgen.SupersedeUntouchedScheduleSessionsParams{
				CommandTime: pgtype.Timestamptz{Time: commandTime, Valid: true},
				TutorID:     tutorID, ClassID: classID,
				AffectedFrom: pgtype.Date{Time: validated.validFrom, Valid: true},
			},
		)
		if err != nil {
			return PutScheduleResult{}, 0, fmt.Errorf("replace untouched schedule sessions: %w", err)
		}
	}
	nextRevision := classRow.ScheduleRevision + 1
	newRule, err := queries.InsertScheduleRule(ctx, sqlcgen.InsertScheduleRuleParams{
		ScheduleRuleID: ruleID, TutorID: tutorID, ClassID: classID, Revision: nextRevision,
		ValidFrom:    pgtype.Date{Time: validated.validFrom, Valid: true},
		ValidThrough: pgtype.Date{Time: validated.validThrough, Valid: true}, TimeZone: timezone,
	})
	if err != nil {
		return PutScheduleResult{}, 0, fmt.Errorf("insert replacement schedule rule: %w", err)
	}
	for _, slot := range validated.slots {
		_, err = queries.InsertScheduleSlot(ctx, sqlcgen.InsertScheduleSlotParams{
			ScheduleRuleID: ruleID, TutorID: tutorID, Weekday: slot.Weekday,
			StartTime: slot.StartTime, EndTime: slot.EndTime,
		})
		if err != nil {
			return PutScheduleResult{}, 0, fmt.Errorf("insert replacement schedule slot: %w", err)
		}
	}
	createdCount, adoptedCount := 0, 0
	for index := range validated.occurrences {
		occurrence := &validated.occurrences[index]
		if _, skip := suppressed[occurrence.originLocalDate.Format(dateLayout)]; skip {
			continue
		}
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
				return PutScheduleResult{}, 0, fmt.Errorf("adopt replacement occurrence: %w", err)
			}
			adoptedCount++
			continue
		}
		if !errors.Is(matchErr, pgx.ErrNoRows) {
			return PutScheduleResult{}, 0, fmt.Errorf("find replacement adoption: %w", matchErr)
		}
		sessionID, idErr := uuid.NewV7()
		if idErr != nil {
			return PutScheduleResult{}, 0, fmt.Errorf("generate replacement session id: %w", idErr)
		}
		row, insertErr := queries.InsertSession(ctx, sqlcgen.InsertSessionParams{
			SessionID: sessionID, ClassID: classID, TutorID: tutorID,
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
			return PutScheduleResult{}, 0, fmt.Errorf("insert replacement occurrence: %w", insertErr)
		}
		createdCount++
		if err = h.writeEvent(ctx, tx, teachingEvent{
			name:       vermouth.EventSessionScheduled,
			tutorID:    tutorID,
			key:        vermouth.Key{Kind: vermouth.KeySessionID, Value: row.SessionID},
			occurredAt: commandTime,
			fields: sessionScheduledFields{
				SessionID: row.SessionID, ClassID: classID, TutorID: tutorID,
				StartsAt: row.StartsAt, EndsAt: row.EndsAt,
				LocalDate: row.LocalDate.Time.Format(dateLayout),
			},
		}); err != nil {
			return PutScheduleResult{}, 0, err
		}
	}
	for index := range superseded {
		row := &superseded[index]
		if err = h.writeSessionCancelledEvent(
			ctx, tx, tutorID, row.SessionID, row.ClassID, commandTime,
		); err != nil {
			return PutScheduleResult{}, 0, err
		}
	}
	updatedClass, err := queries.SetClassScheduleRevision(ctx, sqlcgen.SetClassScheduleRevisionParams{
		TutorID: tutorID, ClassID: classID, ScheduleRevision: nextRevision, UpdatedAt: commandTime,
	})
	if err != nil {
		return PutScheduleResult{}, 0, fmt.Errorf("increment replacement schedule revision: %w", err)
	}
	result := PutScheduleResult{
		Class:          classFromLocked(sqlcgen.LockOwnedClassRow(updatedClass)),
		Rule:           ruleSummaryFromStored(newRule, validated.slots, commandTime),
		CandidateCount: len(validated.occurrences), CreatedCount: createdCount,
		AdoptedCount: adoptedCount, SupersededCount: len(superseded),
		PreservedCount: len(exceptions),
	}
	first, firstErr := queries.GetFirstUpcomingClassSession(ctx, sqlcgen.GetFirstUpcomingClassSessionParams{
		TutorID: tutorID, ClassID: classID, CommandTime: commandTime,
	})
	if firstErr == nil {
		value := sessionFromValues(first.SessionID, first.ClassID, first.StartsAt, first.EndsAt, first.LocalDate.Time)
		result.FirstSession = &value
	} else if !errors.Is(firstErr, pgx.ErrNoRows) {
		return PutScheduleResult{}, 0, fmt.Errorf("read replacement first session: %w", firstErr)
	}
	if err = completeCommand(ctx, queries, tutorID, operationPutSchedule, idempotencyKey, result); err != nil {
		return PutScheduleResult{}, 0, err
	}
	if err = tx.Commit(ctx); err != nil {
		return PutScheduleResult{}, 0, fmt.Errorf("commit replace schedule: %w", err)
	}
	return result, http.StatusOK, nil
}

// EndSchedule closes or retires the latest rule without creating a replacement.
//
//nolint:funlen,gocognit,maintidx,nestif,gocritic,noinlineerr // The rule transition, sessions, receipt, and events share one transaction.
func (h *Handler) EndSchedule(
	ctx context.Context,
	tutorID uuid.UUID,
	classID uuid.UUID,
	idempotencyKey string,
	input EndScheduleInput,
) (EndScheduleResult, int, error) {
	err := validateIdempotencyKey(idempotencyKey)
	if err != nil {
		return EndScheduleResult{}, 0, err
	}
	hash, err := commandHash(classID, input)
	if err != nil {
		return EndScheduleResult{}, 0, err
	}
	var replay EndScheduleResult
	status, found, replayErr := h.replayCommand(
		ctx, tutorID, operationEndSchedule, idempotencyKey, hash[:], &replay,
	)
	if found || replayErr != nil {
		return replay, status, replayErr
	}
	if input.ExpectedRevision < 0 {
		return EndScheduleResult{}, 0, &ValidationError{
			Field: "expected_revision", Message: "must be zero or greater",
		}
	}
	commandTime := h.now().UTC()
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return EndScheduleResult{}, 0, fmt.Errorf("begin end schedule: %w", err)
	}
	defer h.rollback(ctx, tx)
	queries := store.Queries(tx)
	classRow, err := queries.LockOwnedClass(ctx, sqlcgen.LockOwnedClassParams{TutorID: tutorID, ClassID: classID})
	if err != nil {
		return EndScheduleResult{}, 0, ownedReadError("lock end schedule class", err)
	}
	rules, err := queries.ListOwnedScheduleRulesForUpdate(
		ctx, sqlcgen.ListOwnedScheduleRulesForUpdateParams{TutorID: tutorID, ClassID: classID},
	)
	if err != nil {
		return EndScheduleResult{}, 0, fmt.Errorf("lock end schedule rules: %w", err)
	}
	if classRow.ScheduleRevision != input.ExpectedRevision {
		return EndScheduleResult{}, 0, staleScheduleError(classID, classRow.ScheduleRevision, nil)
	}
	var latest *sqlcgen.ScheduleRule
	for index := range rules {
		if !rules[index].RetiredAt.Valid {
			latest = &rules[index]
			break
		}
	}
	if latest == nil {
		return EndScheduleResult{}, 0, invalidScheduleStateError(classID, classRow.ScheduleRevision, "absent")
	}
	location, err := time.LoadLocation(latest.TimeZone)
	if err != nil {
		return EndScheduleResult{}, 0, fmt.Errorf("load stored schedule timezone: %w", err)
	}
	lastDate, err := parseDate("last_date", input.LastDate)
	if err != nil {
		return EndScheduleResult{}, 0, err
	}
	if lastDate.Before(localCalendarDate(commandTime, location)) {
		return EndScheduleResult{}, 0, &ValidationError{Field: "last_date", Message: "must be today or later"}
	}
	contextSnapshot, err := jsonSnapshot(commandContext{
		CommandTime: commandTime, GoverningTimeZone: latest.TimeZone,
		RequestDisplayTimeZone: latest.TimeZone,
	})
	if err != nil {
		return EndScheduleResult{}, 0, err
	}
	_, err = queries.InsertCommandReceipt(ctx, sqlcgen.InsertCommandReceiptParams{
		TutorID: tutorID, Operation: operationEndSchedule, IdempotencyKey: idempotencyKey,
		RequestHash: hash[:], PrimaryResourceID: classID,
		RelatedResourceID: pgtype.UUID{Bytes: latest.ScheduleRuleID, Valid: true},
		ContextSnapshot:   contextSnapshot,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		h.rollback(ctx, tx)
		replayStatus, _, retryErr := h.replayCommand(
			ctx, tutorID, operationEndSchedule, idempotencyKey, hash[:], &replay,
		)
		return replay, replayStatus, retryErr
	}
	if err != nil {
		return EndScheduleResult{}, 0, fmt.Errorf("claim end schedule receipt: %w", err)
	}
	var endedRule sqlcgen.ScheduleRule
	if lastDate.Before(latest.ValidFrom.Time) {
		endedRule, err = queries.RetireScheduleRule(ctx, sqlcgen.RetireScheduleRuleParams{
			TutorID: tutorID, ClassID: classID, ScheduleRuleID: latest.ScheduleRuleID,
			RetiredAt: pgtype.Timestamptz{Time: commandTime, Valid: true},
		})
		if err != nil {
			return EndScheduleResult{}, 0, fmt.Errorf("retire future ended rule: %w", err)
		}
		for index := 1; index < len(rules); index++ {
			predecessor := rules[index]
			if predecessor.RetiredAt.Valid || predecessor.ValidThrough.Time.Before(lastDate) {
				continue
			}
			_, err = queries.EndScheduleRule(ctx, sqlcgen.EndScheduleRuleParams{
				TutorID: tutorID, ClassID: classID, ScheduleRuleID: predecessor.ScheduleRuleID,
				ValidThrough: pgtype.Date{Time: lastDate, Valid: true},
				EndedAt:      pgtype.Timestamptz{Time: commandTime, Valid: true},
			})
			if err != nil {
				return EndScheduleResult{}, 0, fmt.Errorf("end predecessor schedule rule: %w", err)
			}
			break
		}
	} else {
		if lastDate.After(latest.ValidThrough.Time) || latest.EndedAt.Valid || latest.ReplacedAt.Valid {
			return EndScheduleResult{}, 0, invalidScheduleStateError(
				classID, classRow.ScheduleRevision,
				ruleState(latest.ValidFrom.Time, latest.ValidThrough.Time, latest.TimeZone,
					timestampPointer(latest.ReplacedAt), timestampPointer(latest.EndedAt),
					timestampPointer(latest.RetiredAt), commandTime),
			)
		}
		endedRule, err = queries.EndScheduleRule(ctx, sqlcgen.EndScheduleRuleParams{
			TutorID: tutorID, ClassID: classID, ScheduleRuleID: latest.ScheduleRuleID,
			ValidThrough: pgtype.Date{Time: lastDate, Valid: true},
			EndedAt:      pgtype.Timestamptz{Time: commandTime, Valid: true},
		})
		if err != nil {
			return EndScheduleResult{}, 0, fmt.Errorf("end schedule rule: %w", err)
		}
	}
	affectedFrom := lastDate.AddDate(0, 0, 1)
	exceptions, err := queries.ListRetainedScheduleExceptions(ctx, sqlcgen.ListRetainedScheduleExceptionsParams{
		TutorID: tutorID, ClassID: classID, OriginLocalDate: pgtype.Date{Time: affectedFrom, Valid: true},
	})
	if err != nil {
		return EndScheduleResult{}, 0, fmt.Errorf("read end schedule exceptions: %w", err)
	}
	superseded, err := queries.SupersedeUntouchedScheduleSessions(
		ctx, sqlcgen.SupersedeUntouchedScheduleSessionsParams{
			CommandTime: pgtype.Timestamptz{Time: commandTime, Valid: true},
			TutorID:     tutorID, ClassID: classID,
			AffectedFrom: pgtype.Date{Time: affectedFrom, Valid: true},
		},
	)
	if err != nil {
		return EndScheduleResult{}, 0, fmt.Errorf("replace sessions after schedule end: %w", err)
	}
	for index := range superseded {
		row := &superseded[index]
		if err = h.writeSessionCancelledEvent(
			ctx, tx, tutorID, row.SessionID, row.ClassID, commandTime,
		); err != nil {
			return EndScheduleResult{}, 0, err
		}
	}
	updatedClass, err := queries.SetClassScheduleRevision(ctx, sqlcgen.SetClassScheduleRevisionParams{
		TutorID: tutorID, ClassID: classID,
		ScheduleRevision: classRow.ScheduleRevision + 1, UpdatedAt: commandTime,
	})
	if err != nil {
		return EndScheduleResult{}, 0, fmt.Errorf("increment ended schedule revision: %w", err)
	}
	slotRows, err := queries.ListScheduleSlots(ctx, sqlcgen.ListScheduleSlotsParams{
		TutorID: tutorID, ScheduleRuleIds: []uuid.UUID{endedRule.ScheduleRuleID},
	})
	if err != nil {
		return EndScheduleResult{}, 0, fmt.Errorf("read ended schedule slots: %w", err)
	}
	slots := make([]WeeklyScheduleSlotInput, 0, len(slotRows))
	for _, slot := range slotRows {
		slots = append(slots, WeeklyScheduleSlotInput{
			Weekday: slot.Weekday, StartTime: slot.StartTime, EndTime: slot.EndTime,
		})
	}
	result := EndScheduleResult{
		Class:           classFromLocked(sqlcgen.LockOwnedClassRow(updatedClass)),
		Rule:            ruleSummaryFromStored(endedRule, slots, commandTime),
		SupersededCount: len(superseded), PreservedCount: len(exceptions),
	}
	if err = completeCommand(ctx, queries, tutorID, operationEndSchedule, idempotencyKey, result); err != nil {
		return EndScheduleResult{}, 0, err
	}
	if err = tx.Commit(ctx); err != nil {
		return EndScheduleResult{}, 0, fmt.Errorf("commit end schedule: %w", err)
	}
	return result, http.StatusOK, nil
}

func completeCommand(
	ctx context.Context,
	queries *sqlcgen.Queries,
	tutorID uuid.UUID,
	operation string,
	idempotencyKey string,
	result any,
) error {
	snapshot, err := jsonSnapshot(result)
	if err != nil {
		return err
	}
	_, err = queries.CompleteCommandReceipt(ctx, sqlcgen.CompleteCommandReceiptParams{
		TutorID: tutorID, Operation: operation, IdempotencyKey: idempotencyKey,
		ResponseSnapshot: snapshot,
		ResponseStatus:   pgtype.Int4{Int32: http.StatusOK, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("complete %s receipt: %w", operation, err)
	}
	return nil
}

func (h *Handler) writeSessionCancelledEvent(
	ctx context.Context,
	tx pgx.Tx,
	tutorID uuid.UUID,
	sessionID uuid.UUID,
	classID uuid.UUID,
	commandTime time.Time,
) error {
	return h.writeEvent(ctx, tx, teachingEvent{
		name:       vermouth.EventSessionCancelled,
		tutorID:    tutorID,
		key:        vermouth.Key{Kind: vermouth.KeySessionID, Value: sessionID},
		occurredAt: commandTime,
		fields: sessionCancelledFields{
			SessionID: sessionID, ClassID: classID, TutorID: tutorID,
		},
	})
}
