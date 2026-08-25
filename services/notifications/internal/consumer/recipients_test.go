package consumer_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"
	"github.com/stretchr/testify/require"

	"github.com/nguyen-duc-loc/vermouth/services/notifications/internal/consumer"
)

// TestHandleTutorEventRejectsAConflictingPayloadTenant covers AC-4. A
// conflicting payload is rejected before any projection can be written.
func TestHandleTutorEventRejectsAConflictingPayloadTenant(t *testing.T) {
	t.Parallel()

	envelopeTutorID := uuid.Must(uuid.NewV7())
	payloadTutorID := uuid.Must(uuid.NewV7())
	env, err := vermouth.NewEnvelope(
		t.Context(),
		vermouth.EventTutorRegistered,
		1,
		envelopeTutorID,
		vermouth.Key{Kind: vermouth.KeyTutorID, Value: envelopeTutorID},
		map[string]any{
			"tutor_id":     payloadTutorID,
			"email":        "tutor@example.com",
			"display_name": "Tutor",
			"timezone":     "Asia/Ho_Chi_Minh",
			"language":     "vi",
		},
	)
	require.NoError(t, err)

	var tx pgx.Tx
	err = consumer.Recipients().Handle(t.Context(), tx, env)
	require.ErrorAs(t, err, new(*vermouth.TutorIDConflictError))
}

// TestHandleTutorEventRejectsAMissingEnvelopeTenant covers AC-4. A payload
// cannot supply the tenant when the trusted envelope omitted it.
func TestHandleTutorEventRejectsAMissingEnvelopeTenant(t *testing.T) {
	t.Parallel()

	keyID := uuid.Must(uuid.NewV7())
	env, err := vermouth.NewEnvelope(
		t.Context(),
		vermouth.EventTutorRegistered,
		1,
		uuid.Nil,
		vermouth.Key{Kind: vermouth.KeyTutorID, Value: keyID},
		map[string]any{
			"tutor_id":     keyID,
			"email":        "tutor@example.com",
			"display_name": "Tutor",
			"timezone":     "Asia/Ho_Chi_Minh",
			"language":     "vi",
		},
	)
	require.NoError(t, err)

	var tx pgx.Tx
	err = consumer.Recipients().Handle(t.Context(), tx, env)
	require.ErrorContains(t, err, "tutor_id is required")
}
