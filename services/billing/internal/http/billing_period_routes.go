package http

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"

	"github.com/nguyen-duc-loc/vermouth/services/billing/internal/handler"
)

const billingMaxRequestBody = 64 << 10

func getBillingPeriodDefault(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		claims, ok := verifiedBillingClaims(deps, w, r)
		if !ok {
			return
		}
		result, err := deps.Billing.DefaultPeriod(claims.Timezone)
		if err != nil {
			writeBillingFailure(deps, w, r, "default period", err)
			return
		}
		vermouth.WriteJSON(r.Context(), deps.Logger, w, http.StatusOK, result)
	}
}

func getProjectedClassRates(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		claims, ok := verifiedBillingClaims(deps, w, r)
		if !ok {
			return
		}
		classID, err := uuid.Parse(r.PathValue("class_id"))
		if err != nil {
			vermouth.WriteError(r.Context(), w, http.StatusBadRequest, "invalid_input", "class_id must be a UUID")
			return
		}
		result, err := deps.Billing.ReadClassRateHistory(r.Context(), claims.TutorID, classID)
		if err != nil {
			writeBillingFailure(deps, w, r, "class rate history", err)
			return
		}
		vermouth.WriteJSON(r.Context(), deps.Logger, w, http.StatusOK, result)
	}
}

func getBillingPeriod(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		claims, ok := verifiedBillingClaims(deps, w, r)
		if !ok {
			return
		}
		period, ok := parseBillingPeriod(w, r)
		if !ok {
			return
		}
		result, err := deps.Billing.ReadPeriod(
			r.Context(), claims.TutorID, claims.Timezone, period,
		)
		if err != nil {
			writeBillingFailure(deps, w, r, "read period", err)
			return
		}
		vermouth.WriteJSON(r.Context(), deps.Logger, w, http.StatusOK, result)
	}
}

func previewBillingPeriod(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		claims, ok := verifiedBillingClaims(deps, w, r)
		if !ok {
			return
		}
		period, ok := parseBillingPeriod(w, r)
		if !ok {
			return
		}
		result, err := deps.Billing.Preview(
			r.Context(), claims.TutorID, claims.Timezone, period,
		)
		if err != nil {
			writeBillingFailure(deps, w, r, "preview period", err)
			return
		}
		vermouth.WriteJSON(r.Context(), deps.Logger, w, http.StatusOK, result)
	}
}

func issueBillingPeriod(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		claims, ok := verifiedBillingClaims(deps, w, r)
		if !ok {
			return
		}
		period, ok := parseBillingPeriod(w, r)
		if !ok {
			return
		}
		var input handler.IssueBillingInput
		if !decodeBillingJSON(w, r, &input) {
			return
		}
		result, status, err := deps.Billing.Issue(
			r.Context(), claims.TutorID, claims.Timezone, period, input,
		)
		if err != nil {
			writeBillingFailure(deps, w, r, "issue period", err)
			return
		}
		vermouth.WriteJSON(r.Context(), deps.Logger, w, status, result)
	}
}

func verifiedBillingClaims(
	deps Deps,
	w http.ResponseWriter,
	r *http.Request,
) (vermouth.Claims, bool) {
	claims, err := deps.Verifier.Verify(vermouth.BearerToken(r))
	if err != nil {
		vermouth.WriteError(
			r.Context(), w, http.StatusUnauthorized, "invalid_token", "a valid bearer token is required",
		)
		return vermouth.Claims{}, false
	}
	return claims, true
}

func parseBillingPeriod(w http.ResponseWriter, r *http.Request) (handler.BillingPeriod, bool) {
	year, yearErr := strconv.Atoi(r.PathValue("year"))
	month, monthErr := strconv.Atoi(r.PathValue("month"))
	if yearErr != nil || monthErr != nil {
		vermouth.WriteError(
			r.Context(), w, http.StatusBadRequest, "invalid_period", "year and month must be integers",
		)
		return handler.BillingPeriod{}, false
	}
	return handler.BillingPeriod{Year: year, Month: month}, true
}

func decodeBillingJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, billingMaxRequestBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err := decoder.Decode(target)
	if err != nil {
		vermouth.WriteError(r.Context(), w, http.StatusBadRequest, "invalid_input", "the JSON body is invalid")
		return false
	}
	err = decoder.Decode(&struct{}{})
	if !errors.Is(err, io.EOF) {
		vermouth.WriteError(r.Context(), w, http.StatusBadRequest, "invalid_input", "the JSON body must contain one value")
		return false
	}
	return true
}

func writeBillingFailure(
	deps Deps,
	w http.ResponseWriter,
	r *http.Request,
	operation string,
	err error,
) {
	periodErr, ok := errors.AsType[*handler.BillingPeriodError](err)
	if ok {
		if periodErr.RetryAfter > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(periodErr.RetryAfter))
		}
		vermouth.WriteErrorDetails(
			r.Context(), w, periodErr.Status, periodErr.Code, periodErr.Message, periodErr.Details,
		)
		return
	}
	deps.Logger.ErrorContext(r.Context(), "Billing request failed",
		slog.String("request_id", vermouth.RequestID(r.Context())),
		slog.String("operation", operation),
		slog.String("error", err.Error()),
	)
	vermouth.WriteError(
		r.Context(), w, http.StatusInternalServerError, "internal", "billing could not complete the request",
	)
}
