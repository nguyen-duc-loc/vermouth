package handler

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	urlpkg "net/url"
	"path"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"

	"github.com/nguyen-duc-loc/vermouth/services/identity/internal/store"
	"github.com/nguyen-duc-loc/vermouth/services/identity/internal/store/sqlcgen"
)

// The five reasons the sign in screen has a sentence for. They are the only
// thing a refused sign in tells the browser: the reason itself goes to the log
// with the request id (spec 0004).
const (
	SignInNotAllowed    = "not_allowed"
	SignInEmailConflict = "email_conflict"
	SignInCancelled     = "cancelled"
	SignInExpiredState  = "expired_state"
	SignInProviderError = "provider_error"
)

// randomTokenBytes is the length of every unguessable value in sign in: the
// state, the nonce, and the refresh token.
const randomTokenBytes = 32

// attemptTTL is how long a half finished sign in stays usable. Anything older is
// the sweep's, and the callback treats it as unknown either way.
const attemptTTL = 10 * time.Minute

// What a browser that says nothing about itself is assumed to be (AC-10).
const (
	defaultTimezone = "Asia/Ho_Chi_Minh"
	defaultLanguage = "vi"
)

// The refusals that are decisions rather than failures, so the callback can log
// which one happened while the browser only sees the code beside it.
var (
	errNotAllowed      = errors.New("email is not on the sign up allowlist")
	errEmailBelongs    = errors.New("email already belongs to another tutor")
	errAttemptNotFound = errors.New("no live sign in attempt for that state")
	errCancelled       = errors.New("google reported the sign in was not completed")
)

// SignInError is a sign in that was refused, carrying the code the sign in
// screen turns into a sentence.
type SignInError struct {
	Code string
	Err  error
}

func (e *SignInError) Error() string { return e.Code + ": " + e.Err.Error() }

// Unwrap keeps the reason matchable, so a log line can name it.
func (e *SignInError) Unwrap() error { return e.Err }

// refuse builds the one refusal shape, so no path invents a code.
func refuse(code string, err error) *SignInError {
	return &SignInError{Code: code, Err: err}
}

// StartInput is what the browser tells the start call about itself. All three are
// optional, and none of them is trusted further than a fallback (AC-10).
type StartInput struct {
	Timezone   string
	Language   string
	RedirectTo string
}

// StartSignInResult carries the provider redirect and the independent browser
// binding cookie. Neither value is a session credential.
type StartSignInResult struct {
	AuthorizeURL   string
	BrowserBinding string
	ExpiresAt      time.Time
}

// StartSignIn writes the in flight sign in and answers with Google's authorize
// URL. The state, the PKCE verifier and the nonce are generated here and only
// their public halves travel, so a forged callback has no row to match.
func (h *Handler) StartSignIn(ctx context.Context, in StartInput) (StartSignInResult, error) {
	state, err := randomToken()
	if err != nil {
		return StartSignInResult{}, err
	}
	nonce, err := randomToken()
	if err != nil {
		return StartSignInResult{}, err
	}
	browserBinding, err := randomToken()
	if err != nil {
		return StartSignInResult{}, err
	}
	verifier := newPKCEVerifier()
	clean := cleanStartInput(in)
	expiresAt := time.Now().UTC().Add(attemptTTL)

	err = store.Queries(h.pool).InsertLoginAttempt(ctx, sqlcgen.InsertLoginAttemptParams{
		State:              state,
		CodeVerifier:       verifier,
		Nonce:              nonce,
		BrowserBindingHash: hashOpaqueToken(browserBinding),
		RedirectTo:         clean.RedirectTo,
		Timezone:           clean.Timezone,
		Language:           clean.Language,
		ExpiresAt:          expiresAt,
	})
	if err != nil {
		return StartSignInResult{}, fmt.Errorf("write sign in attempt: %w", err)
	}
	return StartSignInResult{
		AuthorizeURL:   h.google.authorizeURL(state, verifier, nonce),
		BrowserBinding: browserBinding,
		ExpiresAt:      expiresAt,
	}, nil
}

// cleanStartInput falls back rather than refusing: a browser sending a timezone
// this build has never heard of is not a reason to keep a tutor out. The redirect
// is the exception that has to be strict, because a path is all it may be.
func cleanStartInput(in StartInput) StartInput {
	clean := StartInput{
		Timezone:   defaultTimezone,
		Language:   defaultLanguage,
		RedirectTo: "/",
	}
	if timezone := strings.TrimSpace(in.Timezone); timezone != "" {
		_, err := time.LoadLocation(timezone)
		if err == nil {
			clean.Timezone = timezone
		}
	}
	if language := strings.TrimSpace(in.Language); language == "vi" || language == "en" {
		clean.Language = language
	}
	clean.RedirectTo = cleanRelativeRedirect(in.RedirectTo)
	return clean
}

