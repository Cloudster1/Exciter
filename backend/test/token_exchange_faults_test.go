//go:build integration

package test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/armature"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/netguard"
	"github.com/cloudster1/exciter/backend/internal/secret"
	"github.com/cloudster1/exciter/backend/internal/stator"
	"github.com/cloudster1/exciter/backend/internal/tokenexchange"
)

// A partner that is slow at its token endpoint, which the stubs never are: the sign-in keeps why it timed out (#348).

// faultyPartner answers the exchange after exchangeDelay with an access token that is due at once, and every refresh
// with refreshStatus, counting the refreshes.
type faultyPartner struct {
	exchangeDelay time.Duration
	refreshStatus int
	refreshes     atomic.Int32
	url           string
}

func newFaultyPartner(t *testing.T, exchangeDelay time.Duration, refreshStatus int) *faultyPartner {
	t.Helper()
	p := &faultyPartner{exchangeDelay: exchangeDelay, refreshStatus: refreshStatus}
	me := map[string]any{"id": uuid.NewString(), "name": "Ada", "email": "ada@partner.example"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == tokenexchange.TokenPath && r.FormValue("grant_type") == tokenexchange.GrantType:
			select {
			case <-r.Context().Done():
				return
			case <-time.After(p.exchangeDelay):
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "fault_access", "token_type": "Bearer", "expires_in": 1,
				"refresh_token": "fault_refresh", "scope": "read write", "issued_token_type": tokenexchange.TokenTypeAccess})
		case r.URL.Path == tokenexchange.TokenPath && r.FormValue("grant_type") == tokenexchange.GrantRefresh:
			p.refreshes.Add(1)
			w.WriteHeader(p.refreshStatus)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid_client", "error_description": "The app's secret was changed."})
		case r.URL.Path == "/api/v1/auth/me":
			// Armature names the person under principal, Stator at the top.
			_ = json.NewEncoder(w).Encode(map[string]any{"principal": map[string]any{"user": me}, "user": me})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	p.url = srv.URL
	return p
}

// faultyService is one partner's service as cmd/api builds it, pointed at a faultyPartner on this process's loopback.
type faultyService struct {
	name, partner string
	connect       func(t *testing.T, h *harness, owner *client, p *faultyPartner)
	exchange      func(ctx context.Context, user uuid.UUID) db.LSN
	viewer        func(ctx context.Context) (string, error)
}

func faultyServices(t *testing.T, h *harness) []faultyService {
	t.Helper()
	box, err := secret.New(testSecretKey(t))
	if err != nil {
		t.Fatal(err)
	}
	loopback := netguard.ParseAllow("127.0.0.0/8")
	arm := armature.NewService(h.cluster, box, armature.NewClient(loopback, nil), nil,
		armature.Options{AppURL: "http://" + appHost, Allow: loopback, Development: true, TokenExchange: true, Log: discard()})
	stat := stator.NewService(h.cluster, box, stator.NewClient(loopback, nil), nil,
		stator.Options{AppURL: "http://" + appHost, Allow: loopback, Development: true, TokenExchange: true, Log: discard()})
	app := map[string]any{"clientId": "exciter-faults", "clientSecret": exchangeSecret}
	return []faultyService{
		{
			name: "Armature", partner: tokenexchange.PartnerArmature,
			connect: func(t *testing.T, h *harness, owner *client, p *faultyPartner) {
				want(t, owner.put(t, "/api/v1/armature/connection", merged(app, "baseUrl", armatureURL(t))), http.StatusOK, "connect Armature as the app")
				// The address is changed apart from what it offers, which a new address forgets.
				h.cleanupExec(t, h.super, `UPDATE armature_connection SET base_url = $1`, p.url)
				h.cleanupExec(t, h.super, `UPDATE armature_connection SET capabilities = ARRAY[$1]`, tokenexchange.Capability)
			},
			exchange: func(ctx context.Context, user uuid.UUID) db.LSN { return arm.ExchangeAtSignIn(ctx, user, "subject") },
			viewer: func(ctx context.Context) (string, error) {
				_, status, err := arm.Viewer(ctx)
				return string(status), err
			},
		},
		{
			name: "Stator", partner: tokenexchange.PartnerStator,
			connect: func(t *testing.T, h *harness, owner *client, p *faultyPartner) {
				statorOffersItsOwn(t)
				want(t, owner.put(t, "/api/v1/stator/connection", merged(app, "baseUrl", statorURL(t))), http.StatusOK, "connect Stator as the app")
				h.cleanupExec(t, h.super, `UPDATE stator_connection SET base_url = $1`, p.url)
				h.cleanupExec(t, h.super, `UPDATE stator_connection SET token_exchange = true`)
			},
			exchange: func(ctx context.Context, user uuid.UUID) db.LSN { return stat.ExchangeAtSignIn(ctx, user, "subject") },
			viewer: func(ctx context.Context) (string, error) {
				_, status, err := stat.Viewer(ctx)
				return string(status), err
			},
		},
	}
}

func TestASignInWhoseExchangeTimesOutSaysSo(t *testing.T) {
	h := newHarness(t)
	h.keepArmature(t)
	h.keepStator(t)
	api := newAPIServer(t, h)
	owner := api.as(t, h.owner(t, "exchange-timeout").user)
	for _, svc := range faultyServices(t, h) {
		t.Run(svc.name, func(t *testing.T) {
			p := newFaultyPartner(t, tokenexchange.SignInTimeout+2*time.Second, http.StatusUnauthorized)
			svc.connect(t, h, owner, p)
			user := h.addPerson(t, "member")
			svc.exchange(context.Background(), user)
			var message string
			if err := h.super.QueryRow(context.Background(), `SELECT message FROM partner_exchange WHERE partner = $1 AND user_id = $2`,
				svc.partner, user).Scan(&message); err != nil {
				t.Fatalf("no outcome of the exchange was kept: %v", err)
			}
			if !strings.Contains(message, "did not answer within") || !strings.HasPrefix(message, svc.name+" ") {
				t.Errorf("the timed out exchange is kept as %q", message)
			}
		})
	}
}
