//nolint:testpackage,paralleltest // White box integration tests need package internals and share one database.
package handler

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	urlpkg "net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/idtoken"

	identitytoken "github.com/nguyen-duc-loc/vermouth/services/identity/internal/token"
)

const clearIdentityAuthenticationRows = `
	TRUNCATE TABLE
		refresh_tokens,
		auth_sessions,
		login_attempts,
		tutor_identities,
		tutors,
		outbox
	RESTART IDENTITY CASCADE`

var (
	errPlainTest        = errors.New("plain test error")
	errExchangeTest     = errors.New("exchange failed")
	errForgedTokenTest  = errors.New("forged token")
	errExpiredTokenTest = errors.New("expired token")
)

func identityIntegrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("IDENTITY_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("IDENTITY_DATABASE_URL is not set, run task infra:up and task migrate:up")
	}
	pool, err := pgxpool.New(t.Context(), databaseURL)
	require.NoError(t, err)
	require.NoError(t, pool.Ping(t.Context()))
	_, err = pool.Exec(t.Context(), clearIdentityAuthenticationRows)
	require.NoError(t, err)
	t.Cleanup(func() {
		teardownCtx := context.Background() //nolint:usetesting // Cleanup runs after t.Context is canceled.
		_, _ = pool.Exec(teardownCtx, clearIdentityAuthenticationRows)
		pool.Close()
	})
	return pool
}

func integrationAuthConfig() AuthConfig {
	return AuthConfig{
		GoogleEnabled:      true,
		GoogleClientID:     "client-id",
		GoogleClientSecret: "client-secret",
		GoogleRedirectURL:  "https://app.example/api/auth/google/callback",
		AppURL:             "https://app.example",
		SignupAllowlist:    map[string]bool{"tutor@example.com": true},
		RefreshTTL:         time.Hour,
		RefreshGrace:       10 * time.Second,
		SweepInterval:      time.Minute,
		CookieSecure:       true,
	}
}

func newIntegrationHandler(t *testing.T, pool *pgxpool.Pool, auth AuthConfig) *Handler {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	t.Setenv("IDENTITY_TOKEN_KID", "integration-test")
	t.Setenv("IDENTITY_TOKEN_PRIVATE_KEY", base64.StdEncoding.EncodeToString(privateKey))
	t.Setenv("IDENTITY_TOKEN_TTL", "15m")
	signer, err := identitytoken.NewSignerFromEnv()
	require.NoError(t, err)
	return New(
		pool,
		slog.New(slog.DiscardHandler),
		signer,
		vermouth.TopicIdentity,
		auth,
	)
}

type integrationAttempt struct {
	state   string
	nonce   string
	binding string
}

func startIntegrationAttempt(t *testing.T, h *Handler) integrationAttempt {
	t.Helper()
	return startIntegrationAttemptWithInput(t, h, StartInput{
		Timezone:   "Asia/Ho_Chi_Minh",
		Language:   "vi",
		RedirectTo: "/thread?day=1",
	})
}

func startIntegrationAttemptWithInput(t *testing.T, h *Handler, input StartInput) integrationAttempt {
	t.Helper()
	result, err := h.StartSignIn(t.Context(), input)
	require.NoError(t, err)
	authorizeURL, err := urlpkg.Parse(result.AuthorizeURL)
	require.NoError(t, err)
	query := authorizeURL.Query()
	require.NotEmpty(t, query.Get("state"))
	require.NotEmpty(t, query.Get("nonce"))
	return integrationAttempt{
		state:   query.Get("state"),
		nonce:   query.Get("nonce"),
		binding: result.BrowserBinding,
	}
}

func completeIntegrationSignIn(
	t *testing.T,
	h *Handler,
	payload *idtoken.Payload,
) SignInResult {
	t.Helper()
	return completeIntegrationSignInWithInput(t, h, payload, StartInput{
		Timezone:   "Asia/Ho_Chi_Minh",
		Language:   "vi",
		RedirectTo: "/thread?day=1",
	})
}

