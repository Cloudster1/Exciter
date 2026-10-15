package armature

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/netguard"
)

// APIPath is where an Armature serves its API under its base URL.
const APIPath = "/api/v1"

// DocumentTitle is info.title of Armature's own OpenAPI document, which tells
// an Armature apart from anything else answering at an address.
const DocumentTitle = "Armature"

var (
	// ErrRejected is Armature answering 401 to the token. It is never relayed
	// as a 401, which would read as "sign in to Exciter".
	ErrRejected = errors.New("Armature did not accept the token")
	// ErrUnreachable covers no answer in time, an address the guard refused,
	// a 5xx, a 429 and an answer that could not be read.
	ErrUnreachable = errors.New("Armature could not be reached")
	// ErrNotArmature is an address that answers, but not as an Armature.
	ErrNotArmature = errors.New("the address does not answer as an Armature")
	// errUnreadable is an answer that is not the JSON asked for.
	errUnreadable = errors.New("the answer is not the JSON expected")
)

// RefusedError is any other refusal from Armature, with its own words, so a
// write can relay it: 403 for rights, 404, 422 for a field, 400 bad_query.
type RefusedError struct {
	Status   int
	Code     string
	Message  string
	Fields   map[string]string
	Position *int
}

func (e *RefusedError) Error() string {
	return fmt.Sprintf("Armature refused with %d %s: %s", e.Status, e.Code, e.Message)
}

// StatusOf names why a read could not be answered, for the status field of
// reads: rejected, unreachable, or ok for no error.
func StatusOf(err error) Status {
	switch {
	case err == nil:
		return StatusOK
	case errors.Is(err, ErrRejected):
		return StatusRejected
	default:
		return StatusUnreachable
	}
}

// Client reaches Armature instances through the SSRF guard. One is shared by
// the process; each call names the instance and the token it is made with.
type Client struct {
	http *http.Client
}

// NewClient returns the client every Armature call goes through: netguard
// with allow, backchannel rewrites first, each call bounded by CallTimeout.
func NewClient(allow netguard.Allow, backchannel map[string]string) *Client {
	return &Client{http: netguard.WithTransport(CallTimeout, netguard.Rewriting(backchannel, netguard.Transport(allow)))}
}

// Describe asks baseURL for its OpenAPI document, which Armature serves to
// anybody, and says whether it is Armature's.
func (c *Client) Describe(ctx context.Context, baseURL string) error {
	var doc struct {
		Info struct {
			Title string `json:"title"`
		} `json:"info"`
	}
	err := c.do(ctx, http.MethodGet, baseURL+APIPath+"/openapi.json", "", nil, &doc)
	var refused *RefusedError
	switch {
	case err == nil && doc.Info.Title == DocumentTitle:
		return nil
	case err == nil, errors.As(err, &refused), errors.Is(err, ErrRejected), errors.Is(err, errUnreadable):
		return ErrNotArmature
	}
	return err
}

// As is a caller acting as the owner of token at baseURL.
func (c *Client) As(baseURL, token string) *Caller {
	return &Caller{client: c, baseURL: strings.TrimSuffix(baseURL, "/"), token: token}
}

// Caller makes calls to one Armature as one person, with their own token.
type Caller struct {
	client  *Client
	baseURL string
	token   string
}

// BaseURL is where the caller's Armature is opened.
func (a *Caller) BaseURL() string { return a.baseURL }

// Get calls GET path, relative to /api/v1, with query, into out.
func (a *Caller) Get(ctx context.Context, path string, query url.Values, out any) error {
	target := a.baseURL + APIPath + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	return a.client.do(ctx, http.MethodGet, target, a.token, nil, out)
}

// GetWithin is Get for an answer that may be larger than MaxResponseBytes,
// such as a theme file, up to limit.
func (a *Caller) GetWithin(ctx context.Context, path string, limit int64, out any) error {
	return a.client.doWithin(ctx, http.MethodGet, a.baseURL+APIPath+path, a.token, nil, out, limit)
}

// Send calls method on path, relative to /api/v1, with body as JSON, into
// out; a nil out ignores the answer.
func (a *Caller) Send(ctx context.Context, method, path string, body, out any) error {
	return a.client.do(ctx, method, a.baseURL+APIPath+path, a.token, body, out)
}

// Me is who a token acts as, from Armature's GET /auth/me, and whether it may only read.
type Me struct {
	User     AccountUser
	OrgID    uuid.UUID
	ReadOnly bool
}

// Me asks Armature who the token acts as, and in which of its organizations while it still reports one.
func (a *Caller) Me(ctx context.Context) (*Me, error) {
	var answer struct {
		Principal struct {
			User AccountUser `json:"user"`
			Org  *struct {
				ID uuid.UUID `json:"id"`
			} `json:"org"`
		} `json:"principal"`
		Token *struct {
			Scopes []string `json:"scopes"`
		} `json:"token"`
	}
	if err := a.Get(ctx, "/auth/me", nil, &answer); err != nil {
		return nil, err
	}
	me := &Me{User: answer.Principal.User}
	if answer.Token != nil {
		me.ReadOnly = slices.Contains(answer.Token.Scopes, ScopeRead)
	}
	if answer.Principal.Org != nil {
		me.OrgID = answer.Principal.Org.ID
	}
	return me, nil
}

func (c *Client) do(ctx context.Context, method, target, token string, body, out any) error {
	return c.doWithin(ctx, method, target, token, body, out, MaxResponseBytes)
}

func (c *Client) doWithin(ctx context.Context, method, target, token string, body, out any, limit int64) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnreachable, err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnreachable, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return fmt.Errorf("%w: read the answer: %w", ErrUnreachable, err)
	}
	if int64(len(raw)) > limit {
		return fmt.Errorf("%w: the answer is larger than %d bytes", ErrUnreachable, limit)
	}

	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return ErrRejected
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError:
		return fmt.Errorf("%w: %s answered %d", ErrUnreachable, req.URL.Host, resp.StatusCode)
	case resp.StatusCode >= http.StatusBadRequest:
		return refusal(resp.StatusCode, raw)
	case resp.StatusCode >= http.StatusMultipleChoices:
		return fmt.Errorf("%w: %s answered %d", ErrUnreachable, req.URL.Host, resp.StatusCode)
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%w: %w: %w", ErrUnreachable, errUnreadable, err)
	}
	return nil
}

// refusal reads Armature's error envelope, which every endpoint answers with.
func refusal(status int, raw []byte) error {
	var envelope struct {
		Error struct {
			Code     string            `json:"code"`
			Message  string            `json:"message"`
			Fields   map[string]string `json:"fields"`
			Position *int              `json:"position"`
		} `json:"error"`
	}
	e := &RefusedError{Status: status, Code: "armature_refused", Message: http.StatusText(status)}
	if json.Unmarshal(raw, &envelope) == nil && envelope.Error.Code != "" {
		e.Code, e.Message, e.Fields, e.Position = envelope.Error.Code, envelope.Error.Message, envelope.Error.Fields, envelope.Error.Position
	}
	return e
}
