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
	Details   any    `json:"details,omitempty"`
}

// WriteError answers in the one error shape.
func WriteError(ctx context.Context, w http.ResponseWriter, status int, code, message string) {
	WriteErrorDetails(ctx, w, status, code, message, nil)
}

// WriteErrorDetails adds safe structured recovery data to the shared boundary
// error without changing its stable outer shape.
func WriteErrorDetails(
	ctx context.Context,
	w http.ResponseWriter,
	status int,
	code string,
	message string,
	details any,
) {
	// Marshal before the status goes out, because after WriteHeader there is no
	// way left to replace unsafe detail data with the stable string fields.
	body, err := json.Marshal(APIError{Error: APIErrorBody{
		Code:      code,
		Message:   message,
		RequestID: RequestID(ctx),
		Details:   details,
	}})
	if err != nil {
		body, err = json.Marshal(APIError{Error: APIErrorBody{
			Code: code, Message: message, RequestID: RequestID(ctx),
		}})
		if err != nil {
			return
		}
	}
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
