package vermouth

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/google/uuid"
)

// RequestIDHeader travels with every request and onto every event published
// while handling it (INV-15).
const RequestIDHeader = "X-Request-Id"

type requestIDKey struct{}

// NewLogger builds the one logger shape the whole system uses: log/slog, JSON,
// stdout, with service on every line (STK-9).
func NewLogger(service, level string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})
	return slog.New(handler).With(slog.String("service", service))
}

// WithRequestID puts a request id on the context so every log line and every
// published event can carry it.
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, requestID)
}

// RequestID reads the request id back out. Empty when there is none, which
// happens for work that no request started, such as the scheduler.
func RequestID(ctx context.Context) string {
	if value, ok := ctx.Value(requestIDKey{}).(string); ok {
		return value
	}
	return ""
}

// RequestIDMiddleware creates the request id when it is absent, echoes it on
// the response, and puts it on the context and on the request's logger.
func RequestIDMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := strings.TrimSpace(r.Header.Get(RequestIDHeader))
		if requestID == "" {
			requestID = uuid.NewString()
		}
		w.Header().Set(RequestIDHeader, requestID)
		ctx := WithRequestID(r.Context(), requestID)
		logger.InfoContext(ctx, "Request",
			slog.String("request_id", requestID),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
		)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
