//go:build integration

package test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/objectstore"
)

// themeFixture is an organization with an owner, a plain member, an administrator and somebody from elsewhere,
// and a private theme of the owner's.
type themeFixture struct {
	h                            *harness
	api                          *apiServer
	home, away                   member
	owner, other, admin, outside *client
	themeID                      string
}

var magentaSpec = map[string]any{
	"colors": map[string]any{"light": map[string]string{"accent": "#ff0066"}, "dark": map[string]string{"accent": "#00ffcc"}},
	"shape":  map[string]any{"radiusControl": 2},
	"css":    "[data-rail] { opacity: .95 }",
}

func newThemeFixture(t *testing.T) *themeFixture {
	t.Helper()
	h := newHarness(t)
	f := &themeFixture{h: h, api: newAPIServer(t, h), home: h.makeMember(t, "themes"), away: h.makeMember(t, "themes-away")}
	f.owner = f.api.as(t, f.home.user, f.home.org)
	f.other = f.api.as(t, h.addPerson(t, f.home.org, "member"), f.home.org)
	f.admin = f.api.as(t, h.addPerson(t, f.home.org, "admin"), f.home.org)
	f.outside = f.api.as(t, f.away.user, f.away.org)
	made := want(t, f.owner.post(t, "/api/v1/themes", map[string]any{"name": "Magenta", "spec": magentaSpec}), http.StatusCreated, "make a theme")
	theme := obj(t, made, "theme")
	f.themeID = theme["id"].(string)
	if theme["shared"] != false || theme["active"] != false || theme["inUse"].(float64) != 0 || theme["ownerId"] != f.home.user.String() {
		t.Fatalf("a new theme is the maker's, private, unchosen and unused: %s", made.Raw)
	}
	if obj(t, made, "theme", "spec", "colors", "light")["accent"] != "#ff0066" {
		t.Fatalf("the spec did not come back: %s", made.Raw)
	}
	return f
}

// refuse fails unless the answer is the refusal named.
func refuse(t *testing.T, what string, got response, status int, code string) {
	t.Helper()
	if got.Status != status {
		t.Errorf("%s: got %d, want %d: %s", what, got.Status, status, got.Raw)
		return
	}
	if c := errorCode(t, got); c != code {
		t.Errorf("%s: code %q, want %q", what, c, code)
	}
}

