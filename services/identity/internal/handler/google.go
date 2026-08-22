package handler

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/idtoken"
)

// googleProviderName is the only provider today. It is stored on every link
// rather than assumed, so a second one later is a row and a branch instead of a
// schema change.
const googleProviderName = "google"

// idTokenSkew is how much difference between this machine's clock and Google's
// is tolerated while reading the ID token's expiry (AC-14).
const idTokenSkew = 60 * time.Second

// The ways Google's answer is refused. Static so the callback can log the reason
// while the browser only ever sees provider_error.
var (
	errNoIDToken        = errors.New("google returned no id_token")
	errIssuerNotGoogle  = errors.New("id token was not issued by google")
	errAudienceMismatch = errors.New("id token was minted for another client")
	errIDTokenExpired   = errors.New("id token has expired")
	errNonceMismatch    = errors.New("id token nonce does not match the sign in attempt")
	errEmailUnverified  = errors.New("google has not verified this email address")
	errNoEmail          = errors.New("id token carries no email")
)

// googleProvider is identity's side of the OAuth exchange. It holds the client
// secret, which is the whole reason this half runs on the server and the browser
// never sees it (spec 0004).
type googleProvider struct {
	oauth    *oauth2.Config
	clientID string
}

// newGoogleProvider builds the client from configuration read at startup.
func newGoogleProvider(cfg AuthConfig) *googleProvider {
	return &googleProvider{
		clientID: cfg.GoogleClientID,
		oauth: &oauth2.Config{
			ClientID:     cfg.GoogleClientID,
			ClientSecret: cfg.GoogleClientSecret,
			RedirectURL:  cfg.GoogleRedirectURL,
			Endpoint:     google.Endpoint,
			// openid gets the ID token, email and profile the two claims a tutor
			// row is built from. Nothing more is asked for.
			Scopes: []string{"openid", "email", "profile"},
		},
	}
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
	token, err := p.oauth.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return googleAccount{}, fmt.Errorf("exchange code with google: %w", err)
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok || raw == "" {
		return googleAccount{}, errNoIDToken
	}
	// Validate covers the signature against Google's key set and the audience.
	// It refuses an expired token with no skew at all, so the expiry check below
	// only ever widens what is accepted, never narrows it.
	payload, err := idtoken.Validate(ctx, raw, p.clientID)
	if err != nil {
		return googleAccount{}, fmt.Errorf("validate google id token: %w", err)
	}
	err = p.checkClaims(payload, nonce)
	if err != nil {
		return googleAccount{}, err
	}
	return accountFromPayload(payload)
}

// checkClaims reads the claims the transport cannot vouch for: who the token was
// minted for, by whom, until when, for which sign in attempt, and whether Google
// stands behind the email.
func (p *googleProvider) checkClaims(payload *idtoken.Payload, nonce string) error {
	if !isGoogleIssuer(payload.Issuer) {
		return fmt.Errorf("%w: %s", errIssuerNotGoogle, payload.Issuer)
	}
	if payload.Audience != p.clientID {
		return errAudienceMismatch
	}
	if time.Unix(payload.Expires, 0).Add(idTokenSkew).Before(time.Now()) {
		return errIDTokenExpired
	}
	if stringClaim(payload, "nonce") != nonce {
		return errNonceMismatch
	}
	verified, _ := payload.Claims["email_verified"].(bool)
	if !verified {
		return errEmailUnverified
	}
	return nil
}

// accountFromPayload takes the three values a tutor row is built from. A missing
// name falls back to the part of the email before the @, so a tutor is never
// created with an empty display name.
func accountFromPayload(payload *idtoken.Payload) (googleAccount, error) {
	email := strings.ToLower(strings.TrimSpace(stringClaim(payload, "email")))
	if email == "" {
		return googleAccount{}, errNoEmail
	}
	name := strings.TrimSpace(stringClaim(payload, "name"))
	if name == "" {
		name, _, _ = strings.Cut(email, "@")
	}
	return googleAccount{subject: payload.Subject, email: email, name: name}, nil
}

// isGoogleIssuer accepts the two spellings Google mints with, both legitimate,
// and nothing else.
func isGoogleIssuer(issuer string) bool {
	return issuer == "https://accounts.google.com" || issuer == "accounts.google.com"
}

// stringClaim reads one claim as a string, answering empty for anything else, so
// a claim of the wrong type is refused by the check that wanted it rather than
// by a panic here.
func stringClaim(payload *idtoken.Payload, name string) string {
	value, _ := payload.Claims[name].(string)
	return value
}

// newPKCEVerifier is the secret half of the PKCE pair. It stays on the login
// attempt row until the callback redeems the code with it.
func newPKCEVerifier() string { return oauth2.GenerateVerifier() }
