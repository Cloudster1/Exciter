package httpapi

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// realIPHeader is where nginx, as deploy/nginx.conf sets it up, names the address it was reached from.
const realIPHeader = "X-Real-IP"

// clientIP is the caller's address, recorded on a session and in the audit log. Behind a trusted proxy that is
// the address the proxy names; anybody else could name any address, so their own peer address is taken.
func (s *Server) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return ""
	}
	peer, err := netip.ParseAddr(host)
	if err != nil || !s.trustsProxy(peer) {
		return host
	}
	named, err := netip.ParseAddr(strings.TrimSpace(r.Header.Get(realIPHeader)))
	if err != nil {
		return host
	}
	return named.Unmap().String()
}

// trustsProxy reports whether a peer is one of the proxies whose word on the client's address is taken.
func (s *Server) trustsProxy(peer netip.Addr) bool {
	peer = peer.Unmap()
	for _, prefix := range s.TrustedProxies {
		if prefix.Contains(peer) {
			return true
		}
	}
	return false
}
