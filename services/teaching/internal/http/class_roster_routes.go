package http

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"

	"github.com/nguyen-duc-loc/vermouth/services/teaching/internal/handler"
)

func readClassRoster(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := verifiedClaims(deps, w, r)
		if !ok {
			return
		}
		classID, ok := classIDFromPath(w, r)
		if !ok {
			return
		}
		result, err := deps.Handler.ReadClassRoster(
			r.Context(), claims.TutorID, classID, claims.Timezone, r.URL.Query().Get("date"),
		)
		if err != nil {
			writeHandlerError(deps, w, r, "Read class roster", err)
			return
		}
		vermouth.WriteJSON(r.Context(), deps.Logger, w, http.StatusOK, result)
	}
}

func changeClassRoster(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := verifiedClaims(deps, w, r)
		if !ok {
			return
		}
		classID, ok := classIDFromPath(w, r)
		if !ok {
			return
		}
		var input handler.ChangeRosterInput
		if !decodeJSON(w, r, &input) {
			return
		}
		result, err := deps.Handler.ChangeClassRoster(
			r.Context(),
			claims.TutorID,
			classID,
			claims.Timezone,
			r.Header.Get("Idempotency-Key"),
			input,
		)
		if err != nil {
			writeHandlerError(deps, w, r, "Change class roster", err)
			return
		}
		vermouth.WriteJSON(r.Context(), deps.Logger, w, http.StatusOK, result)
	}
}

func classIDFromPath(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	classID, err := uuid.Parse(r.PathValue("class_id"))
	if err != nil {
		vermouth.WriteError(
			r.Context(), w, http.StatusBadRequest, "invalid_input", "class_id must be a UUID",
		)
		return uuid.Nil, false
	}
	return classID, true
}
