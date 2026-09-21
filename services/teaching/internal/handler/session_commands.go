package handler

import (
	"context"
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

const (
	operationMoveSession    = "move_session"
	operationCancelSession  = "cancel_session"
	operationRestoreSession = "restore_session"
)

type sessionStateChange string

const (
	sessionStateCancel  sessionStateChange = "cancel"
	sessionStateRestore sessionStateChange = "restore"
)

// SessionVersionInput guards a cancel or restore against a stale details sheet.
type SessionVersionInput struct {
	ExpectedVersion int64 `json:"expected_version"`
}

// MoveSessionInput moves one active session using local clock values.
type MoveSessionInput struct {
	ExpectedVersion int64  `json:"expected_version"`
	LocalDate       string `json:"local_date"`
	StartTime       string `json:"start_time"`
	EndTime         string `json:"end_time"`
}

func sessionState(cancelledAt, supersededAt pgtype.Timestamptz) string {
	if cancelledAt.Valid {
		return stateCancelled
	}
	if supersededAt.Valid {
		return stateReplaced
	}
	return stateActive
}

func canonicalSession(
	sessionID uuid.UUID,
	classID uuid.UUID,
	startsAt time.Time,
	endsAt time.Time,
	localDate pgtype.Date,
	originLocalDate pgtype.Date,
	ruleID pgtype.UUID,
	version int64,
	movedAt pgtype.Timestamptz,
	cancelledAt pgtype.Timestamptz,
	supersededAt pgtype.Timestamptz,
	updatedAt time.Time,
	sourceZone *string,
	displayZone string,
) CanonicalSession {
	location, err := time.LoadLocation(displayZone)
	if err != nil {
		location = time.UTC
	}
	var scheduleRuleID *uuid.UUID
	if ruleID.Valid {
		value := uuid.UUID(ruleID.Bytes)
		scheduleRuleID = &value
	}
	return CanonicalSession{
		SessionID: sessionID, ClassID: classID, StartsAt: startsAt, EndsAt: endsAt,
		LocalDate:       localDate.Time.Format(dateLayout),
		OriginLocalDate: originLocalDate.Time.Format(dateLayout),
		ScheduleRuleID:  scheduleRuleID, SourceTimeZone: sourceZone,
		DisplayTimeZone: displayZone, StartUTCOffset: numericOffset(startsAt.In(location)),
		EndUTCOffset: numericOffset(endsAt.In(location)), Version: version,
		State: sessionState(cancelledAt, supersededAt), MovedAt: timestampPointer(movedAt),
		CancelledAt: timestampPointer(cancelledAt), SupersededAt: timestampPointer(supersededAt),
		UpdatedAt: updatedAt,
	}
}

func staleSessionError(session CanonicalSession) error {
	return &ConflictError{
		Code: "stale_session", Message: "the session changed; refresh before trying again",
		Details: map[string]any{"session": session},
	}
}

func invalidSessionStateError(sessionID uuid.UUID, state string, version int64) error {
	allowed := []string{"move", "cancel"}
	if state == stateCancelled {
		allowed = []string{"restore"}
	}
	if state == stateReplaced {
		allowed = []string{}
	}
	return &ConflictError{
		Code: "invalid_session_state", Message: "the session cannot make that transition",
		Details: map[string]any{
			"session_id": sessionID, "state": state, "version": version,
			"allowed_actions": allowed,
		},
	}
}

func overlapError(conflict *sqlcgen.FindOwnedSessionConflictRow, displayZone string) error {
	details := map[string]any{
		"session_id": nil, "class_name": nil, "starts_at": nil, "ends_at": nil,
		"display_time_zone": displayZone, "display_start": nil, "display_end": nil,
	}
	if conflict != nil {
		location, err := time.LoadLocation(displayZone)
		if err != nil {
			location = time.UTC
		}
		details["session_id"] = conflict.SessionID
		details["class_name"] = conflict.ClassName
		details["starts_at"] = conflict.StartsAt
		details["ends_at"] = conflict.EndsAt
		details["display_start"] = conflict.StartsAt.In(location).Format("2006-01-02 15:04 -07:00")
		details["display_end"] = conflict.EndsAt.In(location).Format("2006-01-02 15:04 -07:00")
	}
	return &ConflictError{
		Code: "session_overlap", Message: "the session overlaps another committed session",
		Details: details,
	}
}

func (h *Handler) committedOverlapError(
	ctx context.Context,
	tutorID uuid.UUID,
	excludedSessionID uuid.UUID,
	startsAt time.Time,
	endsAt time.Time,
	displayZone string,
) error {
	conflict, err := store.Queries(h.pool).FindOwnedSessionConflict(
		ctx, sqlcgen.FindOwnedSessionConflictParams{
			TutorID: tutorID, ExcludedSessionID: excludedSessionID,
			StartsAt: startsAt, EndsAt: endsAt,
		},
	)
	if err == nil {
		return overlapError(&conflict, displayZone)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return overlapError(nil, displayZone)
	}
	return fmt.Errorf("read committed session overlap: %w", err)
}

func (h *Handler) recoverSessionConstraintOverlap(
	ctx context.Context,
	tx pgx.Tx,
	tutorID uuid.UUID,
	excludedSessionID uuid.UUID,
	startsAt time.Time,
	endsAt time.Time,
	displayZone string,
	remainingOverlapRetries int,
) (bool, error) {
	h.rollback(ctx, tx)
	conflict, err := store.Queries(h.pool).FindOwnedSessionConflict(
		ctx, sqlcgen.FindOwnedSessionConflictParams{
			TutorID: tutorID, ExcludedSessionID: excludedSessionID,
			StartsAt: startsAt, EndsAt: endsAt,
		},
	)
	if err == nil {
		return false, overlapError(&conflict, displayZone)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("read committed session overlap: %w", err)
	}
	if remainingOverlapRetries > 0 {
		return true, nil
	}
	return false, overlapError(nil, displayZone)
}

// MoveSession changes one active session while retaining its identifier and attendance.
func (h *Handler) MoveSession(
	ctx context.Context,
	tutorID uuid.UUID,
	sessionID uuid.UUID,
	requestZone string,
	idempotencyKey string,
	input MoveSessionInput,
) (CanonicalSession, int, error) {
	return h.moveSession(ctx, tutorID, sessionID, requestZone, idempotencyKey, input, 1)
}

//nolint:funlen,gocognit,gocritic,maintidx,noinlineerr // Lock order, validation, receipt, update, and event are one auditable command.
func (h *Handler) moveSession(
	ctx context.Context,
	tutorID uuid.UUID,
	sessionID uuid.UUID,
	requestZone string,
	idempotencyKey string,
	input MoveSessionInput,
	remainingOverlapRetries int,
) (CanonicalSession, int, error) {
	err := validateIdempotencyKey(idempotencyKey)
	if err != nil {
		return CanonicalSession{}, 0, err
	}
	hash, err := commandHash(sessionID, input)
	if err != nil {
		return CanonicalSession{}, 0, err
	}
	var replay CanonicalSession
	status, found, replayErr := h.replayCommand(
		ctx, tutorID, operationMoveSession, idempotencyKey, hash[:], &replay,
	)
	if found || replayErr != nil {
		return replay, status, replayErr
	}
	if input.ExpectedVersion < 1 {
		return CanonicalSession{}, 0, &ValidationError{
			Field: "expected_version", Message: "must be one or greater",
		}
	}
	commandTime := h.now().UTC()
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return CanonicalSession{}, 0, fmt.Errorf("begin move session: %w", err)
	}
	defer h.rollback(ctx, tx)
	queries := store.Queries(tx)
	initial, err := queries.GetOwnedSession(ctx, sqlcgen.GetOwnedSessionParams{TutorID: tutorID, SessionID: sessionID})
	if err != nil {
		return CanonicalSession{}, 0, ownedReadError("read move session", err)
	}
	_, err = queries.LockOwnedClass(ctx, sqlcgen.LockOwnedClassParams{TutorID: tutorID, ClassID: initial.ClassID})
	if err != nil {
		return CanonicalSession{}, 0, ownedReadError("lock move session class", err)
	}
	current, err := queries.GetOwnedSessionForUpdate(
		ctx, sqlcgen.GetOwnedSessionForUpdateParams{TutorID: tutorID, SessionID: sessionID},
	)
	if err != nil {
		return CanonicalSession{}, 0, ownedReadError("lock move session", err)
	}
	sourceZone := requestZone
	anchor := current.OriginLocalDate.Time
	var sourceZonePointer *string
	if current.ScheduleRuleID.Valid {
		rule, ruleErr := queries.GetSessionSourceRule(ctx, sqlcgen.GetSessionSourceRuleParams{
			TutorID: tutorID, SessionID: sessionID,
		})
		if ruleErr != nil {
			return CanonicalSession{}, 0, fmt.Errorf("read move source rule: %w", ruleErr)
		}
		sourceZone = rule.TimeZone
		anchor = rule.ValidFrom.Time
		sourceZonePointer = &sourceZone
	}
	currentView := canonicalSession(
		current.SessionID, current.ClassID, current.StartsAt, current.EndsAt,
		current.LocalDate, current.OriginLocalDate, current.ScheduleRuleID, current.Version,
		current.MovedAt, current.CancelledAt, current.SupersededAt, current.UpdatedAt,
		sourceZonePointer, requestZone,
	)
	if current.Version != input.ExpectedVersion {
		return CanonicalSession{}, 0, staleSessionError(currentView)
	}
	if current.CancelledAt.Valid || current.SupersededAt.Valid {
		return CanonicalSession{}, 0, invalidSessionStateError(
			sessionID, sessionState(current.CancelledAt, current.SupersededAt), current.Version,
		)
	}
	location, err := time.LoadLocation(sourceZone)
	if err != nil {
		return CanonicalSession{}, 0, fmt.Errorf("load move timezone: %w", err)
	}
	moveDate, err := parseDate("local_date", input.LocalDate)
	if err != nil {
		return CanonicalSession{}, 0, err
	}
	if moveDate.Before(clampedYearDate(anchor, -maxRuleYears)) || moveDate.After(clampedYearDate(anchor, maxRuleYears)) {
		return CanonicalSession{}, 0, &ValidationError{
			Field: "local_date", Message: "must be within two calendar years of the session anchor",
		}
	}
	occurrence, err := resolveWeeklyOccurrence(
		"session", moveDate,
		WeeklyScheduleSlotInput{
			Weekday: weekdayNumber(moveDate), StartTime: input.StartTime, EndTime: input.EndTime,
		},
		location, moveDate, moveDate,
	)
	if err != nil {
		return CanonicalSession{}, 0, err
	}
	conflict, conflictErr := queries.FindOwnedSessionConflict(ctx, sqlcgen.FindOwnedSessionConflictParams{
		TutorID: tutorID, ExcludedSessionID: sessionID,
		StartsAt: occurrence.startsAt, EndsAt: occurrence.endsAt,
	})
	if conflictErr == nil {
		return CanonicalSession{}, 0, overlapError(&conflict, requestZone)
	}
	if !errors.Is(conflictErr, pgx.ErrNoRows) {
		return CanonicalSession{}, 0, fmt.Errorf("find move conflict: %w", conflictErr)
	}
	contextSnapshot, err := jsonSnapshot(commandContext{
		CommandTime: commandTime, GoverningTimeZone: sourceZone,
		RequestDisplayTimeZone: requestZone,
	})
	if err != nil {
		return CanonicalSession{}, 0, err
	}
	_, err = queries.InsertCommandReceipt(ctx, sqlcgen.InsertCommandReceiptParams{
		TutorID: tutorID, Operation: operationMoveSession, IdempotencyKey: idempotencyKey,
		RequestHash: hash[:], PrimaryResourceID: sessionID,
		ContextSnapshot: contextSnapshot,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		h.rollback(ctx, tx)
		replayStatus, _, retryErr := h.replayCommand(
			ctx, tutorID, operationMoveSession, idempotencyKey, hash[:], &replay,
		)
		return replay, replayStatus, retryErr
	}
	if err != nil {
		return CanonicalSession{}, 0, fmt.Errorf("claim move session receipt: %w", err)
	}
	row, err := queries.MoveOwnedSession(ctx, sqlcgen.MoveOwnedSessionParams{
		TutorID: tutorID, SessionID: sessionID, StartsAt: occurrence.startsAt, EndsAt: occurrence.endsAt,
		LocalDate: pgtype.Date{Time: occurrence.localDate, Valid: true},
		MovedAt:   pgtype.Timestamptz{Time: commandTime, Valid: true},
	})
	if err != nil {
		if constraintConflict(err) {
			retry, overlapErr := h.recoverSessionConstraintOverlap(
				ctx, tx, tutorID, sessionID, occurrence.startsAt, occurrence.endsAt,
				requestZone, remainingOverlapRetries,
			)
			if retry {
				return h.moveSession(
					ctx, tutorID, sessionID, requestZone, idempotencyKey, input, remainingOverlapRetries-1,
				)
			}
			return CanonicalSession{}, 0, overlapErr
		}
		return CanonicalSession{}, 0, fmt.Errorf("move session: %w", err)
	}
	result := canonicalSession(
		row.SessionID, row.ClassID, row.StartsAt, row.EndsAt, row.LocalDate,
		row.OriginLocalDate, row.ScheduleRuleID, row.Version, row.MovedAt,
		row.CancelledAt, row.SupersededAt, row.UpdatedAt, sourceZonePointer, requestZone,
	)
	if err = h.writeEvent(ctx, tx, teachingEvent{
		name:       vermouth.EventSessionMoved,
		tutorID:    tutorID,
		key:        vermouth.Key{Kind: vermouth.KeySessionID, Value: sessionID},
		occurredAt: commandTime,
		fields: sessionScheduledFields{
			SessionID: sessionID, ClassID: row.ClassID, TutorID: tutorID,
			StartsAt: row.StartsAt, EndsAt: row.EndsAt,
			LocalDate: row.LocalDate.Time.Format(dateLayout),
		},
	}); err != nil {
		return CanonicalSession{}, 0, err
	}
	if err = completeCommand(ctx, queries, tutorID, operationMoveSession, idempotencyKey, result); err != nil {
		return CanonicalSession{}, 0, err
	}
	if err = tx.Commit(ctx); err != nil {
		if constraintConflict(err) {
			retry, overlapErr := h.recoverSessionConstraintOverlap(
				ctx, tx, tutorID, sessionID, occurrence.startsAt, occurrence.endsAt,
				requestZone, remainingOverlapRetries,
			)
			if retry {
				return h.moveSession(
					ctx, tutorID, sessionID, requestZone, idempotencyKey, input, remainingOverlapRetries-1,
				)
			}
			return CanonicalSession{}, 0, overlapErr
		}
		return CanonicalSession{}, 0, fmt.Errorf("commit move session: %w", err)
	}
	return result, http.StatusOK, nil
}

// CancelSession keeps attendance history while making one session inactive.
func (h *Handler) CancelSession(
	ctx context.Context,
	tutorID uuid.UUID,
	sessionID uuid.UUID,
	requestZone string,
	idempotencyKey string,
	input SessionVersionInput,
) (CanonicalSession, int, error) {
	return h.changeSessionState(
		ctx, tutorID, sessionID, requestZone, idempotencyKey, input,
		operationCancelSession, sessionStateCancel, 0,
	)
}

// RestoreSession reactivates one tutor cancelled session at its stored time.
func (h *Handler) RestoreSession(
	ctx context.Context,
	tutorID uuid.UUID,
	sessionID uuid.UUID,
	requestZone string,
	idempotencyKey string,
	input SessionVersionInput,
) (CanonicalSession, int, error) {
	return h.changeSessionState(
		ctx, tutorID, sessionID, requestZone, idempotencyKey, input,
		operationRestoreSession, sessionStateRestore, 1,
	)
}

//nolint:funlen,gocognit,gocritic,maintidx,nestif,noinlineerr // Cancel and restore intentionally share the same lock and receipt protocol.
func (h *Handler) changeSessionState(
	ctx context.Context,
	tutorID uuid.UUID,
	sessionID uuid.UUID,
	requestZone string,
	idempotencyKey string,
	input SessionVersionInput,
	operation string,
	change sessionStateChange,
	remainingOverlapRetries int,
) (CanonicalSession, int, error) {
	restore := change == sessionStateRestore
	err := validateIdempotencyKey(idempotencyKey)
	if err != nil {
		return CanonicalSession{}, 0, err
	}
	hash, err := commandHash(sessionID, input)
	if err != nil {
		return CanonicalSession{}, 0, err
	}
	var replay CanonicalSession
	status, found, replayErr := h.replayCommand(
		ctx, tutorID, operation, idempotencyKey, hash[:], &replay,
	)
	if found || replayErr != nil {
		return replay, status, replayErr
	}
	if input.ExpectedVersion < 1 {
		return CanonicalSession{}, 0, &ValidationError{
			Field: "expected_version", Message: "must be one or greater",
		}
	}
	commandTime := h.now().UTC()
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return CanonicalSession{}, 0, fmt.Errorf("begin %s: %w", operation, err)
	}
	defer h.rollback(ctx, tx)
	queries := store.Queries(tx)
	initial, err := queries.GetOwnedSession(ctx, sqlcgen.GetOwnedSessionParams{TutorID: tutorID, SessionID: sessionID})
	if err != nil {
		return CanonicalSession{}, 0, ownedReadError("read session state", err)
	}
	_, err = queries.LockOwnedClass(ctx, sqlcgen.LockOwnedClassParams{TutorID: tutorID, ClassID: initial.ClassID})
	if err != nil {
		return CanonicalSession{}, 0, ownedReadError("lock session class", err)
	}
	current, err := queries.GetOwnedSessionForUpdate(
		ctx, sqlcgen.GetOwnedSessionForUpdateParams{TutorID: tutorID, SessionID: sessionID},
	)
	if err != nil {
		return CanonicalSession{}, 0, ownedReadError("lock session state", err)
	}
	var sourceZone *string
	if current.ScheduleRuleID.Valid {
		rule, ruleErr := queries.GetSessionSourceRule(ctx, sqlcgen.GetSessionSourceRuleParams{
			TutorID: tutorID, SessionID: sessionID,
		})
		if ruleErr != nil {
			return CanonicalSession{}, 0, fmt.Errorf("read session source rule: %w", ruleErr)
		}
		sourceZone = &rule.TimeZone
	}
	currentView := canonicalSession(
		current.SessionID, current.ClassID, current.StartsAt, current.EndsAt,
		current.LocalDate, current.OriginLocalDate, current.ScheduleRuleID, current.Version,
		current.MovedAt, current.CancelledAt, current.SupersededAt, current.UpdatedAt,
		sourceZone, requestZone,
	)
	if current.Version != input.ExpectedVersion {
		return CanonicalSession{}, 0, staleSessionError(currentView)
	}
	state := sessionState(current.CancelledAt, current.SupersededAt)
	if current.SupersededAt.Valid || restore && !current.CancelledAt.Valid || !restore && current.CancelledAt.Valid {
		return CanonicalSession{}, 0, invalidSessionStateError(sessionID, state, current.Version)
	}
	if restore {
		conflict, conflictErr := queries.FindOwnedSessionConflict(ctx, sqlcgen.FindOwnedSessionConflictParams{
			TutorID: tutorID, ExcludedSessionID: sessionID,
			StartsAt: current.StartsAt, EndsAt: current.EndsAt,
		})
		if conflictErr == nil {
			return CanonicalSession{}, 0, overlapError(&conflict, requestZone)
		}
		if !errors.Is(conflictErr, pgx.ErrNoRows) {
			return CanonicalSession{}, 0, fmt.Errorf("find restore conflict: %w", conflictErr)
		}
	}
	governingZone := requestZone
	if sourceZone != nil {
		governingZone = *sourceZone
	}
	contextSnapshot, err := jsonSnapshot(commandContext{
		CommandTime: commandTime, GoverningTimeZone: governingZone,
		RequestDisplayTimeZone: requestZone,
	})
	if err != nil {
		return CanonicalSession{}, 0, err
	}
	_, err = queries.InsertCommandReceipt(ctx, sqlcgen.InsertCommandReceiptParams{
		TutorID: tutorID, Operation: operation, IdempotencyKey: idempotencyKey,
		RequestHash: hash[:], PrimaryResourceID: sessionID, ContextSnapshot: contextSnapshot,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		h.rollback(ctx, tx)
		replayStatus, _, retryErr := h.replayCommand(
			ctx, tutorID, operation, idempotencyKey, hash[:], &replay,
		)
		return replay, replayStatus, retryErr
	}
	if err != nil {
		return CanonicalSession{}, 0, fmt.Errorf("claim %s receipt: %w", operation, err)
	}
	var result CanonicalSession
	if restore {
		row, updateErr := queries.RestoreOwnedSession(ctx, sqlcgen.RestoreOwnedSessionParams{
			TutorID: tutorID, SessionID: sessionID,
			UpdatedAt: commandTime,
		})
		if updateErr != nil {
			if constraintConflict(updateErr) {
				retry, overlapErr := h.recoverSessionConstraintOverlap(
					ctx, tx, tutorID, sessionID, current.StartsAt, current.EndsAt,
					requestZone, remainingOverlapRetries,
				)
				if retry {
					return h.changeSessionState(
						ctx, tutorID, sessionID, requestZone, idempotencyKey, input,
						operation, change, remainingOverlapRetries-1,
					)
				}
				return CanonicalSession{}, 0, overlapErr
			}
			return CanonicalSession{}, 0, fmt.Errorf("restore session: %w", updateErr)
		}
		result = canonicalSession(
			row.SessionID, row.ClassID, row.StartsAt, row.EndsAt, row.LocalDate,
			row.OriginLocalDate, row.ScheduleRuleID, row.Version, row.MovedAt,
			row.CancelledAt, row.SupersededAt, row.UpdatedAt, sourceZone, requestZone,
		)
		err = h.writeEvent(ctx, tx, teachingEvent{
			name:       vermouth.EventSessionScheduled,
			tutorID:    tutorID,
			key:        vermouth.Key{Kind: vermouth.KeySessionID, Value: sessionID},
			occurredAt: commandTime,
			fields: sessionScheduledFields{
				SessionID: sessionID, ClassID: row.ClassID, TutorID: tutorID,
				StartsAt: row.StartsAt, EndsAt: row.EndsAt,
				LocalDate: row.LocalDate.Time.Format(dateLayout),
			},
		})
	} else {
		row, updateErr := queries.CancelOwnedSession(ctx, sqlcgen.CancelOwnedSessionParams{
			TutorID: tutorID, SessionID: sessionID,
			CancelledAt: pgtype.Timestamptz{Time: commandTime, Valid: true},
		})
		if updateErr != nil {
			return CanonicalSession{}, 0, fmt.Errorf("cancel session: %w", updateErr)
		}
		result = canonicalSession(
			row.SessionID, row.ClassID, row.StartsAt, row.EndsAt, row.LocalDate,
			row.OriginLocalDate, row.ScheduleRuleID, row.Version, row.MovedAt,
			row.CancelledAt, row.SupersededAt, row.UpdatedAt, sourceZone, requestZone,
		)
		err = h.writeSessionCancelledEvent(
			ctx, tx, tutorID, sessionID, row.ClassID, commandTime,
		)
	}
	if err != nil {
		return CanonicalSession{}, 0, err
	}
	if err = completeCommand(ctx, queries, tutorID, operation, idempotencyKey, result); err != nil {
		return CanonicalSession{}, 0, err
	}
	if err = tx.Commit(ctx); err != nil {
		if constraintConflict(err) {
			retry, overlapErr := h.recoverSessionConstraintOverlap(
				ctx, tx, tutorID, sessionID, current.StartsAt, current.EndsAt,
				requestZone, remainingOverlapRetries,
			)
			if retry {
				return h.changeSessionState(
					ctx, tutorID, sessionID, requestZone, idempotencyKey, input,
					operation, change, remainingOverlapRetries-1,
				)
			}
			return CanonicalSession{}, 0, overlapErr
		}
		return CanonicalSession{}, 0, fmt.Errorf("commit %s: %w", operation, err)
	}
	return result, http.StatusOK, nil
}

//nolint:mnd // ISO weekday values are the domain enumeration fixed by the schedule contract.
func weekdayNumber(value time.Time) int16 {
	switch value.Weekday() {
	case time.Sunday:
		return 7
	case time.Monday:
		return 1
	case time.Tuesday:
		return 2
	case time.Wednesday:
		return 3
	case time.Thursday:
		return 4
	case time.Friday:
		return 5
	case time.Saturday:
		return 6
	default:
		return 0
	}
}