func completeIntegrationSignInWithInput(
	t *testing.T,
	h *Handler,
	payload *idtoken.Payload,
	input StartInput,
) SignInResult {
	t.Helper()
	attempt := startIntegrationAttemptWithInput(t, h, input)
	payload.Claims["nonce"] = attempt.nonce
	h.google.exchanger = fakeCodeExchanger{raw: "raw-token"}
	h.google.validator = fakeIDTokenValidator{payload: payload}
	result, err := h.CompleteSignIn(
		t.Context(),
		attempt.state,
		"code",
		"",
		attempt.binding,
	)
	require.NoError(t, err)
	return result
}

type attemptCodeExchanger struct {
	ready   chan<- struct{}
	release <-chan struct{}
}

func (e attemptCodeExchanger) exchange(ctx context.Context, code, _ string) (string, error) {
	select {
	case e.ready <- struct{}{}:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	select {
	case <-e.release:
		return code, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

type attemptPayloadValidator map[string]*idtoken.Payload

func (v attemptPayloadValidator) validate(_ context.Context, raw, _ string) (*idtoken.Payload, error) {
	return v[raw], nil
}

func requireNoAuthenticationWrites(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	var tutors, links, sessions, events int
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT count(*) FROM tutors").Scan(&tutors))
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT count(*) FROM tutor_identities").Scan(&links))
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT count(*) FROM auth_sessions").Scan(&sessions))
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT count(*) FROM outbox").Scan(&events))
	require.Zero(t, tutors)
	require.Zero(t, links)
	require.Zero(t, sessions)
	require.Zero(t, events)
}

func waitForBlockedAuthQuery(t *testing.T, pool *pgxpool.Pool, fragment string) {
	t.Helper()
	require.Eventually(t, func() bool {
		var count int
		err := pool.QueryRow(t.Context(), `
			SELECT count(*)
			FROM pg_stat_activity
			WHERE pid <> pg_backend_pid()
			  AND wait_event_type = 'Lock'
			  AND query ILIKE $1`, "%"+fragment+"%").Scan(&count)
		return err == nil && count > 0
	}, 5*time.Second, 10*time.Millisecond)
}

// covers: AC-10, AC-15
func TestCleanStartInput_KeepsSupportedPreferencesAndSafeRedirect(t *testing.T) {
	t.Parallel()

	got := cleanStartInput(StartInput{
		Timezone:   " Europe/Paris ",
		Language:   " en ",
		RedirectTo: " /thread?day=1 ",
	})

	require.Equal(t, StartInput{
		Timezone:   "Europe/Paris",
		Language:   "en",
		RedirectTo: "/thread?day=1",
	}, got)
}

// covers: AC-10
func TestCleanStartInput_UsesDefaultsForUnsupportedPreferences(t *testing.T) {
	t.Parallel()

	got := cleanStartInput(StartInput{Timezone: "Mars/Olympus", Language: "fr"})

	require.Equal(t, defaultTimezone, got.Timezone)
	require.Equal(t, defaultLanguage, got.Language)
	require.Equal(t, "/", got.RedirectTo)
}

// covers: AC-15
func TestCleanRelativeRedirect_RejectsValuesThatCanEscapeTheApp(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "empty", raw: "", want: "/"},
		{name: "protocol relative", raw: "//evil.example", want: "/"},
		{name: "backslash", raw: `/\\evil.example`, want: "/"},
		{name: "encoded protocol relative", raw: "/%2f%2fevil.example", want: "/"},
		{name: "encoded backslash", raw: "/%5cevil.example", want: "/"},
		{name: "full URL", raw: "https://evil.example/thread", want: "/"},
		{name: "control character", raw: "/thread\nnext", want: "/"},
		{name: "fragment", raw: "/thread#secret", want: "/"},
		{name: "invalid escape", raw: "/%zz", want: "/"},
		{name: "clean path", raw: "/thread", want: "/thread"},
		{name: "clean query", raw: "/thread?day=1", want: "/thread?day=1"},
		{name: "normalizes path", raw: "/classes/../thread", want: "/thread"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, test.want, cleanRelativeRedirect(test.raw))
		})
	}
}

