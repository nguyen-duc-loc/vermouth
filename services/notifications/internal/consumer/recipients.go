// Package consumer keeps notifications' local copies up to date from events.
// It is the only writer of those tables: nothing else may write a projection.
package consumer

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"

	"github.com/nguyen-duc-loc/vermouth/services/notifications/internal/store"
	"github.com/nguyen-duc-loc/vermouth/services/notifications/internal/store/sqlcgen"
)

// RecipientsConsumerName is the consumer name, so its group is
// notifications.recipients and its handled_events rows carry the same string
// (STK-12). `task replay:notifications:recipients` resets exactly this pair.
const RecipientsConsumerName = "recipients"

// tutorFacts is the tolerant reader's view of the identity events: only the
// fields notifications actually needs, so a field added later is ignored
// rather than demanded (INV-12).
type tutorFacts struct {
	TutorID     uuid.UUID `json:"tutor_id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	Timezone    string    `json:"timezone"`
	Language    string    `json:"language"`
}

// Recipients keeps the recipient copy current from identity's events. Spec
// 0001's catalogue: identity.tutor.registered creates the recipient row, and
// identity.tutor.profile.changed updates it.
func Recipients() vermouth.Consumer {
	return vermouth.Consumer{
		Name:    RecipientsConsumerName,
		Topics:  []string{vermouth.TopicIdentity},
		Accepts: []int{1},
		Handle:  handleTutorEvent,
	}
}

// errNoTutorID is a publisher mistake rather than a version problem: the event
// arrived at a version this consumer accepts, but without the field the
// projection is keyed by.
var errNoTutorID = errors.New("event carried no tutor_id")

// handleTutorEvent runs inside the transaction that also inserted the
// handled_events row, so handling the same message twice changes nothing the
// second time (INV-5).
//
// It stores the facts as they arrive and derives nothing (INV-7), which is why
// order across two different tutors never matters.
func handleTutorEvent(ctx context.Context, tx pgx.Tx, env vermouth.Envelope) error {
	switch env.EventName {
	case vermouth.EventTutorRegistered, vermouth.EventTutorProfileChanged:
		var facts tutorFacts
		err := vermouth.DecodeInto(env, []int{1}, &facts)
		if err != nil {
			return err
		}
		if facts.TutorID == uuid.Nil {
			return fmt.Errorf("%s: %w", env.EventName, errNoTutorID)
		}
		return store.Queries(tx).UpsertRecipient(ctx, sqlcgen.UpsertRecipientParams{
			TutorID:     facts.TutorID,
			Email:       facts.Email,
			DisplayName: facts.DisplayName,
			Timezone:    facts.Timezone,
			Language:    facts.Language,
		})
	default:
		// An event this consumer has no interest in is not a failure: the topic
		// carries every event of the publishing service (STK-11).
		return nil
	}
}
