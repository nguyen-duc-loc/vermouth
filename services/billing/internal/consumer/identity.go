package consumer

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"

	"github.com/nguyen-duc-loc/vermouth/services/billing/internal/store"
)

// IdentityConsumerName is the replayable consumer that seeds invoice profiles.
const IdentityConsumerName = "identity"

type identityFacts struct {
	TutorID uuid.UUID `json:"tutor_id"`
}

// Identity consumes only registration facts and ignores later identity detail changes.
func Identity() vermouth.Consumer {
	return vermouth.Consumer{
		Name:    IdentityConsumerName,
		Topics:  []string{vermouth.TopicIdentity},
		Accepts: []int{1},
		Handle:  handleIdentityEvent,
	}
}

func handleIdentityEvent(ctx context.Context, tx pgx.Tx, env vermouth.Envelope) error {
	if env.EventName != vermouth.EventTutorRegistered {
		return nil
	}
	var facts identityFacts
	err := vermouth.DecodeInto(env, []int{1}, &facts)
	if err != nil {
		return err
	}
	tutorID, err := vermouth.TrustedTutorID(env, facts.TutorID)
	if err != nil {
		return err
	}
	err = store.Queries(tx).SeedInvoiceProfile(ctx, tutorID)
	if err != nil {
		return fmt.Errorf("seed invoice profile: %w", err)
	}
	return nil
}
