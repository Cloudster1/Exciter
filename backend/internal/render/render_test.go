package render

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudster1/exciter/backend/internal/config"
)

const testPDF = "%PDF-1.4\n%printed by a test\n"

// service answers a print the way the handler says, after checking the body
// carries the path, the credential and a budget under the client's deadline.
func service(t *testing.T, answer func(w http.ResponseWriter, got wire)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/pdf" {
			t.Errorf("asked %s %s", r.Method, r.URL.Path)
		}
		var got wire
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("the body is not the wire format: %v", err)
		}
		answer(w, got)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestAPrintSendsThePathTheTokenAndWhatIsLeftOfTheDeadline(t *testing.T) {
	var asked wire
	srv := service(t, func(w http.ResponseWriter, got wire) {
		asked = got
		_, _ = w.Write([]byte(testPDF))
	})
	pdf, err := New(srv.URL+"/", Options{Timeout: 10 * time.Second}).PDF(context.Background(), Request{Path: "/print/p/1", Token: "secret", Language: "de"})
	if err != nil {
		t.Fatal(err)
	}
	if string(pdf) != testPDF {
		t.Errorf("got %q", pdf)
	}
	if asked.Path != "/print/p/1" || asked.Token != "secret" || asked.Language != "de" {
		t.Errorf("sent %+v", asked)
	}
	if asked.BudgetMS <= 0 || asked.BudgetMS > (10*time.Second-budgetMargin).Milliseconds() {
		t.Errorf("a budget of %dms does not leave the client the last word", asked.BudgetMS)
	}
}

func TestARefusalSaysWhatItMeans(t *testing.T) {
	for status, want := range map[int]error{
		http.StatusBadGateway:         ErrFailed,
		http.StatusBadRequest:         ErrFailed,
		http.StatusServiceUnavailable: ErrBusy,
		http.StatusGatewayTimeout:     ErrTimeout,
		http.StatusNotFound:           ErrUnavailable,
	} {
		srv := service(t, func(w http.ResponseWriter, _ wire) { http.Error(w, `{"error":"no"}`, status) })
		if _, err := New(srv.URL, Options{}).PDF(context.Background(), Request{Path: "/"}); !errors.Is(err, want) {
			t.Errorf("%d: got %v, want %v", status, err, want)
		}
	}
}

func TestAnAnswerThatIsNoPDFIsAFailure(t *testing.T) {
	srv := service(t, func(w http.ResponseWriter, _ wire) { _, _ = w.Write([]byte("<html>sign in</html>")) })
	if _, err := New(srv.URL, Options{}).PDF(context.Background(), Request{Path: "/"}); !errors.Is(err, ErrFailed) {
		t.Errorf("got %v", err)
	}
}

func TestAPDFOverTheLimitIsRefused(t *testing.T) {
	srv := service(t, func(w http.ResponseWriter, _ wire) { _, _ = w.Write([]byte(testPDF + strings.Repeat("x", 64))) })
	if _, err := New(srv.URL, Options{MaxSize: 32}).PDF(context.Background(), Request{Path: "/"}); !errors.Is(err, ErrTooLarge) {
		t.Errorf("got %v", err)
	}
}

func TestAServiceGoneIsUnavailable(t *testing.T) {
	srv := service(t, func(http.ResponseWriter, wire) {})
	srv.Close()
	if _, err := New(srv.URL, Options{}).PDF(context.Background(), Request{Path: "/"}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("got %v", err)
	}
}

func TestASlowPrintRunsOutOfTime(t *testing.T) {
	release := make(chan struct{})
	srv := service(t, func(http.ResponseWriter, wire) { <-release })
	defer close(release)
	if _, err := New(srv.URL, Options{Timeout: minBudget + budgetMargin + 200*time.Millisecond}).PDF(context.Background(), Request{Path: "/"}); !errors.Is(err, ErrTimeout) {
		t.Errorf("got %v", err)
	}
}

func TestPrintsBeyondTheConcurrencyWaitAndThenGiveUp(t *testing.T) {
	var running, most atomic.Int32
	release := make(chan struct{})
	srv := service(t, func(w http.ResponseWriter, _ wire) {
		now := running.Add(1)
		for {
			seen := most.Load()
			if now <= seen || most.CompareAndSwap(seen, now) {
				break
			}
		}
		<-release
		running.Add(-1)
		_, _ = w.Write([]byte(testPDF))
	})
	client := New(srv.URL, Options{Timeout: minBudget + budgetMargin + time.Second, Concurrency: 1})
	first := make(chan error, 1)
	go func() {
		_, err := client.PDF(context.Background(), Request{Path: "/first"})
		first <- err
	}()
	for running.Load() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	short, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := client.PDF(short, Request{Path: "/second"}); !errors.Is(err, ErrBusy) {
		t.Errorf("a print with every slot taken: %v", err)
	}
	close(release)
	if err := <-first; err != nil {
		t.Errorf("the first print: %v", err)
	}
	if most.Load() != 1 {
		t.Errorf("%d prints ran at once with one slot", most.Load())
	}
}

func TestWithoutAServiceExportSaysSo(t *testing.T) {
	if _, err := (Unavailable{}).PDF(context.Background(), Request{Path: "/"}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("got %v", err)
	}
}

func TestTheConfigurationDefaultsAreTheClients(t *testing.T) {
	if config.DefaultRenderTimeout != DefaultTimeout || config.DefaultRenderConcurrency != DefaultConcurrency || config.DefaultRenderMaxSize != DefaultMaxSize {
		t.Error("the configuration's render defaults have drifted from the client's")
	}
}
