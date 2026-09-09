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

// TestTrustedTutorIDUsesTheEnvelope covers AC-4 at the event boundary. The
// envelope is the trusted tenant source, while the payload can only agree or
// omit its duplicate value.
func TestTrustedTutorIDUsesTheEnvelope(t *testing.T) {
	t.Parallel()

	envelopeTutorID := uuid.New()
	tests := []struct {
		name            string
		envelopeTutorID uuid.UUID
		payloadTutorID  uuid.UUID
		want            uuid.UUID
		wantConflict    bool
		wantError       bool
	}{
		{
			name:            "matching payload tenant",
			envelopeTutorID: envelopeTutorID,
			payloadTutorID:  envelopeTutorID,
			want:            envelopeTutorID,
		},
		{
			name:            "payload omits tenant",
			envelopeTutorID: envelopeTutorID,
			want:            envelopeTutorID,
		},
		{
			name:            "payload conflicts with envelope",
			envelopeTutorID: envelopeTutorID,
			payloadTutorID:  uuid.New(),
			wantConflict:    true,
			wantError:       true,
		},
		{
			name:      "envelope omits tenant",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			must := require.New(t)
			env := vermouth.Envelope{
				EventName: vermouth.EventTutorRegistered,
				TutorID:   tt.envelopeTutorID,
			}

			got, err := vermouth.TrustedTutorID(env, tt.payloadTutorID)
			if tt.wantError {
				must.Error(err)
			} else {
				must.NoError(err)
			}
			must.Equal(tt.want, got)
			if tt.wantConflict {
				var conflict *vermouth.TutorIDConflictError
				must.ErrorAs(err, &conflict)
				must.Equal(vermouth.EventTutorRegistered, conflict.EventName)
				must.Equal(tt.envelopeTutorID, conflict.EnvelopeTutorID)
				must.Equal(tt.payloadTutorID, conflict.PayloadTutorID)
			}
		})
	}
}
