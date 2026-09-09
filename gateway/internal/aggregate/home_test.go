package aggregate_test

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"
	"github.com/stretchr/testify/require"

	"github.com/nguyen-duc-loc/vermouth/gateway/internal/aggregate"
)

type upstreamRequest struct {
	path          string
	cursor        string
	authorization string
	requestID     string
}

// covers: AC-7, AC-8, AC-9, AC-15
func TestHome_CombinesRequiredTruthWithOptionalProjectionProgress(t *testing.T) {
	t.Parallel()

	var (
		mu       sync.Mutex
		requests []upstreamRequest
	)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, upstreamRequest{
			path:          r.URL.Path,
			cursor:        r.URL.Query().Get("cursor"),
			authorization: r.Header.Get("Authorization"),
			requestID:     r.Header.Get(vermouth.RequestIDHeader),
		})
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/me":
			_, _ = w.Write([]byte(`{
				"tutor_id":"018f8f7e-91b0-7cc4-bd8c-f4d9030ca421",
				"email":"tutor@example.com",
				"display_name":"Tutor",
				"timezone":"Asia/Ho_Chi_Minh",
				"language":"en",
				"created_at":"2026-08-30T00:00:00Z"
			}`))
		case "/home":
			_, _ = w.Write([]byte(`{
				"request_time_zone":"Asia/Ho_Chi_Minh",
				"local_date":"2026-08-30",
				"next_local_midnight_at":"2026-08-30T17:00:00Z",
				"setup_defaults":{"local_date":"2026-08-30","start_time":"10:00","end_time":"11:00"},
				"sessions":[],
				"next_cursor":null
			}`))
		case "/projections/teaching/status":
			_, _ = w.Write([]byte(`{
				"state":"active",
				"class_count":1,
				"session_count":1,
				"student_count":1,
				"open_roster_count":1,
				"attendance_count":1,
				"latest_updated_at":"2026-08-30T03:00:00Z"
			}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(upstream.Close)

	client := aggregate.NewClient(aggregate.Upstreams{
		Identity: upstream.URL,
		Teaching: upstream.URL,
		Billing:  upstream.URL,
	})
	ctx := vermouth.WithRequestID(t.Context(), "request-home")

	home, err := client.Home(ctx, "access-token", "same day + cursor")

	require.NoError(t, err)
	require.Equal(t, "Tutor", home.Tutor.DisplayName)
	require.Equal(t, "Asia/Ho_Chi_Minh", home.RequestTimeZone)
	require.Empty(t, home.Sessions)
	require.NotNil(t, home.BillingProjection)
	require.EqualValues(t, 1, home.BillingProjection.AttendanceCount)
	require.Nil(t, home.BillingProjectionUnavailable)
	require.Len(t, requests, 3)
	for _, request := range requests {
		require.Equal(t, "Bearer access-token", request.authorization)
		require.Equal(t, "request-home", request.requestID)
		if request.path == "/home" {
			require.Equal(t, "same day + cursor", request.cursor)
		}
	}
}

// covers: AC-8, AC-9, AC-15
func TestHome_BillingFailureLeavesRequiredTeachingTruthAvailable(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/me":
			_, _ = w.Write([]byte(`{"display_name":"Tutor"}`))
		case "/home":
			_, _ = w.Write([]byte(`{
				"request_time_zone":"UTC",
				"local_date":"2026-08-30",
				"next_local_midnight_at":"2026-08-31T00:00:00Z",
				"setup_defaults":{"local_date":"2026-08-30","start_time":"10:00","end_time":"11:00"},
				"sessions":[],
				"next_cursor":null
			}`))
		case "/projections/teaching/status":
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"database unavailable"}`))
		}
	}))
	t.Cleanup(upstream.Close)

	client := aggregate.NewClient(aggregate.Upstreams{
		Identity: upstream.URL,
		Teaching: upstream.URL,
		Billing:  upstream.URL,
	})

	home, err := client.Home(t.Context(), "access-token", "")

	require.NoError(t, err)
	require.Equal(t, "Tutor", home.Tutor.DisplayName)
	require.Empty(t, home.Sessions)
	require.Nil(t, home.BillingProjection)
	require.NotNil(t, home.BillingProjectionUnavailable)
	require.Equal(t, "billing projection is temporarily unavailable", *home.BillingProjectionUnavailable)
}

// covers: AC-7, AC-12
func TestHome_RequiredServiceErrorKeepsItsBoundaryAnswer(t *testing.T) {
	t.Parallel()

	identity := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"code":"unauthenticated","message":"token expired","request_id":"request-home"}}`))
	}))
	t.Cleanup(identity.Close)
	teaching := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{
			"request_time_zone":"UTC",
			"local_date":"2026-08-30",
			"next_local_midnight_at":"2026-08-31T00:00:00Z",
			"setup_defaults":{"local_date":"2026-08-30","start_time":"10:00","end_time":"11:00"},
			"sessions":[],
			"next_cursor":null
		}`))
	}))
	t.Cleanup(teaching.Close)
	billing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(billing.Close)

	client := aggregate.NewClient(aggregate.Upstreams{
		Identity: identity.URL,
		Teaching: teaching.URL,
		Billing:  billing.URL,
	})

	_, err := client.Home(t.Context(), "expired-token", "")

	var responseError *aggregate.RequiredResponseError
	require.ErrorAs(t, err, &responseError)
	require.Equal(t, "identity", responseError.Service)
	require.Equal(t, http.StatusUnauthorized, responseError.Response.Status)
	require.Contains(t, string(responseError.Response.Body), "token expired")
}

// covers: AC-9, AC-15
func TestHomeBillingProjection_ReturnsUnavailableWithoutCallingRequiredServices(t *testing.T) {
	t.Parallel()

	called := make(chan string, 1)
	billing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called <- r.URL.Path
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(billing.Close)
	client := aggregate.NewClient(aggregate.Upstreams{Billing: billing.URL})

	result := client.HomeBillingProjection(t.Context(), "access-token")

	require.Equal(t, "/projections/teaching/status", <-called)
	require.Nil(t, result.BillingProjection)
	require.NotNil(t, result.BillingProjectionUnavailable)
}
