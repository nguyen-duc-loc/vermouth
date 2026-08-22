// Package token mints the access tokens identity signs.
//
// Minting lives in identity and only in identity, because identity alone holds
// the private key (spec 0001, identity propagation). Every other service holds
// only the public key and verifies locally, which is why a verifying service
// can never mint (INV-14).
//
// Feature 7 owns token lifetime, refresh rotation and browser storage. What is
// here is the mechanism spec 0001 fixes, with the lifetime taken from
// configuration so feature 7 changes a value rather than this code.
package token

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// defaultTokenTTL is how long an access token lives when IDENTITY_TOKEN_TTL
// says nothing. Feature 7 owns the real number; this is the recommendation
// spec 0002 carried into it.
const defaultTokenTTL = 15 * time.Minute

// The three ways the signing configuration is refused, static so startup fails
// with a reason a caller can match on (STK-8).
var (
	errNoKID        = errors.New("configuration: IDENTITY_TOKEN_KID is required and not set")
	errNoPrivateKey = errors.New("configuration: IDENTITY_TOKEN_PRIVATE_KEY is required and not set")
	errNotEd25519   = errors.New("configuration: IDENTITY_TOKEN_PRIVATE_KEY is not an Ed25519 private key")
)

// Signer holds the private key and the kid that names it.
type Signer struct {
	kid        string
	privateKey ed25519.PrivateKey
	ttl        time.Duration
}

// NewSignerFromEnv reads IDENTITY_TOKEN_KID, IDENTITY_TOKEN_PRIVATE_KEY (base64
// of the raw 64 byte Ed25519 private key) and IDENTITY_TOKEN_TTL. A missing or
// unparseable value stops startup with a named error (STK-8).
func NewSignerFromEnv() (*Signer, error) {
	kid := strings.TrimSpace(os.Getenv("IDENTITY_TOKEN_KID"))
	if kid == "" {
		return nil, errNoKID
	}
	encoded := strings.TrimSpace(os.Getenv("IDENTITY_TOKEN_PRIVATE_KEY"))
	if encoded == "" {
		return nil, errNoPrivateKey
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("configuration: IDENTITY_TOKEN_PRIVATE_KEY is not valid base64: %w", err)
	}
	if len(decoded) != ed25519.PrivateKeySize {
		return nil, errNotEd25519
	}
	ttl := defaultTokenTTL
	if raw := strings.TrimSpace(os.Getenv("IDENTITY_TOKEN_TTL")); raw != "" {
		ttl, err = time.ParseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("configuration: IDENTITY_TOKEN_TTL is not a duration such as 15m: %w", err)
		}
	}
	return &Signer{kid: kid, privateKey: ed25519.PrivateKey(decoded), ttl: ttl}, nil
}

// KID names the key that signs, so a rotation adds a key before switching.
func (s *Signer) KID() string { return s.kid }

// PublicKeyBase64 is what other services hold in configuration, and what
// /.well-known/jwks.json serves.
func (s *Signer) PublicKeyBase64() string {
	public := s.privateKey.Public().(ed25519.PublicKey)
	return base64.StdEncoding.EncodeToString(public)
}

// Mint signs an access token for one tutor. The claims are exactly the ones
// spec 0001 fixes: sub, tz, language, iat, exp, with kid in the header.
func (s *Signer) Mint(tutorID uuid.UUID, timezone, language string) (string, time.Time, error) {
	now := time.Now().UTC()
	expiresAt := now.Add(s.ttl)
	claims := jwt.MapClaims{
		"sub":      tutorID.String(),
		"tz":       timezone,
		"language": language,
		"iat":      now.Unix(),
		"exp":      expiresAt.Unix(),
	}
	jwtToken := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	jwtToken.Header["kid"] = s.kid
	signed, err := jwtToken.SignedString(s.privateKey)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign token: %w", err)
	}
	return signed, expiresAt, nil
}
