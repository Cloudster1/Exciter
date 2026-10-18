// Package render asks the render service, a headless Chromium behind a small
// HTTP door, for a page of the web application as a PDF. Adapted from Stator's.
package render

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultTimeout is how long a print may take, waiting for a free browser
// included; it stays under the api's request timeout so a slow page answers
// with a reason rather than a cut line.
const DefaultTimeout = 20 * time.Second

// DefaultConcurrency is how many prints one api process runs at once.
const DefaultConcurrency = 4

// DefaultMaxSize is the largest PDF handed back, in bytes.
const DefaultMaxSize int64 = 50 << 20

// budgetMargin is what the service's budget leaves of the client's deadline,
// so the service gives up first and says why.
const budgetMargin = 2 * time.Second

// minBudget is the least a print is started with; less would fail anyway.
const minBudget = 3 * time.Second

// errorBodyLimit is how much of a refusal's body is kept for the log.
const errorBodyLimit = 512

var (
	// ErrUnavailable is a deployment without a render service, or one that
	// could not be reached.
	ErrUnavailable = errors.New("PDF export is not set up on this server")
	// ErrBusy is every browser of this process taken until the deadline.
	ErrBusy = errors.New("too many PDFs are being made right now")
	// ErrTimeout is a page that took longer than the deadline to print.
	ErrTimeout = errors.New("the page took too long to print")
	// ErrFailed is a print the service tried and could not finish.
	ErrFailed = errors.New("the page could not be printed")
	// ErrTooLarge is a PDF larger than the client takes.
	ErrTooLarge = errors.New("the PDF is larger than this server hands out")
)

// Request is what to print: a path of the web application, the credential
// its API calls carry, if any, and the reader's language, which the browser
// then prefers. The credential never goes in the path.
type Request struct {
	Path     string
	Token    string
	Language string
}

// Renderer turns a path of the web application into a PDF.
type Renderer interface {
	PDF(ctx context.Context, req Request) ([]byte, error)
}

// Options bound a Client; a zero field takes its default.
type Options struct {
	Timeout     time.Duration
	Concurrency int
	MaxSize     int64
}

// Client is the render service at an address. Its URL is the operator's, so it
// is not held to the outbound guard.
type Client struct {
	base    string
	http    *http.Client
	timeout time.Duration
	maxSize int64
	slots   chan struct{}
}

// New makes a client of the service at baseURL.
func New(baseURL string, opts Options) *Client {
	if opts.Timeout <= 0 {
		opts.Timeout = DefaultTimeout
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = DefaultConcurrency
	}
	if opts.MaxSize <= 0 {
		opts.MaxSize = DefaultMaxSize
	}
	return &Client{
		base: strings.TrimRight(baseURL, "/"), http: &http.Client{},
		timeout: opts.Timeout, maxSize: opts.MaxSize, slots: make(chan struct{}, opts.Concurrency),
	}
}

// wire is the service's request body: Armature's path, with the credential
// and the time left beside it.
type wire struct {
	Path     string `json:"path"`
	Token    string `json:"token,omitempty"`
	Language string `json:"language,omitempty"`
	BudgetMS int64  `json:"budgetMs"`
}

// PDF waits for a free slot, asks the service to print and reads the PDF back.
// One deadline covers both, so a queue never makes a request outlive the api's.
func (c *Client) PDF(ctx context.Context, req Request) ([]byte, error) {
	return c.ask(ctx, "/pdf", req, func(answer []byte) bool { return bytes.HasPrefix(answer, []byte("%PDF-")) })
}

// HTMLRenderer takes a page of the web client as HTML, its styles inlined and its scripts left out, for a site that
// opens offline (#214).
type HTMLRenderer interface {
	HTML(ctx context.Context, req Request) ([]byte, error)
}

// HTML waits for a free slot and asks the service for the page's HTML once it has drawn.
func (c *Client) HTML(ctx context.Context, req Request) ([]byte, error) {
	return c.ask(ctx, "/html", req, func(answer []byte) bool {
		return bytes.Contains(answer[:min(len(answer), 64)], []byte("<!doctype html"))
	})
}

// ask posts a request to one of the service's endpoints and reads back what holds.
func (c *Client) ask(ctx context.Context, endpoint string, req Request, holds func([]byte) bool) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	select {
	case c.slots <- struct{}{}:
		defer func() { <-c.slots }()
	case <-ctx.Done():
		return nil, ErrBusy
	}
	deadline, _ := ctx.Deadline()
	budget := time.Until(deadline) - budgetMargin
	if budget < minBudget {
		return nil, ErrBusy
	}
	body, _ := json.Marshal(wire{Path: req.Path, Token: req.Token, Language: req.Language, BudgetMS: budget.Milliseconds()})
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ErrTimeout
		}
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		said, _ := io.ReadAll(io.LimitReader(resp.Body, errorBodyLimit))
		return nil, fmt.Errorf("%w: the service answered %d: %s", classify(resp.StatusCode), resp.StatusCode, strings.TrimSpace(string(said)))
	}
	pdf, err := io.ReadAll(io.LimitReader(resp.Body, c.maxSize+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, ErrTimeout
		}
		return nil, fmt.Errorf("%w: %v", ErrFailed, err)
	}
	if int64(len(pdf)) > c.maxSize {
		return nil, ErrTooLarge
	}
	if !holds(pdf) {
		return nil, fmt.Errorf("%w: the answer is not what %s gives", ErrFailed, endpoint)
	}
	return pdf, nil
}

// classify says what a refusal of the service means to the reader.
func classify(status int) error {
	switch status {
	case http.StatusServiceUnavailable, http.StatusTooManyRequests:
		return ErrBusy
	case http.StatusGatewayTimeout:
		return ErrTimeout
	case http.StatusNotFound:
		return ErrUnavailable
	}
	return ErrFailed
}

// Unavailable is the renderer of a deployment without one.
type Unavailable struct{}

// PDF says so.
func (Unavailable) PDF(context.Context, Request) ([]byte, error) {
	return nil, ErrUnavailable
}
