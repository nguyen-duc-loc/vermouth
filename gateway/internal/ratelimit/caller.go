package ratelimit

import (
	"crypto/sha256"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"
)

const (
	maxForwardedAddresses = 32
	invalidRemoteKey      = "invalid_remote"
	clientIPDomain        = "vermouth:gateway:auth-rate-limit:client-ip:v1\x00"
	refreshTokenDomain    = "vermouth:gateway:auth-rate-limit:refresh-token:v1\x00"
	ipv6CallerPrefixBits  = 64
)

// Key is a domain separated digest for one caller policy. The raw caller
// address and refresh token never enter registry state.
type Key struct {
	Scope  Scope
	Digest [sha256.Size]byte
}

// ClientIPKey derives the caller key from the direct peer and, only for a
// trusted peer, a strictly parsed forwarding chain.
func ClientIPKey(request *http.Request, trusted []netip.Prefix) Key {
	address, valid := parseRemoteAddress(request.RemoteAddr)
	material := []byte(invalidRemoteKey)
	if valid {
		address = address.Unmap()
		if addressTrusted(address, trusted) {
			if forwarded, ok := parseForwardedAddresses(request.Header.Values("X-Forwarded-For")); ok {
				address = selectForwardedAddress(forwarded, trusted)
			}
		}
		material = normalizeAddress(address).AsSlice()
	}
	return Key{Scope: ScopeIP, Digest: digest(clientIPDomain, material)}
}

// RefreshTokenKey hashes one validated nonempty refresh cookie value.
func RefreshTokenKey(value string) Key {
	return Key{Scope: ScopeRefreshToken, Digest: digest(refreshTokenDomain, []byte(value))}
}

func digest(domain string, material []byte) [sha256.Size]byte {
	hash := sha256.New()
	_, _ = hash.Write([]byte(domain))
	_, _ = hash.Write(material)
	var result [sha256.Size]byte
	copy(result[:], hash.Sum(nil))
	return result
}

func parseRemoteAddress(raw string) (netip.Addr, bool) {
	host, _, splitErr := net.SplitHostPort(raw)
	if splitErr == nil {
		address, parseErr := netip.ParseAddr(host)
		return address, parseErr == nil
	}
	address, err := netip.ParseAddr(raw)
	return address, err == nil
}

func parseForwardedAddresses(lines []string) ([]netip.Addr, bool) {
	if len(lines) == 0 {
		return nil, false
	}
	addresses := make([]netip.Addr, 0, len(lines))
	for _, line := range lines {
		for value := range strings.SplitSeq(line, ",") {
			text := strings.TrimSpace(value)
			if text == "" || len(addresses) == maxForwardedAddresses {
				return nil, false
			}
			address, err := netip.ParseAddr(text)
			if err != nil {
				return nil, false
			}
			addresses = append(addresses, address.Unmap())
		}
	}
	return addresses, len(addresses) > 0
}

func selectForwardedAddress(addresses []netip.Addr, trusted []netip.Prefix) netip.Addr {
	for _, address := range slices.Backward(addresses) {
		if !addressTrusted(address, trusted) {
			return address
		}
	}
	return addresses[0]
}

func normalizeAddress(address netip.Addr) netip.Addr {
	address = address.Unmap()
	if address.Is6() {
		return netip.PrefixFrom(address, ipv6CallerPrefixBits).Masked().Addr()
	}
	return address
}

func addressTrusted(address netip.Addr, trusted []netip.Prefix) bool {
	for _, prefix := range trusted {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}
