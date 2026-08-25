package handler

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"
)

// The environment identity's sign in reads. These are identity's own variables
// rather than the shared module's, so they are parsed here the way
// token.NewSignerFromEnv already parses the signing key (STK-8): a missing
// required one stops startup instead of defaulting silently.
const (
	envGoogleEnabled   = "VERMOUTH_GOOGLE_AUTH_ENABLED"
	envGoogleClientID  = "IDENTITY_GOOGLE_CLIENT_ID"
	envGoogleSecret    = "IDENTITY_GOOGLE_CLIENT_SECRET"
	envGoogleRedirect  = "IDENTITY_GOOGLE_REDIRECT_URL"
	envAppURL          = "IDENTITY_APP_URL"
	envSignupAllowlist = "IDENTITY_SIGNUP_ALLOWLIST"
	envRefreshTTL      = "IDENTITY_REFRESH_TTL"
	envRefreshGrace    = "IDENTITY_REFRESH_GRACE"
	envSweepInterval   = "IDENTITY_SWEEP_INTERVAL"
	envCookieSecure    = "IDENTITY_COOKIE_SECURE"
)

// The defaults, named so one place answers how long a session lives and how
// often the sweep runs. The sweep interval is its own value rather than the
// relay's 750ms, because these rows live ten minutes and thirty days.
const (
	defaultRefreshTTL    = 30 * 24 * time.Hour
	defaultRefreshGrace  = 10 * time.Second
	defaultSweepInterval = 10 * time.Minute
)

// AuthConfig is what Google sign in needs from the environment. The client
// secret is in here and nowhere else in the system: the gateway and the browser
// never hold it, which is what makes identity the confidential client (spec
// 0004).
type AuthConfig struct {
	GoogleEnabled      bool
	GoogleClientID     string
	GoogleClientSecret string
	GoogleRedirectURL  string
	// AppURL is where the callback sends the browser back to. Every redirect is
	// built from this rather than from anything the caller sent, which is what
	// closes the open redirect.
	AppURL string
	// SignupAllowlist holds the trimmed, lowercased emails allowed to create a
	// tutor. Empty means nobody new, so an unset variable fails closed.
	SignupAllowlist map[string]bool
	RefreshTTL      time.Duration
	// RefreshGrace is how long after a rotation the old token still rotates
	// instead of ending the family, which is what keeps two tabs booting
	// together from looking like theft. Zero turns the grace off.
	RefreshGrace  time.Duration
	SweepInterval time.Duration
	// CookieSecure is true everywhere except an origin a browser does not treat
	// as trustworthy. localhost already counts as one.
	CookieSecure bool
}

// AuthConfigFromEnv reads the local auth switch and the values its selected
// mode needs. It is called once at startup, and a missing or malformed required
// value stops it there (STK-8).
func AuthConfigFromEnv() (AuthConfig, error) {
	cfg := AuthConfig{
		SignupAllowlist: make(map[string]bool),
		RefreshTTL:      defaultRefreshTTL,
		RefreshGrace:    defaultRefreshGrace,
		SweepInterval:   defaultSweepInterval,
		CookieSecure:    true,
	}
	var err error
	cfg.GoogleEnabled, err = envBool(envGoogleEnabled, true)
	if err != nil {
		return AuthConfig{}, err
	}
	required := map[string]*string{
		envAppURL: &cfg.AppURL,
	}
	if cfg.GoogleEnabled {
		required[envGoogleClientID] = &cfg.GoogleClientID
		required[envGoogleSecret] = &cfg.GoogleClientSecret
		required[envGoogleRedirect] = &cfg.GoogleRedirectURL
	}
	for name, target := range required {
		value := strings.TrimSpace(os.Getenv(name))
		if value == "" {
			return AuthConfig{}, &vermouth.MissingEnvError{Name: name, Reason: ""}
		}
		*target = value
	}
	cfg.AppURL = strings.TrimSuffix(cfg.AppURL, "/")

	allowlist, err := parseAllowlist(os.Getenv(envSignupAllowlist))
	if err != nil {
		return AuthConfig{}, err
	}
	cfg.SignupAllowlist = allowlist

	err = readAuthDurations(&cfg)
	if err != nil {
		return AuthConfig{}, err
	}
	cfg.CookieSecure, err = envBool(envCookieSecure, true)
	if err != nil {
		return AuthConfig{}, err
	}
	return cfg, nil
}

// parseAllowlist puts both sides of the comparison into one normal form. An
// entry with no @ in it stops startup rather than sitting in the list matching
// nothing, because a typo in a gate should be loud.
func parseAllowlist(raw string) (map[string]bool, error) {
	allowlist := make(map[string]bool)
	for entry := range strings.SplitSeq(raw, ",") {
		address := strings.ToLower(strings.TrimSpace(entry))
		if address == "" {
			continue
		}
		if !strings.Contains(address, "@") {
			return nil, &vermouth.MissingEnvError{
				Name:   envSignupAllowlist,
				Reason: "entry " + address + " is not an email address",
			}
		}
		allowlist[address] = true
	}
	return allowlist, nil
}

// readAuthDurations fills the three optional durations in place, so the caller
// reads as the one list of variables it is.
func readAuthDurations(cfg *AuthConfig) error {
	durations := map[string]*time.Duration{
		envRefreshTTL:    &cfg.RefreshTTL,
		envRefreshGrace:  &cfg.RefreshGrace,
		envSweepInterval: &cfg.SweepInterval,
	}
	for name, target := range durations {
		value, err := envDuration(name, *target)
		if err != nil {
			return err
		}
		*target = value
	}
	return nil
}

// envDuration reads an optional duration, keeping the fallback when the variable
// says nothing and refusing what it cannot parse.
func envDuration(name string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, &vermouth.MissingEnvError{
			Name:   name,
			Reason: fmt.Sprintf("not a duration such as 10s or 720h: %v", err),
		}
	}
	return value, nil
}

// envBool reads an optional true or false.
func envBool(name string, fallback bool) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "":
		return fallback, nil
	case "true", "1", "yes":
		return true, nil
	case "false", "0", "no":
		return false, nil
	default:
		return false, &vermouth.MissingEnvError{Name: name, Reason: "must be true or false"}
	}
}
