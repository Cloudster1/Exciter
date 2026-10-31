package httpapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cloudster1/exciter/backend/internal/config"
	"github.com/cloudster1/exciter/backend/internal/oidc"
)

// The cookie binding a sign-in to its browser goes only to the callback the browser really reaches, Secure as the
// session cookie is, and lasts as long as the sign-in may.
func TestTheBrowserBindingReachesOnlyTheCallback(t *testing.T) {
	for _, c := range []struct {
		name, callback, path string
		secure               bool
	}{
		{"served at the root", "https://exciter.example" + config.OIDCCallbackPath, config.OIDCCallbackPath, true},
		{"served under a proxy's prefix", "https://suite.example/exciter" + config.OIDCCallbackPath, "/exciter" + config.OIDCCallbackPath, true},
		{"in local development", "http://localhost:5173" + config.OIDCCallbackPath, config.OIDCCallbackPath, false},
		{"with no callback address", "", config.OIDCCallbackPath, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := &Server{CookieName: config.DefaultSessionCookie, Secure: c.secure, OIDCCallbackURL: c.callback}
			w := httptest.NewRecorder()
			s.setOIDCBrowserCookie(w, "the-secret")
			cookies := w.Result().Cookies()
			if len(cookies) != 1 {
				t.Fatalf("set %d cookies", len(cookies))
			}
			got := cookies[0]
			if got.Name != config.DefaultSessionCookie+oidcBrowserCookieSuffix || got.Value != "the-secret" || got.Path != c.path ||
				!got.HttpOnly || got.Secure != c.secure || got.SameSite != http.SameSiteLaxMode ||
				got.MaxAge != int(oidc.HowLongALoginMayTake.Seconds()) {
				t.Fatalf("cookie = %+v", got)
			}

			req := httptest.NewRequest(http.MethodGet, c.path, nil)
			req.AddCookie(got)
			if s.oidcBrowserBinding(req) != "the-secret" {
				t.Fatal("the callback does not read the secret back")
			}
			if s.oidcBrowserBinding(httptest.NewRequest(http.MethodGet, c.path, nil)) != "" {
				t.Fatal("a browser holding no cookie reads as holding one")
			}

			w = httptest.NewRecorder()
			s.clearOIDCBrowserCookie(w)
			if cleared := w.Result().Cookies(); len(cleared) != 1 || cleared[0].MaxAge >= 0 || cleared[0].Path != c.path || cleared[0].Value != "" {
				t.Fatalf("clearing set %+v", cleared)
			}
		})
	}
}

func TestACallbackInAnotherBrowserIsExplained(t *testing.T) {
	if got := signInFailure(oidc.ErrOtherBrowser); got != "other_browser" {
		t.Fatalf("signInFailure = %q", got)
	}
	if got := signInFailure(errors.Join(errors.New("wrapped"), oidc.ErrOtherBrowser)); got != "other_browser" {
		t.Fatalf("a wrapped refusal = %q", got)
	}
}
