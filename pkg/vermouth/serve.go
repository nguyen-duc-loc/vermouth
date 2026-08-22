package vermouth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

const (
	// readHeaderTimeout bounds how long a client may take to send its
	// headers, which is the cheap half of slow client protection.
	readHeaderTimeout = 10 * time.Second
	// shutdownTimeout is how long a stop waits for requests in flight before
	// it gives up on them.
	shutdownTimeout = 10 * time.Second
)

// Serve runs an HTTP server and shuts it down cleanly when the context ends,
// so a stop never cuts a request in half.
func Serve(ctx context.Context, logger *slog.Logger, addr string, handler http.Handler) error {
	server := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
	}

	errs := make(chan error, 1)
	go func() {
		logger.InfoContext(ctx, "Http listening", slog.String("addr", addr))
		err := server.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs <- err
			return
		}
		errs <- nil
	}()

	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
		defer cancel()
		logger.InfoContext(ctx, "Http shutting down")
		return server.Shutdown(shutdownCtx)
	}
}
