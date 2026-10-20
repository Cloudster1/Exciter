// Package diagram asks the renderer, a Kroki container only the api reaches, to draw a diagram from its source (#284).
// Only the source is stored; the picture is drawn when it is shown, and kept a while in memory.
package diagram

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cloudster1/exciter/backend/internal/document"
)

// Defaults bound a Client whose Options leave them out.
const (
	// DefaultTimeout is how long one drawing may take, a wait for a free slot included.
	DefaultTimeout = 10 * time.Second
	// DefaultConcurrency is how many drawings one api process asks for at once.
	DefaultConcurrency = 4
	// DefaultMaxSize is the largest picture handed back, in bytes.
	DefaultMaxSize int64 = 10 << 20
	// DefaultCacheSize is how many bytes of drawn pictures one process keeps.
	DefaultCacheSize int64 = 32 << 20
)

// errorBodyLimit is how much of a refusal's body is read for its sentence.
const errorBodyLimit = 2048

// Format is what a drawing is handed back as.
type Format string

const (
	SVG Format = "svg"
	PNG Format = "png"
)

var (
	// ErrUnavailable is a deployment without a renderer, or one that could not be reached.
	ErrUnavailable = errors.New("diagrams are not drawn on this server")
	// ErrBusy is every slot of this process taken until the deadline.
	ErrBusy = errors.New("too many diagrams are being drawn right now")
	// ErrTimeout is a drawing that took longer than the deadline.
	ErrTimeout = errors.New("the diagram took too long to draw")
	// ErrTooLarge is a picture larger than the client takes.
	ErrTooLarge = errors.New("the drawn diagram is larger than this server hands out")
	// ErrFailed is a drawing the renderer could not finish for a reason not in the source.
	ErrFailed = errors.New("the diagram could not be drawn")
	// ErrInvalid is a language the server does not draw, or a source empty or too long.
	ErrInvalid = errors.New("the diagram is not one this server draws")
)

// Request is a diagram to draw, as the web client sends it.
type Request struct {
	Language string `json:"language"`
	Source   string `json:"source"`
}

// Picture is a drawn diagram as SVG text, which the web client shows as an image so nothing in it runs.
type Picture struct {
	SVG string `json:"svg"`
}

// SourceError is source the renderer could not read, in its own words.
type SourceError struct{ Message string }

func (e *SourceError) Error() string { return e.Message }

// Drawer draws a diagram's source as a picture.
type Drawer interface {
	Draw(ctx context.Context, language string, format Format, source string) ([]byte, error)
}

// Check refuses a language the server does not draw and a source a diagram may not hold, as the allowlist would.
func Check(language, source string) error {
	switch {
	case !slices.Contains(document.DiagramLanguages, language):
		return fmt.Errorf("%w: the language %q", ErrInvalid, language)
	case strings.TrimSpace(source) == "":
		return fmt.Errorf("%w: the source is empty", ErrInvalid)
	case utf8.RuneCountInString(source) > document.MaxDiagramLength:
		return fmt.Errorf("%w: the source is over %d characters", ErrInvalid, document.MaxDiagramLength)
	}
	return nil
}

// Options bound a Client; a zero field takes its default.
type Options struct {
	Timeout     time.Duration
	Concurrency int
	MaxSize     int64
	CacheSize   int64
}

// Client is Kroki at an address. Its URL is the operator's, so it is not held to the outbound guard.
type Client struct {
	base    string
	http    *http.Client
	timeout time.Duration
	maxSize int64
	slots   chan struct{}
	cache   *cache
}

// New makes a client of the renderer at baseURL.
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
	if opts.CacheSize <= 0 {
		opts.CacheSize = DefaultCacheSize
	}
	return &Client{
		base: strings.TrimRight(baseURL, "/"), http: &http.Client{}, timeout: opts.Timeout, maxSize: opts.MaxSize,
		slots: make(chan struct{}, opts.Concurrency), cache: newCache(opts.CacheSize),
	}
}

// Draw hands back the picture of source, drawn once and then kept while it fits the cache.
func (c *Client) Draw(ctx context.Context, language string, format Format, source string) ([]byte, error) {
	if err := Check(language, source); err != nil {
		return nil, err
	}
	if format != SVG && format != PNG {
		return nil, fmt.Errorf("%w: the format %q", ErrInvalid, format)
	}
	key := sha256.Sum256([]byte(language + "\x00" + string(format) + "\x00" + source))
	if picture, ok := c.cache.get(key); ok {
		return picture, nil
	}
	picture, err := c.ask(ctx, language, format, source)
	if err != nil {
		return nil, err
	}
	c.cache.put(key, picture)
	return picture, nil
}

func (c *Client) ask(ctx context.Context, language string, format Format, source string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	select {
	case c.slots <- struct{}{}:
		defer func() { <-c.slots }()
	case <-ctx.Done():
		return nil, ErrBusy
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/"+language+"/"+string(format), strings.NewReader(source))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ErrTimeout
		}
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		said, _ := io.ReadAll(io.LimitReader(resp.Body, errorBodyLimit))
		return nil, refusal(resp.StatusCode, string(said))
	}
	picture, err := io.ReadAll(io.LimitReader(resp.Body, c.maxSize+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, ErrTimeout
		}
		return nil, fmt.Errorf("%w: %v", ErrFailed, err)
	}
	if int64(len(picture)) > c.maxSize {
		return nil, ErrTooLarge
	}
	if !looksLike(format, picture) {
		return nil, fmt.Errorf("%w: the answer is not %s", ErrFailed, format)
	}
	return picture, nil
}

// refusal says what an answer other than a picture means: a 400 is the source's fault, in Kroki's words.
func refusal(status int, said string) error {
	switch status {
	case http.StatusBadRequest:
		return &SourceError{Message: sentence(said)}
	case http.StatusServiceUnavailable, http.StatusTooManyRequests:
		return ErrBusy
	case http.StatusGatewayTimeout:
		return ErrTimeout
	case http.StatusNotFound:
		return ErrUnavailable
	}
	return fmt.Errorf("%w: the renderer answered %d: %s", ErrFailed, status, strings.TrimSpace(said))
}

// sentence is the first line of Kroki's refusal without its status, such as "Syntax Error? (line: 2)".
func sentence(said string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(said), "\n")
	line = strings.TrimSpace(line)
	if rest, ok := strings.CutPrefix(line, "Error 400:"); ok {
		line = strings.TrimSpace(rest)
	}
	if line == "" {
		return "The renderer could not read this source."
	}
	return line
}

func looksLike(format Format, picture []byte) bool {
	if format == PNG {
		return bytes.HasPrefix(picture, []byte("\x89PNG\r\n\x1a\n"))
	}
	head := picture[:min(len(picture), errorBodyLimit)]
	return bytes.Contains(head, []byte("<svg"))
}

// Unavailable is the drawer of a deployment without a renderer.
type Unavailable struct{}

// Draw says so.
func (Unavailable) Draw(context.Context, string, Format, string) ([]byte, error) {
	return nil, ErrUnavailable
}