// covers: AC-8
func TestRandomToken_ReturnsIndependentThirtyTwoByteValues(t *testing.T) {
	t.Parallel()

	first, err := randomToken()
	require.NoError(t, err)
	second, err := randomToken()
	require.NoError(t, err)

	decoded, err := base64.RawURLEncoding.DecodeString(first)
	require.NoError(t, err)
	require.Len(t, decoded, randomTokenBytes)
	require.NotEqual(t, first, second)
	require.Len(t, hashOpaqueToken(first), 32)
	require.NotEqual(t, hashOpaqueToken(first), hashOpaqueToken(second))
}

// covers: AC-1, AC-2
func TestIsConcurrentFirstSignIn_RecognizesOnlyRetryableUniqueConflicts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "email conflict",
			err:  &pgconn.PgError{Code: "23505", ConstraintName: "tutors_email_key"},
			want: true,
		},
		{
			name: "wrapped subject conflict",
			err: fmt.Errorf("outer: %w", &pgconn.PgError{
				Code: "23505", ConstraintName: "tutor_identities_pkey",
			}),
			want: true,
		},
		{
			name: "other unique conflict",
			err:  &pgconn.PgError{Code: "23505", ConstraintName: "other_key"},
			want: false,
		},
		{
			name: "other postgres error",
			err:  &pgconn.PgError{Code: "23503", ConstraintName: "tutors_email_key"},
			want: false,
		},
		{name: "plain error", err: errPlainTest, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, test.want, isConcurrentFirstSignIn(test.err))
		})
	}
}

// covers: AC-1, AC-2, AC-8
func TestCompleteSignInCreatesOneTutorForConcurrentFirstCallbacks(t *testing.T) {
	pool := identityIntegrationPool(t)
	h := newIntegrationHandler(t, pool, integrationAuthConfig())
	firstAttempt := startIntegrationAttempt(t, h)
	secondAttempt := startIntegrationAttempt(t, h)
	ready := make(chan struct{}, 2)
	release := make(chan struct{})
	firstPayload := validGooglePayload()
	firstPayload.Claims["nonce"] = firstAttempt.nonce
	secondPayload := validGooglePayload()
	secondPayload.Claims["nonce"] = secondAttempt.nonce
	h.google.exchanger = attemptCodeExchanger{ready: ready, release: release}
	h.google.validator = attemptPayloadValidator{
		firstAttempt.state:  firstPayload,
		secondAttempt.state: secondPayload,
	}

	type outcome struct {
		result SignInResult
		err    error
	}
	results := make(chan outcome, 2)
	for _, attempt := range []integrationAttempt{firstAttempt, secondAttempt} {
		go func() {
			result, err := h.CompleteSignIn(
				t.Context(),
				attempt.state,
				attempt.state,
				"",
				attempt.binding,
			)
			results <- outcome{result: result, err: err}
		}()
	}
	<-ready
	<-ready
	close(release)

	first := <-results
	second := <-results
	require.NoError(t, first.err)
	require.NoError(t, second.err)
	require.Equal(t, "https://app.example/thread?day=1", first.result.RedirectTo)
	require.Equal(t, "https://app.example/thread?day=1", second.result.RedirectTo)
	require.NotEmpty(t, first.result.Refresh.Value)
	require.NotEmpty(t, second.result.Refresh.Value)
	require.NotEqual(t, first.result.Refresh.Value, second.result.Refresh.Value)

	var tutors, links, sessions, tokens, events int
	require.NoError(t, pool.QueryRow(t.Context(),
		"SELECT count(*) FROM tutors WHERE email = 'tutor@example.com'").Scan(&tutors))
	require.NoError(t, pool.QueryRow(t.Context(),
		"SELECT count(*) FROM tutor_identities WHERE provider = 'google' AND provider_subject = 'subject-1'").Scan(&links))
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT count(*) FROM auth_sessions").Scan(&sessions))
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT count(*) FROM refresh_tokens").Scan(&tokens))
	require.NoError(t, pool.QueryRow(t.Context(),
		"SELECT count(*) FROM outbox WHERE event_name = 'identity.tutor.registered'").Scan(&events))
	require.Equal(t, 1, tutors)
	require.Equal(t, 1, links)
	require.Equal(t, 2, sessions)
	require.Equal(t, 2, tokens)
	require.Equal(t, 1, events)
}