// A theme is its maker's, shared when they say so, chosen per person, and walled by the tenant like everything else.
func TestThemesOverTheAPI(t *testing.T) {
	f := newThemeFixture(t)
	owner, other, admin, outside := f.owner, f.other, f.admin, f.outside
	themeID := f.themeID

	t.Run("the shipped examples are offered, and one saves as a theme of one's own", func(t *testing.T) {
		examples := list(t, want(t, owner.get(t, "/api/v1/themes/examples"), http.StatusOK, "examples"), "examples")
		if len(examples) != 2 {
			t.Fatalf("want Deep-Tech and Constellation: %v", examples)
		}
		first, second := examples[0].(map[string]any), examples[1].(map[string]any)
		if first["key"] != "deep-tech" || first["name"] != "Deep-Tech" || second["key"] != "constellation" {
			t.Fatalf("the examples are %v and %v", first["name"], second["name"])
		}
		saved := want(t, owner.post(t, "/api/v1/themes", map[string]any{"name": "My Constellation", "spec": second["spec"]}), http.StatusCreated, "save an example")
		if obj(t, saved, "theme", "spec")["effect"] != "constellation" || obj(t, saved, "theme", "spec", "colors", "dark")["accent"] != "#4fd1c5" {
			t.Fatalf("the example did not survive saving: %s", saved.Raw)
		}
		want(t, owner.delete(t, "/api/v1/themes/"+obj(t, saved, "theme")["id"].(string)), http.StatusNoContent, "tidy up")
	})

	t.Run("what is refused on the way in", func(t *testing.T) {
		nobody := f.api.anonymous()
		refuse(t, "no name", owner.post(t, "/api/v1/themes", map[string]any{"spec": magentaSpec}), http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "the same name twice", owner.post(t, "/api/v1/themes", map[string]any{"name": "magenta"}), http.StatusConflict, "conflict")
		refuse(t, "a colour that is a word", owner.post(t, "/api/v1/themes", map[string]any{"name": "Words", "spec": map[string]any{"colors": map[string]any{"light": map[string]string{"accent": "red"}}}}), http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "css that imports", owner.post(t, "/api/v1/themes", map[string]any{"name": "Importer", "spec": map[string]any{"css": "@import url(http://x)"}}), http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "an unknown field", owner.post(t, "/api/v1/themes", map[string]any{"name": "Typo", "colours": map[string]any{}}), http.StatusBadRequest, "bad_request")
		refuse(t, "css that loads elsewhere", owner.patch(t, "/api/v1/themes/"+themeID, map[string]any{"spec": map[string]any{"css": ".x{background:url(https://evil/p.png)}"}}), http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "a file the theme does not have", owner.patch(t, "/api/v1/themes/"+themeID, map[string]any{"spec": map[string]any{"backdrop": map[string]any{"assetId": uuid.NewString(), "fit": "cover"}}}), http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "choosing a theme that is not there", owner.put(t, "/api/v1/themes/active", map[string]any{"themeId": uuid.NewString()}), http.StatusNotFound, "not_found")
		refuse(t, "a theme that is not there", owner.get(t, "/api/v1/themes/"+uuid.NewString()), http.StatusNotFound, "not_found")
		refuse(t, "an id that is not one", owner.get(t, "/api/v1/themes/nonsense"), http.StatusBadRequest, "bad_request")
		refuse(t, "nobody signed in", nobody.get(t, "/api/v1/themes"), http.StatusUnauthorized, "unauthorized")
		refuse(t, "nobody signed in asking for the examples", nobody.get(t, "/api/v1/themes/examples"), http.StatusUnauthorized, "unauthorized")
		refuse(t, "nobody signed in asking what is active", nobody.get(t, "/api/v1/themes/active"), http.StatusUnauthorized, "unauthorized")
		refuse(t, "nobody signed in asking for the default", nobody.get(t, "/api/v1/themes/default"), http.StatusUnauthorized, "unauthorized")
		refuse(t, "nobody signed in importing", nobody.upload(t, "/api/v1/themes/import", "x.json", []byte("{}")), http.StatusUnauthorized, "unauthorized")
	})

	t.Run("choosing, and the active answer following", func(t *testing.T) {
		none := want(t, owner.get(t, "/api/v1/themes/active"), http.StatusOK, "nothing chosen")
		if none.Body["theme"] != nil || none.Body["source"] != "" {
			t.Fatalf("a fresh account has a theme: %s", none.Raw)
		}
		if obj(t, want(t, owner.put(t, "/api/v1/themes/active", map[string]any{"themeId": themeID}), http.StatusOK, "choose"), "theme")["active"] != true {
			t.Fatal("chosen but not active")
		}
		active := want(t, owner.get(t, "/api/v1/themes/active"), http.StatusOK, "read the active theme")
		if obj(t, active, "theme")["id"] != themeID || obj(t, active, "theme")["inUse"].(float64) != 1 || active.Body["source"] != "chosen" {
			t.Fatalf("the active theme is not the chosen one: %s", active.Raw)
		}
		if back := want(t, owner.put(t, "/api/v1/themes/active", map[string]any{"themeId": nil}), http.StatusOK, "back to the built-in"); back.Body["theme"] != nil {
			t.Fatalf("null did not return to the built-in theme: %s", back.Raw)
		}
		want(t, owner.put(t, "/api/v1/themes/active", map[string]any{"themeId": themeID}), http.StatusOK, "choose again")
	})

	t.Run("a private theme is nobody else's until shared, and then not theirs to change", func(t *testing.T) {
		if got := other.get(t, "/api/v1/themes/"+themeID); got.Status != http.StatusNotFound {
			t.Fatalf("a private theme was found by somebody else: %d", got.Status)
		}
		refuse(t, "somebody else changing a private theme", other.patch(t, "/api/v1/themes/"+themeID, map[string]any{"name": "Mine now"}), http.StatusNotFound, "not_found")
		if seen := list(t, want(t, other.get(t, "/api/v1/themes"), http.StatusOK, "list as another"), "themes"); len(seen) != 0 {
			t.Fatalf("somebody else sees %d themes before any is shared", len(seen))
		}
		want(t, owner.patch(t, "/api/v1/themes/"+themeID, map[string]any{"shared": true}), http.StatusOK, "share")
		seen := list(t, want(t, other.get(t, "/api/v1/themes"), http.StatusOK, "list as another"), "themes")
		if len(seen) != 1 || seen[0].(map[string]any)["ownerId"] != f.home.user.String() || seen[0].(map[string]any)["ownerName"] == "" {
			t.Fatalf("the shared theme is not seen with its owner: %v", seen)
		}
		refuse(t, "a member changing a shared theme", other.patch(t, "/api/v1/themes/"+themeID, map[string]any{"name": "Mine now"}), http.StatusForbidden, "forbidden")
		refuse(t, "a member deleting a shared theme", other.delete(t, "/api/v1/themes/"+themeID), http.StatusForbidden, "forbidden")
		renamed := want(t, admin.patch(t, "/api/v1/themes/"+themeID, map[string]any{"name": "Magenta Tidied"}), http.StatusOK, "an administrator tidies a shared theme")
		if obj(t, renamed, "theme")["name"] != "Magenta Tidied" {
			t.Fatalf("the administrator's rename did not stick: %s", renamed.Raw)
		}
		want(t, owner.patch(t, "/api/v1/themes/"+themeID, map[string]any{"name": "Magenta"}), http.StatusOK, "the owner renames it back")
		want(t, other.put(t, "/api/v1/themes/active", map[string]any{"themeId": themeID}), http.StatusOK, "they use it")
		if obj(t, want(t, owner.get(t, "/api/v1/themes/"+themeID), http.StatusOK, "read"), "theme")["inUse"].(float64) != 2 {
			t.Error("two people use it now")
		}
	})

	assetID := themeFiles(t, f)
	themeDefault(t, f)

	t.Run("another organization sees nothing", func(t *testing.T) {
		refuse(t, "another organization reading the theme", outside.get(t, "/api/v1/themes/"+themeID), http.StatusNotFound, "not_found")
		refuse(t, "another organization choosing the theme", outside.put(t, "/api/v1/themes/active", map[string]any{"themeId": themeID}), http.StatusNotFound, "not_found")
		refuse(t, "another organization changing the theme", outside.patch(t, "/api/v1/themes/"+themeID, map[string]any{"name": "Taken"}), http.StatusNotFound, "not_found")
		if seen := list(t, want(t, outside.get(t, "/api/v1/themes"), http.StatusOK, "list"), "themes"); len(seen) != 0 {
			t.Errorf("another organization lists %d themes", len(seen))
		}
	})

	t.Run("deleting returns everybody to the built-in theme and empties the bucket", func(t *testing.T) {
		want(t, owner.delete(t, "/api/v1/themes/"+themeID), http.StatusNoContent, "delete")
		if got := want(t, other.get(t, "/api/v1/themes/active"), http.StatusOK, "the other's active theme"); got.Body["theme"] != nil {
			t.Errorf("a deleted theme is still somebody's: %s", got.Raw)
		}
		refuse(t, "a deleted theme", owner.get(t, "/api/v1/themes/"+themeID), http.StatusNotFound, "not_found")
		if _, err := f.api.store.Get(context.Background(), themeObject(f.home.org, themeID, assetID)); !errors.Is(err, objectstore.ErrNoObject) {
			t.Errorf("a deleted theme's file is still in the bucket: %v", err)
		}
	})
}

