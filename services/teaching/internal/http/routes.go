// Package http is how teaching is reached. Only the gateway calls it (INV-1).
package http

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"

	"github.com/nguyen-duc-loc/vermouth/services/teaching/internal/handler"
)

const maxRequestBody = 1 << 20

// Deps is what the routes need: the logger every answer is traced through, and
// the health checks this service owes.
type Deps struct {
	Handler  *handler.Handler
	Verifier *vermouth.Verifier
	Logger   *slog.Logger
	Health   vermouth.Health
}

// Mux carries health plus teaching's authenticated command and home surface.
func Mux(deps Deps) http.Handler {
	mux := http.NewServeMux()
	deps.Health.Mount(mux)
	mux.HandleFunc("POST /classes", createClass(deps))
	mux.HandleFunc("POST /students", createStudent(deps))
	mux.HandleFunc("POST /classes/{class_id}/roster", joinRoster(deps))
	mux.HandleFunc("PUT /sessions/{session_id}/attendance/{student_id}", markAttendance(deps))
	mux.HandleFunc("GET /home", readHome(deps))
	return vermouth.RequestIDMiddleware(deps.Logger, mux)
}

func createClass(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := verifiedClaims(deps, w, r)
		if !ok {
			return
		}
		var input handler.CreateClassInput
		if !decodeJSON(w, r, &input) {
			return
		}
		result, created, err := deps.Handler.CreateClass(
			r.Context(),
			claims.TutorID,
			claims.Timezone,
			r.Header.Get("Idempotency-Key"),
			input,
		)
		if err != nil {
			writeHandlerError(deps, w, r, "Create class", err)
			return
		}
		status := http.StatusOK
		if created {
			status = http.StatusCreated
		}
		vermouth.WriteJSON(r.Context(), deps.Logger, w, status, result)
	}
}

func createStudent(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := verifiedClaims(deps, w, r)
		if !ok {
			return
		}
		var input handler.CreateStudentInput
		if !decodeJSON(w, r, &input) {
			return
		}
		result, created, err := deps.Handler.CreateStudent(
			r.Context(),
			claims.TutorID,
			r.Header.Get("Idempotency-Key"),
			input,
		)
		if err != nil {
			writeHandlerError(deps, w, r, "Create student", err)
			return
		}
		status := http.StatusOK
		if created {
			status = http.StatusCreated
		}
		vermouth.WriteJSON(r.Context(), deps.Logger, w, status, result)
	}
}

func joinRoster(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := verifiedClaims(deps, w, r)
		if !ok {
			return
		}
		classID, err := uuid.Parse(r.PathValue("class_id"))
		if err != nil {
			vermouth.WriteError(r.Context(), w, http.StatusBadRequest, "invalid_input", "class_id must be a UUID")
			return
		}
		var input handler.JoinRosterInput
		if !decodeJSON(w, r, &input) {
			return
		}
		result, created, err := deps.Handler.JoinRoster(r.Context(), claims.TutorID, classID, input)
		if err != nil {
			writeHandlerError(deps, w, r, "Join roster", err)
			return
		}
		status := http.StatusOK
		if created {
			status = http.StatusCreated
		}
		vermouth.WriteJSON(r.Context(), deps.Logger, w, status, result)
	}
}

func markAttendance(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := verifiedClaims(deps, w, r)
		if !ok {
			return
		}
		sessionID, sessionErr := uuid.Parse(r.PathValue("session_id"))
		studentID, studentErr := uuid.Parse(r.PathValue("student_id"))
		if sessionErr != nil || studentErr != nil {
			vermouth.WriteError(
				r.Context(),
				w,
				http.StatusBadRequest,
				"invalid_input",
				"session_id and student_id must be UUID values",
			)
			return
		}
		var input handler.MarkAttendanceInput
		if !decodeJSON(w, r, &input) {
			return
		}
		result, err := deps.Handler.MarkAttendance(
			r.Context(),
			claims.TutorID,
			sessionID,
			studentID,
			input,
		)
		if err != nil {
			writeHandlerError(deps, w, r, "Mark attendance", err)
			return
		}
		vermouth.WriteJSON(r.Context(), deps.Logger, w, http.StatusOK, result)
	}
}

func readHome(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := verifiedClaims(deps, w, r)
		if !ok {
			return
		}
		result, err := deps.Handler.ReadHome(
			r.Context(),
			claims.TutorID,
			claims.Timezone,
			r.URL.Query().Get("cursor"),
		)
		if err != nil {
			writeHandlerError(deps, w, r, "Read home", err)
			return
		}
		vermouth.WriteJSON(r.Context(), deps.Logger, w, http.StatusOK, result)
	}
}

func verifiedClaims(deps Deps, w http.ResponseWriter, r *http.Request) (vermouth.Claims, bool) {
	claims, err := deps.Verifier.Verify(vermouth.BearerToken(r))
	if err != nil {
		vermouth.WriteError(
			r.Context(),
			w,
			http.StatusUnauthorized,
			"unauthenticated",
			"a valid bearer token is required",
		)
		return vermouth.Claims{}, false
	}
	return claims, true
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
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

func writeHandlerError(deps Deps, w http.ResponseWriter, r *http.Request, operation string, err error) {
	ctx := r.Context()
	if validation, ok := errors.AsType[*handler.ValidationError](err); ok {
		vermouth.WriteError(ctx, w, http.StatusBadRequest, "invalid_input", validation.Error())
		return
	}
	switch {
	case errors.Is(err, handler.ErrNotFound):
		vermouth.WriteError(ctx, w, http.StatusNotFound, "not_found", "no owned teaching resource matched")
	case errors.Is(err, handler.ErrConflict), errors.Is(err, handler.ErrIdempotencyConflict):
		vermouth.WriteError(ctx, w, http.StatusConflict, "conflict", "the request conflicts with committed state")
	default:
		deps.Logger.ErrorContext(ctx, "Teaching request failed",
			slog.String("request_id", vermouth.RequestID(ctx)),
			slog.String("operation", operation),
			slog.String("error", err.Error()),
		)
		vermouth.WriteError(ctx, w, http.StatusInternalServerError, "internal", "teaching could not complete the request")
	}
}