// covers: AC-8
func TestCompleteSignInAllowsOnlyOneCallbackToConsumeTheSameState(t *testing.T) {
	pool := identityIntegrationPool(t)
	h := newIntegrationHandler(t, pool, integrationAuthConfig())
	attempt := startIntegrationAttempt(t, h)
	ready := make(chan struct{}, 2)
	release := make(chan struct{})
	payload := validGooglePayload()
	payload.Claims["nonce"] = attempt.nonce
	h.google.exchanger = attemptCodeExchanger{ready: ready, release: release}
	h.google.validator = attemptPayloadValidator{attempt.state: payload}

	type outcome struct {
		result SignInResult
		err    error
	}
	results := make(chan outcome, 2)
	for range 2 {
		go func() {
			result, err := h.CompleteSignIn(
				t.Context(), attempt.state, attempt.state, "", attempt.binding,
			)
			results <- outcome{result: result, err: err}
		}()
	}
	<-ready
	<-ready
	close(release)

	successes := 0
	refusals := 0
	for range 2 {
		result := <-results
		if result.err == nil {
			successes++
			require.NotEmpty(t, result.result.Refresh.Value)
			continue
		}
		var refused *SignInError
		require.ErrorAs(t, result.err, &refused)
		require.Equal(t, SignInExpiredState, refused.Code)
		refusals++
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, refusals)

	var tutors, links, sessions, tokens, events int
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT count(*) FROM tutors").Scan(&tutors))
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT count(*) FROM tutor_identities").Scan(&links))
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT count(*) FROM auth_sessions").Scan(&sessions))
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT count(*) FROM refresh_tokens").Scan(&tokens))
	require.NoError(t, pool.QueryRow(t.Context(),
		"SELECT count(*) FROM outbox WHERE event_name = 'identity.tutor.registered'").Scan(&events))
	require.Equal(t, 1, tutors)
	require.Equal(t, 1, links)
	require.Equal(t, 1, sessions)
	require.Equal(t, 1, tokens)
	require.Equal(t, 1, events)
}

// covers: AC-8
func TestCompleteSignInRequiresTheMatchingBrowserBindingAndConsumesCancellation(t *testing.T) {
	pool := identityIntegrationPool(t)
	h := newIntegrationHandler(t, pool, integrationAuthConfig())
	h.google.exchanger = fakeCodeExchanger{raw: "raw-token"}
	h.google.validator = fakeIDTokenValidator{payload: validGooglePayload()}

	wrongBindingAttempt := startIntegrationAttempt(t, h)
	_, err := h.CompleteSignIn(
		t.Context(), wrongBindingAttempt.state, "code", "", "another-browser",
	)
	var refused *SignInError
	require.ErrorAs(t, err, &refused)
	require.Equal(t, SignInExpiredState, refused.Code)

	cancelledAttempt := startIntegrationAttempt(t, h)
	_, err = h.CompleteSignIn(
		t.Context(), cancelledAttempt.state, "", "access_denied", cancelledAttempt.binding,
	)
	require.ErrorAs(t, err, &refused)
	require.Equal(t, SignInCancelled, refused.Code)

	var attempts, tutors, sessions int
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT count(*) FROM login_attempts").Scan(&attempts))
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT count(*) FROM tutors").Scan(&tutors))
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT count(*) FROM auth_sessions").Scan(&sessions))
	require.Equal(t, 1, attempts)
	require.Zero(t, tutors)
	require.Zero(t, sessions)
}