// themeFiles puts files on the shared theme: kept in the bucket, served as a download, refused when unsafe, and
// kept while the theme names them. It leaves one on the theme and answers its id.
func themeFiles(t *testing.T, f *themeFixture) string {
	owner, other := f.owner, f.other
	base := "/api/v1/themes/" + f.themeID + "/assets"
	safe := []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16"><path d="M2 8h12"/></svg>`)
	var assetID string
	t.Run("files: a safe SVG is kept in the bucket and served as a download, an unsafe one is refused", func(t *testing.T) {
		asset := obj(t, want(t, owner.upload(t, base, "glyph.svg", safe), http.StatusCreated, "upload an svg"), "asset")
		if asset["contentType"] != "image/svg+xml" || asset["size"].(float64) != float64(len(safe)) {
			t.Fatalf("asset = %v", asset)
		}
		assetID = asset["id"].(string)
		stored, err := f.api.store.Get(context.Background(), themeObject(f.home.org, f.themeID, assetID))
		if err != nil {
			t.Fatalf("the bytes are not in the bucket: %v", err)
		}
		inBucket, _ := io.ReadAll(stored)
		stored.Close()
		if string(inBucket) != string(safe) {
			t.Fatalf("the bucket holds %q", inBucket)
		}
		got, data := owner.download(t, base+"/"+assetID)
		if got.StatusCode != http.StatusOK || string(data) != string(safe) {
			t.Fatalf("the file came back as %d with %d bytes", got.StatusCode, len(data))
		}
		if got.Header.Get("X-Content-Type-Options") != "nosniff" || !strings.HasPrefix(got.Header.Get("Content-Disposition"), "attachment") ||
			!strings.Contains(got.Header.Get("Cache-Control"), "immutable") || !strings.Contains(got.Header.Get("Content-Security-Policy"), "default-src 'none'") {
			t.Errorf("headers = %v", got.Header)
		}
		if got, _ := other.download(t, base+"/"+assetID); got.StatusCode != http.StatusOK {
			t.Errorf("a member cannot read a shared theme's file: %d", got.StatusCode)
		}
		if got, _ := f.outside.download(t, base+"/"+assetID); got.StatusCode != http.StatusNotFound {
			t.Errorf("another organization read the file: %d", got.StatusCode)
		}
		refuse(t, "a scripted svg", owner.upload(t, base, "evil.svg", []byte(`<svg onload="alert(1)"><script>1</script></svg>`)), http.StatusBadRequest, "bad_request")
		refuse(t, "text", owner.upload(t, base, "notes.txt", []byte("words")), http.StatusBadRequest, "bad_request")
		refuse(t, "a member adding a file to somebody's theme", other.upload(t, base, "glyph.svg", safe), http.StatusForbidden, "forbidden")

		want(t, owner.patch(t, "/api/v1/themes/"+f.themeID, map[string]any{"spec": map[string]any{"icons": map[string]any{"home": map[string]any{"assetId": assetID}}}}), http.StatusOK, "use the file as an icon")
		refuse(t, "removing a file in use", owner.delete(t, base+"/"+assetID), http.StatusConflict, "conflict")
		want(t, owner.patch(t, "/api/v1/themes/"+f.themeID, map[string]any{"spec": magentaSpec}), http.StatusOK, "stop using the file")
		want(t, owner.delete(t, base+"/"+assetID), http.StatusNoContent, "remove the file")
		if got, _ := owner.download(t, base+"/"+assetID); got.StatusCode != http.StatusNotFound {
			t.Errorf("a removed file still serves: %d", got.StatusCode)
		}
		if _, err := f.api.store.Get(context.Background(), themeObject(f.home.org, f.themeID, assetID)); !errors.Is(err, objectstore.ErrNoObject) {
			t.Errorf("a removed file is still in the bucket: %v", err)
		}
		// One more, left on the theme, so deleting the theme has something to take.
		assetID = obj(t, want(t, owner.upload(t, base, "glyph.svg", safe), http.StatusCreated, "upload again"), "asset")["id"].(string)
	})
	return assetID
}

