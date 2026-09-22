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

const operationPutClassRate = "put_class_rate"

var errIncompleteClassRateReceipt = errors.New("class rate receipt is incomplete")

// PutClassRateInput is one dated amount in integer dong.
type PutClassRateInput struct {
	RateAmount int64 `json:"rate_amount"`
}

// ClassRateCurrent is teaching's current rate truth after a command.
type ClassRateCurrent struct {
	RateAmount    int64  `json:"rate_amount"`
	Currency      string `json:"currency"`
	EffectiveFrom string `json:"effective_from"`
	RateRevision  int64  `json:"rate_revision"`
}

// ClassRateAllowedRange is the date range in which teaching accepts a rate.
type ClassRateAllowedRange struct {
	From    string `json:"from"`
	Through string `json:"through"`
}

// TeachingClassRateState is the authoritative rate portion used by the
// gateway read fan out.
type TeachingClassRateState struct {
	ClassID      uuid.UUID             `json:"class_id"`
	Current      ClassRateCurrent      `json:"current"`
	AllowedRange ClassRateAllowedRange `json:"allowed_range"`
	Archived     bool                  `json:"archived"`
}

// PutClassRateResult confirms the dated fact and warns that issued invoices
// remain immutable.
type PutClassRateResult struct {
	ClassID                 uuid.UUID             `json:"class_id"`
	EffectiveFrom           string                `json:"effective_from"`
	RateAmount              int64                 `json:"rate_amount"`
	Currency                string                `json:"currency"`
	RateRevision            int64                 `json:"rate_revision"`
	Current                 ClassRateCurrent      `json:"current"`
	AllowedRange            ClassRateAllowedRange `json:"allowed_range"`
	IssuedInvoicesUnchanged bool                  `json:"issued_invoices_unchanged"`
	HistoryState            string                `json:"history_state"`
}

type rateRequestFingerprint struct {
	Operation     string    `json:"operation"`
	ClassID       uuid.UUID `json:"class_id"`
	EffectiveFrom string    `json:"effective_date"`
	RateAmount    int64     `json:"rate_amount"`
	Currency      string    `json:"currency"`
}

