//go:build integration

package test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/armature"
	"github.com/cloudster1/exciter/backend/internal/theme"
)

// Following the Armature theme (#30), Stator's #34: the theme the stub shows a person is
// kept as their mirror and answered by GET /themes/active.

func activeTheme(t *testing.T, c *client) (map[string]any, string) {
	t.Helper()
	r := want(t, c.get(t, "/api/v1/themes/active"), http.StatusOK, "the active theme")
	seen, _ := r.Body["theme"].(map[string]any)
	source, _ := r.Body["source"].(string)
	return seen, source
}

func themeFollow(t *testing.T, c *client) map[string]any {
	t.Helper()
	return obj(t, want(t, c.get(t, "/api/v1/armature/theme"), http.StatusOK, "the follow"), "follow")
}

func TestAPersonFollowsTheThemeArmatureShowsThem(t *testing.T) {
	h := newHarness(t)
	h.keepArmature(t)
	api := newAPIServer(t, h)
	home := h.owner(t, "arm-theme")
	slug := stubTenant(t)
	stubControl(t, http.MethodDelete, slug, nil)
	owner := api.as(t, home.user)
	bobID := h.addPerson(t, "member")
	bob := api.as(t, bobID)
	t.Cleanup(func() { stubControl(t, http.MethodDelete, slug, nil) })
	setTheme := func(person, key string, broken bool) {
		t.Helper()
		stubControl(t, http.MethodPut, slug+"/people/"+person+"/theme", map[string]any{"theme": key, "broken": broken})
	}
	mirrors := func(user uuid.UUID) int {
		return h.countRows(t, `SELECT count(*) FROM theme WHERE owner_id = $1 AND armature_theme_id IS NOT NULL`, user)
	}

	if f := themeFollow(t, owner); f["following"] != false || f["status"] != "not_configured" {
		t.Errorf("before a connection the follow is %v", f)
	}
	if code := errorCode(t, want(t, owner.put(t, "/api/v1/armature/theme", nil), http.StatusConflict, "follow without a connection")); code != "armature_not_configured" {
		t.Errorf("code %s", code)
	}
	want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": armatureURL(t)}), http.StatusOK, "connect Armature")
	if code := errorCode(t, want(t, owner.put(t, "/api/v1/armature/theme", nil), http.StatusConflict, "follow without a token")); code != "armature_not_connected" {
		t.Errorf("code %s", code)
	}
	want(t, owner.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "alice")}), http.StatusOK, "the owner connects")
	want(t, bob.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "bob")}), http.StatusOK, "bob connects")
	h.settle(t)

	own := obj(t, want(t, owner.post(t, "/api/v1/themes", map[string]any{"name": "Deep-Tech"}), http.StatusCreated, "a theme of her own"), "theme")
	want(t, owner.put(t, "/api/v1/themes/active", map[string]any{"themeId": own["id"]}), http.StatusOK, "choose her own")
	setTheme("alice", "deep-tech", false)

	var mirrorID string
	t.Run("following replaces her choice with a copy of Armature's theme", func(t *testing.T) {
		f := obj(t, want(t, owner.put(t, "/api/v1/armature/theme", nil), http.StatusOK, "follow"), "follow")
		if f["following"] != true || f["status"] != "ok" || f["error"] != "" {
			t.Fatalf("following answers %v", f)
		}
		seen, source := activeTheme(t, owner)
		if source != "armature" || seen == nil || seen["name"] != "Deep-Tech" || seen["id"] == own["id"] {
			t.Fatalf("while following the active theme is %v from %q", seen, source)
		}
		mirrorID = seen["id"].(string)
		if mirrors(home.user) != 1 {
			t.Errorf("%d mirrors, want 1", mirrors(home.user))
		}
		for _, each := range list(t, want(t, owner.get(t, "/api/v1/themes"), http.StatusOK, "list"), "themes") {
			if each.(map[string]any)["id"] == mirrorID {
				t.Error("the mirror is listed among her themes")
			}
		}
		errorCode(t, want(t, owner.get(t, "/api/v1/themes/"+mirrorID), http.StatusNotFound, "read the mirror as a theme"))
		errorCode(t, want(t, owner.patch(t, "/api/v1/themes/"+mirrorID, map[string]any{"shared": true}), http.StatusNotFound, "share the mirror"))
		errorCode(t, want(t, owner.get(t, "/api/v1/themes/"+mirrorID+"/export"), http.StatusNotFound, "export the mirror"))
		errorCode(t, want(t, owner.delete(t, "/api/v1/themes/"+mirrorID), http.StatusNotFound, "delete the mirror"))
		errorCode(t, want(t, owner.put(t, "/api/v1/themes/active", map[string]any{"themeId": mirrorID}), http.StatusNotFound, "choose the mirror"))
		if seen, _ := activeTheme(t, bob); seen != nil && seen["id"] == mirrorID {
			t.Error("bob sees alice's mirror")
		}
	})

	t.Run("the database keeps the mirror hers, private, and never a choice", func(t *testing.T) {
		conn := h.appConn(t, home.user)
		for _, tc := range []struct{ name, sql, arg string }{
			{"share it", `UPDATE theme SET shared = true WHERE id = $1`, mirrorID},
			{"choose it", `UPDATE user_theme SET theme_id = $1, follow_armature = false WHERE user_id = '` + home.user.String() + `'`, mirrorID},
			{"follow and choose at once", `UPDATE user_theme SET theme_id = $1 WHERE user_id = '` + home.user.String() + `'`, own["id"].(string)},
		} {
			if _, err := conn.Exec(context.Background(), tc.sql, tc.arg); !isCheckViolation(err) {
				t.Errorf("%s through SQL as exciter_app: %v, want a check violation", tc.name, err)
			}
		}
	})

	t.Run("a change in Armature shows once the cache runs out or the settings ask", func(t *testing.T) {
		setTheme("alice", "constellation", false)
		if seen, _ := activeTheme(t, owner); seen["id"] != mirrorID {
			t.Errorf("within the cache's time the theme changed to %v", seen["name"])
		}
		if f := themeFollow(t, owner); f["status"] != "ok" {
			t.Errorf("the settings read %v", f)
		}
		seen, source := activeTheme(t, owner)
		if source != "armature" || seen["name"] != "Constellation" || seen["id"] == mirrorID {
			t.Errorf("after the settings asked the theme is %v from %q", seen["name"], source)
		}
		if mirrors(home.user) != 1 {
			t.Errorf("%d mirrors after a change, want the new one alone", mirrors(home.user))
		}

		setTheme("alice", "", false)
		themeFollow(t, owner)
		if seen, source := activeTheme(t, owner); seen != nil || source != "armature" {
			t.Errorf("with Armature's built-in theme the answer is %v from %q", seen, source)
		}
	})

	t.Run("a slow Armature falls back to what she would see without following", func(t *testing.T) {
		setTheme("alice", "deep-tech", false)
		themeFollow(t, owner)
		stubControl(t, http.MethodPut, slug+"/people/alice/theme-delay", map[string]any{"ms": 3000})
		defer stubControl(t, http.MethodPut, slug+"/people/alice/theme-delay", map[string]any{"ms": 0})
		var tokenID uuid.UUID
		if err := h.super.QueryRow(context.Background(), `SELECT id FROM armature_token WHERE user_id = $1`, home.user).Scan(&tokenID); err != nil {
			t.Fatal(err)
		}
		if err := h.valkey(t).Del(context.Background(), armature.ThemeKey(tokenID)).Err(); err != nil {
			t.Fatal(err)
		}
		started := time.Now()
		seen, source := activeTheme(t, owner)
		if took := time.Since(started); took >= armature.ThemeTimeout+time.Second {
			t.Errorf("the active theme took %s with a slow Armature", took)
		}
		if seen != nil || source != "" {
			t.Errorf("the fallback is %v from %q, want the built-in theme", seen, source)
		}
		if f := themeFollow(t, owner); f["following"] != true || f["status"] != "unreachable" {
			t.Errorf("the settings read %v", f)
		}
	})

	t.Run("a theme that fails the checks is refused, and the follower falls back", func(t *testing.T) {
		setTheme("alice", "constellation", true)
		f := themeFollow(t, owner)
		if f["following"] != true || f["status"] != "ok" || f["error"] == "" {
			t.Fatalf("with a broken theme the settings read %v", f)
		}
		if seen, source := activeTheme(t, owner); seen != nil || source != "" {
			t.Errorf("with a broken theme the active one is %v from %q", seen, source)
		}

		setTheme("bob", "deep-tech", true)
		if code := errorCode(t, want(t, bob.put(t, "/api/v1/armature/theme", nil), http.StatusUnprocessableEntity, "bob follows a broken theme")); code != "armature_theme_invalid" {
			t.Errorf("code %s", code)
		}
		if f := themeFollow(t, bob); f["following"] != false {
			t.Errorf("a refused follow was kept: %v", f)
		}
		if mirrors(bobID) != 0 {
			t.Error("a refused theme left a mirror behind")
		}
	})

	t.Run("choosing a theme ends following, and so does stopping", func(t *testing.T) {
		setTheme("alice", "deep-tech", false)
		themeFollow(t, owner)
		want(t, owner.put(t, "/api/v1/themes/active", map[string]any{"themeId": own["id"]}), http.StatusOK, "choose her own again")
		if f := themeFollow(t, owner); f["following"] != false {
			t.Errorf("after choosing she still follows: %v", f)
		}
		if seen, source := activeTheme(t, owner); source != "chosen" || seen["id"] != own["id"] {
			t.Errorf("after choosing the theme is %v from %q", seen, source)
		}
		if mirrors(home.user) != 0 {
			t.Error("the mirror outlived following")
		}

		want(t, owner.put(t, "/api/v1/armature/theme", nil), http.StatusOK, "follow again")
		want(t, owner.delete(t, "/api/v1/armature/theme"), http.StatusNoContent, "stop following")
		if f := themeFollow(t, owner); f["following"] != false {
			t.Errorf("after stopping she still follows: %v", f)
		}
		if seen, source := activeTheme(t, owner); seen != nil || source != "" {
			t.Errorf("after stopping the theme is %v from %q, want the default for everyone", seen, source)
		}
		if mirrors(home.user) != 0 {
			t.Error("the mirror outlived following")
		}
	})

	// A copy takes several writes; a request that hangs up between them, or a
	// reader that arrives between them, once left the page in the built-in theme (#200).
	t.Run("a mirror still being copied is neither shown nor removed by another copy", func(t *testing.T) {
		setTheme("alice", "deep-tech", false)
		want(t, owner.put(t, "/api/v1/armature/theme", nil), http.StatusOK, "follow")
		complete, _ := activeTheme(t, owner)
		if complete == nil {
			t.Fatal("following shows no theme")
		}
		var halfID uuid.UUID
		if err := h.appConn(t, home.user).QueryRow(context.Background(), `
			INSERT INTO theme (owner_id, name, shared, spec, armature_theme_id, armature_updated_at)
			VALUES ($1, 'Half a copy', false, '{}', $2, $3) RETURNING id`, home.user, uuid.New(), theme.MirrorPending).Scan(&halfID); err != nil {
			t.Fatal(err)
		}
		halfLeft := func() int {
			return h.countRows(t, `SELECT count(*) FROM theme WHERE id = $1`, halfID)
		}

		if seen, _ := activeTheme(t, owner); seen == nil || seen["id"] != complete["id"] {
			t.Errorf("with a copy under way the active theme is %v, want the complete mirror", seen)
		}
		setTheme("alice", "constellation", false)
		themeFollow(t, owner)
		if seen, _ := activeTheme(t, owner); seen == nil || seen["name"] != "Constellation" {
			t.Errorf("after a change the active theme is %v", seen)
		}
		if halfLeft() != 1 {
			t.Error("a finished copy removed one still under way")
		}
		want(t, owner.delete(t, "/api/v1/armature/theme"), http.StatusNoContent, "stop following")
		if mirrors(home.user) != 0 {
			t.Error("stopping left a copy under way behind")
		}
	})

	t.Run("a read-only token reads the follow and changes nothing", func(t *testing.T) {
		_, readOnly := makeToken(t, bob, map[string]any{"name": "theme-reader", "scopes": []string{"read"}})
		want(t, api.withToken(bobID, readOnly).get(t, "/api/v1/armature/theme"), http.StatusOK, "a read-only token reads")
		errorCode(t, want(t, api.withToken(bobID, readOnly).delete(t, "/api/v1/armature/theme"), http.StatusForbidden, "a read-only token stops following"))
		errorCode(t, want(t, api.anonymous().get(t, "/api/v1/armature/theme"), http.StatusUnauthorized, "nobody signed in reads the follow"))
	})
}
