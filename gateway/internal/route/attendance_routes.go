package route

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"

	"github.com/nguyen-duc-loc/vermouth/gateway/internal/apitypes"
)

func proxyGetAttendance(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID, ok := gatewaySessionID(w, r)
		if !ok {
			return
		}
		response, err := deps.Client.Call(
			r.Context(),
			http.MethodGet,
			deps.Client.Upstreams().Teaching,
			"/sessions/"+sessionID.String()+"/attendance",
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

func proxySaveAttendance(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID, ok := gatewaySessionID(w, r)
		if !ok {
			return
		}
		var input apitypes.SaveAttendanceRequest
		if !decodeJSONBody(w, r, &input) {
			return
		}
		response, err := deps.Client.CallWithHeaders(
			r.Context(),
			http.MethodPut,
			deps.Client.Upstreams().Teaching,
			"/sessions/"+sessionID.String()+"/attendance",
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

func gatewaySessionID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	sessionID, err := uuid.Parse(r.PathValue("session_id"))
	if err != nil {
		vermouth.WriteError(
			r.Context(), w, http.StatusBadRequest, "invalid_input", "session_id must be a UUID",
		)
		return uuid.Nil, false
	}
	return sessionID, true
}
