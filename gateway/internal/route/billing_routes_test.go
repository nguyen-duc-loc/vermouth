package route_test

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nguyen-duc-loc/vermouth/gateway/internal/aggregate"
	"github.com/nguyen-duc-loc/vermouth/gateway/internal/apitypes"
	"github.com/nguyen-duc-loc/vermouth/gateway/internal/route"
)

// covers: spec 0013 AC-19, AC-21
func TestMuxCombinesTeachingTruthWithProjectedRateHistory(t *testing.T) {
	t.Parallel()

	classID := "018f8f7e-91b0-7cc4-bd8c-f4d9030ca421"
	teaching := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/classes/"+classID+"/rates", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"class_id":"018f8f7e-91b0-7cc4-bd8c-f4d9030ca421",
			"current":{
				"rate_amount":300000,
				"currency":"VND",
				"effective_from":"2026-09-01",
				"rate_revision":3
			},
			"allowed_range":{"from":"2026-08-01","through":"2026-09-22"},
			"archived":false
		}`))
	}))
	t.Cleanup(teaching.Close)
	billing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/classes/"+classID+"/rates", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"rates":[
				{"effective_from":"2026-09-01","rate_amount":300000,"currency":"VND","rate_revision":3},
				{"effective_from":"2026-08-01","rate_amount":250000,"currency":"VND","rate_revision":1}
			],
			"projected_revision":3
		}`))
	}))
	t.Cleanup(billing.Close)
	verifier, token := gatewayTestToken(t)
	handler := route.Mux(route.Deps{
		Client: aggregate.NewClient(aggregate.Upstreams{
			Teaching: teaching.URL,
			Billing:  billing.URL,
		}),
		Verifier: verifier,
		Logger:   slog.New(slog.DiscardHandler),
		Service:  "gateway",
	})
	request := httptest.NewRequestWithContext(
		t.Context(), http.MethodGet, "/api/classes/"+classID+"/rates", http.NoBody,
	)
	request.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
	require.JSONEq(t, `{
		"class_id":"018f8f7e-91b0-7cc4-bd8c-f4d9030ca421",
		"current":{
			"rate_amount":300000,
			"currency":"VND",
			"effective_from":"2026-09-01",
			"rate_revision":3
		},
		"allowed_range":{"from":"2026-08-01","through":"2026-09-22"},
		"archived":false,
		"rates":[
			{"effective_from":"2026-09-01","rate_amount":300000,"currency":"VND","rate_revision":3},
			{"effective_from":"2026-08-01","rate_amount":250000,"currency":"VND","rate_revision":1}
		],
		"projected_revision":3,
		"history_state":"synced"
	}`, recorder.Body.String())
}

// covers: spec 0013 AC-1, AC-2, AC-21
func TestMuxForwardsRateCommandsWithTheReceiptKey(t *testing.T) {
	t.Parallel()

	type forwardedRequest struct {
		method         string
		path           string
		authorization  string
		idempotencyKey string
		body           string
	}
	forwarded := make(chan forwardedRequest, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		forwarded <- forwardedRequest{
			method: r.Method, path: r.URL.Path,
			authorization:  r.Header.Get("Authorization"),
			idempotencyKey: r.Header.Get("Idempotency-Key"),
			body:           string(body),
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"rate_revision":2}`))
	}))
	t.Cleanup(upstream.Close)
	verifier, token := gatewayTestToken(t)
	handler := route.Mux(route.Deps{
		Client:   aggregate.NewClient(aggregate.Upstreams{Teaching: upstream.URL}),
		Verifier: verifier,
		Logger:   slog.New(slog.DiscardHandler),
		Service:  "gateway",
	})
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPut,
		"/api/classes/018f8f7e-91b0-7cc4-bd8c-f4d9030ca421/rates/2026-09-01",
		strings.NewReader(`{"rate_amount":300000}`),
	)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Idempotency-Key", "rate-command")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	received := <-forwarded
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, http.MethodPut, received.method)
	require.Equal(t, "/classes/018f8f7e-91b0-7cc4-bd8c-f4d9030ca421/rates/2026-09-01", received.path)
	require.Equal(t, "Bearer "+token, received.authorization)
	require.Equal(t, "rate-command", received.idempotencyKey)
	require.JSONEq(t, `{"rate_amount":300000}`, received.body)
}

// covers: spec 0013 AC-8, AC-21
func TestMuxPreservesProjectionRetryHeaders(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The real barrier spends five seconds after request parsing and the run read.
		select {
		case <-time.After(5100 * time.Millisecond):
		case <-r.Context().Done():
			return
		}
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/billing-periods/2026/8/preview", r.URL.Path)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{
			"error":{
				"code":"projection_sync_pending",
				"message":"billing is still catching up with teaching",
				"request_id":"request-billing"
			}
		}`))
	}))
	t.Cleanup(upstream.Close)
	verifier, token := gatewayTestToken(t)
	handler := route.Mux(route.Deps{
		Client:   aggregate.NewClient(aggregate.Upstreams{Billing: upstream.URL}),
		Verifier: verifier,
		Logger:   slog.New(slog.DiscardHandler),
		Service:  "gateway",
	})
	request := httptest.NewRequestWithContext(
		t.Context(), http.MethodPost, "/api/billing-periods/2026/8/preview", http.NoBody,
	)
	request.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
	require.Equal(t, "1", recorder.Header().Get("Retry-After"))
	require.Contains(t, recorder.Body.String(), `"code":"projection_sync_pending"`)
}