// ReadClassRateState returns teaching's owned current state and date limits.
func (h *Handler) ReadClassRateState(
	ctx context.Context,
	tutorID uuid.UUID,
	classID uuid.UUID,
	timezone string,
) (TeachingClassRateState, error) {
	queries := store.Queries(h.pool)
	classRow, err := queries.GetOwnedClass(ctx, sqlcgen.GetOwnedClassParams{
		TutorID: tutorID, ClassID: classID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return TeachingClassRateState{}, ErrNotFound
	}
	if err != nil {
		return TeachingClassRateState{}, fmt.Errorf("read owned class rate: %w", err)
	}
	earliest, err := queries.GetEarliestRetainedClassSessionDate(
		ctx,
		sqlcgen.GetEarliestRetainedClassSessionDateParams{TutorID: tutorID, ClassID: classID},
	)
	if err != nil {
		return TeachingClassRateState{}, fmt.Errorf("read earliest retained class session: %w", err)
	}
	allowed, err := h.classRateAllowedRange(
		classRow.RateEffectiveFrom, classRow.ArchivedAt, earliest, timezone,
	)
	if err != nil {
		return TeachingClassRateState{}, err
	}
	return TeachingClassRateState{
		ClassID: classRow.ClassID,
		Current: classRateCurrentValues(
			classRow.RateAmount,
			classRow.Currency,
			classRow.RateEffectiveFrom,
			classRow.RateRevision,
		),
		AllowedRange: allowed,
		Archived:     classRow.ArchivedAt.Valid,
	}, nil
}

// PutClassRate records one dated correction, its receipt, and its outbox fact
// in the same transaction.
//
//nolint:funlen,gocognit // The lock, receipt, revision, event, and response are one auditable command.
func (h *Handler) PutClassRate(
	ctx context.Context,
	tutorID uuid.UUID,
	classID uuid.UUID,
	timezone string,
	idempotencyKey string,
	effectiveDate string,
	input PutClassRateInput,
) (PutClassRateResult, int, error) {
	err := validateIdempotencyKey(idempotencyKey)
	if err != nil {
		return PutClassRateResult{}, 0, err
	}
	effectiveFrom, err := parseDate("effective_date", effectiveDate)
	if err != nil {
		return PutClassRateResult{}, 0, err
	}
	if input.RateAmount < 0 || input.RateAmount > maxRateAmount {
		return PutClassRateResult{}, 0, &ValidationError{
			Field: "rate_amount", Message: "must be an integer from 0 through 1000000000 dong",
		}
	}
	requestHash, err := classRateRequestHash(classID, effectiveFrom, input.RateAmount)
	if err != nil {
		return PutClassRateResult{}, 0, err
	}
	replay, status, found, replayErr := h.replayClassRate(
		ctx, tutorID, idempotencyKey, requestHash[:],
	)
	if found || replayErr != nil {
		return replay, status, replayErr
	}

	commandTime := h.now().UTC()
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return PutClassRateResult{}, 0, fmt.Errorf("start class rate transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := store.Queries(tx)
	classRow, err := queries.LockOwnedClass(ctx, sqlcgen.LockOwnedClassParams{
		TutorID: tutorID, ClassID: classID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return PutClassRateResult{}, 0, ErrNotFound
	}
	if err != nil {
		return PutClassRateResult{}, 0, fmt.Errorf("lock owned class for rate: %w", err)
	}
	earliest, err := queries.GetEarliestRetainedClassSessionDate(
		ctx,
		sqlcgen.GetEarliestRetainedClassSessionDateParams{TutorID: tutorID, ClassID: classID},
	)
	if err != nil {
		return PutClassRateResult{}, 0, fmt.Errorf("read earliest retained class session: %w", err)
	}
	allowed, err := h.classRateAllowedRange(
		classRow.RateEffectiveFrom, classRow.ArchivedAt, earliest, timezone,
	)
	if err != nil {
		return PutClassRateResult{}, 0, err
	}
	allowedFrom, parseErr := time.Parse(dateLayout, allowed.From)
	if parseErr != nil {
		return PutClassRateResult{}, 0, fmt.Errorf("parse allowed rate start: %w", parseErr)
	}
	allowedThrough, parseErr := time.Parse(dateLayout, allowed.Through)
	if parseErr != nil {
		return PutClassRateResult{}, 0, fmt.Errorf("parse allowed rate end: %w", parseErr)
	}
	if effectiveFrom.Before(allowedFrom) || effectiveFrom.After(allowedThrough) {
		return PutClassRateResult{}, 0, &ValidationError{
			Field: "effective_date", Message: "must be inside the allowed class rate date range",
		}
	}

	contextSnapshot, err := jsonSnapshot(commandContext{
		CommandTime: commandTime, GoverningTimeZone: timezone, RequestDisplayTimeZone: timezone,
	})
	if err != nil {
		return PutClassRateResult{}, 0, err
	}
	_, err = queries.InsertCommandReceipt(ctx, sqlcgen.InsertCommandReceiptParams{
		TutorID: tutorID, Operation: operationPutClassRate,
		IdempotencyKey: idempotencyKey, RequestHash: requestHash[:],
		PrimaryResourceID: classID, ContextSnapshot: contextSnapshot,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		_ = tx.Rollback(ctx)
		result, replayStatus, _, conflictReplayErr := h.replayClassRate(
			ctx, tutorID, idempotencyKey, requestHash[:],
		)
		return result, replayStatus, conflictReplayErr
	}
	if err != nil {
		return PutClassRateResult{}, 0, fmt.Errorf("claim class rate receipt: %w", err)
	}

	updated, err := queries.SetOwnedClassRate(ctx, sqlcgen.SetOwnedClassRateParams{
		TutorID: tutorID, ClassID: classID,
		EffectiveFrom: pgtype.Date{Time: effectiveFrom, Valid: true},
		RateAmount:    input.RateAmount, Currency: currencyVND, UpdatedAt: commandTime,
	})
	if err != nil {
		return PutClassRateResult{}, 0, fmt.Errorf("set owned class rate: %w", err)
	}
	result := PutClassRateResult{
		ClassID: classID, EffectiveFrom: effectiveFrom.Format(dateLayout),
		RateAmount: input.RateAmount, Currency: currencyVND,
		RateRevision: updated.RateRevision,
		Current: classRateCurrentValues(
			updated.RateAmount,
			updated.Currency,
			updated.RateEffectiveFrom,
			updated.RateRevision,
		),
		AllowedRange:            allowed,
		IssuedInvoicesUnchanged: true, HistoryState: "pending",
	}
	writeErr := h.writeEvent(ctx, tx, teachingEvent{
		name: vermouth.EventClassRateChanged, tutorID: tutorID,
		key:        vermouth.Key{Kind: vermouth.KeyClassID, Value: classID},
		occurredAt: commandTime,
		fields: classRateChangedFields{
			ClassID: classID, TutorID: tutorID,
			EffectiveFrom: result.EffectiveFrom, RateAmount: result.RateAmount,
			Currency: currencyVND, RateRevision: result.RateRevision,
		},
	})
	if writeErr != nil {
		return PutClassRateResult{}, 0, writeErr
	}
	snapshot, err := jsonSnapshot(result)
	if err != nil {
		return PutClassRateResult{}, 0, err
	}
	_, err = queries.CompleteCommandReceipt(ctx, sqlcgen.CompleteCommandReceiptParams{
		TutorID: tutorID, Operation: operationPutClassRate, IdempotencyKey: idempotencyKey,
		ResponseSnapshot: snapshot,
		ResponseStatus:   pgtype.Int4{Int32: http.StatusOK, Valid: true},
	})
	if err != nil {
		return PutClassRateResult{}, 0, fmt.Errorf("complete class rate receipt: %w", err)
	}
	err = tx.Commit(ctx)
	if err != nil {
		return PutClassRateResult{}, 0, fmt.Errorf("commit class rate: %w", err)
	}
	h.logger.InfoContext(ctx, "Class rate recorded",
		"class_id", classID,
		"tutor_id", tutorID,
		"rate_revision", result.RateRevision,
	)
	return result, http.StatusOK, nil
}

func (h *Handler) classRateAllowedRange(
	currentEffectiveFrom pgtype.Date,
	archivedAt pgtype.Timestamptz,
	earliest pgtype.Date,
	timezone string,
) (ClassRateAllowedRange, error) {
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return ClassRateAllowedRange{}, fmt.Errorf("load request timezone: %w", err)
	}
	if archivedAt.Valid && !earliest.Valid {
		return ClassRateAllowedRange{}, &ConflictError{
			Code:    "archived_class_has_no_sessions",
			Message: "an archived class without a retained session cannot accept a rate correction",
		}
	}
	from := currentEffectiveFrom.Time
	if earliest.Valid {
		from = earliest.Time
	}
	through := localCalendarDate(h.now().UTC(), location)
	if archivedAt.Valid {
		through = localCalendarDate(archivedAt.Time, location)
	}
	return ClassRateAllowedRange{
		From: from.Format(dateLayout), Through: through.Format(dateLayout),
	}, nil
}

func classRateCurrentValues(
	rateAmount int64,
	currency string,
	effectiveFrom pgtype.Date,
	revision int64,
) ClassRateCurrent {
	return ClassRateCurrent{
		RateAmount:    rateAmount,
		Currency:      currency,
		EffectiveFrom: effectiveFrom.Time.Format(dateLayout),
		RateRevision:  revision,
	}
}

func classRateRequestHash(
	classID uuid.UUID,
	effectiveFrom time.Time,
	rateAmount int64,
) ([sha256.Size]byte, error) {
	encoded, err := json.Marshal(rateRequestFingerprint{
		Operation: operationPutClassRate, ClassID: classID,
		EffectiveFrom: effectiveFrom.Format(dateLayout), RateAmount: rateAmount, Currency: currencyVND,
	})
	if err != nil {
		return [sha256.Size]byte{}, fmt.Errorf("encode class rate request hash: %w", err)
	}
	return sha256.Sum256(encoded), nil
}

func (h *Handler) replayClassRate(
	ctx context.Context,
	tutorID uuid.UUID,
	idempotencyKey string,
	requestHash []byte,
) (PutClassRateResult, int, bool, error) {
	receipt, err := store.Queries(h.pool).GetCommandReceipt(ctx, sqlcgen.GetCommandReceiptParams{
		TutorID: tutorID, Operation: operationPutClassRate, IdempotencyKey: idempotencyKey,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return PutClassRateResult{}, 0, false, nil
	}
	if err != nil {
		return PutClassRateResult{}, 0, false, fmt.Errorf("read class rate receipt: %w", err)
	}
	if !bytes.Equal(receipt.RequestHash, requestHash) {
		return PutClassRateResult{}, 0, true, ErrIdempotencyConflict
	}
	if len(receipt.ResponseSnapshot) == 0 || !receipt.ResponseStatus.Valid {
		return PutClassRateResult{}, 0, true, errIncompleteClassRateReceipt
	}
	var result PutClassRateResult
	err = json.Unmarshal(receipt.ResponseSnapshot, &result)
	if err != nil {
		return PutClassRateResult{}, 0, true, fmt.Errorf("decode class rate receipt: %w", err)
	}
	return result, int(receipt.ResponseStatus.Int32), true, nil
}
