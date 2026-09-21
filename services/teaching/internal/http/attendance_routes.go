package http

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"

	"github.com/nguyen-duc-loc/vermouth/services/teaching/internal/handler"
)

func readAttendance(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := verifiedClaims(deps, w, r)
		if !ok {
			return
		}
		sessionID, ok := attendanceSessionID(w, r)
		if !ok {
			return
		}
		result, err := deps.Handler.ReadAttendance(r.Context(), claims.TutorID, sessionID)
		if err != nil {
			writeHandlerError(deps, w, r, "Read attendance", err)
			return
		}
		vermouth.WriteJSON(r.Context(), deps.Logger, w, http.StatusOK, result)
	}
}

func saveAttendance(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := verifiedClaims(deps, w, r)
		if !ok {
			return
		}
		sessionID, ok := attendanceSessionID(w, r)
		if !ok {
			return
		}
		var input handler.SaveAttendanceInput
		if !decodeJSON(w, r, &input) {
			return
		}
		result, err := deps.Handler.SaveAttendance(
			r.Context(), claims.TutorID, sessionID, r.Header.Get("Idempotency-Key"), input,
		)
		if err != nil {
			writeHandlerError(deps, w, r, "Save attendance", err)
			return
		}
		vermouth.WriteJSON(r.Context(), deps.Logger, w, http.StatusOK, result)
	}
}

func attendanceSessionID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	sessionID, err := uuid.Parse(r.PathValue("session_id"))
	if err != nil {
		vermouth.WriteError(
			r.Context(), w, http.StatusBadRequest, "invalid_input", "session_id must be a UUID",
		)
		return uuid.Nil, false
	}
	return sessionID, true
}
