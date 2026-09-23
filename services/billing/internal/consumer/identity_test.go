//nolint:testpackage // The conflicting tutor check must run before a database transaction is touched.
package consumer

import (
	"testing"

	"github.com/google/uuid"
	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"
	"github.com/stretchr/testify/require"
)

// covers: AC-13
func TestIdentity_DeclaresRegistrationConsumerAndRejectsConflictingTutor(t *testing.T) {
	t.Parallel()

	definition := Identity()
	require.Equal(t, IdentityConsumerName, definition.Name)
	require.Equal(t, []string{vermouth.TopicIdentity}, definition.Topics)
	require.Equal(t, []int{1}, definition.Accepts)

	envelopeTutorID := uuid.Must(uuid.NewV7())
	payloadTutorID := uuid.Must(uuid.NewV7())
	envelope, err := vermouth.NewEnvelope(
		t.Context(), vermouth.EventTutorRegistered, 1, envelopeTutorID,
		vermouth.Key{Kind: vermouth.KeyTutorID, Value: envelopeTutorID},
		map[string]any{"tutor_id": payloadTutorID},
	)
	require.NoError(t, err)

	err = handleIdentityEvent(t.Context(), nil, envelope, vermouth.SourcePosition{})
	var conflict *vermouth.TutorIDConflictError
	require.ErrorAs(t, err, &conflict)
}

// covers: AC-13
func TestIdentity_IgnoresUnknownFactsWithoutTouchingATransaction(t *testing.T) {
	t.Parallel()

	tutorID := uuid.Must(uuid.NewV7())
	envelope, err := vermouth.NewEnvelope(
		t.Context(), vermouth.EventTutorProfileChanged, 1, tutorID,
		vermouth.Key{Kind: vermouth.KeyTutorID, Value: tutorID},
		map[string]any{"tutor_id": tutorID},
	)
	require.NoError(t, err)
	require.NoError(t, handleIdentityEvent(t.Context(), nil, envelope, vermouth.SourcePosition{}))
}
