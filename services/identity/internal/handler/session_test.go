//nolint:testpackage,paralleltest // White box integration tests need package internals and share one database.
package handler

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"
	"github.com/stretchr/testify/require"

	"github.com/nguyen-duc-loc/vermouth/services/identity/internal/store/sqlcgen"
	identitytoken "github.com/nguyen-duc-loc/vermouth/services/identity/internal/token"
)

var errMintTest = errors.New("mint failed")

type failingAccessTokenSigner struct{}

func (failingAccessTokenSigner) Mint(uuid.UUID, string, string) (string, time.Time, error) {
	return "", time.Time{}, errMintTest
}

type sessionBarrierObserver struct {
	afterLockArrived    chan<- uuid.UUID
	afterLockRelease    <-chan struct{}
	beforeInsertArrived chan<- uuid.UUID
	beforeInsertRelease <-chan struct{}
}

func (o sessionBarrierObserver) afterSessionLock(ctx context.Context, sessionID uuid.UUID) {
	waitAtSessionBarrier(ctx, sessionID, o.afterLockArrived, o.afterLockRelease)
}

func (o sessionBarrierObserver) beforeRefreshTokenInsert(ctx context.Context, sessionID uuid.UUID) {
	waitAtSessionBarrier(ctx, sessionID, o.beforeInsertArrived, o.beforeInsertRelease)
}

func waitAtSessionBarrier(
	ctx context.Context,
	sessionID uuid.UUID,
	arrived chan<- uuid.UUID,
	release <-chan struct{},
) {
	if arrived == nil {
		return
	}
	select {
	case arrived <- sessionID:
	case <-ctx.Done():
		return
	}
	select {
	case <-release:
	case <-ctx.Done():
	}
}

// covers: AC-7
func TestWithinGrace_IncludesOnlyNonNegativeElapsedTimeInsideTheWindow(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		usedAt time.Time
		grace  time.Duration
		want   bool
	}{
		{name: "unused window disabled", usedAt: now, grace: 0, want: false},
		{name: "same instant", usedAt: now, grace: time.Second, want: true},
		{name: "inside window", usedAt: now.Add(-time.Second), grace: 2 * time.Second, want: true},
		{name: "exact boundary", usedAt: now.Add(-2 * time.Second), grace: 2 * time.Second, want: true},
		{name: "after boundary", usedAt: now.Add(-2*time.Second - time.Nanosecond), grace: 2 * time.Second, want: false},
		{name: "future use", usedAt: now.Add(time.Nanosecond), grace: 2 * time.Second, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, test.want, withinGrace(test.usedAt, now, test.grace))
		})
	}
}

// covers: AC-7
func TestLockedRotation_ReusesOnlyAUsedTokenOutsideGrace(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	fresh := lockedRotation{now: now}
	within := lockedRotation{
		now: now,
		token: sqlcgen.RefreshToken{UsedAt: pgtype.Timestamptz{
			Time: now.Add(-time.Second), Valid: true,
		}},
	}
	late := lockedRotation{
		now: now,
		token: sqlcgen.RefreshToken{UsedAt: pgtype.Timestamptz{
			Time: now.Add(-3 * time.Second), Valid: true,
		}},
	}

	require.False(t, fresh.reusedAfterGrace(2*time.Second))
	require.False(t, within.reusedAfterGrace(2*time.Second))
	require.True(t, late.reusedAfterGrace(2*time.Second))
}

// covers: AC-6, AC-16
func TestSessionMethodsHandleMissingCookiesWithoutDatabaseAccess(t *testing.T) {
	t.Parallel()

	h := &Handler{}
	_, err := h.Refresh(t.Context(), "  ")
	require.ErrorIs(t, err, ErrSessionRefused)
	require.NoError(t, h.SignOut(t.Context(), "  "))
}

// covers: AC-3, AC-7
func TestRefreshRotatesOnePresentedTokenForConcurrentTabs(t *testing.T) {
	pool := identityIntegrationPool(t)
	h := newIntegrationHandler(t, pool, integrationAuthConfig())
	signedIn := completeIntegrationSignIn(t, h, validGooglePayload())
	start := make(chan struct{})

	type outcome struct {
		result RefreshResult
		err    error
	}
	results := make(chan outcome, 2)
	for range 2 {
		go func() {
			<-start
			result, err := h.Refresh(t.Context(), signedIn.Refresh.Value)
			results <- outcome{result: result, err: err}
		}()
	}
	close(start)

	first := <-results
	second := <-results
	require.NoError(t, first.err)
	require.NoError(t, second.err)
	require.NotEmpty(t, first.result.AccessToken)
	require.NotEmpty(t, second.result.AccessToken)
	require.NotEqual(t, first.result.Refresh.Value, second.result.Refresh.Value)

	var sessions, tokens, tutors int
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT count(*) FROM auth_sessions").Scan(&sessions))
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT count(*) FROM refresh_tokens").Scan(&tokens))
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT count(DISTINCT tutor_id) FROM auth_sessions").Scan(&tutors))
	require.Equal(t, 1, sessions)
	require.Equal(t, 3, tokens)
	require.Equal(t, 1, tutors)
}