// covers: spec 0013 AC-1, AC-21
func TestMuxRequiresAnExplicitRateAmount(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		body   string
		status int
	}{
		{"empty object", `{}`, http.StatusBadRequest},
		{"null body", `null`, http.StatusBadRequest},
		{"null amount", `{"rate_amount":null}`, http.StatusBadRequest},
		{"explicit zero", `{"rate_amount":0}`, http.StatusOK},
	} {
		for _, endpoint := range []struct{ name, method, path string }{
			{"rate", http.MethodPut, "/api/classes/018f8f7e-91b0-7cc4-bd8c-f4d9030ca421/rates/2026-08-01"},
			{"class", http.MethodPost, "/api/classes"},
		} {
			t.Run(endpoint.name+"/"+test.name, func(t *testing.T) {
				t.Parallel()
				forwarded := make(chan string, 1)
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					body, _ := io.ReadAll(r.Body)
					forwarded <- string(body)
					_, _ = w.Write([]byte(`{}`))
				}))
				t.Cleanup(upstream.Close)
				verifier, token := gatewayTestToken(t)
				handler := route.Mux(route.Deps{
					Client:   aggregate.NewClient(aggregate.Upstreams{Teaching: upstream.URL}),
					Verifier: verifier, Logger: slog.New(slog.DiscardHandler), Service: "gateway",
				})
				request := httptest.NewRequestWithContext(t.Context(), endpoint.method, endpoint.path, strings.NewReader(test.body))
				request.Header.Set("Authorization", "Bearer "+token)
				recorder := httptest.NewRecorder()
				handler.ServeHTTP(recorder, request)
				require.Equal(t, test.status, recorder.Code)
				if test.status == http.StatusOK {
					var amount struct {
						RateAmount *int64 `json:"rate_amount"`
					}
					require.NoError(t, json.Unmarshal([]byte(<-forwarded), &amount))
					require.NotNil(t, amount.RateAmount)
					require.Zero(t, *amount.RateAmount)
				} else {
					require.Empty(t, forwarded, "invalid money must not reach teaching")
				}
			})
		}
	}
}

// covers: spec 0013 AC-20, AC-21
//
//nolint:paralleltest // Large response cases run sequentially to bound test memory.
func TestMuxPreservesTheLargestSupportedBillingPreview(t *testing.T) {
	for _, label := range []string{"\U00020BB7", "<"} {
		t.Run(label, func(t *testing.T) {
			preview, run := maximumBillingResponses(label)
			for _, test := range []struct {
				name, method, path, requestBody string
				response                        any
			}{
				{"preview", http.MethodPost, "/preview", "", preview},
				{"issue", http.MethodPost, "/issue", `{"preview_fingerprint":"reviewed"}`, run},
				{"read", http.MethodGet, "", "", apitypes.BillingPeriodState{Status: "already_issued", Period: run.Period, Run: &run}},
			} {
				t.Run(test.name, func(t *testing.T) {
					body, err := json.Marshal(test.response)
					require.NoError(t, err)
					require.Greater(t, len(body), 4<<20)
					upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						w.Header().Set("Content-Type", "application/json")
						_, _ = w.Write(body)
					}))
					t.Cleanup(upstream.Close)
					verifier, token := gatewayTestToken(t)
					handler := route.Mux(route.Deps{
						Client:   aggregate.NewClient(aggregate.Upstreams{Billing: upstream.URL}),
						Verifier: verifier, Logger: slog.New(slog.DiscardHandler), Service: "gateway",
					})
					request := httptest.NewRequestWithContext(t.Context(), test.method,
						"/api/billing-periods/2026/8"+test.path, strings.NewReader(test.requestBody))
					request.Header.Set("Authorization", "Bearer "+token)
					recorder := httptest.NewRecorder()
					handler.ServeHTTP(recorder, request)
					require.Equal(t, http.StatusOK, recorder.Code)
					require.Equal(t, len(body), recorder.Body.Len())
					require.True(t, json.Valid(recorder.Body.Bytes()))
					require.Equal(t, body, recorder.Body.Bytes())
				})
			}
		})
	}
}

