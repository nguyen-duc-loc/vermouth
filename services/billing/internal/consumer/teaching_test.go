//nolint:testpackage // These white box tests prove rejection before any transaction write.
package consumer

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"
	"github.com/stretchr/testify/require"
)

// covers: AC-8, AC-12
func TestTeaching_DeclaresTheProductionTopicGroupAndVersion(t *testing.T) {
	t.Parallel()

	definition := Teaching()

	require.Equal(t, TeachingConsumerName, definition.Name)
	require.Equal(t, []string{vermouth.TopicTeaching}, definition.Topics)
	require.Equal(t, []int{1}, definition.Accepts)
	require.NotNil(t, definition.Handle)
}

// covers: AC-6, AC-8, AC-12
func TestHandleTeachingEvent_RejectsAConflictingPayloadTutorBeforeWriting(t *testing.T) {
	t.Parallel()

	envelopeTutorID := uuid.Must(uuid.NewV7())
	payloadTutorID := uuid.Must(uuid.NewV7())
	studentID := uuid.Must(uuid.NewV7())
	envelope, err := vermouth.NewEnvelope(
		t.Context(),
		vermouth.EventStudentRegistered,
		1,
		envelopeTutorID,
		vermouth.Key{Kind: vermouth.KeyStudentID, Value: studentID},
		map[string]any{
			"tutor_id":   payloadTutorID,
			"student_id": studentID,
			"name":       "Mai",
		},
	)
	require.NoError(t, err)

	err = handleTeachingEvent(t.Context(), nil, envelope)

	var conflict *vermouth.TutorIDConflictError
	require.ErrorAs(t, err, &conflict)
}

// covers: AC-8, AC-11, AC-12
func TestHandleTeachingEvent_RejectsInvalidCalendarDaysAndIgnoresUnknownFacts(t *testing.T) {
	t.Parallel()

	tutorID := uuid.Must(uuid.NewV7())
	sessionID := uuid.Must(uuid.NewV7())
	classID := uuid.Must(uuid.NewV7())
	invalidDate, err := vermouth.NewEnvelope(
		t.Context(),
		vermouth.EventSessionScheduled,
		1,
		tutorID,
		vermouth.Key{Kind: vermouth.KeySessionID, Value: sessionID},
		map[string]any{
			"tutor_id":   tutorID,
			"session_id": sessionID,
			"class_id":   classID,
			"starts_at":  time.Date(2026, time.August, 30, 3, 0, 0, 0, time.UTC),
			"ends_at":    time.Date(2026, time.August, 30, 4, 0, 0, 0, time.UTC),
			"local_date": "30-08-2026",
		},
	)
	require.NoError(t, err)

	err = handleTeachingEvent(t.Context(), nil, invalidDate)
	require.ErrorContains(t, err, "parse teaching local date")

	unknown, err := vermouth.NewEnvelope(
		t.Context(),
		"teaching.class.future_fact",
		1,
		tutorID,
		vermouth.Key{Kind: vermouth.KeyClassID, Value: classID},
		map[string]any{"future": true},
	)
	require.NoError(t, err)
	require.NoError(t, handleTeachingEvent(t.Context(), nil, unknown))
}
