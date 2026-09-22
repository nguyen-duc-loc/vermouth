package route

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"
	"golang.org/x/sync/errgroup"

	"github.com/nguyen-duc-loc/vermouth/gateway/internal/aggregate"
	"github.com/nguyen-duc-loc/vermouth/gateway/internal/apitypes"
)

func readClassRates(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		classID, err := uuid.Parse(r.PathValue("class_id"))
		if err != nil {
			vermouth.WriteError(r.Context(), w, http.StatusBadRequest, "invalid_input", "class_id must be a UUID")
			return
		}
		path := "/classes/" + classID.String() + "/rates"
		var teachingResponse aggregate.Response
		var billingResponse aggregate.Response
		var teachingErr error
		var billingErr error
		group, ctx := errgroup.WithContext(r.Context())
		group.Go(func() error {
			teachingResponse, teachingErr = deps.Client.Call(
				ctx, http.MethodGet, deps.Client.Upstreams().Teaching,
				path, vermouth.BearerToken(r), nil,
			)
			return nil
		})
		group.Go(func() error {
			billingResponse, billingErr = deps.Client.Call(
				ctx, http.MethodGet, deps.Client.Upstreams().Billing,
				path, vermouth.BearerToken(r), nil,
			)
			return nil
		})
		_ = group.Wait()
		if teachingErr != nil {
			upstreamFailed(r.Context(), deps.Logger, w, "teaching", teachingErr)
			return
		}
		if teachingResponse.Status != http.StatusOK {
			passThrough(r.Context(), deps.Logger, w, teachingResponse)
			return
		}
		var result apitypes.ClassRates
		err = json.Unmarshal(teachingResponse.Body, &result)
		if err != nil {
			upstreamFailed(r.Context(), deps.Logger, w, "teaching", fmt.Errorf("decode class rate state: %w", err))
			return
		}
		result.Rates = []apitypes.ProjectedClassRate{}
		result.HistoryState = apitypes.Unavailable
		if billingErr == nil && billingResponse.Status == http.StatusOK {
			var projected apitypes.ClassRates
			err = json.Unmarshal(billingResponse.Body, &projected)
			if err != nil {
				upstreamFailed(r.Context(), deps.Logger, w, "billing", fmt.Errorf("decode class rate history: %w", err))
				return
			}
			result.Rates = projected.Rates
			result.ProjectedRevision = projected.ProjectedRevision
			result.HistoryState = apitypes.Syncing
			// The current revision counts commands on every date, including
			// backdated corrections. Each history row keeps its own revision.
			if len(projected.Rates) > 0 && projected.ProjectedRevision >= result.Current.RateRevision {
				result.HistoryState = apitypes.Synced
			}
		}
		w.Header().Set("Cache-Control", "no-store")
		vermouth.WriteJSON(r.Context(), deps.Logger, w, http.StatusOK, result)
	}
}

func putClassRate(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		classID, err := uuid.Parse(r.PathValue("class_id"))
		if err != nil {
			vermouth.WriteError(r.Context(), w, http.StatusBadRequest, "invalid_input", "class_id must be a UUID")
			return
		}
		effectiveDate := r.PathValue("effective_date")
		_, err = time.Parse(time.DateOnly, effectiveDate)
		if err != nil {
			vermouth.WriteError(r.Context(), w, http.StatusBadRequest, "invalid_input", "effective_date must be a calendar date")
			return
		}
		var input struct {
			apitypes.PutClassRateRequest

			RateAmount *int64 `json:"rate_amount"`
		}
		if !decodeJSONBody(w, r, &input) {
			return
		}
		if input.RateAmount == nil {
			vermouth.WriteError(r.Context(), w, http.StatusBadRequest, "invalid_input", "rate_amount must be an integer")
			return
		}
		input.PutClassRateRequest.RateAmount = *input.RateAmount
		response, err := deps.Client.CallWithHeaders(
			r.Context(), http.MethodPut, deps.Client.Upstreams().Teaching,
			"/classes/"+classID.String()+"/rates/"+effectiveDate,
			vermouth.BearerToken(r),
			http.Header{idempotencyHeader: []string{r.Header.Get(idempotencyHeader)}},
			input.PutClassRateRequest,
		)
		if err != nil {
			upstreamFailed(r.Context(), deps.Logger, w, "teaching", err)
			return
		}
		passThrough(r.Context(), deps.Logger, w, response)
	}
}

func proxyBillingPeriodDefault(deps Deps) http.HandlerFunc {
	return proxyBillingRead(deps, "/billing-periods/default")
}

func proxyGetBillingPeriod(deps Deps) http.HandlerFunc {
	return proxyBillingPeriod(deps, http.MethodGet, "", nil)
}

func proxyPreviewBillingPeriod(deps Deps) http.HandlerFunc {
	return proxyBillingPeriod(deps, http.MethodPost, "/preview", nil)
}

func proxyIssueBillingPeriod(deps Deps) http.HandlerFunc {
	return proxyBillingPeriod(deps, http.MethodPost, "/issue", decodeIssueBillingBody)
}

type billingBodyDecoder func(http.ResponseWriter, *http.Request) (any, bool)

func proxyBillingPeriod(
	deps Deps,
	method string,
	suffix string,
	decodeBody billingBodyDecoder,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		year, month, ok := parsedPeriodPath(w, r)
		if !ok {
			return
		}
		path := "/billing-periods/" + strconv.Itoa(year) + "/" + strconv.Itoa(month) + suffix
		var body any
		if decodeBody != nil {
			var decoded bool
			body, decoded = decodeBody(w, r)
			if !decoded {
				return
			}
		}
		response, err := deps.Client.CallBilling(
			r.Context(), method,
			path, vermouth.BearerToken(r), body,
		)
		if err != nil {
			upstreamFailed(r.Context(), deps.Logger, w, "billing", err)
			return
		}
		passThroughBilling(r.Context(), deps, w, response)
	}
}

func decodeIssueBillingBody(w http.ResponseWriter, r *http.Request) (any, bool) {
	var input apitypes.IssueBillingRequest
	if !decodeJSONBody(w, r, &input) {
		return nil, false
	}
	return input, true
}

func parsedPeriodPath(w http.ResponseWriter, r *http.Request) (year int, month int, ok bool) {
	year, yearErr := strconv.Atoi(r.PathValue("year"))
	month, monthErr := strconv.Atoi(r.PathValue("month"))
	if yearErr != nil || monthErr != nil || year < 2000 || month < 1 || month > 12 {
		vermouth.WriteError(
			r.Context(), w, http.StatusBadRequest, "invalid_period",
			"year and month must name a month from year 2000",
		)
		return 0, 0, false
	}
	return year, month, true
}

func passThroughBilling(
	ctx context.Context,
	deps Deps,
	w http.ResponseWriter,
	response aggregate.Response,
) {
	if response.Status != http.StatusServiceUnavailable {
		passThrough(ctx, deps.Logger, w, response)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if cacheControl := response.Header.Get("Cache-Control"); cacheControl != "" {
		w.Header().Set("Cache-Control", cacheControl)
	}
	if retryAfter := response.Header.Get("Retry-After"); retryAfter != "" {
		w.Header().Set("Retry-After", retryAfter)
	}
	w.WriteHeader(response.Status)
	_, _ = w.Write(response.Body)
}
