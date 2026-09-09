//nolint:testpackage // White box tests verify normalized private parser state.
package ratelimit

import (
	"net/netip"
	"testing"

	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"
	"github.com/stretchr/testify/require"
)

// covers: AC-2, AC-9, AC-14
func TestParseConfig_LoadsEveryRequiredValue(t *testing.T) {
	t.Parallel()

	values := validEnvironment()
	values[EnvTrustedProxyCIDRs] = " 10.42.0.7/16, 2001:db8:1::7/64 "
	values[EnvStartIP] = "20/1h,005/1m"

	config, err := ParseConfig(func(name string) string { return values[name] })

	require.NoError(t, err)
	require.Equal(t, BucketPairConfig{Minute: 5, Hour: 20}, config.Start.IP)
	require.Equal(t, []string{"10.42.0.0/16", "2001:db8:1::/64"}, prefixStrings(config.TrustedProxyCIDRs))
	require.Equal(t, "5/1m,20/1h", config.NormalizedThresholds()[EnvStartIP])
}

// covers: AC-9
func TestParseConfig_ReportsEachMissingRequiredValue(t *testing.T) {
	t.Parallel()

	for _, missingName := range append([]string{EnvTrustedProxyCIDRs}, thresholdEnvironmentNames()...) {
		t.Run(missingName, func(t *testing.T) {
			t.Parallel()

			values := validEnvironment()
			delete(values, missingName)

			_, err := ParseConfig(func(name string) string { return values[name] })

			var missing *vermouth.MissingEnvError
			require.ErrorAs(t, err, &missing)
			require.Equal(t, missingName, missing.Name)
		})
	}
}

// covers: AC-2, AC-9, AC-14
func TestParseConfig_RejectsMalformedRatePolicies(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		raw  string
	}{
		{name: "one entry", raw: "5/1m"},
		{name: "extra entry", raw: "5/1m,20/1h,1/1m"},
		{name: "duplicate minute", raw: "5/1m,20/1m"},
		{name: "unsupported period", raw: "5/1m,20/1d"},
		{name: "zero", raw: "0/1m,20/1h"},
		{name: "negative", raw: "-5/1m,20/1h"},
		{name: "fraction", raw: "1.5/1m,20/1h"},
		{name: "overflow", raw: "2147483648/1m,20/1h"},
		{name: "empty entry", raw: "5/1m,"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			values := validEnvironment()
			values[EnvRefreshGlobal] = test.raw

			_, err := ParseConfig(func(name string) string { return values[name] })

			var missing *vermouth.MissingEnvError
			require.ErrorAs(t, err, &missing)
			require.Equal(t, EnvRefreshGlobal, missing.Name)
		})
	}
}

// covers: AC-6, AC-9
func TestParseConfig_RejectsUnsafeTrustedProxySets(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		"NONE",
		"10.42.0.0/16,",
		"not-a-prefix",
		"10.42.0.0/16,10.42.0.0/16",
		"10.42.0.0/16,10.42.1.0/24",
	} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()

			values := validEnvironment()
			values[EnvTrustedProxyCIDRs] = raw

			_, err := ParseConfig(func(name string) string { return values[name] })

			var missing *vermouth.MissingEnvError
			require.ErrorAs(t, err, &missing)
			require.Equal(t, EnvTrustedProxyCIDRs, missing.Name)
		})
	}
}

// covers: AC-9, AC-14
func TestConfigNormalizedThresholds_ReturnsAnIndependentCopy(t *testing.T) {
	t.Parallel()

	values := validEnvironment()
	config, err := ParseConfig(func(name string) string { return values[name] })
	require.NoError(t, err)

	first := config.NormalizedThresholds()
	first[EnvStartIP] = "999/1m,999/1h"
	second := config.NormalizedThresholds()

	require.Equal(t, "5/1m,20/1h", second[EnvStartIP])
}

func validEnvironment() map[string]string {
	return map[string]string{
		EnvTrustedProxyCIDRs: "none",
		EnvStartIP:           "5/1m,20/1h",
		EnvStartGlobal:       "100/1m,500/1h",
		EnvCallbackIP:        "10/1m,60/1h",
		EnvCallbackGlobal:    "200/1m,1000/1h",
		EnvRefreshIP:         "30/1m,120/1h",
		EnvRefreshToken:      "10/1m,120/1h",
		EnvRefreshGlobal:     "300/1m,3000/1h",
	}
}

func prefixStrings(prefixes []netip.Prefix) []string {
	values := make([]string, 0, len(prefixes))
	for _, prefix := range prefixes {
		values = append(values, prefix.String())
	}
	return values
}