func cleanRelativeRedirect(raw string) string {
	target := strings.TrimSpace(raw)
	if target == "" || strings.Contains(target, "\\") || strings.Contains(target, "#") {
		return "/"
	}
	for _, char := range target {
		if unicode.IsControl(char) {
			return "/"
		}
	}
	parsed, err := urlpkg.Parse(target)
	if err != nil || parsed.Scheme != "" || parsed.Host != "" || parsed.User != nil ||
		parsed.Opaque != "" || parsed.Fragment != "" {
		return "/"
	}
	decoded, err := urlpkg.PathUnescape(parsed.EscapedPath())
	if err != nil || !strings.HasPrefix(decoded, "/") || strings.HasPrefix(decoded, "//") ||
		strings.Contains(decoded, "\\") {
		return "/"
	}
	for _, char := range decoded {
		if unicode.IsControl(char) {
			return "/"
		}
	}
	decodedQuery, err := urlpkg.QueryUnescape(parsed.RawQuery)
	if err != nil || strings.Contains(decodedQuery, "\\") {
		return "/"
	}
	for _, char := range decodedQuery {
		if unicode.IsControl(char) {
			return "/"
		}
	}
	cleaned := path.Clean(decoded)
	if !strings.HasPrefix(cleaned, "/") || strings.HasPrefix(cleaned, "//") {
		return "/"
	}
	return (&urlpkg.URL{Path: cleaned, RawQuery: parsed.RawQuery}).RequestURI()
}

// SignInResult is what the callback produces: where to land the browser, and the
// refresh token the cookie carries. There is deliberately no access token here,
// so none can end up in a URL or in history.
type SignInResult struct {
	RedirectTo string
	Refresh    IssuedRefreshToken
}

// CompleteSignIn is the callback. The attempt is read first because the PKCE
// verifier it holds is what the exchange needs; nothing is written until Google's
// ID token has passed every check (AC-14).
func (h *Handler) CompleteSignIn(
	ctx context.Context, state, code, providerError, browserBinding string,
) (SignInResult, error) {
	attempt, err := store.Queries(h.pool).GetLoginAttempt(ctx, state)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SignInResult{}, refuse(SignInExpiredState, errAttemptNotFound)
		}
		return SignInResult{}, fmt.Errorf("read sign in attempt: %w", err)
	}
	if subtle.ConstantTimeCompare(
		hashOpaqueToken(browserBinding), attempt.BrowserBindingHash,
	) != 1 {
		return SignInResult{}, refuse(SignInExpiredState, errAttemptNotFound)
	}
	if providerError != "" {
		err = h.consumeCancelledAttempt(ctx, attempt.State)
		if err != nil {
			return SignInResult{}, err
		}
		return SignInResult{}, refuse(SignInCancelled, errCancelled)
	}
	account, err := h.google.exchange(ctx, code, attempt.CodeVerifier, attempt.Nonce)
	if err != nil {
		return SignInResult{}, refuse(SignInProviderError, err)
	}
	return h.signIn(ctx, attempt, account)
}

func (h *Handler) consumeCancelledAttempt(ctx context.Context, state string) error {
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin cancelled sign in: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = store.Queries(tx).ConsumeLoginAttempt(ctx, state)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return refuse(SignInExpiredState, errAttemptNotFound)
		}
		return fmt.Errorf("consume cancelled sign in: %w", err)
	}
	err = tx.Commit(ctx)
	if err != nil {
		return fmt.Errorf("commit cancelled sign in: %w", err)
	}
	return nil
}

// signIn is the one transaction the callback commits: the attempt consumed by a
// delete, the tutor found or created with its link and its event, and the first
// refresh token of a new session family. The callback never mints an access
// token, so every browser reaches that operation through refresh.
func (h *Handler) signIn(
	ctx context.Context, attempt sqlcgen.GetLoginAttemptRow, account googleAccount,
) (SignInResult, error) {
	result, tutor, sessionID, err := h.signInOnce(ctx, attempt, account)
	if err != nil && isConcurrentFirstSignIn(err) {
		result, tutor, sessionID, err = h.signInOnce(ctx, attempt, account)
	}
	if err != nil {
		return SignInResult{}, err
	}
	h.logger.InfoContext(ctx, "Signed in",
		slog.String("request_id", vermouth.RequestID(ctx)),
		slog.String("tutor_id", tutor.tutorID.String()),
		slog.String("session_id", sessionID.String()),
	)
	return result, nil
}

