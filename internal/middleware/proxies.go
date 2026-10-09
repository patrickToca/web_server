// Package middleware provides reusable Gin middleware for the mywebapp
// HTTP layer.
//
// This file handles the trusted-proxy configuration: the set of
// addresses Gin will accept as the origin of an X-Forwarded-For
// header. Getting this wrong is a security problem: if the set is too
// broad, an attacker can spoof the header and defeat IP-based rate
// limiting. If it is too narrow, the real client IP is hidden behind
// the proxy and the rate limiter treats all traffic as coming from
// one address.
package middleware

import (
	"fmt"
	"net"
	"os"
	"strings"
)

// LoadTrustedProxies returns the list of trusted proxy CIDRs from the
// TRUSTED_PROXIES environment variable.
//
// The variable is a comma-separated list of IP addresses or CIDR
// blocks:
//
//	TRUSTED_PROXIES=10.0.0.0/8,192.168.0.0/16
//	TRUSTED_PROXIES=fdaa::/16
//	TRUSTED_PROXIES=173.245.48.0/20
//
// The default is an empty list, which means "trust nothing". In that
// configuration, Gin's ClientIP() returns the immediate peer address
// and ignores any X-Forwarded-For header the client sends. That is the
// correct posture for an application exposed directly to the internet
// or sitting behind a load balancer whose range you have not declared.
//
// An empty return is not an error. Gin accepts a nil slice for
// SetTrustedProxies and treats it as "no proxies are trusted".
func LoadTrustedProxies() ([]string, error) {
	raw := strings.TrimSpace(os.Getenv("TRUSTED_PROXIES"))
	if raw == "" {
		return nil, nil
	}

	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for i, p := range parts {
		cidr := strings.TrimSpace(p)
		if cidr == "" {
			continue
		}
		// Accept a bare IP as shorthand for a /32 or /128. net.ParseCIDR
		// rejects bare addresses, so normalise first.
		if !strings.Contains(cidr, "/") {
			ip := net.ParseIP(cidr)
			if ip == nil {
				return nil, fmt.Errorf(
					"TRUSTED_PROXIES entry %d (%q) is not a valid IP or CIDR",
					i+1, cidr)
			}
			if ip.To4() != nil {
				cidr = cidr + "/32"
			} else {
				cidr = cidr + "/128"
			}
		}
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return nil, fmt.Errorf(
				"TRUSTED_PROXIES entry %d (%q) is not a valid CIDR: %w",
				i+1, p, err)
		}
		out = append(out, cidr)
	}
	return out, nil
}

// MustLoadTrustedProxies is LoadTrustedProxies with a panic on parse
// error. It exists for the common case where the caller has already
// decided that an invalid value should abort the boot, and the panic
// is recovered by the top-level main.
//
// Prefer LoadTrustedProxies and explicit error handling in new code.
func MustLoadTrustedProxies() []string {
	proxies, err := LoadTrustedProxies()
	if err != nil {
		panic(err)
	}
	return proxies
}
