package diagram

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudster1/exciter/backend/internal/config"
	"github.com/cloudster1/exciter/backend/internal/document"
)

const (
	svg    = `<svg xmlns="http://www.w3.org/2000/svg"><text>A</text></svg>`
	png    = "\x89PNG\r\n\x1a\nrest"
	source = "@startuml\nA -> B\n@enduml"
)

// kroki answers as Kroki does: the picture for a known language and format, and a 400 with its sentence for
// source that says "broken".
func kroki(t *testing.T, calls *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		body, _ := io.ReadAll(r.Body)
		switch {
		case r.Method != http.MethodPost || !strings.HasPrefix(r.Header.Get("Content-Type"), "text/plain"):
			http.Error(w, "wrong request", http.StatusMethodNotAllowed)
		case strings.Contains(string(body), "broken"):
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, "Error 400: Syntax Error? (Assumed diagram type: sequence) (line: 2)\n")
		case r.URL.Path == "/plantuml/svg":
			_, _ = io.WriteString(w, svg)
		case r.URL.Path == "/plantuml/png":
			_, _ = io.WriteString(w, png)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDrawsSVGAndPNGAndKeepsThem(t *testing.T) {
	var calls atomic.Int32
	c := New(kroki(t, &calls).URL, Options{})
	got, err := c.Draw(t.Context(), document.DiagramPlantUML, SVG, source)
	if err != nil || string(got) != svg {
		t.Fatalf("svg: %q %v", got, err)
	}
	if got, err := c.Draw(t.Context(), document.DiagramPlantUML, PNG, source); err != nil || string(got) != png {
		t.Fatalf("png: %q %v", got, err)
	}
	if _, err := c.Draw(t.Context(), document.DiagramPlantUML, SVG, source); err != nil {
		t.Fatal(err)
	}
	if n := calls.Load(); n != 2 {
		t.Errorf("a picture drawn once is kept: the renderer was asked %d times", n)
	}
}

func TestBrokenSourceIsRefusedInTheRenderersWords(t *testing.T) {
	var calls atomic.Int32
	_, err := New(kroki(t, &calls).URL, Options{}).Draw(t.Context(), document.DiagramPlantUML, SVG, "broken")
	var refused *SourceError
	if !errors.As(err, &refused) || refused.Message != "Syntax Error? (Assumed diagram type: sequence) (line: 2)" {
		t.Fatalf("broken source: %v", err)
	}
}

func TestWhatIsNotADiagramIsNotSent(t *testing.T) {
	var calls atomic.Int32
	c := New(kroki(t, &calls).URL, Options{})
	for name, try := range map[string]func() error{
		"another language": func() error { _, err := c.Draw(t.Context(), "mermaid", SVG, source); return err },
		"a path as language": func() error {
			_, err := c.Draw(t.Context(), "../admin", SVG, source)
			return err
		},
		"blank source":   func() error { _, err := c.Draw(t.Context(), document.DiagramPlantUML, SVG, " \n"); return err },
		"another format": func() error { _, err := c.Draw(t.Context(), document.DiagramPlantUML, "pdf", source); return err },
		"too long": func() error {
			_, err := c.Draw(t.Context(), document.DiagramPlantUML, SVG, strings.Repeat("a", document.MaxDiagramLength+1))
			return err
		},
	} {
		if err := try(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if calls.Load() != 0 {
		t.Error("the renderer was asked for something it should not draw")
	}
}

func TestRefusalsSayWhatWentWrong(t *testing.T) {
	answer := func(status int, body string) *httptest.Server {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = io.WriteString(w, body)
		}))
		t.Cleanup(srv.Close)
		return srv
	}
	for name, tc := range map[string]struct {
		status int
		body   string
		want   error
	}{
		"busy":         {http.StatusServiceUnavailable, "", ErrBusy},
		"gone":         {http.StatusNotFound, "", ErrUnavailable},
		"broken":       {http.StatusInternalServerError, "boom", ErrFailed},
		"not a svg":    {http.StatusOK, "<html>hello</html>", ErrFailed},
		"slow gateway": {http.StatusGatewayTimeout, "", ErrTimeout},
	} {
		if _, err := New(answer(tc.status, tc.body).URL, Options{}).Draw(t.Context(), document.DiagramPlantUML, SVG, source); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", name, err, tc.want)
		}
	}
	if _, err := New("http://127.0.0.1:1", Options{}).Draw(t.Context(), document.DiagramPlantUML, SVG, source); !errors.Is(err, ErrUnavailable) {
		t.Errorf("nobody listening: %v", err)
	}
	if _, err := (Unavailable{}).Draw(t.Context(), document.DiagramPlantUML, SVG, source); !errors.Is(err, ErrUnavailable) {
		t.Errorf("no renderer: %v", err)
	}
}

func TestAPictureTooLargeIsRefused(t *testing.T) {
	var calls atomic.Int32
	_, err := New(kroki(t, &calls).URL, Options{MaxSize: 8}).Draw(t.Context(), document.DiagramPlantUML, SVG, source)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("a picture over the limit: %v", err)
	}
}

func TestASlowRendererTimesOutAndAFullOneIsBusy(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
		_, _ = io.WriteString(w, svg)
	}))
	t.Cleanup(func() { close(release); srv.Close() })
	const brief = 200 * time.Millisecond
	c := New(srv.URL, Options{Timeout: brief, Concurrency: 1})
	done := make(chan error, 1)
	go func() {
		_, err := c.Draw(context.Background(), document.DiagramPlantUML, SVG, source)
		done <- err
	}()
	time.Sleep(brief / 4)
	waiting, cancel := context.WithTimeout(t.Context(), brief/4)
	defer cancel()
	if _, err := c.Draw(waiting, document.DiagramPlantUML, SVG, source+" "); !errors.Is(err, ErrBusy) {
		t.Errorf("with every slot taken: %v", err)
	}
	if err := <-done; !errors.Is(err, ErrTimeout) {
		t.Errorf("a drawing past the deadline: %v", err)
	}
}

func TestTheConfigurationDefaultsAreTheClients(t *testing.T) {
	if config.DefaultKrokiTimeout != DefaultTimeout || config.DefaultKrokiConcurrency != DefaultConcurrency {
		t.Error("the configuration's renderer defaults have drifted from the client's")
	}
}

func TestTheCacheDropsWhatWasReadLeastRecently(t *testing.T) {
	c := newCache(10)
	a, b, d := cacheKey{1}, cacheKey{2}, cacheKey{3}
	c.put(a, []byte("aaaa"))
	c.put(b, []byte("bbbb"))
	c.get(a)
	c.put(d, []byte("dddd"))
	if _, ok := c.get(b); ok {
		t.Error("the least recently read picture is kept past the limit")
	}
	if _, ok := c.get(a); !ok {
		t.Error("a picture read since is dropped")
	}
	c.put(cacheKey{4}, []byte("far too large for it"))
	if _, ok := c.get(cacheKey{4}); ok {
		t.Error("a picture larger than the cache is kept")
	}
}
