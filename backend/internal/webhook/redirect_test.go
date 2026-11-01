package webhook

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/netguard"
)

// A receiver that redirects gets its answer logged, and the signed body goes nowhere the endpoint does not name.
func TestADeliveryFollowsNoRedirect(t *testing.T) {
	var elsewhere atomic.Int32
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		elsewhere.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer second.Close()
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, second.URL, http.StatusTemporaryRedirect)
	}))
	defer first.Close()

	s := NewService(nil, nil, Options{Allow: netguard.ParseAllow("127.0.0.0/8, ::1/128")})
	status, err := s.post(context.Background(), first.URL, "secret", TopicPing, Version, uuid.New(), []byte(`{}`))
	if err != nil || status != http.StatusTemporaryRedirect {
		t.Fatalf("a redirected post answers %d, %v; want %d", status, err, http.StatusTemporaryRedirect)
	}
	if n := elsewhere.Load(); n != 0 {
		t.Errorf("the redirect's target was posted %d times", n)
	}
}