// covers: AC-3, AC-7
func TestRefreshRollsBackTheRotationWhenSigningFails(t *testing.T) {
	pool := identityIntegrationPool(t)
	h := newIntegrationHandler(t, pool, integrationAuthConfig())
	signedIn := completeIntegrationSignIn(t, h, validGooglePayload())
	h.signer = failingAccessTokenSigner{}

	result, err := h.Refresh(t.Context(), signedIn.Refresh.Value)

	require.ErrorIs(t, err, errMintTest)
	require.Empty(t, result.AccessToken)
	require.Empty(t, result.Refresh.Value)
	var tokens, unused int
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT count(*) FROM refresh_tokens").Scan(&tokens))
	require.NoError(t, pool.QueryRow(t.Context(),
		"SELECT count(*) FROM refresh_tokens WHERE used_at IS NULL").Scan(&unused))
	require.Equal(t, 1, tokens)
	require.Equal(t, 1, unused)
}

// covers: AC-6, AC-7
func TestRefreshThenSignOutSerializesOnTheSessionFamily(t *testing.T) {
	pool := identityIntegrationPool(t)
	h := newIntegrationHandler(t, pool, integrationAuthConfig())
	signedIn := completeIntegrationSignIn(t, h, validGooglePayload())
	locked := make(chan uuid.UUID, 1)
	release := make(chan struct{})
	h.observer = sessionBarrierObserver{
		afterLockArrived: locked,
		afterLockRelease: release,
	}

	type refreshOutcome struct {
		result RefreshResult
		err    error
	}
	refreshResult := make(chan refreshOutcome, 1)
	go func() {
		result, err := h.Refresh(t.Context(), signedIn.Refresh.Value)
		refreshResult <- refreshOutcome{result: result, err: err}
	}()
	<-locked
	signOutResult := make(chan error, 1)
	go func() { signOutResult <- h.SignOut(t.Context(), signedIn.Refresh.Value) }()
	waitForBlockedAuthQuery(t, pool, "FROM auth_sessions")
	close(release)

	refreshed := <-refreshResult
	require.NoError(t, refreshed.err)
	require.NoError(t, <-signOutResult)
	h.observer = nil
	_, err := h.Refresh(t.Context(), refreshed.result.Refresh.Value)
	require.ErrorIs(t, err, ErrSessionRefused)
	var revoked bool
	require.NoError(t, pool.QueryRow(t.Context(),
		"SELECT revoked_at IS NOT NULL FROM auth_sessions").Scan(&revoked))
	require.True(t, revoked)
}

// covers: AC-7
func TestLateReuseRevokesARefreshPausedBeforeItsInsert(t *testing.T) {
	pool := identityIntegrationPool(t)
	auth := integrationAuthConfig()
	auth.RefreshGrace = time.Second
	h := newIntegrationHandler(t, pool, auth)
	signedIn := completeIntegrationSignIn(t, h, validGooglePayload())
	active, err := h.Refresh(t.Context(), signedIn.Refresh.Value)
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), `
		UPDATE refresh_tokens
		SET used_at = now() - interval '1 minute'
		WHERE token_hash = $1`, hashOpaqueToken(signedIn.Refresh.Value))
	require.NoError(t, err)

	beforeInsert := make(chan uuid.UUID, 1)
	release := make(chan struct{})
	h.observer = sessionBarrierObserver{
		beforeInsertArrived: beforeInsert,
		beforeInsertRelease: release,
	}
	type refreshOutcome struct {
		result RefreshResult
		err    error
	}
	refreshResult := make(chan refreshOutcome, 1)
	go func() {
		result, refreshErr := h.Refresh(t.Context(), active.Refresh.Value)
		refreshResult <- refreshOutcome{result: result, err: refreshErr}
	}()
	<-beforeInsert
	reuseResult := make(chan error, 1)
	go func() {
		_, reuseErr := h.Refresh(t.Context(), signedIn.Refresh.Value)
		reuseResult <- reuseErr
	}()
	waitForBlockedAuthQuery(t, pool, "FROM auth_sessions")
	close(release)

	refreshed := <-refreshResult
	require.NoError(t, refreshed.err)
	require.ErrorIs(t, <-reuseResult, ErrSessionRefused)
	h.observer = nil
	_, err = h.Refresh(t.Context(), refreshed.result.Refresh.Value)
	require.ErrorIs(t, err, ErrSessionRefused)
	var revoked bool
	require.NoError(t, pool.QueryRow(t.Context(),
		"SELECT revoked_at IS NOT NULL FROM auth_sessions").Scan(&revoked))
	require.True(t, revoked)
}