func maximumBillingResponses(label string) (apitypes.BillingPreview, apitypes.BillingRun) {
	period := apitypes.BillingPeriod{Year: 2026, Month: 8}
	preview := apitypes.BillingPreview{
		Status: "ready", Period: period, Currency: "VND", Students: []apitypes.BillingStudentTotal{},
		Blockers: []apitypes.BillingBlocker{}, PreviewFingerprint: new(strings.Repeat("a", 43)),
	}
	run := apitypes.BillingRun{
		BillingRunId: uuid.Must(uuid.NewV7()), Period: period, Generation: 1, Currency: "VND", CreatedAt: time.Now().UTC(),
	}
	classID := uuid.Must(uuid.NewV7())
	lines := make([]apitypes.BillingLine, 0, 20)
	for index := range 20 {
		lines = append(lines, apitypes.BillingLine{
			SessionId: uuid.Must(uuid.NewV7()), ClassId: &classID, ClassName: strings.Repeat(label, 120),
			LocalDate:  types.Date{Time: time.Date(2026, time.August, index+1, 0, 0, 0, 0, time.UTC)},
			RateAmount: 1_000_000_000, Amount: 1_000_000_000, Currency: "VND",
		})
	}
	for index := range 500 {
		student := apitypes.BillingStudentTotal{
			StudentId: uuid.Must(uuid.NewV7()), StudentName: strings.Repeat(label, 160), Lines: lines,
			TotalAmount: 20_000_000_000, Currency: "VND",
		}
		preview.Students = append(preview.Students, student)
		preview.GrandTotal += student.TotalAmount
		run.Invoices = append(run.Invoices, apitypes.IssuedInvoice{
			InvoiceId: uuid.Must(uuid.NewV7()), StudentId: student.StudentId, StudentName: student.StudentName,
			InvoiceNumber: fmt.Sprintf("2026-%04d", index+1), IssuedAt: run.CreatedAt,
			Lines: lines, TotalAmount: student.TotalAmount, Currency: "VND",
		})
	}
	run.GrandTotal = preview.GrandTotal
	return preview, run
}

// covers: spec 0013 AC-19
func TestMuxRecognizesAProjectedBackdatedRateCorrection(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/teaching/") {
			_, _ = w.Write([]byte(`{"current":{"effective_from":"2026-09-01","rate_revision":4}}`))
			return
		}
		_, _ = w.Write([]byte(`{"projected_revision":4,"rates":[
			{"effective_from":"2026-09-01","rate_revision":3},
			{"effective_from":"2026-08-01","rate_revision":4}]}`))
	}))
	t.Cleanup(upstream.Close)
	verifier, token := gatewayTestToken(t)
	handler := route.Mux(route.Deps{
		Client:   aggregate.NewClient(aggregate.Upstreams{Teaching: upstream.URL + "/teaching", Billing: upstream.URL}),
		Verifier: verifier, Logger: slog.New(slog.DiscardHandler), Service: "gateway",
	})
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet,
		"/api/classes/018f8f7e-91b0-7cc4-bd8c-f4d9030ca421/rates", http.NoBody)
	request.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	var result apitypes.ClassRates
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &result))
	require.Equal(t, apitypes.Synced, result.HistoryState)
}

// covers: spec 0013 AC-4, AC-21
func TestMuxRejectsInvalidBillingPeriodsBeforeForwarding(t *testing.T) {
	t.Parallel()

	verifier, token := gatewayTestToken(t)
	handler := route.Mux(route.Deps{
		Client:   aggregate.NewClient(aggregate.Upstreams{}),
		Verifier: verifier,
		Logger:   slog.New(slog.DiscardHandler),
		Service:  "gateway",
	})
	tests := []string{
		"/api/billing-periods/1999/12",
		"/api/billing-periods/2026/13",
		"/api/billing-periods/year/8",
	}
	for _, path := range tests {
		request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, http.NoBody)
		request.Header.Set("Authorization", "Bearer "+token)
		recorder := httptest.NewRecorder()

		handler.ServeHTTP(recorder, request)

		require.Equal(t, http.StatusBadRequest, recorder.Code)
		require.Contains(t, recorder.Body.String(), `"code":"invalid_period"`)
	}
}
