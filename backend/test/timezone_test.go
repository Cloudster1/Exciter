//go:build integration

package test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// The time zone is the person's, chosen beside the language: answered with
// the session, handed back to the browser, and held by the database to the
// zones it knows.
func TestProfileTimezone(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	person := h.addPerson(t, "member")
	me := api.as(t, person)
	zone := func(r response) any { return obj(t, r, "user")["timezone"] }
	locale := func(r response) any { return obj(t, r, "user")["locale"] }

	t.Run("nobody has chosen until they do, so the browser decides", func(t *testing.T) {
		if got := zone(want(t, me.get(t, "/api/v1/auth/me"), http.StatusOK, "read me")); got != "" {
			t.Fatalf("a new account's zone is %v, want empty", got)
		}
	})

	t.Run("a zone and a language are saved together and kept for the next request", func(t *testing.T) {
		r := want(t, me.patch(t, "/api/v1/auth/me", map[string]any{"locale": "de", "timezone": "Europe/Berlin"}), http.StatusOK, "save both")
		if got := zone(r); got != "Europe/Berlin" {
			t.Fatalf("the answer says %v, want Europe/Berlin", got)
		}
		if got := zone(want(t, me.get(t, "/api/v1/auth/me"), http.StatusOK, "read me again")); got != "Europe/Berlin" {
			t.Fatalf("the next request says %v, want Europe/Berlin", got)
		}
		if got := zone(want(t, me.patch(t, "/api/v1/auth/me", map[string]any{"locale": "en"}), http.StatusOK, "change the language alone")); got != "Europe/Berlin" {
			t.Fatalf("a body without a zone changed it to %v", got)
		}
	})

	t.Run("a zone nobody knows is refused with a sentence, and the language beside it is not saved alone", func(t *testing.T) {
		r := want(t, me.patch(t, "/api/v1/auth/me", map[string]any{"locale": "de", "timezone": "Mars/Olympus"}), http.StatusUnprocessableEntity, "choose Mars")
		if code := errorCode(t, r); code != "validation_failed" {
			t.Fatalf("code %q", code)
		}
		r = want(t, me.get(t, "/api/v1/auth/me"), http.StatusOK, "read me")
		if got := zone(r); got != "Europe/Berlin" {
			t.Fatalf("a refused choice left %v", got)
		}
		if got := locale(r); got != "en" {
			t.Fatalf("a refused save changed the language to %v", got)
		}
	})

	t.Run("an empty choice hands the zone back to the browser", func(t *testing.T) {
		if got := zone(want(t, me.patch(t, "/api/v1/auth/me", map[string]any{"timezone": ""}), http.StatusOK, "follow the browser")); got != "" {
			t.Fatalf("the answer says %v, want empty", got)
		}
		var stored *string
		if err := h.super.QueryRow(context.Background(), `SELECT timezone FROM app_user WHERE id = $1`, person).Scan(&stored); err != nil {
			t.Fatal(err)
		}
		if stored != nil {
			t.Fatalf("the row holds %q, want null", *stored)
		}
	})

	t.Run("the database refuses a zone the service would", func(t *testing.T) {
		_, err := h.super.Exec(context.Background(), `UPDATE app_user SET timezone = 'Mars/Olympus' WHERE id = $1`, person)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) {
			t.Fatalf("storing Mars/Olympus = %v, want the database to refuse it", err)
		}
	})
}