// covers: AC-7, AC-16
func TestRefreshRevokesTheFamilyWhenAnOldTokenIsReusedAfterGrace(t *testing.T) {
	pool := identityIntegrationPool(t)
	auth := integrationAuthConfig()
	auth.RefreshGrace = time.Second
	h := newIntegrationHandler(t, pool, auth)
	signedIn := completeIntegrationSignIn(t, h, validGooglePayload())
	replacement, err := h.Refresh(t.Context(), signedIn.Refresh.Value)
	require.NoError(t, err)

	_, err = pool.Exec(t.Context(), `
		UPDATE refresh_tokens
		SET used_at = now() - interval '1 minute'
		WHERE token_hash = $1`, hashOpaqueToken(signedIn.Refresh.Value))
	require.NoError(t, err)

	_, err = h.Refresh(t.Context(), signedIn.Refresh.Value)
	require.ErrorIs(t, err, ErrSessionRefused)
	_, err = h.Refresh(t.Context(), replacement.Refresh.Value)
	require.ErrorIs(t, err, ErrSessionRefused)

	var revoked bool
	require.NoError(t, pool.QueryRow(t.Context(),
		"SELECT revoked_at IS NOT NULL FROM auth_sessions").Scan(&revoked))
	require.True(t, revoked)
}

// covers: AC-6
func TestSignOutIsIdempotentAndLeavesTheSessionTerminal(t *testing.T) {
	pool := identityIntegrationPool(t)
	h := newIntegrationHandler(t, pool, integrationAuthConfig())
	signedIn := completeIntegrationSignIn(t, h, validGooglePayload())
	refreshed, err := h.Refresh(t.Context(), signedIn.Refresh.Value)
	require.NoError(t, err)

	require.NoError(t, h.SignOut(t.Context(), refreshed.Refresh.Value))
	require.NoError(t, h.SignOut(t.Context(), refreshed.Refresh.Value))
	_, err = h.Refresh(t.Context(), refreshed.Refresh.Value)
	require.ErrorIs(t, err, ErrSessionRefused)

	signer, ok := h.signer.(*identitytoken.Signer)
	require.True(t, ok)
	publicKey, err := base64.StdEncoding.DecodeString(signer.PublicKeyBase64())
	require.NoError(t, err)
	verifier, err := vermouth.NewVerifier(map[string]ed25519.PublicKey{
		signer.KID(): ed25519.PublicKey(publicKey),
	})
	require.NoError(t, err)
	_, err = verifier.Verify(refreshed.AccessToken)
	require.NoError(t, err)
	require.WithinDuration(t, time.Now().UTC().Add(15*time.Minute), refreshed.AccessExpiresAt, time.Second)
}

// covers: AC-7, AC-8
func TestSweepDeletesOnlyExpiredAttemptsAndFinishedSessions(t *testing.T) {
	pool := identityIntegrationPool(t)
	h := newIntegrationHandler(t, pool, integrationAuthConfig())
	completeIntegrationSignIn(t, h, validGooglePayload())

	_, err := pool.Exec(t.Context(), `
		INSERT INTO login_attempts (
			state, code_verifier, nonce, browser_binding_hash,
			redirect_to, timezone, language, expires_at
		) VALUES (
			'expired-state', 'verifier', 'nonce', decode('00', 'hex'),
			'/', 'UTC', 'en', now() - interval '1 minute'
		)`)
	require.NoError(t, err)
	expiredSessionID := uuid.MustParse("0199345c-7a00-7000-8000-000000000003")
	_, err = pool.Exec(t.Context(), `
		INSERT INTO auth_sessions (session_id, tutor_id, expires_at)
		SELECT $1, tutor_id, now() - interval '1 minute'
		FROM tutors
		LIMIT 1`, expiredSessionID)
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), `
		INSERT INTO refresh_tokens (token_hash, session_id, expires_at)
		VALUES (decode('01', 'hex'), $1, now() - interval '1 minute')`, expiredSessionID)
	require.NoError(t, err)

	require.NoError(t, h.sweepOnce(t.Context()))

	var expiredAttempts, expiredSessions, expiredTokens, activeSessions int
	require.NoError(t, pool.QueryRow(t.Context(),
		"SELECT count(*) FROM login_attempts WHERE state = 'expired-state'").Scan(&expiredAttempts))
	require.NoError(t, pool.QueryRow(t.Context(),
		"SELECT count(*) FROM auth_sessions WHERE session_id = $1", expiredSessionID).Scan(&expiredSessions))
	require.NoError(t, pool.QueryRow(t.Context(),
		"SELECT count(*) FROM refresh_tokens WHERE session_id = $1", expiredSessionID).Scan(&expiredTokens))
	require.NoError(t, pool.QueryRow(t.Context(),
		"SELECT count(*) FROM auth_sessions WHERE revoked_at IS NULL AND expires_at > now()").Scan(&activeSessions))
	require.Zero(t, expiredAttempts)
	require.Zero(t, expiredSessions)
	require.Zero(t, expiredTokens)
	require.Equal(t, 1, activeSessions)
}
