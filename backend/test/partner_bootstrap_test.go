//go:build integration

package test

import (
	"context"
	"testing"

	"github.com/cloudster1/exciter/backend/internal/stator"
)

// The bootstrap connects a partner the chart names (#104): before it answers too, and never over a connection that
// is already there.

type connectFirst func(ctx context.Context, baseURL, ip string) (bool, error)

// connectsOnce runs the bootstrap's connect against a partner's table: silent first, then over that, then answering.
// asked is the SQL that says the partner was asked what it offers.
func connectsOnce(t *testing.T, h *harness, connect connectFirst, table, asked, silent, answering string) {
	t.Helper()
	boss := h.owner(t, "boot-"+table)
	saved := func() (base string, offered bool) {
		t.Helper()
		if err := h.super.QueryRow(context.Background(), `SELECT base_url, `+asked+` FROM `+table).Scan(&base, &offered); err != nil {
			t.Fatalf("read %s: %v", table, err)
		}
		return base, offered
	}

	if made, err := connect(boss.ctx, silent, ""); err != nil || !made {
		t.Fatalf("connecting a partner that does not answer yet = %t, %v; want it connected", made, err)
	}
	if base, asked := saved(); base != silent || asked {
		t.Errorf("after connecting a silent partner the row says %s, asked %t", base, asked)
	}
	if made, err := connect(boss.ctx, answering, ""); err != nil || made {
		t.Errorf("connecting over a connection = %t, %v; want it left alone", made, err)
	}
	if base, _ := saved(); base != silent {
		t.Errorf("a second bootstrap moved the connection to %s", base)
	}

	h.cleanupExec(t, h.super, `DELETE FROM `+table)
	h.settle(t)
	if made, err := connect(boss.ctx, answering, ""); err != nil || !made {
		t.Fatalf("connecting a partner that answers = %t, %v", made, err)
	}
	if base, asked := saved(); base != answering || !asked {
		t.Errorf("after connecting an answering partner the row says %s, asked %t", base, asked)
	}
}

func TestTheBootstrapConnectsArmatureOnce(t *testing.T) {
	h := newHarness(t)
	h.keepArmature(t)
	connectsOnce(t, h, h.armature(t).ConnectFirst, "armature_connection", "capabilities IS NOT NULL", "http://armature-stub:9", armatureURL(t))
}

func TestTheBootstrapConnectsStatorOnce(t *testing.T) {
	h := newHarness(t)
	h.keepStator(t)
	statorOffers(t, stator.RemoteLinksCapability)
	connectsOnce(t, h, h.stator(t).ConnectFirst, "stator_connection", "remote_links", "http://stator-stub:9", statorURL(t))
}
