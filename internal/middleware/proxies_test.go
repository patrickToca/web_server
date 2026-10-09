package middleware

import (
	"strings"
	"testing"
)

func TestLoadTrustedProxies_EmptyWhenUnset(t *testing.T) {
	t.Setenv("TRUSTED_PROXIES", "")

	got, err := LoadTrustedProxies()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for unset variable, got %v", got)
	}
}

func TestLoadTrustedProxies_EmptyWhenWhitespace(t *testing.T) {
	t.Setenv("TRUSTED_PROXIES", "   ")

	got, err := LoadTrustedProxies()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for whitespace, got %v", got)
	}
}

func TestLoadTrustedProxies_SingleCIDR(t *testing.T) {
	t.Setenv("TRUSTED_PROXIES", "10.0.0.0/8")

	got, err := LoadTrustedProxies()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0] != "10.0.0.0/8" {
		t.Fatalf("expected [10.0.0.0/8], got %v", got)
	}
}

func TestLoadTrustedProxies_MultipleCIDRs(t *testing.T) {
	t.Setenv("TRUSTED_PROXIES", "10.0.0.0/8, 192.168.0.0/16, 172.16.0.0/12")

	got, err := LoadTrustedProxies()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"10.0.0.0/8", "192.168.0.0/16", "172.16.0.0/12"}
	if len(got) != len(want) {
		t.Fatalf("expected %d entries, got %d: %v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestLoadTrustedProxies_BareIPv4BecomesSlash32(t *testing.T) {
	t.Setenv("TRUSTED_PROXIES", "10.0.0.1")

	got, err := LoadTrustedProxies()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0] != "10.0.0.1/32" {
		t.Fatalf("expected [10.0.0.1/32], got %v", got)
	}
}

func TestLoadTrustedProxies_BareIPv6BecomesSlash128(t *testing.T) {
	t.Setenv("TRUSTED_PROXIES", "2001:db8::1")

	got, err := LoadTrustedProxies()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0] != "2001:db8::1/128" {
		t.Fatalf("expected [2001:db8::1/128], got %v", got)
	}
}

func TestLoadTrustedProxies_FlyIPv6CIDR(t *testing.T) {
	// The value used in production on Fly.
	t.Setenv("TRUSTED_PROXIES", "fdaa::/16")

	got, err := LoadTrustedProxies()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0] != "fdaa::/16" {
		t.Fatalf("expected [fdaa::/16], got %v", got)
	}
}

func TestLoadTrustedProxies_RejectsGarbage(t *testing.T) {
	t.Setenv("TRUSTED_PROXIES", "not-an-ip")

	_, err := LoadTrustedProxies()
	if err == nil {
		t.Fatal("expected error for garbage input, got nil")
	}
	if !strings.Contains(err.Error(), "not-an-ip") {
		t.Errorf("error should name the offending entry: %v", err)
	}
}

func TestLoadTrustedProxies_RejectsInvalidCIDR(t *testing.T) {
	t.Setenv("TRUSTED_PROXIES", "10.0.0.0/33")

	_, err := LoadTrustedProxies()
	if err == nil {
		t.Fatal("expected error for /33, got nil")
	}
	if !strings.Contains(err.Error(), "10.0.0.0/33") {
		t.Errorf("error should name the offending entry: %v", err)
	}
}

func TestLoadTrustedProxies_IgnoresEmptyEntries(t *testing.T) {
	t.Setenv("TRUSTED_PROXIES", "10.0.0.0/8,,192.168.0.0/16,")

	got, err := LoadTrustedProxies()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 entries, got %d: %v", len(got), got)
	}
}
