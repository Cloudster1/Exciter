// Package tokenexchange is Exciter as a partner app of Armature and Stator (#274): it trades a person's access token
// from the shared identity provider for that person's tokens at the partner (RFC 8693), and refreshes them, as
// Armature's docs/oauth.md has partners do.
package tokenexchange

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

const (
	// Capability is what a partner lists on GET /meta when it serves the exchange, compared by its exact name.
	Capability = "oauth.token-exchange@1"
	// GrantType and TokenTypeAccess are RFC 8693's names for the exchange and the token it takes and gives.
	GrantType       = "urn:ietf:params:oauth:grant-type:token-exchange"
	GrantRefresh    = "refresh_token"
	TokenTypeAccess = "urn:ietf:params:oauth:token-type:access_token"
	// TokenPath is the partner's token endpoint under its base URL.
	TokenPath = "/api/v1/oauth/token"
	// SignInTimeout bounds the exchange a sign-in waits for: a partner that does not answer costs the person at
	// most this, and they are signed in all the same.
	SignInTimeout = 3 * time.Second
	// NoteTimeout bounds the write of what a sign-in's exchange came to, made after SignInTimeout may have run out.
	NoteTimeout = 2 * time.Second
	// RefreshMargin is how long before its access token runs out a stored grant is refreshed, so a call that
	// starts just before the hour does not meet a 401 halfway.
	RefreshMargin = time.Minute
	// maxAnswerBytes bounds the token endpoint's answer, a handful of short fields.
	maxAnswerBytes = 64 << 10
)

// ErrUnreachable is a partner that did not answer, answered 5xx or 429, or answered something unreadable.
var ErrUnreachable = errors.New("the partner's token endpoint could not be reached")

// Refusal is the partner's OAuth error (RFC 6749 section 5.2), with its own sentence.
type Refusal struct {
	Status      int
	Code        string
	Description string
}

func (r *Refusal) Error() string {
	return fmt.Sprintf("the token endpoint refused with %s: %s", r.Code, r.Description)
}

// Ended says the grant is over: the partner disconnected the person, and only a new exchange connects them again.
func (r *Refusal) Ended() bool { return r.Code == "invalid_grant" }

// Tokens are a person's tokens at the partner.
type Tokens struct {
	Access, Refresh string
	// ExpiresAt is when the access token runs out, by Exciter's clock.
	ExpiresAt time.Time
	Scopes    []string
}

// Has says whether the tokens were given a scope.
func (t *Tokens) Has(scope string) bool { return slices.Contains(t.Scopes, scope) }

// App is Exciter as the partner app registered at a partner: its client id and secret there.
type App struct {
	BaseURL, ClientID, ClientSecret string
}

// Exchange trades the person's access token from the identity provider for their tokens at the partner. No scope
// is asked, so the partner gives what its administrator allowed the app.
func Exchange(ctx context.Context, client *http.Client, app App, subject string) (*Tokens, error) {
	return post(ctx, client, app, url.Values{
		"grant_type":         {GrantType},
		"subject_token":      {subject},
		"subject_token_type": {TokenTypeAccess},
	})
}

// Refresh rotates the tokens of a grant: the answer's refresh token replaces the one sent, which ends.
func Refresh(ctx context.Context, client *http.Client, app App, refresh string) (*Tokens, error) {
	return post(ctx, client, app, url.Values{"grant_type": {GrantRefresh}, "refresh_token": {refresh}})
}

func post(ctx context.Context, client *http.Client, app App, form url.Values) (*Tokens, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(app.BaseURL, "/")+TokenPath, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnreachable, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	// RFC 6749 section 2.3.1 form-encodes both before joining them.
	req.SetBasicAuth(url.QueryEscape(app.ClientID), url.QueryEscape(app.ClientSecret))
	sent := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnreachable, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswerBytes))
	if err != nil {
		return nil, fmt.Errorf("%w: read the answer: %w", ErrUnreachable, err)
	}
	switch {
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError:
		return nil, fmt.Errorf("%w: %s answered %d", ErrUnreachable, req.URL.Host, resp.StatusCode)
	case resp.StatusCode >= http.StatusBadRequest:
		return nil, refusal(resp.StatusCode, raw)
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("%w: %s answered %d", ErrUnreachable, req.URL.Host, resp.StatusCode)
	}
	var answer struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
		RefreshToken string `json:"refresh_token"`
		Scope        string `json:"scope"`
	}
	if err := json.Unmarshal(raw, &answer); err != nil || answer.AccessToken == "" || !strings.EqualFold(answer.TokenType, "Bearer") {
		return nil, fmt.Errorf("%w: the answer is no bearer token", ErrUnreachable)
	}
	// Without a refresh token Exciter could not act for the person past the hour, so such an answer is refused.
	if answer.RefreshToken == "" || answer.ExpiresIn <= 0 {
		return nil, &Refusal{Status: resp.StatusCode, Code: "no_refresh_token", Description: "The partner answered without a refresh token, which Exciter needs to act for you later."}
	}
	return &Tokens{Access: answer.AccessToken, Refresh: answer.RefreshToken, Scopes: strings.Fields(answer.Scope),
		ExpiresAt: sent.Add(time.Duration(answer.ExpiresIn) * time.Second)}, nil
}

func refusal(status int, raw []byte) error {
	var body struct {
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	r := &Refusal{Status: status, Code: "refused", Description: http.StatusText(status)}
	if json.Unmarshal(raw, &body) == nil && body.Error != "" {
		r.Code, r.Description = body.Error, strings.TrimSpace(body.Description)
	}
	return r
}

// Due says whether a stored access token should be refreshed before it is used.
func Due(expiresAt, now time.Time) bool { return !now.Before(expiresAt.Add(-RefreshMargin)) }

// Why is the sentence the account page shows when the exchange at partner, Armature or Stator, failed.
func Why(partner string, err error) string {
	var r *Refusal
	switch {
	case errors.As(err, &r) && r.Code == "invalid_client":
		return partner + " did not accept Exciter's client id and secret, so you were not connected at sign-in. An Exciter administrator checks them under Settings, " + partner + "; until then, connect with a token below."
	case errors.As(err, &r) && r.Code == "unauthorized_client":
		return partner + " does not let Exciter connect people at sign-in yet: " + sentence(r.Description) + " Connect with a token below meanwhile."
	case errors.As(err, &r):
		return partner + " did not connect you at sign-in: " + sentence(r.Description) + " Connect with a token below, or ask " + partner + "'s administrator."
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Sprintf("%s did not answer within %d seconds when you signed in, so you were not connected there. Sign in again later, or connect with a token below.",
			partner, int(SignInTimeout.Seconds()))
	default:
		return partner + " did not answer when you signed in, so you were not connected there. Sign in again later, or connect with a token below."
	}
}

// sentence ends a partner's description with a full stop, which OAuth does not ask of it.
func sentence(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "it gave no reason."
	}
	if !strings.HasSuffix(s, ".") && !strings.HasSuffix(s, "!") && !strings.HasSuffix(s, "?") {
		s += "."
	}
	return s
}
