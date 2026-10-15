// Package netguard makes the HTTP client for an address somebody else chose.
// Inside a server's network things answer without asking who is calling.
// Adapted from Armature's internal/netguard.
package netguard

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

// ErrBlocked is returned for an address inside the server's own network.
var ErrBlocked = errors.New("that address is not one this server may reach")

const (
	// dialTimeout bounds one connection attempt; the client's own timeout
	// bounds the whole call.
	dialTimeout = 5 * time.Second
	// MaxRedirects is how far a caller is followed. A redirect is checked
	// like any other address, so this only bounds the walk.
	MaxRedirects = 3
)

// Allow is what an operator lets through beyond the public internet: host
// names and CIDRs, as EXCITER_OUTBOUND_ALLOW lists them, comma separated.
type Allow struct {
	hosts    map[string]bool
	prefixes []netip.Prefix
}

// ParseAllow reads the operator's list. Anything unreadable is left out rather
// than silently widening what the server may reach.
func ParseAllow(list string) Allow {
	allow := Allow{hosts: map[string]bool{}}
	for _, item := range strings.Split(list, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if prefix, err := netip.ParsePrefix(item); err == nil {
			allow.prefixes = append(allow.prefixes, prefix.Masked())
			continue
		}
		if addr, err := netip.ParseAddr(item); err == nil {
			allow.prefixes = append(allow.prefixes, netip.PrefixFrom(addr, addr.BitLen()))
			continue
		}
		// A slash meant a range, and this one did not read as one.
		if strings.Contains(item, "/") {
			continue
		}
		allow.hosts[strings.ToLower(item)] = true
	}
	return allow
}

// permits says whether the operator named this address.
func (a Allow) permits(addr netip.Addr) bool {
	for _, prefix := range a.prefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// Names says whether the operator named this host, by its name or, for an
// address, by a range holding it.
func (a Allow) Names(host string) bool {
	host = strings.Trim(strings.ToLower(host), "[]")
	if a.hosts[host] {
		return true
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		return a.permits(addr.Unmap())
	}
	return false
}

// Reserved says whether an address belongs to the network the server is in
// rather than to the internet: loopback, private, link-local, and the rest.
func Reserved(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsValid() || addr.IsLoopback() || addr.IsPrivate() || addr.IsUnspecified() ||
		addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsMulticast() || addr.IsInterfaceLocalMulticast() {
		return true
	}
	// Carrier grade NAT and the two ranges a cloud's metadata sits behind.
	for _, prefix := range []string{"100.64.0.0/10", "169.254.0.0/16", "fd00::/8"} {
		if netip.MustParsePrefix(prefix).Contains(addr) {
			return true
		}
	}
	return false
}

// Transport reaches the internet and nothing else, unless the operator said
// otherwise. It dials the address it checked, so a name cannot swap it after.
func Transport(allow Allow) *http.Transport {
	dialer := &net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second}
	return &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		DialContext:         guardedDial(dialer, allow),
		TLSHandshakeTimeout: dialTimeout,
		MaxIdleConns:        20,
		IdleConnTimeout:     30 * time.Second,
	}
}

// Client is Transport with a timeout for the whole call and at most
// MaxRedirects redirects, each dialled through the same check.
func Client(timeout time.Duration, allow Allow) *http.Client {
	return WithTransport(timeout, Transport(allow))
}

// WithTransport is Client over a round tripper that ends in Transport, such
// as one that first rewrites the address.
func WithTransport(timeout time.Duration, transport http.RoundTripper) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= MaxRedirects {
				return fmt.Errorf("stopped after %d redirects", MaxRedirects)
			}
			return nil
		},
	}
}

func guardedDial(dialer *net.Dialer, allow Allow) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		if allow.hosts[strings.ToLower(host)] {
			return dialer.DialContext(ctx, network, address)
		}
		found, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
		var last error
		for _, addr := range found {
			addr = addr.Unmap()
			if Reserved(addr) && !allow.permits(addr) {
				last = fmt.Errorf("%w: %s", ErrBlocked, addr)
				continue
			}
			conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(addr.String(), port))
			if err == nil {
				return conn, nil
			}
			last = err
		}
		if last == nil {
			last = fmt.Errorf("%w: %s", ErrBlocked, host)
		}
		return nil, last
	}
}

// Rewriting sends a request for a public origin to where this process reaches it, keeping the public Host header,
// as oidc.Backchannel does; next still guards the address it reaches. Without rewrites it is next.
func Rewriting(rewrites map[string]string, next http.RoundTripper) http.RoundTripper {
	if len(rewrites) == 0 {
		return next
	}
	return &rewriting{rewrites: rewrites, next: next}
}

type rewriting struct {
	rewrites map[string]string
	next     http.RoundTripper
}

func (r *rewriting) RoundTrip(req *http.Request) (*http.Response, error) {
	target, ok := r.rewrites[req.URL.Scheme+"://"+req.URL.Host]
	if !ok {
		return r.next.RoundTrip(req)
	}
	reach, err := url.Parse(target)
	if err != nil || reach.Host == "" {
		return r.next.RoundTrip(req)
	}
	clone := req.Clone(req.Context())
	clone.URL.Scheme = reach.Scheme
	clone.URL.Host = reach.Host
	clone.Host = req.URL.Host
	return r.next.RoundTrip(clone)
}
