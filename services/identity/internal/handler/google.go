package handler

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/idtoken"
)

// googleProviderName is the only provider today. It is stored on every link
// rather than assumed, so a second one later is a row and a branch instead of a
// schema change.
const googleProviderName = "google"

// The ways Google's answer is refused. Static so the callback can log the reason
// while the browser only ever sees provider_error.
var (
	errNoIDToken        = errors.New("google returned no id_token")
	errNoIDTokenPayload = errors.New("google returned no id token payload")
	errIssuerNotGoogle  = errors.New("id token was not issued by google")
	errAudienceMismatch = errors.New("id token was minted for another client")
	errNonceMismatch    = errors.New("id token nonce does not match the sign in attempt")
	errEmailUnverified  = errors.New("google has not verified this email address")
	errNoSubject        = errors.New("id token carries no subject")
	errNoEmail          = errors.New("id token carries no email")
)

// codeExchanger is the one outbound OAuth operation the callback needs. Keeping
// it behind a small interface lets callback tests supply a raw ID token without
// contacting Google.
type codeExchanger interface {
	exchange(ctx context.Context, code, verifier string) (string, error)
}

// idTokenValidator is the signature, key set, audience and expiry boundary.
// Tests can inject a payload while production delegates to Google's validator.
type idTokenValidator interface {
	validate(ctx context.Context, raw, audience string) (*idtoken.Payload, error)
}

// googleProvider is identity's side of the OAuth exchange. It holds the client
// secret, which is the whole reason this half runs on the server and the browser
// never sees it (spec 0004).
type googleProvider struct {
	oauth     *oauth2.Config
	clientID  string
	exchanger codeExchanger
	validator idTokenValidator
}

// newGoogleProvider builds the client from configuration read at startup.
func newGoogleProvider(cfg AuthConfig) *googleProvider {
	oauthConfig := &oauth2.Config{
		ClientID:     cfg.GoogleClientID,
		ClientSecret: cfg.GoogleClientSecret,
		RedirectURL:  cfg.GoogleRedirectURL,
		Endpoint:     google.Endpoint,
		// openid gets the ID token, email and profile the two claims a tutor
		// row is built from. Nothing more is asked for.
		Scopes: []string{"openid", "email", "profile"},
	}
	return &googleProvider{
		oauth:     oauthConfig,
		clientID:  cfg.GoogleClientID,
		exchanger: &oauthCodeExchanger{oauth: oauthConfig},
		validator: googleIDTokenValidator{},
	}
}

// oauthCodeExchanger owns the confidential code exchange and returns only the
// raw ID token. Claim decisions stay in googleProvider.
type oauthCodeExchanger struct {
	oauth *oauth2.Config
}

func (e *oauthCodeExchanger) exchange(ctx context.Context, code, verifier string) (string, error) {
	token, err := e.oauth.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return "", fmt.Errorf("exchange code with google: %w", err)
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok || strings.TrimSpace(raw) == "" {
		return "", errNoIDToken
	}
	return raw, nil
}

// googleIDTokenValidator delegates signature, key set, exact audience and
// expiry checks to the supported Google library.
type googleIDTokenValidator struct{}

func (googleIDTokenValidator) validate(
	ctx context.Context, raw, audience string,
) (*idtoken.Payload, error) {
	return idtoken.Validate(ctx, raw, audience)
}

// authorizeURL is where the browser is sent. The verifier stays here and only
// its challenge travels, which is what stops a stolen code being redeemed by
// anybody else (PKCE, RFC 7636).
func (p *googleProvider) authorizeURL(state, verifier, nonce string) string {
	return p.oauth.AuthCodeURL(state,
		oauth2.AccessTypeOnline,
		oauth2.S256ChallengeOption(verifier),
		oauth2.SetAuthURLParam("nonce", nonce),
	)
}

// googleAccount is the little identity takes out of Google's answer: the stable
// subject that is the identity, the verified email that is only a copy of it,
// and a name to show.
type googleAccount struct {
	subject string
	email   string
	name    string
}

// exchange trades the code for tokens over a connection identity opened itself,
// then refuses everything about the ID token that does not check out. It writes
// nothing: the caller only reaches the database once this has returned (AC-14).
func (p *googleProvider) exchange(ctx context.Context, code, verifier, nonce string) (googleAccount, error) {
	raw, err := p.exchanger.exchange(ctx, code, verifier)
	if err != nil {
		return googleAccount{}, err
	}
	payload, err := p.validator.validate(ctx, raw, p.clientID)
	if err != nil {
		return googleAccount{}, fmt.Errorf("validate google id token: %w", err)
	}
	if payload == nil {
		return googleAccount{}, errNoIDTokenPayload
	}
	err = p.checkClaims(payload, nonce)
	if err != nil {
		return googleAccount{}, err
	}
	return accountFromPayload(payload)
}

// checkClaims reads the typed claims the handler owns after the validator has
// proved the signature, key set, exact audience and expiry.
func (p *googleProvider) checkClaims(payload *idtoken.Payload, nonce string) error {
	issuer, ok := stringClaim(payload, "iss")
	if !ok || !isGoogleIssuer(issuer) || issuer != payload.Issuer {
		return errIssuerNotGoogle
	}
	audience, ok := stringClaim(payload, "aud")
	if !ok || audience != p.clientID || payload.Audience != p.clientID {
		return errAudienceMismatch
	}
	subject, ok := stringClaim(payload, "sub")
	if !ok || strings.TrimSpace(subject) == "" || subject != payload.Subject {
		return errNoSubject
	}
	tokenNonce, ok := stringClaim(payload, "nonce")
	if !ok || tokenNonce == "" || tokenNonce != nonce {
		return errNonceMismatch
	}
	verified, ok := payload.Claims["email_verified"].(bool)
	if !ok || !verified {
		return errEmailUnverified
	}
	if email, emailOK := stringClaim(payload, "email"); !emailOK || strings.TrimSpace(email) == "" {
		return errNoEmail
	}
	return nil
}

// accountFromPayload takes the three values a tutor row is built from. A missing
// name falls back to the part of the email before the @, so a tutor is never
// created with an empty display name.
func accountFromPayload(payload *idtoken.Payload) (googleAccount, error) {
	subject, subjectOK := stringClaim(payload, "sub")
	emailClaim, emailOK := stringClaim(payload, "email")
	if !subjectOK || strings.TrimSpace(subject) == "" {
		return googleAccount{}, errNoSubject
	}
	email := strings.ToLower(strings.TrimSpace(emailClaim))
	if !emailOK || email == "" {
		return googleAccount{}, errNoEmail
	}
	nameClaim, _ := stringClaim(payload, "name")
	name := strings.TrimSpace(nameClaim)
	if name == "" {
		name, _, _ = strings.Cut(email, "@")
	}
	return googleAccount{subject: subject, email: email, name: name}, nil
}

// isGoogleIssuer accepts the two spellings Google mints with, both legitimate,
// and nothing else.
func isGoogleIssuer(issuer string) bool {
	return issuer == "https://accounts.google.com" || issuer == "accounts.google.com"
}

// stringClaim reads one claim without coercion, so a wrong type is refused
// rather than made to look like a valid empty value.
func stringClaim(payload *idtoken.Payload, name string) (string, bool) {
	value, ok := payload.Claims[name].(string)
	return value, ok
}

// newPKCEVerifier is the secret half of the PKCE pair. It stays on the login
// attempt row until the callback redeems the code with it.
func newPKCEVerifier() string { return oauth2.GenerateVerifier() }
