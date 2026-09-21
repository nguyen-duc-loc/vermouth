package http

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"

	"github.com/nguyen-duc-loc/vermouth/services/teaching/internal/handler"
)

func listStudents(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := verifiedClaims(deps, w, r)
		if !ok {
			return
		}
		result, err := deps.Handler.ListStudents(
			r.Context(),
			claims.TutorID,
			claims.Timezone,
			r.URL.Query().Get("q"),
			r.URL.Query().Get("cursor"),
		)
		if err != nil {
			writeHandlerError(deps, w, r, "List students", err)
			return
		}
		vermouth.WriteJSON(r.Context(), deps.Logger, w, http.StatusOK, result)
	}
}

func readStudent(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := verifiedClaims(deps, w, r)
		if !ok {
			return
		}
		studentID, ok := studentIDFromPath(w, r)
		if !ok {
			return
		}
		result, err := deps.Handler.ReadStudent(
			r.Context(), claims.TutorID, studentID, claims.Timezone,
		)
		if err != nil {
			writeHandlerError(deps, w, r, "Read student", err)
			return
		}
		vermouth.WriteJSON(r.Context(), deps.Logger, w, http.StatusOK, result)
	}
}

func updateStudent(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := verifiedClaims(deps, w, r)
		if !ok {
			return
		}
		studentID, ok := studentIDFromPath(w, r)
		if !ok {
			return
		}
		var input handler.PatchStudentInput
		if !decodeJSON(w, r, &input) {
			return
		}
		result, err := deps.Handler.UpdateStudent(
			r.Context(),
			claims.TutorID,
			studentID,
			r.Header.Get("Idempotency-Key"),
			input,
		)
		if err != nil {
			writeHandlerError(deps, w, r, "Update student", err)
			return
		}
		vermouth.WriteJSON(r.Context(), deps.Logger, w, http.StatusOK, result)
	}
}

func archiveStudent(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := verifiedClaims(deps, w, r)
		if !ok {
			return
		}
		studentID, ok := studentIDFromPath(w, r)
		if !ok {
			return
		}
		err := deps.Handler.ArchiveStudent(
			r.Context(),
			claims.TutorID,
			studentID,
			claims.Timezone,
			r.Header.Get("Idempotency-Key"),
		)
		if err != nil {
			writeHandlerError(deps, w, r, "Archive student", err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func studentIDFromPath(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	studentID, err := uuid.Parse(r.PathValue("student_id"))
	if err != nil {
		vermouth.WriteError(
			r.Context(), w, http.StatusBadRequest, "invalid_input", "student_id must be a UUID",
		)
		return uuid.Nil, false
	}
	return studentID, true
}
