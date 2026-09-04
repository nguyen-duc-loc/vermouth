package ratelimit

import (
	"maps"
	"net/netip"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"
)

// Environment names are exported so deployment evidence hashes the exact
// values the gateway parser accepts instead of maintaining a second list.
const (
	EnvTrustedProxyCIDRs = "GATEWAY_TRUSTED_PROXY_CIDRS"
	EnvStartIP           = "GATEWAY_AUTH_RATE_START_IP"
	EnvStartGlobal       = "GATEWAY_AUTH_RATE_START_GLOBAL"
	EnvCallbackIP        = "GATEWAY_AUTH_RATE_CALLBACK_IP"
	EnvCallbackGlobal    = "GATEWAY_AUTH_RATE_CALLBACK_GLOBAL"
	EnvRefreshIP         = "GATEWAY_AUTH_RATE_REFRESH_IP"
	EnvRefreshToken      = "GATEWAY_AUTH_RATE_REFRESH_TOKEN" //nolint:gosec // This is a configuration name, not a token.
	EnvRefreshGlobal     = "GATEWAY_AUTH_RATE_REFRESH_GLOBAL"
	trustedProxyNone     = "none"
	policyEntryCount     = 2
	minutePeriod         = "1m"
	hourPeriod           = "1h"
)

// Endpoint identifies one fixed auth route. Callers never supply this value.
type Endpoint string

// These are the only routes with rate policy in spec 0007.
const (
	EndpointStart    Endpoint = "start"
	EndpointCallback Endpoint = "callback"
	EndpointRefresh  Endpoint = "refresh"
)

// Scope identifies one bounded and safe log dimension.
type Scope string

// The scope values are fixed so caller identity never becomes a log field.
const (
	ScopeIP           Scope = "ip"
	ScopeRefreshToken Scope = "refresh_token"
	ScopeGlobal       Scope = "global"
)

// BucketPairConfig carries the minute and hour capacities for one scope.
type BucketPairConfig struct {
	Minute int
	Hour   int
}

// EndpointPolicy contains every bucket pair an endpoint uses.
type EndpointPolicy struct {
	IP           BucketPairConfig
	RefreshToken BucketPairConfig
	Global       BucketPairConfig
}

// Config is parsed once at startup. No required rate has a default.
type Config struct {
	TrustedProxyCIDRs []netip.Prefix
	Start             EndpointPolicy
	Callback          EndpointPolicy
	Refresh           EndpointPolicy
	normalized        map[string]string
}

// ConfigFromEnv parses every gateway rate and proxy value as one startup unit.
func ConfigFromEnv() (Config, error) {
	return ParseConfig(os.Getenv)
}

// ParseConfig parses configuration through lookup so startup and evidence use
// the same rules while tests remain independent of process environment state.
func ParseConfig(lookup func(string) string) (Config, error) {
	trusted, err := parseTrustedProxyCIDRs(lookup(EnvTrustedProxyCIDRs))
	if err != nil {
		return Config{}, err
	}

	names := thresholdEnvironmentNames()
	parsed := make(map[string]BucketPairConfig, len(names))
	normalized := make(map[string]string, len(names))
	for _, name := range names {
		pair, value, parseErr := parseBucketPair(name, lookup(name))
		if parseErr != nil {
			return Config{}, parseErr
		}
		parsed[name] = pair
		normalized[name] = value
	}

	return Config{
		TrustedProxyCIDRs: trusted,
		Start: EndpointPolicy{
			IP:     parsed[EnvStartIP],
			Global: parsed[EnvStartGlobal],
		},
		Callback: EndpointPolicy{
			IP:     parsed[EnvCallbackIP],
			Global: parsed[EnvCallbackGlobal],
		},
		Refresh: EndpointPolicy{
			IP:           parsed[EnvRefreshIP],
			RefreshToken: parsed[EnvRefreshToken],
			Global:       parsed[EnvRefreshGlobal],
		},
		normalized: normalized,
	}, nil
}

// NormalizedThresholds returns a copy of the seven values with minute first
// and hour second. Evidence can sort these without mutating live config.
func (c Config) NormalizedThresholds() map[string]string {
	values := make(map[string]string, len(c.normalized))
	maps.Copy(values, c.normalized)
	return values
}

func (c Config) policy(endpoint Endpoint) EndpointPolicy {
	switch endpoint {
	case EndpointStart:
		return c.Start
	case EndpointCallback:
		return c.Callback
	case EndpointRefresh:
		return c.Refresh
	default:
		return EndpointPolicy{}
	}
}

