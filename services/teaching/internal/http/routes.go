// Package http is how teaching is reached. Only the gateway calls it (INV-1).
package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"

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
	mux.HandleFunc("PUT /classes/{class_id}/schedule", putSchedule(deps))
	mux.HandleFunc("POST /classes/{class_id}/schedule/end", endSchedule(deps))
	mux.HandleFunc("POST /sessions/{session_id}/move", moveSession(deps))
	mux.HandleFunc("POST /sessions/{session_id}/cancel", cancelSession(deps))
	mux.HandleFunc("POST /sessions/{session_id}/restore", restoreSession(deps))
	mux.HandleFunc("GET /students", listStudents(deps))
	mux.HandleFunc("POST /students", createStudent(deps))
	mux.HandleFunc("GET /students/{student_id}", readStudent(deps))
	mux.HandleFunc("PATCH /students/{student_id}", updateStudent(deps))
	mux.HandleFunc("DELETE /students/{student_id}", archiveStudent(deps))
	mux.HandleFunc("GET /classes/{class_id}/roster", readClassRoster(deps))
	mux.HandleFunc("PUT /classes/{class_id}/roster", changeClassRoster(deps))
	mux.HandleFunc("GET /sessions/{session_id}/attendance", readAttendance(deps))
	mux.HandleFunc("PUT /sessions/{session_id}/attendance", saveAttendance(deps))
	mux.HandleFunc("GET /home", readHome(deps))
	mux.HandleFunc("GET /schedule", readSchedule(deps))
	return vermouth.RequestIDMiddleware(deps.Logger, mux)
}

func endSchedule(deps Deps) http.HandlerFunc {
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
		var input handler.EndScheduleInput
		if !decodeJSON(w, r, &input) {
			return
		}
		result, status, err := deps.Handler.EndSchedule(
			r.Context(), claims.TutorID, classID, r.Header.Get("Idempotency-Key"), input,
		)
		if err != nil {
			writeHandlerError(deps, w, r, "End schedule", err)
			return
		}
		vermouth.WriteJSON(r.Context(), deps.Logger, w, status, result)
	}
}

func moveSession(deps Deps) http.HandlerFunc {
	return sessionCommand(deps, "Move session", func(
		ctx context.Context,
		tutorID uuid.UUID,
		sessionID uuid.UUID,
		timezone string,
		key string,
		body []byte,
	) (handler.CanonicalSession, int, error) {
		var input handler.MoveSessionInput
		if !decodeJSONBytes(body, &input) {
			return handler.CanonicalSession{}, 0, &handler.ValidationError{
				Field: "body", Message: "must be valid JSON",
			}
		}
		return deps.Handler.MoveSession(ctx, tutorID, sessionID, timezone, key, input)
	})
}

func cancelSession(deps Deps) http.HandlerFunc {
	return versionedSessionCommand(deps, "Cancel session", deps.Handler.CancelSession)
}

func restoreSession(deps Deps) http.HandlerFunc {
	return versionedSessionCommand(deps, "Restore session", deps.Handler.RestoreSession)
}

type sessionCommandFunc func(
	context.Context,
	uuid.UUID,
	uuid.UUID,
	string,
	string,
	[]byte,
) (handler.CanonicalSession, int, error)

func sessionCommand(deps Deps, operation string, command sessionCommandFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := verifiedClaims(deps, w, r)
		if !ok {
			return
		}
		sessionID, err := uuid.Parse(r.PathValue("session_id"))
		if err != nil {
			vermouth.WriteError(r.Context(), w, http.StatusBadRequest, "invalid_input", "session_id must be a UUID")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			vermouth.WriteError(r.Context(), w, http.StatusBadRequest, "invalid_input", "the JSON body is invalid")
			return
		}
		result, status, err := command(
			r.Context(), claims.TutorID, sessionID, claims.Timezone,
			r.Header.Get("Idempotency-Key"), body,
		)
		if err != nil {
			writeHandlerError(deps, w, r, operation, err)
			return
		}
		vermouth.WriteJSON(r.Context(), deps.Logger, w, status, result)
	}
}