func (h *Handler) signInOnce(
	ctx context.Context, attempt sqlcgen.GetLoginAttemptRow, account googleAccount,
) (SignInResult, signedInTutor, uuid.UUID, error) {
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return SignInResult{}, signedInTutor{}, uuid.Nil, fmt.Errorf("begin sign in: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = store.Queries(tx).ConsumeLoginAttempt(ctx, attempt.State)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SignInResult{}, signedInTutor{}, uuid.Nil,
				refuse(SignInExpiredState, errAttemptNotFound)
		}
		return SignInResult{}, signedInTutor{}, uuid.Nil,
			fmt.Errorf("consume sign in attempt: %w", err)
	}
	tutor, err := h.resolveTutor(ctx, tx, attempt, account)
	if err != nil {
		return SignInResult{}, signedInTutor{}, uuid.Nil, err
	}
	sessionID, err := uuid.NewV7()
	if err != nil {
		return SignInResult{}, signedInTutor{}, uuid.Nil, fmt.Errorf("new session id: %w", err)
	}
	expiresAt := time.Now().UTC().Add(h.auth.RefreshTTL)
	_, err = store.Queries(tx).InsertAuthSession(ctx, sqlcgen.InsertAuthSessionParams{
		SessionID: sessionID,
		TutorID:   tutor.tutorID,
		ExpiresAt: expiresAt,
	})
	if err != nil {
		return SignInResult{}, signedInTutor{}, uuid.Nil,
			fmt.Errorf("create session family: %w", err)
	}
	issued, err := issueRefreshToken(ctx, tx, sessionID, expiresAt)
	if err != nil {
		return SignInResult{}, signedInTutor{}, uuid.Nil, err
	}
	err = tx.Commit(ctx)
	if err != nil {
		return SignInResult{}, signedInTutor{}, uuid.Nil, fmt.Errorf("commit sign in: %w", err)
	}
	return SignInResult{
		RedirectTo: h.auth.ResolveRedirect(attempt.RedirectTo),
		Refresh:    issued,
	}, tutor, sessionID, nil
}

func isConcurrentFirstSignIn(err error) bool {
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	if !ok || pgErr.Code != "23505" {
		return false
	}
	return pgErr.ConstraintName == "tutors_email_key" ||
		pgErr.ConstraintName == "tutor_identities_pkey"
}

// signedInTutor is the tutor a sign in resolved to, in one shape whether it was
// just created or has signed in every day for a year. It carries what minting
// needs and nothing else.
type signedInTutor struct {
	tutorID  uuid.UUID
	timezone string
	language string
}

// resolveTutor answers who is signing in. The lookup is by Google's subject and
// only by that: an email match never finds a link, because an email is a copy of
// the identity rather than the identity itself.
func (h *Handler) resolveTutor(
	ctx context.Context, tx pgx.Tx,
	attempt sqlcgen.GetLoginAttemptRow, account googleAccount,
) (signedInTutor, error) {
	linked, err := store.Queries(tx).GetTutorByProviderSubject(ctx, sqlcgen.GetTutorByProviderSubjectParams{
		Provider:        googleProviderName,
		ProviderSubject: account.subject,
	})
	switch {
	case err == nil:
		return h.signInLinkedTutor(ctx, tx, linked, account)
	case errors.Is(err, pgx.ErrNoRows):
		return h.createTutor(ctx, tx, attempt, account)
	default:
		return signedInTutor{}, fmt.Errorf("find the google link: %w", err)
	}
}

