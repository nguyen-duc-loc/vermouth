//nolint:testpackage // White box tests inspect the sampler's exact internal output.
package ratelimit

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"
	"github.com/stretchr/testify/require"
)

// covers: AC-10
func TestGuard_SamplesDenialsWithoutCallerMaterial(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 4, 8, 0, 0, 0, time.UTC)
	capacity := BucketPairConfig{Minute: 1, Hour: 1}
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	guard := NewGuard(testConfig(capacity, capacity), logger, func() time.Time { return now })
	request := httptest.NewRequestWithContext(
		t.Context(), http.MethodGet, "https://gateway.example/api/auth/google/start", http.NoBody,
	)
	request.RemoteAddr = "198.51.100.8:42000"
	request = request.WithContext(vermouth.WithRequestID(request.Context(), "request-1"))

	require.True(t, guard.Check(request, EndpointStart).Allowed)
	require.False(t, guard.Check(request, EndpointStart).Allowed)
	require.False(t, guard.Check(request, EndpointStart).Allowed)
	now = now.Add(sampleInterval)
	request = request.WithContext(vermouth.WithRequestID(request.Context(), "request-2"))
	require.False(t, guard.Check(request, EndpointStart).Allowed)

	lines := bytes.Split(bytes.TrimSpace(output.Bytes()), []byte("\n"))
	require.Len(t, lines, 4)
	for index, line := range lines {
		var entry map[string]any
		require.NoError(t, json.Unmarshal(line, &entry))
		require.NotContains(t, string(line), "198.51.100.8")
		require.Equal(t, "start", entry["endpoint"])
		require.Contains(t, []any{"ip", "global"}, entry["scope"])
		if index < 2 {
			require.EqualValues(t, 1, entry["count"])
			require.EqualValues(t, 0, entry["suppressed"])
			require.Equal(t, "request-1", entry["request_id"])
		} else {
			require.EqualValues(t, 2, entry["count"])
			require.EqualValues(t, 1, entry["suppressed"])
			require.Equal(t, "request-2", entry["request_id"])
		}
	}
}
