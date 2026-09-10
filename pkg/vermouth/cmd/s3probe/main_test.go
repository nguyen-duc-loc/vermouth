package main

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (function roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestRun_SignedObjectLifecycle(t *testing.T) {
	secretKey := strings.Repeat("a", 64)
	t.Setenv("GARAGE_S3_ENDPOINT", "http://garage:3900")
	t.Setenv("GARAGE_S3_REGION", "garage")
	t.Setenv("GARAGE_BUCKET", "vermouth-invoices")
	t.Setenv("GARAGE_ACCESS_KEY_ID", "GK0123456789abcdef01234567")
	t.Setenv("GARAGE_SECRET_ACCESS_KEY", secretKey)
	t.Setenv("VERMOUTH_RUN_ID", "0123456789ab")

	requests := 0
	client := &http.Client{
		Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			requests++
			require.Equal(t, "/vermouth-invoices/platform-probe/0123456789ab", request.URL.Path)
			require.NotEmpty(t, request.Header.Get("X-Amz-Date"))
			require.NotEmpty(t, request.Header.Get("X-Amz-Content-Sha256"))
			require.Contains(t, request.Header.Get("Authorization"), "Credential=GK0123456789abcdef01234567/")
			require.NotContains(t, request.Header.Get("Authorization"), secretKey)
			status := http.StatusOK
			if request.Method == http.MethodDelete {
				status = http.StatusNoContent
			}
			if request.Method == http.MethodGet && requests == 4 {
				status = http.StatusNotFound
			}
			return &http.Response{
				StatusCode: status,
				Body:       io.NopCloser(strings.NewReader("")),
				Header:     http.Header{},
			}, nil
		}),
		Timeout: 10 * time.Second,
	}
	now := func() time.Time {
		return time.Date(2026, time.August, 26, 12, 0, 0, 0, time.UTC)
	}

	require.NoError(t, run(t.Context(), client, now))
	require.Equal(t, 4, requests)
}

// covers: AC-11, AC-16
func TestRunReportsTheExactFailedS3Operation(t *testing.T) {
	t.Setenv("GARAGE_S3_ENDPOINT", "http://garage:3900")
	t.Setenv("GARAGE_S3_REGION", "garage")
	t.Setenv("GARAGE_BUCKET", "vermouth-invoices")
	t.Setenv("GARAGE_ACCESS_KEY_ID", "GK0123456789abcdef01234567")
	t.Setenv("GARAGE_SECRET_ACCESS_KEY", strings.Repeat("a", 64))
	t.Setenv("VERMOUTH_RUN_ID", "0123456789ab")

	tests := []struct {
		name      string
		failAt    int
		wantError string
	}{
		{name: "put", failAt: 1, wantError: "put probe object"},
		{name: "get", failAt: 2, wantError: "get probe object"},
		{name: "delete", failAt: 3, wantError: "delete probe object"},
		{name: "cleanup confirmation", failAt: 4, wantError: "confirm probe cleanup"},
	}
	//nolint:paralleltest // These subtests inherit the process environment configured by their parent.
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requests := 0
			client := &http.Client{Transport: roundTripperFunc(func(_ *http.Request) (*http.Response, error) {
				requests++
				status := http.StatusOK
				switch requests {
				case 3:
					status = http.StatusNoContent
				case 4:
					status = http.StatusNotFound
				}
				body := ""
				if requests == test.failAt {
					status = http.StatusInternalServerError
					body = "failure"
				}
				return &http.Response{
					StatusCode: status,
					Body:       io.NopCloser(strings.NewReader(body)),
					Header:     http.Header{},
				}, nil
			})}
			now := func() time.Time {
				return time.Date(2026, time.August, 26, 12, 0, 0, 0, time.UTC)
			}

			err := run(t.Context(), client, now)
			require.ErrorContains(t, err, test.wantError)
			require.Equal(t, test.failAt, requests)
		})
	}
}

func TestLoadConfig_AcceptsSupportedRunIdentities(t *testing.T) {
	tests := []struct {
		name  string
		runID string
	}{
		{name: "local hexadecimal identity", runID: "0123456789ab"},
		{name: "production workflow identity", runID: "34436178670-1"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("GARAGE_S3_ENDPOINT", "http://garage:3900")
			t.Setenv("GARAGE_S3_REGION", "garage")
			t.Setenv("GARAGE_BUCKET", "vermouth-invoices")
			t.Setenv("GARAGE_ACCESS_KEY_ID", "GK0123456789abcdef01234567")
			t.Setenv("GARAGE_SECRET_ACCESS_KEY", strings.Repeat("a", 64))
			t.Setenv("VERMOUTH_RUN_ID", test.runID)

			cfg, err := loadConfig()
			require.NoError(t, err)
			require.Equal(t, test.runID, cfg.runID)
		})
	}
}

func TestLoadConfig_RejectsMalformedRunIdentities(t *testing.T) {
	tests := []struct {
		name  string
		runID string
	}{
		{name: "short local identity", runID: "0123456789a"},
		{name: "uppercase local identity", runID: "0123456789aB"},
		{name: "zero workflow run", runID: "0-1"},
		{name: "zero workflow attempt", runID: "123-0"},
		{name: "extra workflow segment", runID: "123-1-1"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("GARAGE_S3_ENDPOINT", "http://garage:3900")
			t.Setenv("GARAGE_S3_REGION", "garage")
			t.Setenv("GARAGE_BUCKET", "vermouth-invoices")
			t.Setenv("GARAGE_ACCESS_KEY_ID", "GK0123456789abcdef01234567")
			t.Setenv("GARAGE_SECRET_ACCESS_KEY", strings.Repeat("a", 64))
			t.Setenv("VERMOUTH_RUN_ID", test.runID)

			_, err := loadConfig()
			require.ErrorContains(t, err, "GitHub run ID and attempt")
		})
	}
}

func TestLoadConfig_RejectsEndpointDrift(t *testing.T) {
	t.Setenv("GARAGE_S3_ENDPOINT", "http://127.0.0.1:3900")
	t.Setenv("GARAGE_S3_REGION", "garage")
	t.Setenv("GARAGE_BUCKET", "vermouth-invoices")
	t.Setenv("GARAGE_ACCESS_KEY_ID", "GK0123456789abcdef01234567")
	t.Setenv("GARAGE_SECRET_ACCESS_KEY", strings.Repeat("a", 64))
	t.Setenv("VERMOUTH_RUN_ID", "0123456789ab")

	_, err := loadConfig()
	require.ErrorContains(t, err, "exactly http://garage:3900")
}
