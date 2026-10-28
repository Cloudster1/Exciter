package webhook

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/cloudster1/exciter/backend/internal/netguard"
)

// A request that never reached its receiver is told in one of three sentences, whatever the error says of the
// network behind it.
func TestWhyNamesWhatStoppedARequest(t *testing.T) {
	hanging := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer hanging.Close()
	const impatient = 50 * time.Millisecond
	_, clientTimeout := (&http.Client{Timeout: impatient}).Get(hanging.URL)
	if clientTimeout == nil {
		t.Fatal("a hanging receiver answered")
	}

	for name, c := range map[string]struct {
		err  error
		want string
	}{
		"blocked":                    {fmt.Errorf("dial: %w: 10.0.0.1", netguard.ErrBlocked), whyBlocked},
		"blocked after a redirect":   {&url.Error{Op: "Post", URL: "http://10.0.0.1/", Err: fmt.Errorf("%w: 10.0.0.1", netguard.ErrBlocked)}, whyBlocked},
		"a deadline":                 {&url.Error{Op: "Post", URL: "https://hooks.example.com/", Err: context.DeadlineExceeded}, whyTimeout},
		"the client's own timeout":   {clientTimeout, whyTimeout},
		"a dial that timed out":      {&net.OpError{Op: "dial", Net: "tcp", Err: &net.DNSError{Err: "i/o timeout", Name: "hooks.example.com", IsTimeout: true}}, whyTimeout},
		"a name that does not exist": {&net.DNSError{Err: "no such host", Name: "receiver.invalid", IsNotFound: true}, whyUnreached},
		"a refused connection":       {errors.New("dial tcp 203.0.113.7:443: connect: connection refused"), whyUnreached},
	} {
		if got := why(c.err); got != c.want {
			t.Errorf("%s (%v): %q, want %q", name, c.err, got, c.want)
		}
	}
}
