package consumer

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"

	"github.com/nguyen-duc-loc/vermouth/services/notifications/internal/store"
	"github.com/nguyen-duc-loc/vermouth/services/notifications/internal/store/sqlcgen"
)

// TeachingConsumerName keeps the teaching projection group and handled event
// identity aligned for replay.
const TeachingConsumerName = "teaching"

type teachingFacts struct {
	TutorID       uuid.UUID `json:"tutor_id"`
	ClassID       uuid.UUID `json:"class_id"`
	SessionID     uuid.UUID `json:"session_id"`
	StudentID     uuid.UUID `json:"student_id"`
	Name          string    `json:"name"`
	StartsAt      time.Time `json:"starts_at"`
	EndsAt        time.Time `json:"ends_at"`
	LocalDate     string    `json:"local_date"`
	EffectiveFrom string    `json:"effective_from"`
	EffectiveTo   string    `json:"effective_to"`
}

// Teaching keeps only the class and concrete session facts the digest needs.
func Teaching() vermouth.Consumer {
	return vermouth.Consumer{
		Name:    TeachingConsumerName,
		Topics:  []string{vermouth.TopicTeaching},
		Accepts: []int{1},
		Handle:  handleTeachingEvent,
	}
}

//nolint:funlen // The catalogue switch keeps every tolerant projection write explicit.
func handleTeachingEvent(
	ctx context.Context,
	tx pgx.Tx,
	env vermouth.Envelope,
	_ vermouth.SourcePosition,
) error {
	switch env.EventName {
	case vermouth.EventClassCreated,
		vermouth.EventSessionScheduled,
		vermouth.EventSessionMoved,
		vermouth.EventSessionCancelled,
		vermouth.EventRosterJoined,
		vermouth.EventRosterLeft:
	default:
		return nil
	}
	var facts teachingFacts
	err := vermouth.DecodeInto(env, []int{1}, &facts)
	if err != nil {
		return err
	}
	tutorID, err := vermouth.TrustedTutorID(env, facts.TutorID)
	if err != nil {
		return err
	}
	queries := store.Queries(tx)
	switch env.EventName {
	case vermouth.EventClassCreated:
		err = queries.UpsertClass(ctx, sqlcgen.UpsertClassParams{
			ClassID: facts.ClassID, TutorID: tutorID, Name: facts.Name,
		})
		if err != nil {
			return fmt.Errorf("project digest class: %w", err)
		}
		return nil
	case vermouth.EventSessionScheduled, vermouth.EventSessionMoved:
		localDate, parseErr := time.Parse(time.DateOnly, facts.LocalDate)
		if parseErr != nil {
			return fmt.Errorf("parse digest session local date: %w", parseErr)
		}
		err = queries.UpsertSession(ctx, sqlcgen.UpsertSessionParams{
			SessionID: facts.SessionID,
			ClassID:   facts.ClassID,
			TutorID:   tutorID,
			StartsAt:  facts.StartsAt,
			EndsAt:    facts.EndsAt,
			LocalDate: pgtype.Date{Time: localDate, Valid: true},
		})
		if err != nil {
			return fmt.Errorf("project digest session: %w", err)
		}
		return nil
	case vermouth.EventSessionCancelled:
		err = queries.MarkSessionCancelled(ctx, sqlcgen.MarkSessionCancelledParams{
			TutorID:     tutorID,
			SessionID:   facts.SessionID,
			CancelledAt: pgtype.Timestamptz{Time: env.OccurredAt, Valid: true},
		})
		if err != nil {
			return fmt.Errorf("cancel digest session: %w", err)
		}
		return nil
	case vermouth.EventRosterJoined:
		effectiveFrom, parseErr := time.Parse(time.DateOnly, facts.EffectiveFrom)
		if parseErr != nil {
			return fmt.Errorf("parse digest roster start: %w", parseErr)
		}
		err = queries.OpenRosterPeriod(ctx, sqlcgen.OpenRosterPeriodParams{
			ClassID:       facts.ClassID,
			StudentID:     facts.StudentID,
			EffectiveFrom: pgtype.Date{Time: effectiveFrom, Valid: true},
			TutorID:       tutorID,
		})
		if err != nil {
			return fmt.Errorf("project digest roster period: %w", err)
		}
		return nil
	case vermouth.EventRosterLeft:
		effectiveTo, parseErr := time.Parse(time.DateOnly, facts.EffectiveTo)
		if parseErr != nil {
			return fmt.Errorf("parse digest roster end: %w", parseErr)
		}
		err = queries.CloseRosterPeriod(ctx, sqlcgen.CloseRosterPeriodParams{
			TutorID:     tutorID,
			ClassID:     facts.ClassID,
			StudentID:   facts.StudentID,
			EffectiveTo: pgtype.Date{Time: effectiveTo, Valid: true},
		})
		if err != nil {
			return fmt.Errorf("close digest roster period: %w", err)
		}
		return nil
	default:
		return nil
	}
}