// covers: AC-14
func TestCompleteSignInWritesNothingForEveryUntrustedGoogleAnswer(t *testing.T) {
	tests := []struct {
		name          string
		exchangeErr   error
		validationErr error
		mutate        func(*idtoken.Payload)
	}{
		{name: "exchange failure", exchangeErr: errExchangeTest},
		{name: "forged signature", validationErr: errForgedTokenTest},
		{name: "stale expiry", validationErr: errExpiredTokenTest},
		{name: "bad issuer", mutate: func(p *idtoken.Payload) { p.Claims["iss"] = "https://evil.example" }},
		{name: "wrong audience", mutate: func(p *idtoken.Payload) { p.Claims["aud"] = "other-client" }},
		{name: "multiple audiences", mutate: func(p *idtoken.Payload) { p.Claims["aud"] = []string{"client-id", "other-client"} }},
		{name: "wrong nonce", mutate: func(p *idtoken.Payload) { p.Claims["nonce"] = "wrong-nonce" }},
		{name: "empty subject", mutate: func(p *idtoken.Payload) { p.Claims["sub"] = "" }},
		{name: "subject wrong type", mutate: func(p *idtoken.Payload) { p.Claims["sub"] = 7 }},
		{name: "empty email", mutate: func(p *idtoken.Payload) { p.Claims["email"] = " " }},
		{name: "email wrong type", mutate: func(p *idtoken.Payload) { p.Claims["email"] = 7 }},
		{name: "unverified email", mutate: func(p *idtoken.Payload) { p.Claims["email_verified"] = false }},
		{name: "verification wrong type", mutate: func(p *idtoken.Payload) { p.Claims["email_verified"] = "true" }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pool := identityIntegrationPool(t)
			h := newIntegrationHandler(t, pool, integrationAuthConfig())
			attempt := startIntegrationAttempt(t, h)
			payload := validGooglePayload()
			payload.Claims["nonce"] = attempt.nonce
			if test.mutate != nil {
				test.mutate(payload)
			}
			h.google.exchanger = fakeCodeExchanger{raw: "raw-token", err: test.exchangeErr}
			h.google.validator = fakeIDTokenValidator{payload: payload, err: test.validationErr}

			_, err := h.CompleteSignIn(
				t.Context(), attempt.state, "code", "", attempt.binding,
			)

			var refused *SignInError
			require.ErrorAs(t, err, &refused)
			require.Equal(t, SignInProviderError, refused.Code)
			requireNoAuthenticationWrites(t, pool)
		})
	}
}

