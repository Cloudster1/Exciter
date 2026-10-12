package oidc

import (
	"context"
	"net/http"
	"net/url"
	"time"

	coreoidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// discoveryTimeout bounds every call to a provider; a sign-in waits on it.
const discoveryTimeout = 10 * time.Second

// Backchannel is an HTTP client for a provider whose public address is not the
// one this process reaches it at; rewrites maps the first to the second.
func Backchannel(rewrites map[string]string) *http.Client {
	// In a compose stack the issuer is localhost:<port>, which inside the api container is the container itself;
	// the request goes to the reachable address with the public Host, so the provider keeps naming its issuer.
	if len(rewrites) == 0 {
		return &http.Client{Timeout: discoveryTimeout}
	}
	return &http.Client{Timeout: discoveryTimeout, Transport: &rewriting{rewrites: rewrites, next: http.DefaultTransport}}
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
	// The provider answers for the name it was asked for.
	clone.Host = req.URL.Host
	return r.next.RoundTrip(clone)
}

// WithHTTPClient sets the client the provider is reached with.
func (s *Service) WithHTTPClient(c *http.Client) *Service {
	s.http = c
	return s
}

// providerContext puts the client on the context for both libraries, which
// each look for it under their own key.
func (s *Service) providerContext(ctx context.Context) context.Context {
	if s.http == nil {
		return ctx
	}
	ctx = coreoidc.ClientContext(ctx, s.http)
	return context.WithValue(ctx, oauth2.HTTPClient, s.http)
}