// themeDefault makes the shared theme the organization's default, which everybody sees until they choose.
func themeDefault(t *testing.T, f *themeFixture) {
	owner, other, admin, outside, themeID := f.owner, f.other, f.admin, f.outside, f.themeID
	t.Run("the organization's default is shared and is what everybody sees until they choose", func(t *testing.T) {
		want(t, owner.patch(t, "/api/v1/themes/"+themeID, map[string]any{"shared": false}), http.StatusOK, "take it private")
		refuse(t, "an administrator naming a theme they cannot see", admin.put(t, "/api/v1/themes/default", map[string]any{"themeId": themeID}), http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "the owner naming a private theme", owner.put(t, "/api/v1/themes/default", map[string]any{"themeId": themeID}), http.StatusUnprocessableEntity, "validation_failed")

		want(t, owner.patch(t, "/api/v1/themes/"+themeID, map[string]any{"shared": true}), http.StatusOK, "share")
		refuse(t, "a member naming the default", other.put(t, "/api/v1/themes/default", map[string]any{"themeId": themeID}), http.StatusForbidden, "forbidden")
		if set := want(t, admin.put(t, "/api/v1/themes/default", map[string]any{"themeId": themeID}), http.StatusOK, "make it the default"); obj(t, set, "theme")["default"] != true {
			t.Fatalf("not marked as the default: %s", set.Raw)
		}
		if n := f.h.countRows(t, `SELECT count(*) FROM audit_log WHERE org_id = $1 AND action = 'theme.default_set' AND target_id = $2`, f.home.org, themeID); n != 1 {
			t.Errorf("naming the default left %d audit entries, want 1", n)
		}

		want(t, other.put(t, "/api/v1/themes/active", map[string]any{"themeId": nil}), http.StatusOK, "nothing chosen")
		for _, who := range []*client{other, admin} {
			seen := want(t, who.get(t, "/api/v1/themes/active"), http.StatusOK, "what a member sees")
			if obj(t, seen, "theme")["id"] != themeID || seen.Body["source"] != "organization" {
				t.Fatalf("a member does not see the organization's default: %s", seen.Raw)
			}
		}
		if got := want(t, outside.get(t, "/api/v1/themes/active"), http.StatusOK, "another organization"); got.Body["theme"] != nil {
			t.Fatalf("another organization sees this one's default: %s", got.Raw)
		}
		want(t, other.put(t, "/api/v1/themes/active", map[string]any{"themeId": nil, "builtIn": true}), http.StatusOK, "keep the built-in")
		if kept := want(t, other.get(t, "/api/v1/themes/active"), http.StatusOK, "built-in kept"); kept.Body["theme"] != nil || kept.Body["source"] != "" {
			t.Fatalf("the built-in theme was not kept: %s", kept.Raw)
		}
		if org := want(t, other.get(t, "/api/v1/themes/default"), http.StatusOK, "the organization's theme"); obj(t, org, "theme")["id"] != themeID {
			t.Fatalf("the organization's theme reads %s for a member who keeps the built-in", org.Raw)
		}
		if got := want(t, outside.get(t, "/api/v1/themes/default"), http.StatusOK, "another organization's"); got.Body["theme"] != nil {
			t.Fatalf("another organization reads this one's theme: %s", got.Raw)
		}
		want(t, other.put(t, "/api/v1/themes/active", map[string]any{"themeId": nil}), http.StatusOK, "back to the default")
		if back := want(t, other.get(t, "/api/v1/themes/active"), http.StatusOK, "default again"); back.Body["source"] != "organization" {
			t.Fatalf("the default did not come back: %s", back.Raw)
		}

		// Taken private, the theme stops being the default at once, by SQL.
		want(t, owner.patch(t, "/api/v1/themes/"+themeID, map[string]any{"shared": false}), http.StatusOK, "unshare")
		if gone := want(t, other.get(t, "/api/v1/themes/active"), http.StatusOK, "after unsharing"); gone.Body["theme"] != nil {
			t.Fatalf("an unshared theme is still shown as the default: %s", gone.Raw)
		}
		if n := f.h.countRows(t, `SELECT count(*) FROM org WHERE id = $1 AND default_theme_id IS NOT NULL`, f.home.org); n != 0 {
			t.Fatal("default_theme_id survived the theme being taken private")
		}
		want(t, owner.patch(t, "/api/v1/themes/"+themeID, map[string]any{"shared": true}), http.StatusOK, "share again")
		if cleared := want(t, admin.put(t, "/api/v1/themes/default", map[string]any{"themeId": nil}), http.StatusOK, "back to the built-in for everybody"); cleared.Body["theme"] != nil {
			t.Fatalf("clearing the default answered a theme: %s", cleared.Raw)
		}
	})
}
