package consumer

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"

	"github.com/nguyen-duc-loc/vermouth/services/billing/internal/store"
	"github.com/nguyen-duc-loc/vermouth/services/billing/internal/store/sqlcgen"
)

// TeachingConsumerName makes the broker group and handled event identity the
// same visible pair used by replay tooling.
const TeachingConsumerName = "teaching"

type teachingFacts struct {
	TutorID           uuid.UUID `json:"tutor_id"`
	ClassID           uuid.UUID `json:"class_id"`
	StudentID         uuid.UUID `json:"student_id"`
	SessionID         uuid.UUID `json:"session_id"`
	Name              string    `json:"name"`
	RateAmount        int64     `json:"rate_amount"`
	Currency          string    `json:"currency"`
	RateEffectiveFrom string    `json:"rate_effective_from"`
	EffectiveFrom     string    `json:"effective_from"`
	StartsAt          time.Time `json:"starts_at"`
	EndsAt            time.Time `json:"ends_at"`
	LocalDate         string    `json:"local_date"`
	State             string    `json:"state"`
	MarkedAt          time.Time `json:"marked_at"`
}

// Teaching projects the five facts used by the first product thread. Unknown
// teaching facts remain successful no operations for tolerant reading.
func Teaching() vermouth.Consumer {
	return vermouth.Consumer{
		Name:    TeachingConsumerName,
		Topics:  []string{vermouth.TopicTeaching},
		Accepts: []int{1},
		Handle:  handleTeachingEvent,
	}
}

//nolint:funlen,gocognit // The event catalogue switch is deliberately explicit so each projection write stays easy to audit.
func handleTeachingEvent(ctx context.Context, tx pgx.Tx, env vermouth.Envelope) error {
	switch env.EventName {
	case vermouth.EventClassCreated,
		vermouth.EventSessionScheduled,
		vermouth.EventStudentRegistered,
		vermouth.EventRosterJoined,
		vermouth.EventAttendanceMarked:
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
			return fmt.Errorf("project class: %w", err)
		}
		effectiveFrom, err := parseDay(facts.RateEffectiveFrom)
		if err != nil {
			return err
		}
		err = queries.UpsertClassRate(ctx, sqlcgen.UpsertClassRateParams{
			ClassID: facts.ClassID, EffectiveFrom: effectiveFrom, TutorID: tutorID,
			RateAmount: facts.RateAmount, Currency: facts.Currency,
		})
		if err != nil {
			return fmt.Errorf("project first class rate: %w", err)
		}
		return nil
	case vermouth.EventSessionScheduled:
		localDate, err := parseDay(facts.LocalDate)
		if err != nil {
			return err
		}
		err = queries.UpsertSession(ctx, sqlcgen.UpsertSessionParams{
			SessionID: facts.SessionID, ClassID: facts.ClassID, TutorID: tutorID,
			StartsAt: facts.StartsAt, EndsAt: facts.EndsAt, LocalDate: localDate,
		})
		if err != nil {
			return fmt.Errorf("project session: %w", err)
		}
		return nil
	case vermouth.EventStudentRegistered:
		err = queries.UpsertStudent(ctx, sqlcgen.UpsertStudentParams{
			StudentID: facts.StudentID, TutorID: tutorID, Name: facts.Name,
		})
		if err != nil {
			return fmt.Errorf("project student: %w", err)
		}
		return nil
	case vermouth.EventRosterJoined:
		effectiveFrom, err := parseDay(facts.EffectiveFrom)
		if err != nil {
			return err
		}
		err = queries.OpenRosterPeriod(ctx, sqlcgen.OpenRosterPeriodParams{
			ClassID: facts.ClassID, StudentID: facts.StudentID,
			EffectiveFrom: effectiveFrom, TutorID: tutorID,
		})
		if err != nil {
			return fmt.Errorf("project roster period: %w", err)
		}
		return nil
	case vermouth.EventAttendanceMarked:
		err = queries.UpsertAttendance(ctx, sqlcgen.UpsertAttendanceParams{
			SessionID: facts.SessionID, StudentID: facts.StudentID, TutorID: tutorID,
			State: facts.State, MarkedAt: facts.MarkedAt,
		})
		if err != nil {
			return fmt.Errorf("project attendance: %w", err)
		}
		return nil
	default:
		return nil
	}
}

func parseDay(value string) (pgtype.Date, error) {
	moment, err := time.Parse(time.DateOnly, value)
	if err != nil {
		return pgtype.Date{}, fmt.Errorf("parse teaching local date: %w", err)
	}
	return pgtype.Date{Time: moment, Valid: true}, nil
}
