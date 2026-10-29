package main

import (
	"context"
	"log/slog"
	"testing"

	"github.com/cloudster1/exciter/backend/internal/config"
)

func TestEveryCommandIsRead(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want mode
	}{
		{nil, development},
		{[]string{"demo"}, withDemo},
		{[]string{"bootstrap"}, bootstrap},
	} {
		if got, err := parseMode(tc.args); err != nil || got != tc.want {
			t.Errorf("parseMode(%q) = %v, %v; want %v", tc.args, got, err, tc.want)
		}
	}
}

func TestAnUnknownCommandIsRefused(t *testing.T) {
	_, err := parseMode([]string{"grow"})
	want := `unknown command "grow"; run seed bare or seed demo on a development stack, or seed bootstrap`
	if err == nil || err.Error() != want {
		t.Fatalf("parseMode(grow) = %v, want %q", err, want)
	}
}

func TestOnlyTheBootstrapRunsInProduction(t *testing.T) {
	production := config.Config{Env: config.EnvProduction}
	production.Bootstrap.AdminEmail = "root@acme.example"
	const refusal = "bare, seed is for a development stack; in production run seed bootstrap, which asks for a first administrator"
	for _, m := range []mode{development, withDemo} {
		if err := m.allowed(production); err == nil || err.Error() != refusal {
			t.Errorf("mode %v in production = %v, want %q", m, err, refusal)
		}
	}
	if err := bootstrap.allowed(production); err != nil {
		t.Errorf("seed bootstrap in production with an administrator = %v, want it allowed", err)
	}
	for _, m := range []mode{development, withDemo} {
		if err := m.allowed(config.Config{Env: config.EnvDevelopment}); err != nil {
			t.Errorf("mode %v on a development stack = %v, want it allowed", m, err)
		}
	}
}

func TestTheBootstrapNeedsAnAdministrator(t *testing.T) {
	const refusal = "seed bootstrap needs EXCITER_BOOTSTRAP_ADMIN_EMAIL and EXCITER_BOOTSTRAP_ADMIN_PASSWORD: without them nobody could sign in to the installation it makes"
	for _, env := range []string{config.EnvProduction, config.EnvDevelopment} {
		if err := bootstrap.allowed(config.Config{Env: env}); err == nil || err.Error() != refusal {
			t.Errorf("seed bootstrap in %s without an administrator = %v, want %q", env, err, refusal)
		}
	}
}

// Without an administrator in the context nobody may connect a partner, and the refusal comes before any call.
func TestPartnersNeedAnAdministrator(t *testing.T) {
	const refusal = "EXCITER_BOOTSTRAP_ARMATURE_URL and EXCITER_BOOTSTRAP_STATOR_URL need EXCITER_BOOTSTRAP_ADMIN_EMAIL: only an administrator may connect a partner"
	log := slog.New(slog.DiscardHandler)
	for _, tc := range []struct{ armature, stator string }{
		{"https://armature.example.com", ""},
		{"", "https://wiki.example.com"},
	} {
		var cfg config.Config
		cfg.Bootstrap.ArmatureURL, cfg.Bootstrap.StatorURL = tc.armature, tc.stator
		if err := connectPartners(context.Background(), cfg, nil, nil, log); err == nil || err.Error() != refusal {
			t.Errorf("connectPartners(%+v) without an administrator = %v, want %q", tc, err, refusal)
		}
	}
	if err := connectPartners(context.Background(), config.Config{}, nil, nil, log); err != nil {
		t.Errorf("connectPartners with no partner named = %v, want nothing to do", err)
	}
}
