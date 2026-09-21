package route

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"

	"github.com/nguyen-duc-loc/vermouth/gateway/internal/apitypes"
)

func proxyGetClassRoster(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		classID, ok := gatewayClassID(w, r)
		if !ok {
			return
		}
		path := "/classes/" + classID.String() + "/roster"
		if r.URL.RawQuery != "" {
			path += "?" + r.URL.RawQuery
		}
		response, err := deps.Client.Call(
			r.Context(),
			http.MethodGet,
			deps.Client.Upstreams().Teaching,
			path,
			vermouth.BearerToken(r),
			nil,
		)
		if err != nil {
			upstreamFailed(r.Context(), deps.Logger, w, "teaching", err)
			return
		}
		passThrough(r.Context(), deps.Logger, w, response)
	}
}

func proxyChangeClassRoster(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		classID, ok := gatewayClassID(w, r)
		if !ok {
			return
		}
		var input apitypes.ChangeRosterRequest
		if !decodeJSONBody(w, r, &input) {
			return
		}
		response, err := deps.Client.CallWithHeaders(
			r.Context(),
			http.MethodPut,
			deps.Client.Upstreams().Teaching,
			"/classes/"+classID.String()+"/roster",
			vermouth.BearerToken(r),
			http.Header{idempotencyHeader: []string{r.Header.Get(idempotencyHeader)}},
			input,
		)
		if err != nil {
			upstreamFailed(r.Context(), deps.Logger, w, "teaching", err)
			return
		}
		passThrough(r.Context(), deps.Logger, w, response)
	}
}

func gatewayClassID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	classID, err := uuid.Parse(r.PathValue("class_id"))
	if err != nil {
		vermouth.WriteError(
			r.Context(), w, http.StatusBadRequest, "invalid_input", "class_id must be a UUID",
		)
		return uuid.Nil, false
	}
	return classID, true
}
