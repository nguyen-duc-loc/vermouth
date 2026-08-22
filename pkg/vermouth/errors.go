package vermouth

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
)

// APIError is the one error shape at the gateway boundary (spec 0001, the
// contract every service obeys). Services answer in the same shape so the
// gateway maps rather than invents, and nothing raw ever leaks out.
type APIError struct {
	Error APIErrorBody `json:"error"`
}

// APIErrorBody carries the machine readable code, the human readable
// message, and the request_id a report can be traced by (INV-15).
type APIErrorBody struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

// WriteError answers in the one error shape.
func WriteError(ctx context.Context, w http.ResponseWriter, status int, code, message string) {
	// The body is three strings, so encoding it cannot fail. It is marshalled
	// before the status goes out, because after WriteHeader there is no way
	// left to report a problem.
	body, _ := json.Marshal(APIError{Error: APIErrorBody{
		Code:      code,
		Message:   message,
		RequestID: RequestID(ctx),
	}})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// WriteJSON answers with a body, or logs and gives up if encoding fails after
// the status is already written.
func WriteJSON(ctx context.Context, logger *slog.Logger, w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if body == nil {
		return
	}
	err := json.NewEncoder(w).Encode(body)
	if err != nil {
		logger.ErrorContext(ctx, "Encode response", slog.String("error", err.Error()))
	}
}
