package vermouth_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"
)

// STK-19: every event has one test asserting its key is the field spec 0001's
// catalogue names for it. identity.tutor.registered is keyed by tutor_id.
func TestTutorRegisteredIsKeyedByTutorID(t *testing.T) {
	t.Parallel()

	tutorID := uuid.New()
	env, err := vermouth.NewEnvelope(t.Context(), "identity.tutor.registered", 1, tutorID,
		vermouth.Key{Kind: vermouth.KeyTutorID, Value: tutorID},
		map[string]string{"email": "tutor@example.com"})
	require.NoError(t, err)
	require.Equal(t, vermouth.KeyTutorID, env.Key.Kind)
	require.Equal(t, tutorID, env.Key.Value)
	require.Equal(t, []byte(tutorID.String()), env.Key.Bytes())
}

func TestNewEnvelopeRefusesAMissingKey(t *testing.T) {
	t.Parallel()

	_, err := vermouth.NewEnvelope(t.Context(), "identity.tutor.registered", 1, uuid.New(), vermouth.Key{}, nil)
	require.Error(t, err)
}

func TestNewEnvelopeCarriesTheRequestID(t *testing.T) {
	t.Parallel()

	ctx := vermouth.WithRequestID(t.Context(), "req-1")
	env, err := vermouth.NewEnvelope(ctx, "identity.tutor.registered", 1, uuid.New(),
		vermouth.Key{Kind: vermouth.KeyTutorID, Value: uuid.New()}, nil)
	require.NoError(t, err)
	require.Equal(t, "req-1", env.RequestID)
}

// STK-20 and INV-12: the version set is checked before any field is decoded,
// and an unrecognised version is the one thing the reader must not absorb.
func TestDecodeIntoRejectsAnUnknownVersion(t *testing.T) {
	t.Parallel()

	env := vermouth.Envelope{
		EventName:    "identity.tutor.registered",
		EventVersion: 2,
		Data:         json.RawMessage(`{}`),
	}
	var target struct{}
	err := vermouth.DecodeInto(env, []int{1}, &target)
	require.ErrorAs(t, err, new(*vermouth.UnknownVersionError))
}

func TestDecodeIntoIgnoresUnknownFields(t *testing.T) {
	t.Parallel()

	env := vermouth.Envelope{
		EventName:    "identity.tutor.registered",
		EventVersion: 1,
		Data:         json.RawMessage(`{"email":"tutor@example.com","invented_later":"ignored"}`),
	}
	var target struct {
		Email string `json:"email"`
	}
	require.NoError(t, vermouth.DecodeInto(env, []int{1}, &target))
	require.Equal(t, "tutor@example.com", target.Email)
}
