// Package http is how identity is reached. Only the gateway calls it (INV-1).
package http

import (
	"context"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	urlpkg "net/url"
	"time"

	"github.com/google/uuid"
	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"

	"github.com/nguyen-duc-loc/vermouth/services/identity/internal/handler"
	"github.com/nguyen-duc-loc/vermouth/services/identity/internal/token"
)

// The refresh cookie. Its path is the browser facing one, because the browser
// only ever sees the gateway; identity's own path is /auth. Every Set-Cookie
// repeats these attributes exactly, the rotation and the clear alike, since a
// clear whose Path does not match leaves the old cookie in place and a sign out
// then only looks like it worked (spec 0004).
const (
	refreshCookieName = "vermouth_refresh"
	refreshCookiePath = "/api/auth"
)

// signInPath is where a refused sign in lands, with one of the five codes the
// screen has a sentence for.
const signInPath = "/signin"

// Deps is what the routes need.
type Deps struct {
	Handler  *handler.Handler
	Verifier *vermouth.Verifier
	Signer   *token.Signer
	Logger   *slog.Logger
	Auth     handler.AuthConfig
	Health   vermouth.Health
}

// Mux builds identity's routes. Go's ServeMux does the method and pattern
// routing, so no framework hides the request path (spec 0002, HTTP layer).
func Mux(deps Deps) http.Handler {
	mux := http.NewServeMux()
	deps.Health.Mount(mux)

	// The four endpoints that take no token, because between them they are what
	// produces one. Nothing else here is reachable without a verified token.
	mux.HandleFunc("GET /auth/google/start", startSignIn(deps))
	mux.HandleFunc("GET /auth/google/callback", completeSignIn(deps))
	mux.HandleFunc("POST /auth/refresh", refreshSession(deps))
	mux.HandleFunc("POST /auth/signout", signOut(deps))

	// The tutor is read from the token, never from the path (INV-8).
	mux.HandleFunc("GET /me", readTutor(deps))
	// The public half of the signing key, for a rotation step. Never on a
	// request path: every service verifies against configuration (INV-14).
	mux.HandleFunc("GET /.well-known/jwks.json", serveJWKS(deps))

	return vermouth.RequestIDMiddleware(deps.Logger, mux)
}

// startSignIn writes the in flight sign in and sends the browser to Google.
func startSignIn(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		query := r.URL.Query()
		authorizeURL, err := deps.Handler.StartSignIn(ctx, handler.StartInput{
			Timezone:   query.Get("tz"),
			Language:   query.Get("lang"),
			RedirectTo: query.Get("redirect_to"),
		})
		if err != nil {
			deps.Logger.ErrorContext(ctx, "Start sign in",
				slog.String("request_id", vermouth.RequestID(ctx)),
				slog.String("error", err.Error()))
			redirectToSignIn(w, r, deps.Auth.AppURL, handler.SignInProviderError)
			return
		}
		http.Redirect(w, r, authorizeURL, http.StatusFound)
	}
}

// completeSignIn is where Google sends the browser back. It answers with a cookie
// and a redirect and never with a token, so no access token reaches a URL, a
// redirect log, or browser history.
func completeSignIn(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		query := r.URL.Query()
		result, err := deps.Handler.CompleteSignIn(ctx,
			query.Get("state"), query.Get("code"), query.Get("error"))
		if err != nil {
			redirectToSignIn(w, r, deps.Auth.AppURL, refusalCode(ctx, deps.Logger, err))
			return
		}
		setRefreshCookie(w, result.Refresh, deps.Auth.CookieSecure)
		http.Redirect(w, r, result.RedirectTo, http.StatusFound)
	}
}

// refusalCode logs what actually happened and answers with the only thing the
// browser is told: one of the five codes. A refusal is a decision and is logged
// as one; anything else is a failure and is logged as one.
func refusalCode(ctx context.Context, logger *slog.Logger, err error) string {
	refused, isRefusal := errors.AsType[*handler.SignInError](err)
	if isRefusal {
		logger.InfoContext(ctx, "Sign in refused",
			slog.String("request_id", vermouth.RequestID(ctx)),
			slog.String("code", refused.Code),
			slog.String("reason", refused.Err.Error()))
		return refused.Code
	}
	logger.ErrorContext(ctx, "Complete sign in",
		slog.String("request_id", vermouth.RequestID(ctx)),
		slog.String("error", err.Error()))
	return handler.SignInProviderError
}

// session is the boundary shape of Session in api/openapi.yaml: the access token
// and when it stops being valid. The refresh token is never in a body.
type session struct {
	AccessToken     string    `json:"access_token"`
	AccessExpiresAt time.Time `json:"access_expires_at"`
}

