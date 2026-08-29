package handler

import (
	"errors"
	"fmt"
	urlpkg "net/url"
	"os"
	"strings"
	"time"

	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"
)

const (
	localhost       = "localhost"
	localhostSuffix = ".localhost"
)

var (
	errAppOriginShape       = errors.New("must be one origin with no path, query, fragment, or user info")
	errSecureOrigin         = errors.New("must use https, except for http on localhost")
	errCallbackShape        = errors.New("must be an origin plus the exact /api/auth/google/callback path")
	errCallbackOrigin       = errors.New("must use the app origin scheme and hostname")
	errCallbackExternalPort = errors.New("must use the app origin port outside localhost")
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
	appOrigin, err := validateAppOrigin(cfg.AppURL)
	if err != nil {
		return AuthConfig{}, &vermouth.MissingEnvError{Name: envAppURL, Reason: err.Error()}
	}
	cfg.AppURL = appOrigin.String()
	if cfg.GoogleEnabled || strings.TrimSpace(cfg.GoogleRedirectURL) != "" {
		redirectURL, redirectErr := validateGoogleRedirect(cfg.GoogleRedirectURL, appOrigin)
		if redirectErr != nil {
			return AuthConfig{}, &vermouth.MissingEnvError{
				Name: envGoogleRedirect, Reason: redirectErr.Error(),
			}
		}
		cfg.GoogleRedirectURL = redirectURL.String()
	}

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

// AllowsOrigin accepts only the exact browser origin configured for the app.
// Missing, opaque, credentialed, and path bearing values all fail closed.
func (c AuthConfig) AllowsOrigin(raw string) bool {
	origin, err := urlpkg.Parse(strings.TrimSpace(raw))
	if err != nil || origin.Scheme == "" || origin.Host == "" || origin.User != nil ||
		origin.Opaque != "" || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" {
		return false
	}
	appOrigin, err := urlpkg.Parse(c.AppURL)
	if err != nil {
		return false
	}
	return strings.EqualFold(origin.Scheme, appOrigin.Scheme) &&
		strings.EqualFold(origin.Host, appOrigin.Host)
}

// ResolveRedirect keeps a previously cleaned app path on the configured app
// origin even if a caller later changes how the URL is assembled.
func (c AuthConfig) ResolveRedirect(relative string) string {
	appOrigin, err := urlpkg.Parse(c.AppURL)
	if err != nil {
		return c.AppURL + "/"
	}
	target, err := urlpkg.Parse(cleanRelativeRedirect(relative))
	if err != nil {
		return appOrigin.ResolveReference(&urlpkg.URL{Path: "/"}).String()
	}
	resolved := appOrigin.ResolveReference(target)
	if resolved.Scheme != appOrigin.Scheme || resolved.Host != appOrigin.Host {
		return appOrigin.ResolveReference(&urlpkg.URL{Path: "/"}).String()
	}
	return resolved.String()
}

func validateAppOrigin(raw string) (*urlpkg.URL, error) {
	origin, err := urlpkg.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("must be a valid URL: %w", err)
	}
	if origin.Scheme == "" || origin.Host == "" || origin.User != nil || origin.Opaque != "" ||
		origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" {
		return nil, errAppOriginShape
	}
	if origin.Scheme != "https" && (origin.Scheme != "http" || !isLocalhostName(origin.Hostname())) {
		return nil, errSecureOrigin
	}
	return origin, nil
}

func validateGoogleRedirect(raw string, appOrigin *urlpkg.URL) (*urlpkg.URL, error) {
	redirect, err := urlpkg.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("must be a valid URL: %w", err)
	}
	if redirect.Scheme == "" || redirect.Host == "" || redirect.User != nil || redirect.Opaque != "" ||
		redirect.Path != "/api/auth/google/callback" || redirect.RawQuery != "" || redirect.Fragment != "" {
		return nil, errCallbackShape
	}
	if redirect.Scheme != "https" && (redirect.Scheme != "http" || !isLocalhostName(redirect.Hostname())) {
		return nil, errSecureOrigin
	}
	if redirect.Scheme != appOrigin.Scheme || !strings.EqualFold(redirect.Hostname(), appOrigin.Hostname()) {
		return nil, errCallbackOrigin
	}
	if !isLocalhostName(redirect.Hostname()) && redirect.Port() != appOrigin.Port() {
		return nil, errCallbackExternalPort
	}
	return redirect, nil
}

func isLocalhostName(raw string) bool {
	hostname := strings.ToLower(strings.TrimSpace(raw))
	return hostname == localhost ||
		(strings.HasSuffix(hostname, localhostSuffix) && len(hostname) > len(localhostSuffix))
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
