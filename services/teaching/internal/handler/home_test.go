//nolint:testpackage // These white box tests lock the opaque cursor and local clock helpers.
package handler

import (
	"encoding/base64"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/nguyen-duc-loc/vermouth/services/teaching/internal/store/sqlcgen"
)

// covers: AC-7, AC-11, AC-15
func TestHomeCursor_RoundTripsTheStableSessionTuple(t *testing.T) {
	t.Parallel()

	sessionID := uuid.MustParse("018f8f7e-91b0-7cc4-bd8c-f4d9030ca423")
	startsAt := time.Date(2026, time.August, 30, 3, 0, 0, 0, time.UTC)
	encoded, err := encodeHomeCursor(sqlcgen.ListHomeSessionsRow{
		SessionID: sessionID,
		StartsAt:  startsAt,
		LocalDate: pgtype.Date{Time: time.Date(2026, time.August, 30, 0, 0, 0, 0, time.UTC), Valid: true},
	})
	require.NoError(t, err)

	decoded, present, err := decodeHomeCursor(encoded, "2026-08-30")

	require.NoError(t, err)
	require.True(t, present)
	require.Equal(t, "2026-08-30", decoded.LocalDate)
	require.Equal(t, startsAt, decoded.StartsAt)
	require.Equal(t, sessionID, decoded.SessionID)
}

// covers: AC-7, AC-11, AC-15
func TestDecodeHomeCursor_RejectsMalformedAndPriorDateValues(t *testing.T) {
	t.Parallel()

	wrongDate := base64.RawURLEncoding.EncodeToString([]byte(`{
		"local_date":"2026-08-29",
		"starts_at":"2026-08-29T03:00:00Z",
		"session_id":"018f8f7e-91b0-7cc4-bd8c-f4d9030ca423"
	}`))
	missingTuple := base64.RawURLEncoding.EncodeToString([]byte(`{"local_date":"2026-08-30"}`))
	tests := []struct {
		name    string
		value   string
		message string
	}{
		{"bad base64", "%%%", cursorMalformed},
		{"bad JSON", base64.RawURLEncoding.EncodeToString([]byte(`{"local_date":`)), cursorMalformed},
		{"prior date", wrongDate, "does not belong"},
		{"missing tuple", missingTuple, "does not belong"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, _, err := decodeHomeCursor(test.value, "2026-08-30")

			var validation *ValidationError
			require.ErrorAs(t, err, &validation)
			require.Equal(t, cursorField, validation.Field)
			require.Contains(t, validation.Message, test.message)
		})
	}
}

// covers: AC-1, AC-7, AC-11
func TestBuildSetupDefaults_RoundsForwardAndMovesLateSessionsToTomorrow(t *testing.T) {
	t.Parallel()

	location, err := time.LoadLocation("Asia/Ho_Chi_Minh")
	require.NoError(t, err)
	tests := []struct {
		name string
		now  time.Time
		want SetupDefaults
	}{
		{
			name: "next half hour",
			now:  time.Date(2026, time.August, 30, 10, 10, 0, 0, location),
			want: SetupDefaults{LocalDate: "2026-08-30", StartTime: "10:30", EndTime: "11:30"},
		},
		{
			name: "tomorrow after the last full span",
			now:  time.Date(2026, time.August, 30, 22, 45, 0, 0, location),
			want: SetupDefaults{LocalDate: "2026-08-31", StartTime: "00:00", EndTime: "01:00"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			defaults, err := buildSetupDefaults(test.now, location)

			require.NoError(t, err)
			require.Equal(t, test.want, defaults)
		})
	}
}

// covers: AC-7, AC-15
func TestFirstInstantOfDate_UsesTheRequestTimezone(t *testing.T) {
	t.Parallel()

	location, err := time.LoadLocation("Asia/Ho_Chi_Minh")
	require.NoError(t, err)

	instant, err := firstInstantOfDate(time.Date(2026, time.August, 31, 0, 0, 0, 0, time.UTC), location)

	require.NoError(t, err)
	require.Equal(t, time.Date(2026, time.August, 30, 17, 0, 0, 0, time.UTC), instant)
}