// refreshSession rotates the cookie and answers with an access token. It is the
// one path to a token, so a reload and the moment after a callback are the same
// call (AC-3).
func refreshSession(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		result, err := deps.Handler.Refresh(ctx, presentedRefreshToken(r))
		if err != nil {
			if errors.Is(err, handler.ErrSessionRefused) {
				clearRefreshCookie(w, deps.Auth.CookieSecure)
				vermouth.WriteError(ctx, w, http.StatusUnauthorized, "unauthenticated",
					"this session is over, sign in again")
				return
			}
			deps.Logger.ErrorContext(ctx, "Refresh session",
				slog.String("request_id", vermouth.RequestID(ctx)),
				slog.String("error", err.Error()))
			vermouth.WriteError(ctx, w, http.StatusInternalServerError, "internal",
				"could not refresh the session")
			return
		}
		setRefreshCookie(w, result.Refresh, deps.Auth.CookieSecure)
		vermouth.WriteJSON(ctx, deps.Logger, w, http.StatusOK, session{
			AccessToken:     result.AccessToken,
			AccessExpiresAt: result.AccessExpiresAt,
		})
	}
}

// signOut ends the session family and clears the cookie. Signing out twice is not
// an error, so an unknown cookie also answers 204 (AC-6).
func signOut(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		err := deps.Handler.SignOut(ctx, presentedRefreshToken(r))
		if err != nil {
			// The cookie is deliberately left alone: answering 204 here would
			// tell the browser a session ended that is still live.
			deps.Logger.ErrorContext(ctx, "Sign out",
				slog.String("request_id", vermouth.RequestID(ctx)),
				slog.String("error", err.Error()))
			vermouth.WriteError(ctx, w, http.StatusInternalServerError, "internal",
				"could not end the session")
			return
		}
		clearRefreshCookie(w, deps.Auth.CookieSecure)
		w.WriteHeader(http.StatusNoContent)
	}
}

// readTutor answers with the tutor the token names, and nobody else (INV-8).
func readTutor(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		claims, err := deps.Verifier.Verify(vermouth.BearerToken(r))
		if err != nil {
			vermouth.WriteError(ctx, w, http.StatusUnauthorized, "unauthenticated", "a valid bearer token is required")
			return
		}
		tutor, err := deps.Handler.Get(ctx, claims.TutorID)
		if err != nil {
			if errors.Is(err, handler.ErrNotFound) {
				vermouth.WriteError(ctx, w, http.StatusNotFound, "not_found", "no such tutor")
				return
			}
			deps.Logger.ErrorContext(ctx, "Read tutor", slog.String("error", err.Error()))
			vermouth.WriteError(ctx, w, http.StatusInternalServerError, "internal", "could not read the tutor")
			return
		}
		vermouth.WriteJSON(ctx, deps.Logger, w, http.StatusOK, tutor)
	}
}

// serveJWKS publishes the public half of the signing key, so a rotation can add
// a key before anything switches to it.
func serveJWKS(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, err := base64.StdEncoding.DecodeString(deps.Signer.PublicKeyBase64())
		if err != nil {
			vermouth.WriteError(r.Context(), w, http.StatusInternalServerError, "internal", "could not read the signing key")
			return
		}
		vermouth.WriteJSON(r.Context(), deps.Logger, w, http.StatusOK, map[string]any{
			"keys": []map[string]string{{
				"kty": "OKP",
				//nolint:usestdlibvars // this is the JWK curve name, not the TLS signature scheme of the same spelling.
				"crv": "Ed25519",
				"use": "sig",
				"alg": "EdDSA",
				"kid": deps.Signer.KID(),
				"x":   base64.RawURLEncoding.EncodeToString(raw),
			}},
		})
	}
}

// redirectToSignIn lands the browser on the sign in screen with one of the five
// codes. The target is always built from IDENTITY_APP_URL, never from anything
// the caller sent, which is what closes the open redirect.
func redirectToSignIn(w http.ResponseWriter, r *http.Request, appURL, code string) {
	http.Redirect(w, r, appURL+signInPath+"?error="+urlpkg.QueryEscape(code), http.StatusFound)
}

// setRefreshCookie writes the rotating refresh token. HttpOnly so no script can
// read it, SameSite=Lax plus the POST method as the cross site protection, and
// Secure because localhost already counts as a trustworthy origin.
func setRefreshCookie(w http.ResponseWriter, issued handler.IssuedRefreshToken, secure bool) {
	//nolint:gosec // G124: Secure is IDENTITY_COOKIE_SECURE, true unless an origin no browser trusts says otherwise.
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    issued.Value,
		Path:     refreshCookiePath,
		Expires:  issued.ExpiresAt,
		MaxAge:   int(time.Until(issued.ExpiresAt).Seconds()),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// clearRefreshCookie repeats the same attributes with an expiry in the past,
// because a clear that does not match leaves the browser sending the old value.
func clearRefreshCookie(w http.ResponseWriter, secure bool) {
	//nolint:gosec // G124: the same attributes as the cookie this clears, which is the point.
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    "",
		Path:     refreshCookiePath,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// presentedRefreshToken reads the cookie, answering empty when there is none so
// the handler treats a missing cookie and a dead one the same way.
func presentedRefreshToken(r *http.Request) string {
	cookie, err := r.Cookie(refreshCookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

// ParseTutorID is used by routes that take an identifier in the path. Kept here
// so a bad identifier answers in the one error shape rather than panicking.
func ParseTutorID(raw string) (uuid.UUID, bool) {
	parsed, err := uuid.Parse(raw)
	return parsed, err == nil
}