// covers: AC-2, AC-9
func TestLaterSignInPublishesOnlyProfileChangesAndKeepsAConflictingCopy(t *testing.T) {
	pool := identityIntegrationPool(t)
	h := newIntegrationHandler(t, pool, integrationAuthConfig())
	var logOutput bytes.Buffer
	h.logger = slog.New(slog.NewJSONHandler(&logOutput, nil))

	completeIntegrationSignIn(t, h, validGooglePayload())
	completeIntegrationSignInWithInput(t, h, validGooglePayload(), StartInput{
		Timezone:   "Europe/Paris",
		Language:   "en",
		RedirectTo: "/thread?day=2",
	})

	changed := validGooglePayload()
	changed.Claims["email"] = " New@Example.com "
	changed.Claims["name"] = " New Tutor Name "
	completeIntegrationSignIn(t, h, changed)

	var registeredEvents, changedEvents int
	require.NoError(t, pool.QueryRow(t.Context(),
		"SELECT count(*) FROM outbox WHERE event_name = 'identity.tutor.registered'").Scan(&registeredEvents))
	require.NoError(t, pool.QueryRow(t.Context(),
		"SELECT count(*) FROM outbox WHERE event_name = 'identity.tutor.profile.changed'").Scan(&changedEvents))
	require.Equal(t, 1, registeredEvents)
	require.Equal(t, 1, changedEvents)

	_, err := pool.Exec(t.Context(), `
		INSERT INTO tutors (tutor_id, email, display_name, timezone, language)
		VALUES ('0199345c-7a00-7000-8000-000000000002', 'other@example.com', 'Other Tutor', 'UTC', 'en')`)
	require.NoError(t, err)
	conflicting := validGooglePayload()
	conflicting.Claims["email"] = "other@example.com"
	conflicting.Claims["name"] = "Name That Must Not Replace The Profile"
	attempt := startIntegrationAttempt(t, h)
	conflicting.Claims["nonce"] = attempt.nonce
	h.google.exchanger = fakeCodeExchanger{raw: "raw-token"}
	h.google.validator = fakeIDTokenValidator{payload: conflicting}
	requestCtx := vermouth.WithRequestID(t.Context(), "profile-collision-request")
	_, err = h.CompleteSignIn(requestCtx, attempt.state, "code", "", attempt.binding)
	require.NoError(t, err)

	var email, displayName, providerEmail, timezone, language string
	err = pool.QueryRow(t.Context(), `
		SELECT t.email, t.display_name, i.provider_email, t.timezone, t.language
		FROM tutors t
		JOIN tutor_identities i ON i.tutor_id = t.tutor_id
		WHERE i.provider = 'google' AND i.provider_subject = 'subject-1'`).Scan(
		&email,
		&displayName,
		&providerEmail,
		&timezone,
		&language,
	)
	require.NoError(t, err)
	require.Equal(t, "new@example.com", email)
	require.Equal(t, "New Tutor Name", displayName)
	require.Equal(t, "new@example.com", providerEmail)
	require.Equal(t, "Asia/Ho_Chi_Minh", timezone)
	require.Equal(t, "vi", language)
	require.NoError(t, pool.QueryRow(t.Context(),
		"SELECT count(*) FROM outbox WHERE event_name = 'identity.tutor.profile.changed'").Scan(&changedEvents))
	require.Equal(t, 1, changedEvents)
	require.Contains(t, logOutput.String(), `"msg":"Google email belongs to another tutor"`)
	require.Contains(t, logOutput.String(), `"request_id":"profile-collision-request"`)
}

// covers: AC-9
func TestCompleteSignInRefusesAnUnknownSubjectWhenACompetingEmailInsertCommits(t *testing.T) {
	pool := identityIntegrationPool(t)
	h := newIntegrationHandler(t, pool, integrationAuthConfig())
	attempt := startIntegrationAttempt(t, h)
	payload := validGooglePayload()
	payload.Claims["nonce"] = attempt.nonce
	h.google.exchanger = fakeCodeExchanger{raw: "raw-token"}
	h.google.validator = fakeIDTokenValidator{payload: payload}

	competing, err := pool.Begin(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = competing.Rollback(context.Background()) }) //nolint:usetesting // Cleanup runs after t.Context is canceled.
	_, err = competing.Exec(t.Context(), `
		INSERT INTO tutors (tutor_id, email, display_name, timezone, language)
		VALUES ('0199345c-7a00-7000-8000-000000000020', 'tutor@example.com', 'Competing Tutor', 'UTC', 'en')`)
	require.NoError(t, err)

	result := make(chan error, 1)
	go func() {
		_, completeErr := h.CompleteSignIn(
			t.Context(), attempt.state, "code", "", attempt.binding,
		)
		result <- completeErr
	}()
	waitForBlockedAuthQuery(t, pool, "INSERT INTO tutors")
	require.NoError(t, competing.Commit(t.Context()))

	completeErr := <-result
	var refused *SignInError
	require.ErrorAs(t, completeErr, &refused)
	require.Equal(t, SignInEmailConflict, refused.Code)
	var tutors, links, sessions, events int
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT count(*) FROM tutors").Scan(&tutors))
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT count(*) FROM tutor_identities").Scan(&links))
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT count(*) FROM auth_sessions").Scan(&sessions))
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT count(*) FROM outbox").Scan(&events))
	require.Equal(t, 1, tutors)
	require.Zero(t, links)
	require.Zero(t, sessions)
	require.Zero(t, events)
}
