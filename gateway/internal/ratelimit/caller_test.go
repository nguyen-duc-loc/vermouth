//nolint:testpackage // White box tests compare only digests, never raw caller state.
package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// covers: AC-6
func TestClientIPKey_UsesOnlyTrustedForwardingChains(t *testing.T) {
	t.Parallel()

	trusted := []netip.Prefix{
		netip.MustParsePrefix("10.42.0.0/16"),
		netip.MustParsePrefix("192.0.2.0/24"),
	}
	tests := []struct {
		name       string
		remoteAddr string
		headers    []string
		want       string
	}{
		{
			name:       "direct peer",
			remoteAddr: "198.51.100.8:42000",
			want:       "198.51.100.8",
		},
		{
			name:       "untrusted peer ignores forwarding header",
			remoteAddr: "198.51.100.8:42000",
			headers:    []string{"203.0.113.9"},
			want:       "198.51.100.8",
		},
		{
			name:       "trusted peer selects rightmost untrusted address",
			remoteAddr: "10.42.0.5:42000",
			headers:    []string{"203.0.113.9, 192.0.2.4", "10.42.0.6"},
			want:       "203.0.113.9",
		},
		{
			name:       "all trusted selects leftmost address",
			remoteAddr: "10.42.0.5:42000",
			headers:    []string{"192.0.2.9, 10.42.0.6"},
			want:       "192.0.2.9",
		},
		{
			name:       "malformed chain falls back to peer",
			remoteAddr: "10.42.0.5:42000",
			headers:    []string{"203.0.113.9,,10.42.0.6"},
			want:       "10.42.0.5",
		},
		{
			name:       "mapped IPv4 becomes IPv4",
			remoteAddr: "[::ffff:198.51.100.8]:42000",
			want:       "198.51.100.8",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			request := httptest.NewRequestWithContext(
				t.Context(), http.MethodGet, "https://gateway.example/", http.NoBody,
			)
			request.RemoteAddr = test.remoteAddr
			for _, value := range test.headers {
				request.Header.Add("X-Forwarded-For", value)
			}

			require.Equal(t, expectedClientKey(test.want), ClientIPKey(request, trusted))
		})
	}
}

// covers: AC-6, AC-8
func TestClientIPKey_GroupsIPv6ByPrefixAndBoundsForwardingInput(t *testing.T) {
	t.Parallel()

	first := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "https://gateway.example/", http.NoBody)
	first.RemoteAddr = "[2001:db8:abcd:12::1]:1234"
	second := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "https://gateway.example/", http.NoBody)
	second.RemoteAddr = "[2001:db8:abcd:12:ffff::2]:4321"
	require.Equal(t, ClientIPKey(first, nil), ClientIPKey(second, nil))
	third := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "https://gateway.example/", http.NoBody)
	third.RemoteAddr = "[2001:db8:abcd:13::1]:4321"
	require.NotEqual(t, ClientIPKey(first, nil), ClientIPKey(third, nil))

	trusted := []netip.Prefix{netip.MustParsePrefix("10.42.0.0/16")}
	excessive := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "https://gateway.example/", http.NoBody)
	excessive.RemoteAddr = "10.42.0.5:42000"
	excessive.Header.Set("X-Forwarded-For", strings.Repeat("203.0.113.9,", 32)+"203.0.113.10")
	require.Equal(t, expectedClientKey("10.42.0.5"), ClientIPKey(excessive, trusted))

	invalidOne := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "https://gateway.example/", http.NoBody)
	invalidOne.RemoteAddr = "not-an-address"
	invalidTwo := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "https://gateway.example/", http.NoBody)
	invalidTwo.RemoteAddr = "also-invalid"
	require.Equal(t, ClientIPKey(invalidOne, trusted), ClientIPKey(invalidTwo, trusted))
}

// covers: AC-6
func TestClientIPKey_ChecksIPv6ProxyTrustBeforeGroupingCallers(t *testing.T) {
	t.Parallel()

	trusted := []netip.Prefix{netip.MustParsePrefix("2001:db8:abcd:12::5/128")}
	trustedRequest := httptest.NewRequestWithContext(
		t.Context(), http.MethodGet, "https://gateway.example/", http.NoBody,
	)
	trustedRequest.RemoteAddr = "[2001:db8:abcd:12::5]:42000"
	trustedRequest.Header.Set("X-Forwarded-For", "2001:db8:ffff:34::8")
	require.Equal(
		t,
		expectedClientKey("2001:db8:ffff:34::"),
		ClientIPKey(trustedRequest, trusted),
	)

	untrustedNeighbor := httptest.NewRequestWithContext(
		t.Context(), http.MethodGet, "https://gateway.example/", http.NoBody,
	)
	untrustedNeighbor.RemoteAddr = "[2001:db8:abcd:12::6]:42000"
	untrustedNeighbor.Header.Set("X-Forwarded-For", "2001:db8:ffff:34::8")
	require.Equal(
		t,
		expectedClientKey("2001:db8:abcd:12::"),
		ClientIPKey(untrustedNeighbor, trusted),
	)
}

// covers: AC-6, AC-7
func TestRefreshTokenKey_UsesASeparateDigestDomain(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "https://gateway.example/", http.NoBody)
	request.RemoteAddr = "198.51.100.8:42000"

	require.NotEqual(t, ClientIPKey(request, nil).Digest, RefreshTokenKey("198.51.100.8").Digest)
}

func expectedClientKey(address string) Key {
	parsed := netip.MustParseAddr(address)
	return Key{Scope: ScopeIP, Digest: digest(clientIPDomain, normalizeAddress(parsed).AsSlice())}
}
