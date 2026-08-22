// Package auth verifies the token at the gateway.
//
// The gateway verifies on every request, rejects an unsigned or expired one,
// and passes the token onward unchanged, so each service verifies it again
// locally against the public key it holds (spec 0001, identity propagation).
// Forwarding a plain trusted header instead was rejected there: anything that
// could reach a service would then be able to impersonate any tutor.
package auth

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"
)

type claimsKey struct{}

// Middleware refuses the request unless it carries a valid token, and puts the
// claims on the context for the handlers behind it.
func Middleware(verifier *vermouth.Verifier, logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		claims, err := verifier.Verify(vermouth.BearerToken(r))
		if err != nil {
			logger.InfoContext(ctx, "Request refused", slog.String("reason", err.Error()))
			vermouth.WriteError(ctx, w, http.StatusUnauthorized, "unauthenticated", "a valid bearer token is required")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, claimsKey{}, claims)))
	})
}

// Claims reads the verified claims back out. The tutor_id in them is the only
// place a tutor is ever taken from (INV-8).
func Claims(ctx context.Context) (vermouth.Claims, bool) {
	claims, ok := ctx.Value(claimsKey{}).(vermouth.Claims)
	return claims, ok
}