// createTutor is the first sign in: the allowlist decides whether it may happen
// at all, and the tutor, the link and the event are written together.
//
// The allowlist is checked before the email conflict on purpose. A stranger then
// learns only that they are not invited, rather than which addresses already have
// an account here.
func (h *Handler) createTutor(
	ctx context.Context, tx pgx.Tx,
	attempt sqlcgen.GetLoginAttemptRow, account googleAccount,
) (signedInTutor, error) {
	queries := store.Queries(tx)
	if !h.auth.SignupAllowlist[account.email] {
		return signedInTutor{}, refuse(SignInNotAllowed, errNotAllowed)
	}
	_, err := queries.GetTutorByEmail(ctx, account.email)
	switch {
	case err == nil:
		// A different Google account on an email a tutor already holds. Linking
		// is out of scope, so this is refused rather than guessed at.
		return signedInTutor{}, refuse(SignInEmailConflict, errEmailBelongs)
	case !errors.Is(err, pgx.ErrNoRows):
		return signedInTutor{}, fmt.Errorf("look for an existing tutor on that email: %w", err)
	}

	tutorID, err := uuid.NewV7()
	if err != nil {
		return signedInTutor{}, fmt.Errorf("new tutor id: %w", err)
	}
	row, err := queries.InsertTutor(ctx, sqlcgen.InsertTutorParams{
		TutorID:     tutorID,
		Email:       account.email,
		DisplayName: account.name,
		Timezone:    attempt.Timezone,
		Language:    attempt.Language,
	})
	if err != nil {
		return signedInTutor{}, fmt.Errorf("insert tutor: %w", err)
	}
	_, err = queries.InsertTutorIdentity(ctx, sqlcgen.InsertTutorIdentityParams{
		TutorID:         tutorID,
		Provider:        googleProviderName,
		ProviderSubject: account.subject,
		ProviderEmail:   account.email,
	})
	if err != nil {
		return signedInTutor{}, fmt.Errorf("link the google account: %w", err)
	}
	err = h.publishTutorEvent(ctx, tx, vermouth.EventTutorRegistered, toRegisteredFields(row))
	if err != nil {
		return signedInTutor{}, err
	}
	return signedInTutor{tutorID: row.TutorID, timezone: row.Timezone, language: row.Language}, nil
}

// signInLinkedTutor is every sign in after the first. The allowlist is not
// consulted: removing an address must not lock out a tutor who already exists.
//
// Google owns the email, so a change there is copied here and published. When
// the new address already belongs to a different tutor the sign in still
// succeeds with the copy left as it was, because locking a real tutor out over a
// copied field is worse than a stale copy (AC-9).
func (h *Handler) signInLinkedTutor(
	ctx context.Context, tx pgx.Tx,
	linked sqlcgen.GetTutorByProviderSubjectRow, account googleAccount,
) (signedInTutor, error) {
	current := signedInTutor{
		tutorID:  linked.TutorID,
		timezone: linked.Timezone,
		language: linked.Language,
	}
	if account.email == linked.Email && account.name == linked.DisplayName {
		return current, nil
	}
	taken, err := store.Queries(tx).GetTutorByEmail(ctx, account.email)
	switch {
	case err == nil && taken.TutorID != linked.TutorID:
		h.logger.WarnContext(ctx, "Google email belongs to another tutor",
			slog.String("request_id", vermouth.RequestID(ctx)),
			slog.String("tutor_id", linked.TutorID.String()),
			slog.String("other_tutor_id", taken.TutorID.String()),
		)
		return current, nil
	case err != nil && !errors.Is(err, pgx.ErrNoRows):
		return signedInTutor{}, fmt.Errorf("look for a tutor on the new email: %w", err)
	}
	return current, h.copyProviderProfile(ctx, tx, linked, account)
}

// copyProviderProfile writes the new email and name onto both rows and publishes
// the change, all inside the sign in transaction.
func (h *Handler) copyProviderProfile(
	ctx context.Context, tx pgx.Tx,
	linked sqlcgen.GetTutorByProviderSubjectRow, account googleAccount,
) error {
	queries := store.Queries(tx)
	row, err := queries.UpdateTutorFromProvider(ctx, sqlcgen.UpdateTutorFromProviderParams{
		TutorID:     linked.TutorID,
		Email:       account.email,
		DisplayName: account.name,
	})
	if err != nil {
		return fmt.Errorf("copy the google profile onto the tutor: %w", err)
	}
	err = queries.UpdateTutorIdentityEmail(ctx, sqlcgen.UpdateTutorIdentityEmailParams{
		TutorID:       linked.TutorID,
		Provider:      googleProviderName,
		ProviderEmail: account.email,
	})
	if err != nil {
		return fmt.Errorf("copy the google email onto the link: %w", err)
	}
	return h.publishTutorEvent(ctx, tx, vermouth.EventTutorProfileChanged, toChangedFields(row))
}

// randomToken is the one source of an unguessable value here: 32 bytes from
// crypto/rand, base64url so it survives a URL and a cookie unchanged.
func randomToken() (string, error) {
	raw := make([]byte, randomTokenBytes)
	_, err := rand.Read(raw)
	if err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func hashOpaqueToken(value string) []byte {
	sum := sha256.Sum256([]byte(value))
	return sum[:]
}