func thresholdEnvironmentNames() []string {
	return []string{
		EnvCallbackGlobal,
		EnvCallbackIP,
		EnvRefreshGlobal,
		EnvRefreshIP,
		EnvRefreshToken,
		EnvStartGlobal,
		EnvStartIP,
	}
}

func parseBucketPair(name, raw string) (BucketPairConfig, string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return BucketPairConfig{}, "", &vermouth.MissingEnvError{Name: name}
	}
	entries := strings.Split(value, ",")
	if len(entries) != policyEntryCount {
		return invalidBucketPair(name, "must contain one minute rate and one hour rate")
	}

	counts := make(map[string]int, policyEntryCount)
	for _, entry := range entries {
		period, count, parseErr := parseBucketEntry(name, entry)
		if parseErr != nil {
			return BucketPairConfig{}, "", parseErr
		}
		if _, exists := counts[period]; exists {
			return invalidBucketPair(name, "periods must not be duplicated")
		}
		counts[period] = count
	}
	if len(counts) != policyEntryCount {
		return invalidBucketPair(name, "must contain one minute rate and one hour rate")
	}
	pair := BucketPairConfig{Minute: counts[minutePeriod], Hour: counts[hourPeriod]}
	return pair, strconv.Itoa(pair.Minute) + "/1m," + strconv.Itoa(pair.Hour) + "/1h", nil
}

func parseBucketEntry(name, entry string) (string, int, error) {
	countText, period, found := strings.Cut(strings.TrimSpace(entry), "/")
	if !found || countText == "" || period == "" || strings.Contains(period, "/") {
		_, _, err := invalidBucketPair(name, "each rate must be count/1m or count/1h")
		return "", 0, err
	}
	for _, character := range countText {
		if character < '0' || character > '9' {
			_, _, err := invalidBucketPair(name, "counts must be positive integers")
			return "", 0, err
		}
	}
	count, err := strconv.ParseInt(countText, 10, 32)
	if err != nil || count <= 0 {
		_, _, invalidErr := invalidBucketPair(name, "counts must be positive integers")
		return "", 0, invalidErr
	}
	if period != minutePeriod && period != hourPeriod {
		_, _, invalidErr := invalidBucketPair(name, "periods must be exactly 1m and 1h")
		return "", 0, invalidErr
	}
	return period, int(count), nil
}

func invalidBucketPair(name, reason string) (BucketPairConfig, string, error) {
	return BucketPairConfig{}, "", &vermouth.MissingEnvError{Name: name, Reason: reason}
}

func parseTrustedProxyCIDRs(raw string) ([]netip.Prefix, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil, &vermouth.MissingEnvError{Name: EnvTrustedProxyCIDRs}
	}
	if value == trustedProxyNone {
		return nil, nil
	}

	entries := strings.Split(value, ",")
	prefixes := make([]netip.Prefix, 0, len(entries))
	for _, entry := range entries {
		text := strings.TrimSpace(entry)
		prefix, err := netip.ParsePrefix(text)
		if text == "" || err != nil {
			return nil, &vermouth.MissingEnvError{
				Name: EnvTrustedProxyCIDRs, Reason: "must be none or a comma separated CIDR list",
			}
		}
		address := prefix.Addr()
		bits := prefix.Bits()
		if address.Is4In6() {
			address = address.Unmap()
			bits -= 96
		}
		if bits < 0 || bits > address.BitLen() {
			return nil, &vermouth.MissingEnvError{
				Name: EnvTrustedProxyCIDRs, Reason: "contains an invalid mapped IPv4 prefix",
			}
		}
		prefix = netip.PrefixFrom(address, bits).Masked()
		for _, existing := range prefixes {
			if prefixesOverlap(existing, prefix) {
				return nil, &vermouth.MissingEnvError{
					Name: EnvTrustedProxyCIDRs, Reason: "CIDRs must not duplicate or overlap",
				}
			}
		}
		prefixes = append(prefixes, prefix)
	}
	slices.SortFunc(prefixes, func(left, right netip.Prefix) int {
		return strings.Compare(left.String(), right.String())
	})
	return prefixes, nil
}

func prefixesOverlap(left, right netip.Prefix) bool {
	return left.Contains(right.Addr()) || right.Contains(left.Addr())
}
