package oauth

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestAPartnerAsksForReadAndAtMostWhatItHolds(t *testing.T) {
	both := []string{ScopeRead, ScopeWrite}
	for _, c := range []struct {
		asked    string
		allowed  []string
		readOnly bool
		ok       bool
	}{
		{"", both, false, true},
		{"", []string{ScopeRead}, true, true},
		{"read", both, true, true},
		{"read write", both, false, true},
		{"write read", both, false, true},
		{"read write", []string{ScopeRead}, false, false},
		{"write", both, false, false},
		{"mcp", both, false, false},
		{"read mcp", both, false, false},
	} {
		readOnly, ok := partnerScopeAsked(c.asked, c.allowed)
		if ok != c.ok || (ok && readOnly != c.readOnly) {
			t.Errorf("%q of %v = read only %t, ok %t; want %t, %t", c.asked, c.allowed, readOnly, ok, c.readOnly, c.ok)
		}
	}
}

func TestAPartnerScopeIsNamedInItsOwnWords(t *testing.T) {
	if got := (scope{partner: true, readOnly: true}).name(); got != "read" {
		t.Errorf("a reading partner's scope is %q", got)
	}
	if got := (scope{partner: true}).name(); got != "read write" {
		t.Errorf("a writing partner's scope is %q", got)
	}
	if got := (scope{readOnly: true}).name(); got != ScopeMCPRead {
		t.Errorf("an assistant's reading scope is %q", got)
	}
}

func TestAPartnerAppIsHeldToTheRules(t *testing.T) {
	text := func(s string) *string { return &s }
	app, err := cleanPartner(PartnerAppInput{Name: text("  Stator "), KeycloakClientID: text(" stator ")}, nil)
	if err != nil || app.Name != "Stator" || app.KeycloakClientID != "stator" || !slices.Equal(app.Scopes, []string{ScopeRead}) {
		t.Fatalf("a new app = %+v, %v; want it trimmed, reading only", app, err)
	}
	changed, err := cleanPartner(PartnerAppInput{Scopes: []string{"write", "read", "read"}}, &app)
	if err != nil || changed.Name != "Stator" || !slices.Equal(changed.Scopes, []string{ScopeRead, ScopeWrite}) {
		t.Errorf("a change of scopes alone = %+v, %v; want the name kept and the scopes sorted once each", changed, err)
	}
	for name, c := range map[string]struct {
		in    PartnerAppInput
		field string
	}{
		"no name":         {PartnerAppInput{}, "name"},
		"a long name":     {PartnerAppInput{Name: text(strings.Repeat("n", MaxClientNameLength+1))}, "name"},
		"a bad address":   {PartnerAppInput{Name: text("Stator"), ClientURI: text("ftp://stator")}, "clientUri"},
		"a long client":   {PartnerAppInput{Name: text("Stator"), KeycloakClientID: text(strings.Repeat("k", MaxKeycloakClientIDLength+1))}, "keycloakClientId"},
		"write alone":     {PartnerAppInput{Name: text("Stator"), Scopes: []string{"write"}}, "scopes"},
		"an unknown one":  {PartnerAppInput{Name: text("Stator"), Scopes: []string{"read", "mcp"}}, "scopes"},
		"no scope at all": {PartnerAppInput{Name: text("Stator"), Scopes: []string{}}, "scopes"},
	} {
		var field *FieldError
		if _, err := cleanPartner(c.in, nil); !errors.As(err, &field) || field.Field != c.field {
			t.Errorf("%s: %v, want a refusal of %s", name, err, c.field)
		}
	}
}
