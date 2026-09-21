package route

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"

	"github.com/nguyen-duc-loc/vermouth/gateway/internal/apitypes"
)

func proxyListStudents(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := "/students"
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

func proxyGetStudent(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		studentID, ok := gatewayStudentID(w, r)
		if !ok {
			return
		}
		response, err := deps.Client.Call(
			r.Context(),
			http.MethodGet,
			deps.Client.Upstreams().Teaching,
			"/students/"+studentID.String(),
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

func proxyUpdateStudent(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		studentID, ok := gatewayStudentID(w, r)
		if !ok {
			return
		}
		var input apitypes.UpdateStudentRequest
		if !decodeJSONBody(w, r, &input) {
			return
		}
		response, err := deps.Client.CallWithHeaders(
			r.Context(),
			http.MethodPatch,
			deps.Client.Upstreams().Teaching,
			"/students/"+studentID.String(),
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

func proxyArchiveStudent(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		studentID, ok := gatewayStudentID(w, r)
		if !ok {
			return
		}
		response, err := deps.Client.CallWithHeaders(
			r.Context(),
			http.MethodDelete,
			deps.Client.Upstreams().Teaching,
			"/students/"+studentID.String(),
			vermouth.BearerToken(r),
			http.Header{idempotencyHeader: []string{r.Header.Get(idempotencyHeader)}},
			nil,
		)
		if err != nil {
			upstreamFailed(r.Context(), deps.Logger, w, "teaching", err)
			return
		}
		passThrough(r.Context(), deps.Logger, w, response)
	}
}

func gatewayStudentID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	studentID, err := uuid.Parse(r.PathValue("student_id"))
	if err != nil {
		vermouth.WriteError(
			r.Context(), w, http.StatusBadRequest, "invalid_input", "student_id must be a UUID",
		)
		return uuid.Nil, false
	}
	return studentID, true
}
