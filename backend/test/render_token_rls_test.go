//go:build integration

package test

import (
	"context"
	"testing"
)

// Straight through SQL as exciter_app: a print's token reads only, lasts minutes and reaches every project its
// owner does, so no row can pass for one and outlive its print or reach past a limited token.
func TestRawSQLGuardsRenderTokens(t *testing.T) {
	h := newHarness(t)
	person := h.owner(t, "render-tokens")
	conn := h.appConn(t, person.user)
	ctx := context.Background()
	insert := func(scopes, lasts string, projectsOnly bool) error {
		_, err := conn.Exec(ctx, `
			INSERT INTO api_token (user_id, name, token_hash, scopes, expires_at, projects_only, for_render)
			VALUES ($1, 'PDF export', decode(md5(random()::text) || md5(random()::text), 'hex'), $2::text[], now() + $3::interval, $4, true)`,
			person.user, scopes, lasts, projectsOnly)
		return err
	}
	if err := insert("{read}", "2 minutes", false); err != nil {
		t.Fatalf("a print's token as the api makes it: %v", err)
	}
	for what, err := range map[string]error{
		"one that may write":        insert("{}", "2 minutes", false),
		"one that lasts an hour":    insert("{read}", "1 hour", false),
		"one held to some projects": insert("{read}", "2 minutes", true),
	} {
		if !isCheckViolation(err) {
			t.Errorf("%s was taken for a print's token: %v", what, err)
		}
	}
	// The app may not change a token at all, which refuses this before the check would.
	if _, err := conn.Exec(ctx, `UPDATE api_token SET expires_at = now() + interval '1 day' WHERE user_id = $1 AND for_render`, person.user); err == nil {
		t.Error("a print's token was made to outlive its print")
	}
}
