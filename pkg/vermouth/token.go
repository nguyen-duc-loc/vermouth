package vermouth

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// Claims are the claims identity signs (spec 0001, identity propagation). The
// language claim is named for the event field, not for the OIDC convention,
// so one name means one value everywhere.
type Claims struct {
	TutorID  uuid.UUID
	Timezone string
	Language string
}

// Verifier checks a token locally against a public key it already holds,
// keyed by kid (STK-14, INV-14). It performs no network call, so verification
// keeps working with identity down.
type Verifier struct {
	keys   map[string]ed25519.PublicKey
	parser *jwt.Parser
}

// NewVerifier fails when it holds no key, because a service that cannot verify
// would otherwise start and refuse every request at the first call instead.
func NewVerifier(keys map[string]ed25519.PublicKey) (*Verifier, error) {
	if len(keys) == 0 {
		return nil, &MissingEnvError{Name: envTokenPublicKeys, Reason: "at least one kid:base64 key is required to verify tokens"}
	}
	return &Verifier{
		keys:   keys,
		parser: jwt.NewParser(jwt.WithValidMethods([]string{"EdDSA"}), jwt.WithExpirationRequired()),
	}, nil
}

// ErrNoToken separates "you sent nothing" from "you sent something wrong", so
// the caller can answer 401 with a useful code.
var ErrNoToken = errors.New("no bearer token")

// The reasons a token is refused, static so a caller can match on the reason
// and a log line never carries a one off string.
var (
	errTokenNoKID     = errors.New("token has no kid")
	errTokenUnknownID = errors.New("no public key held for this kid")
	errTokenNotValid  = errors.New("token is not valid")
	errTokenNoSubject = errors.New("token has no sub claim")
)

// Verify returns the claims of a valid token, and an error for anything else.
func (v *Verifier) Verify(raw string) (Claims, error) {
	if strings.TrimSpace(raw) == "" {
		return Claims{}, ErrNoToken
	}
	extra := jwt.MapClaims{}
	token, err := v.parser.ParseWithClaims(raw, extra, func(token *jwt.Token) (any, error) {
		kid, _ := token.Header["kid"].(string)
		if kid == "" {
			return nil, errTokenNoKID
		}
		key, ok := v.keys[kid]
		if !ok {
			return nil, fmt.Errorf("%w: %s", errTokenUnknownID, kid)
		}
		return key, nil
	})
	if err != nil {
		return Claims{}, fmt.Errorf("verify token: %w", err)
	}
	if !token.Valid {
		return Claims{}, fmt.Errorf("verify token: %w", errTokenNotValid)
	}
	subject, err := extra.GetSubject()
	if err != nil || subject == "" {
		return Claims{}, fmt.Errorf("verify token: %w", errTokenNoSubject)
	}
	tutorID, err := uuid.Parse(subject)
	if err != nil {
		return Claims{}, fmt.Errorf("verify token: sub is not a tutor id: %w", err)
	}
	timezone, _ := extra["tz"].(string)
	language, _ := extra["language"].(string)
	return Claims{TutorID: tutorID, Timezone: timezone, Language: language}, nil
}

// BearerToken pulls the raw token out of an Authorization header.
func BearerToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	if len(header) > 7 && strings.EqualFold(header[:7], "bearer ") {
		return strings.TrimSpace(header[7:])
	}
	return ""
}
