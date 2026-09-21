package http

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	nethttp "net/http"

	"github.com/google/uuid"
	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"

	"github.com/nguyen-duc-loc/vermouth/services/billing/internal/handler"
)

const profileMaxRequestBody = 64 << 10

type banksResponse struct {
	Banks []handler.Bank `json:"banks"`
}

func getInvoiceProfile(deps Deps) nethttp.HandlerFunc {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		w.Header().Set("Cache-Control", "no-store")
		tutorID, ok := verifiedTutor(deps, w, r)
		if !ok {
			return
		}
		profile, err := deps.Profile.Read(r.Context(), tutorID)
		if err != nil {
			writeProfileFailure(deps, w, r, "read", err)
			return
		}
		vermouth.WriteJSON(r.Context(), deps.Logger, w, nethttp.StatusOK, profile)
	}
}

func putInvoiceProfile(deps Deps) nethttp.HandlerFunc {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		w.Header().Set("Cache-Control", "no-store")
		tutorID, ok := verifiedTutor(deps, w, r)
		if !ok {
			return
		}
		input, ok := decodeProfileInput(w, r)
		if !ok {
			return
		}
		profile, err := deps.Profile.Save(r.Context(), tutorID, input)
		if err != nil {
			writeProfileFailure(deps, w, r, "save", err)
			return
		}
		deps.Logger.InfoContext(r.Context(), "Invoice profile saved",
			slog.String("request_id", vermouth.RequestID(r.Context())),
			slog.String("tutor_id", tutorID.String()),
			slog.Int64("revision", profile.Revision),
			slog.Bool("is_complete", profile.IsComplete),
		)
		vermouth.WriteJSON(r.Context(), deps.Logger, w, nethttp.StatusOK, profile)
	}
}

func getBanks(deps Deps) nethttp.HandlerFunc {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if _, ok := verifiedTutor(deps, w, r); !ok {
			return
		}
		w.Header().Set("Cache-Control", "private, max-age=86400")
		vermouth.WriteJSON(r.Context(), deps.Logger, w, nethttp.StatusOK, banksResponse{Banks: handler.ActiveBanks()})
	}
}

func verifiedTutor(deps Deps, w nethttp.ResponseWriter, r *nethttp.Request) (uuid.UUID, bool) {
	claims, err := deps.Verifier.Verify(vermouth.BearerToken(r))
	if err != nil {
		vermouth.WriteError(
			r.Context(), w, nethttp.StatusUnauthorized, "unauthenticated", "a valid bearer token is required",
		)
		return uuid.Nil, false
	}
	return claims.TutorID, true
}

func decodeProfileInput(w nethttp.ResponseWriter, r *nethttp.Request) (handler.ProfileInput, bool) {
	r.Body = nethttp.MaxBytesReader(w, r.Body, profileMaxRequestBody)
	decoder := json.NewDecoder(r.Body)
	var body map[string]json.RawMessage
	err := decoder.Decode(&body)
	if err != nil {
		writeInvalidProfileShape(w, r)
		return handler.ProfileInput{}, false
	}
	err = decoder.Decode(&struct{}{})
	if !errors.Is(err, io.EOF) {
		writeInvalidProfileShape(w, r)
		return handler.ProfileInput{}, false
	}
	required := []string{
		"expected_revision", "legal_name", "contact_line", "bank_code",
		"bank_account_number", "bank_account_holder",
	}
	if len(body) != len(required) {
		writeInvalidProfileShape(w, r)
		return handler.ProfileInput{}, false
	}
	for _, key := range required {
		if _, found := body[key]; !found {
			writeInvalidProfileShape(w, r)
			return handler.ProfileInput{}, false
		}
	}
	var input handler.ProfileInput
	if json.Unmarshal(body["expected_revision"], &input.ExpectedRevision) != nil || input.ExpectedRevision < 0 ||
		json.Unmarshal(body["legal_name"], &input.LegalName) != nil ||
		json.Unmarshal(body["contact_line"], &input.ContactLine) != nil ||
		json.Unmarshal(body["bank_code"], &input.BankCode) != nil ||
		json.Unmarshal(body["bank_account_number"], &input.BankAccountNumber) != nil ||
		json.Unmarshal(body["bank_account_holder"], &input.BankAccountHolder) != nil {
		writeInvalidProfileShape(w, r)
		return handler.ProfileInput{}, false
	}
	return input, true
}

func writeInvalidProfileShape(w nethttp.ResponseWriter, r *nethttp.Request) {
	vermouth.WriteError(
		r.Context(), w, nethttp.StatusBadRequest, "invalid_input", "the profile body must contain every editable field",
	)
}

func writeProfileFailure(deps Deps, w nethttp.ResponseWriter, r *nethttp.Request, operation string, err error) {
	if validation, ok := errors.AsType[*handler.ProfileValidationError](err); ok {
		vermouth.WriteErrorDetails(
			r.Context(), w, nethttp.StatusUnprocessableEntity, "invalid_profile",
			"the profile contains invalid fields", map[string]any{"fields": validation.Fields},
		)
		return
	}
	if conflict, ok := errors.AsType[*handler.ProfileConflictError](err); ok {
		vermouth.WriteErrorDetails(
			r.Context(), w, nethttp.StatusConflict, "profile_conflict",
			"the profile changed after it was read", map[string]any{"current_revision": conflict.CurrentRevision},
		)
		return
	}
	deps.Logger.ErrorContext(r.Context(), "Invoice profile request failed",
		slog.String("request_id", vermouth.RequestID(r.Context())),
		slog.String("operation", operation),
		slog.String("error", err.Error()),
	)
	vermouth.WriteError(
		r.Context(), w, nethttp.StatusInternalServerError, "internal", "billing could not complete the profile request",
	)
}
