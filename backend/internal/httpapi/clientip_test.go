package httpapi

import (
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestTheClientAddressIsTheProxysWordOnlyBehindATrustedProxy(t *testing.T) {
	behindNginx := &Server{TrustedProxies: []netip.Prefix{netip.MustParsePrefix("172.16.0.0/12"), netip.MustParsePrefix("fd00::/8")}}
	for _, tc := range []struct {
		name     string
		server   *Server
		peer     string
		realIP   string
		expected string
	}{
		{"nobody is trusted by default", &Server{}, "172.18.0.5:41000", "203.0.113.9", "172.18.0.5"},
		{"the proxy names the client", behindNginx, "172.18.0.5:41000", "203.0.113.9", "203.0.113.9"},
		{"a client outside the ranges cannot name another", behindNginx, "198.51.100.7:52000", "203.0.113.9", "198.51.100.7"},
		{"a client outside the ranges cannot claim to be inside", behindNginx, "198.51.100.7:52000", "172.18.0.5", "198.51.100.7"},
		{"a proxy that names nobody leaves its own address", behindNginx, "172.18.0.5:41000", "", "172.18.0.5"},
		{"a proxy that names nonsense leaves its own address", behindNginx, "172.18.0.5:41000", "somebody, 10.0.0.1", "172.18.0.5"},
		{"an IPv6 proxy names an IPv6 client", behindNginx, "[fd00::5]:41000", "2001:db8::7", "2001:db8::7"},
		{"a mapped IPv4 proxy is the IPv4 proxy", behindNginx, "[::ffff:172.18.0.5]:41000", "203.0.113.9", "203.0.113.9"},
		{"a mapped client address is written as IPv4", behindNginx, "172.18.0.5:41000", "::ffff:203.0.113.9", "203.0.113.9"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/api/v1/auth/me", nil)
			r.RemoteAddr = tc.peer
			if tc.realIP != "" {
				r.Header.Set(realIPHeader, tc.realIP)
			}
			if got := tc.server.clientIP(r); got != tc.expected {
				t.Fatalf("clientIP = %q, want %q", got, tc.expected)
			}
		})
	}
}

func TestAPeerWithoutAPortHasNoAddress(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "not an address"
	if got := (&Server{}).clientIP(r); got != "" {
		t.Fatalf("clientIP = %q, want nothing", got)
	}
}
