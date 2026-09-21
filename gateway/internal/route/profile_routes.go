package route

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"

	"github.com/nguyen-duc-loc/vermouth/gateway/internal/apitypes"
)

func proxyGetInvoiceProfile(deps Deps) http.HandlerFunc {
	return proxyBillingRead(deps, "/invoice-profile")
}

func proxyGetBanks(deps Deps) http.HandlerFunc {
	return proxyBillingRead(deps, "/banks")
}

func proxyPutInvoiceProfile(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		input, ok := decodeInvoiceProfileBody(w, r)
		if !ok {
			return
		}
		response, err := deps.Client.Call(
			r.Context(), http.MethodPut, deps.Client.Upstreams().Billing,
			"/invoice-profile", vermouth.BearerToken(r), input,
		)
		if err != nil {
			upstreamFailed(r.Context(), deps.Logger, w, "billing", err)
			return
		}
		passThrough(r.Context(), deps.Logger, w, response)
	}
}

func decodeInvoiceProfileBody(w http.ResponseWriter, r *http.Request) (apitypes.PutInvoiceProfileRequest, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	decoder := json.NewDecoder(r.Body)
	var body map[string]json.RawMessage
	err := decoder.Decode(&body)
	if err != nil {
		writeInvalidInvoiceProfileBody(w, r)
		return apitypes.PutInvoiceProfileRequest{}, false
	}
	err = decoder.Decode(&struct{}{})
	if !errors.Is(err, io.EOF) {
		writeInvalidInvoiceProfileBody(w, r)
		return apitypes.PutInvoiceProfileRequest{}, false
	}
	required := []string{
		"expected_revision", "legal_name", "contact_line", "bank_code",
		"bank_account_number", "bank_account_holder",
	}
	if len(body) != len(required) {
		writeInvalidInvoiceProfileBody(w, r)
		return apitypes.PutInvoiceProfileRequest{}, false
	}
	for _, key := range required {
		if _, found := body[key]; !found {
			writeInvalidInvoiceProfileBody(w, r)
			return apitypes.PutInvoiceProfileRequest{}, false
		}
	}
	var input apitypes.PutInvoiceProfileRequest
	if json.Unmarshal(body["expected_revision"], &input.ExpectedRevision) != nil || input.ExpectedRevision < 0 ||
		json.Unmarshal(body["legal_name"], &input.LegalName) != nil ||
		json.Unmarshal(body["contact_line"], &input.ContactLine) != nil ||
		json.Unmarshal(body["bank_code"], &input.BankCode) != nil ||
		json.Unmarshal(body["bank_account_number"], &input.BankAccountNumber) != nil ||
		json.Unmarshal(body["bank_account_holder"], &input.BankAccountHolder) != nil {
		writeInvalidInvoiceProfileBody(w, r)
		return apitypes.PutInvoiceProfileRequest{}, false
	}
	return input, true
}

func writeInvalidInvoiceProfileBody(w http.ResponseWriter, r *http.Request) {
	vermouth.WriteError(
		r.Context(), w, http.StatusBadRequest, "invalid_input",
		"the profile body must contain every editable field",
	)
}

func proxyBillingRead(deps Deps, path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		response, err := deps.Client.Call(
			r.Context(), http.MethodGet, deps.Client.Upstreams().Billing,
			path, vermouth.BearerToken(r), nil,
		)
		if err != nil {
			upstreamFailed(r.Context(), deps.Logger, w, "billing", err)
			return
		}
		passThrough(r.Context(), deps.Logger, w, response)
	}
}