func versionedSessionCommand(
	deps Deps,
	operation string,
	command func(
		context.Context,
		uuid.UUID,
		uuid.UUID,
		string,
		string,
		handler.SessionVersionInput,
	) (handler.CanonicalSession, int, error),
) http.HandlerFunc {
	return sessionCommand(deps, operation, func(
		ctx context.Context,
		tutorID uuid.UUID,
		sessionID uuid.UUID,
		timezone string,
		key string,
		body []byte,
	) (handler.CanonicalSession, int, error) {
		var input handler.SessionVersionInput
		if !decodeJSONBytes(body, &input) {
			return handler.CanonicalSession{}, 0, &handler.ValidationError{
				Field: "body", Message: "must be valid JSON",
			}
		}
		return command(ctx, tutorID, sessionID, timezone, key, input)
	})
}

func decodeJSONBytes(body []byte, target any) bool {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil {
		return false
	}
	return errors.Is(decoder.Decode(&struct{}{}), io.EOF)
}

func putSchedule(deps Deps) http.HandlerFunc {
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
		var input handler.PutScheduleInput
		if !decodeJSON(w, r, &input) {
			return
		}
		result, status, err := deps.Handler.PutSchedule(
			r.Context(),
			claims.TutorID,
			classID,
			claims.Timezone,
			r.Header.Get("Idempotency-Key"),
			input,
		)
		if err != nil {
			writeHandlerError(deps, w, r, "Put schedule", err)
			return
		}
		vermouth.WriteJSON(r.Context(), deps.Logger, w, status, result)
	}
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

func readSchedule(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := verifiedClaims(deps, w, r)
		if !ok {
			return
		}
		classValues := r.URL.Query()["class_id"]
		classIDs := make([]uuid.UUID, 0, len(classValues))
		seen := make(map[uuid.UUID]struct{}, len(classValues))
		for _, value := range classValues {
			classID, err := uuid.Parse(value)
			if err != nil {
				vermouth.WriteError(
					r.Context(), w, http.StatusBadRequest, "invalid_input",
					"each class_id must be a UUID",
				)
				return
			}
			if _, exists := seen[classID]; exists {
				continue
			}
			seen[classID] = struct{}{}
			classIDs = append(classIDs, classID)
		}
		result, err := deps.Handler.ReadSchedule(
			r.Context(),
			claims.TutorID,
			claims.Timezone,
			r.URL.Query().Get("from"),
			r.URL.Query().Get("through"),
			classIDs,
			r.URL.Query().Get("include_replaced") == "true",
			parseHistoryLimit(r.URL.Query().Get("history_limit")),
			r.URL.Query().Get("history_cursor"),
		)
		if err != nil {
			writeHandlerError(deps, w, r, "Read schedule", err)
			return
		}
		vermouth.WriteJSON(r.Context(), deps.Logger, w, http.StatusOK, result)
	}
}

func parseHistoryLimit(value string) int {
	if value == "" {
		return 0
	}
	result, err := strconv.Atoi(value)
	if err != nil {
		return -1
	}
	return result
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
	if conflict, ok := errors.AsType[*handler.ConflictError](err); ok {
		vermouth.WriteErrorDetails(
			ctx, w, http.StatusConflict, conflict.Code, conflict.Message, conflict.Details,
		)
		return
	}
	switch {
	case errors.Is(err, handler.ErrNotFound):
		vermouth.WriteError(ctx, w, http.StatusNotFound, "not_found", "no owned teaching resource matched")
	case errors.Is(err, handler.ErrIdempotencyConflict):
		vermouth.WriteError(ctx, w, http.StatusConflict, "idempotency_conflict", "the idempotency key names different input")
	case errors.Is(err, handler.ErrConflict):
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
