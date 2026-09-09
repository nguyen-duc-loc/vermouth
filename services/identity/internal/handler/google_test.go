//nolint:testpackage // These white box tests inject the provider boundaries and inspect canonical claims.
package handler

import (
	"context"
	"errors"
	urlpkg "net/url"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/api/idtoken"
)

type fakeCodeExchanger struct {
	raw string
	err error
}

func (f fakeCodeExchanger) exchange(context.Context, string, string) (string, error) {
	return f.raw, f.err
}

type fakeIDTokenValidator struct {
	payload *idtoken.Payload
	err     error
}

func (f fakeIDTokenValidator) validate(context.Context, string, string) (*idtoken.Payload, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.payload, nil
}

var errTestValidation = errors.New("forged or expired token")

func validGooglePayload() *idtoken.Payload {
	return &idtoken.Payload{
		Issuer:   "https://accounts.google.com",
		Audience: "client-id",
		Subject:  "subject-1",
		Claims: map[string]any{
			"iss":            "https://accounts.google.com",
			"aud":            "client-id",
			"sub":            "subject-1",
			"nonce":          "nonce-1",
			"email":          " Tutor@Example.com ",
			"email_verified": true,
			"name":           " Tutor Name ",
		},
	}
}

// covers: AC-14
func TestGoogleProviderExchange_ReturnsCanonicalVerifiedAccount(t *testing.T) {
	t.Parallel()

	provider := &googleProvider{
		clientID:  "client-id",
		exchanger: fakeCodeExchanger{raw: "raw-token"},
		validator: fakeIDTokenValidator{payload: validGooglePayload()},
	}

	account, err := provider.exchange(t.Context(), "code", "verifier", "nonce-1")

	require.NoError(t, err)
	require.Equal(t, googleAccount{
		subject: "subject-1",
		email:   "tutor@example.com",
		name:    "Tutor Name",
	}, account)
}

// covers: AC-14
func TestGoogleProviderExchange_RefusesMalformedOrUntrustedClaims(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*idtoken.Payload)
		wantErr error
	}{
		{name: "bad issuer", mutate: func(p *idtoken.Payload) { p.Claims["iss"] = "https://evil.example" }, wantErr: errIssuerNotGoogle},
		{name: "issuer wrong type", mutate: func(p *idtoken.Payload) { p.Claims["iss"] = true }, wantErr: errIssuerNotGoogle},
		{name: "issuer field mismatch", mutate: func(p *idtoken.Payload) { p.Issuer = "accounts.google.com" }, wantErr: errIssuerNotGoogle},
		{name: "wrong audience", mutate: func(p *idtoken.Payload) { p.Claims["aud"] = "other-client" }, wantErr: errAudienceMismatch},
		{name: "multiple audiences", mutate: func(p *idtoken.Payload) { p.Claims["aud"] = []string{"client-id", "other-client"} }, wantErr: errAudienceMismatch},
		{name: "audience field mismatch", mutate: func(p *idtoken.Payload) { p.Audience = "other-client" }, wantErr: errAudienceMismatch},
		{name: "empty subject", mutate: func(p *idtoken.Payload) { p.Claims["sub"] = "" }, wantErr: errNoSubject},
		{name: "subject wrong type", mutate: func(p *idtoken.Payload) { p.Claims["sub"] = 7 }, wantErr: errNoSubject},
		{name: "subject field mismatch", mutate: func(p *idtoken.Payload) { p.Subject = "subject-2" }, wantErr: errNoSubject},
		{name: "wrong nonce", mutate: func(p *idtoken.Payload) { p.Claims["nonce"] = "nonce-2" }, wantErr: errNonceMismatch},
		{name: "nonce wrong type", mutate: func(p *idtoken.Payload) { p.Claims["nonce"] = true }, wantErr: errNonceMismatch},
		{name: "unverified email", mutate: func(p *idtoken.Payload) { p.Claims["email_verified"] = false }, wantErr: errEmailUnverified},
		{name: "verification wrong type", mutate: func(p *idtoken.Payload) { p.Claims["email_verified"] = "true" }, wantErr: errEmailUnverified},
		{name: "empty email", mutate: func(p *idtoken.Payload) { p.Claims["email"] = " " }, wantErr: errNoEmail},
		{name: "email wrong type", mutate: func(p *idtoken.Payload) { p.Claims["email"] = 7 }, wantErr: errNoEmail},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			payload := validGooglePayload()
			test.mutate(payload)
			provider := &googleProvider{
				clientID:  "client-id",
				exchanger: fakeCodeExchanger{raw: "raw-token"},
				validator: fakeIDTokenValidator{payload: payload},
			}

			_, err := provider.exchange(t.Context(), "code", "verifier", "nonce-1")

			require.ErrorIs(t, err, test.wantErr)
		})
	}
}

// covers: AC-14
func TestGoogleProviderExchange_PropagatesProviderValidationFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		exchanger fakeCodeExchanger
		validator fakeIDTokenValidator
		wantErr   error
	}{
		{name: "exchange failure", exchanger: fakeCodeExchanger{err: errNoIDToken}, wantErr: errNoIDToken},
		{name: "signature or expiry failure", exchanger: fakeCodeExchanger{raw: "raw"}, validator: fakeIDTokenValidator{err: errTestValidation}, wantErr: errTestValidation},
		{name: "nil payload", exchanger: fakeCodeExchanger{raw: "raw"}, validator: fakeIDTokenValidator{}, wantErr: errNoIDTokenPayload},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			provider := &googleProvider{clientID: "client-id", exchanger: test.exchanger, validator: test.validator}
			_, err := provider.exchange(t.Context(), "code", "verifier", "nonce-1")
			require.ErrorIs(t, err, test.wantErr)
		})
	}
}

// covers: AC-14
func TestAccountFromPayload_UsesEmailLocalPartWhenNameIsMissingOrMalformed(t *testing.T) {
	t.Parallel()

	for _, name := range []any{nil, "", "   ", 7} {
		payload := validGooglePayload()
		payload.Claims["name"] = name

		account, err := accountFromPayload(payload)

		require.NoError(t, err)
		require.Equal(t, "tutor", account.name)
	}
}

// covers: AC-1, AC-10
func TestGoogleProviderAuthorizeURLCarriesPKCEStateNonceAndExactScopes(t *testing.T) {
	t.Parallel()

	provider := newGoogleProvider(AuthConfig{
		GoogleClientID:     "client-id",
		GoogleClientSecret: "secret",
		GoogleRedirectURL:  "https://app.example/api/auth/google/callback",
	})

	authorizeURL, err := urlpkg.Parse(provider.authorizeURL("state-1", "verifier-1", "nonce-1"))

	require.NoError(t, err)
	query := authorizeURL.Query()
	require.Equal(t, "state-1", query.Get("state"))
	require.Equal(t, "nonce-1", query.Get("nonce"))
	require.Equal(t, "S256", query.Get("code_challenge_method"))
	require.NotEmpty(t, query.Get("code_challenge"))
	require.Equal(t, "openid email profile", query.Get("scope"))
}
