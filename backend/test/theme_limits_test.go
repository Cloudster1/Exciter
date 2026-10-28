//go:build integration

package test

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/httpapi"
	"github.com/cloudster1/exciter/backend/internal/objectstore"
	"github.com/cloudster1/exciter/backend/internal/render"
	"github.com/cloudster1/exciter/backend/internal/theme"
)

// themeScene is alice, who administers the installation, bob and carol, members, and the bucket their files go to.
type themeScene struct {
	h                  *harness
	api                *apiServer
	alice, bob, carol  *client
	aliceID, bobID     uuid.UUID
	installationPrefix string
}

func newThemeScene(t *testing.T) *themeScene {
	t.Helper()
	h := newHarness(t)
	api := newAPIServer(t, h)
	s := &themeScene{h: h, api: api, aliceID: h.addPerson(t, "admin"), bobID: h.addPerson(t, "member")}
	s.alice, s.bob, s.carol = api.as(t, s.aliceID), api.as(t, s.bobID), api.as(t, h.addPerson(t, "member"))
	s.installationPrefix = objectstore.InstallationPrefix(h.installation(t).ID) + "theme/"
	return s
}

// clearDefault keeps the installation's default for the test's end and starts the test with none.
func (s *themeScene) clearDefault(t *testing.T) {
	t.Helper()
	s.h.keepDefaultTheme(t)
	if _, err := s.h.super.Exec(context.Background(), `UPDATE installation SET default_theme_id = NULL`); err != nil {
		t.Fatal(err)
	}
}

// make is a theme of the client's, shared or not, answering its id.
func (s *themeScene) make(t *testing.T, c *client, name string, shared bool) string {
	t.Helper()
	made := want(t, c.post(t, "/api/v1/themes", map[string]any{"name": name, "shared": shared, "spec": magentaSpec}), http.StatusCreated, "make "+name)
	return obj(t, made, "theme")["id"].(string)
}

// upload puts a file on a theme, answering its id.
func (s *themeScene) upload(t *testing.T, c *client, themeID, name string, data []byte) string {
	t.Helper()
	return obj(t, want(t, c.upload(t, "/api/v1/themes/"+themeID+"/assets", name, data), http.StatusCreated, "upload "+name), "asset")["id"].(string)
}

// objects are the bucket's keys under a theme, or under every theme of the installation for an empty id.
func (s *themeScene) objects(t *testing.T, themeID string) []string {
	t.Helper()
	keys, err := s.api.store.List(context.Background(), s.installationPrefix+themeID)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(keys)
	return keys
}

func (s *themeScene) themesOf(t *testing.T, owner uuid.UUID) int {
	t.Helper()
	return s.h.countRows(t, `SELECT count(*) FROM theme WHERE owner_id = $1`, owner)
}

// chosen is the theme a person's choice names, "" for none.
func (s *themeScene) chosen(t *testing.T, person uuid.UUID) string {
	t.Helper()
	var id *uuid.UUID
	err := s.h.super.QueryRow(context.Background(), `SELECT theme_id FROM user_theme WHERE user_id = $1`, person).Scan(&id)
	if err != nil || id == nil {
		return ""
	}
	return id.String()
}

// uploadWithToken sends a file as upload does, with the script's token, which upload leaves off.
func uploadWithToken(t *testing.T, c *client, path, name string, data []byte) response {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(data)
	_ = form.Close()
	req, err := http.NewRequest(http.MethodPost, c.api.srv.URL+path, &body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+c.bearer)
	return c.do(t, req)
}

// activeOf is what GET /themes/active answers a person: the theme's id, "" for none, and the source.
func activeOf(t *testing.T, c *client) (string, any) {
	t.Helper()
	got := want(t, c.get(t, "/api/v1/themes/active"), http.StatusOK, "read the active theme")
	if got.Body["theme"] == nil {
		return "", got.Body["source"]
	}
	return obj(t, got, "theme")["id"].(string), got.Body["source"]
}

// A theme write is refused for what its caller may not do, with the reason, and leaves everything as it was; the
// writes with nothing to send in the wrong shape refuse a bad id or a missing file with 400 and 404, not 422 (#332).
func TestThemeWritesAreRefusedWithTheirReason(t *testing.T) {
	s := newThemeScene(t)
	shared := s.make(t, s.alice, "Refused shared", true)
	asset := s.upload(t, s.alice, shared, "glyph.png", aPicture(t))
	want(t, s.bob.put(t, "/api/v1/themes/active", map[string]any{"themeId": shared}), http.StatusOK, "bob uses the shared theme")
	_, secret := makeToken(t, s.bob, map[string]any{"name": "theme-reader", "scopes": []string{"read"}})
	reader := s.api.withToken(s.bobID, secret)

	t.Run("a token made to read makes, chooses and imports nothing", func(t *testing.T) {
		before := s.themesOf(t, s.bobID)
		refuse(t, "a read token makes a theme", reader.post(t, "/api/v1/themes", map[string]any{"name": "Through a token"}), http.StatusForbidden, "read_only_token")
		refuse(t, "a read token chooses the built-in theme", reader.put(t, "/api/v1/themes/active", map[string]any{"themeId": nil, "builtIn": true}),
			http.StatusForbidden, "read_only_token")
		pkg := `{"format":"armature-theme/1","name":"Imported through a token","spec":{},"assets":[]}`
		refuse(t, "a read token imports a theme", uploadWithToken(t, reader, "/api/v1/themes/import", "x.json", []byte(pkg)), http.StatusForbidden, "read_only_token")
		if after := s.themesOf(t, s.bobID); after != before {
			t.Errorf("bob had %d themes and has %d after the refusals", before, after)
		}
		if got := s.chosen(t, s.bobID); got != shared {
			t.Errorf("bob's choice is %q after the refused one, want the shared theme", got)
		}
	})

	t.Run("a member takes no file off somebody's shared theme, and it is still served", func(t *testing.T) {
		refuse(t, "a member removes a file", s.bob.delete(t, "/api/v1/themes/"+shared+"/assets/"+asset), http.StatusForbidden, "forbidden")
		if got, data := s.bob.download(t, "/api/v1/themes/"+shared+"/assets/"+asset); got.StatusCode != http.StatusOK || !bytes.Equal(data, aPicture(t)) {
			t.Errorf("after the refusal the file serves %d with %d bytes", got.StatusCode, len(data))
		}
		if n := len(s.objects(t, shared)); n != 1 {
			t.Errorf("the bucket holds %d objects of the theme, want its one file", n)
		}
	})

	t.Run("what has no input to check refuses a bad id or a missing thing, never 422", func(t *testing.T) {
		refuse(t, "choosing an id that is none", s.bob.put(t, "/api/v1/themes/active", map[string]any{"themeId": "nonsense"}), http.StatusBadRequest, "bad_request")
		refuse(t, "deleting a theme nobody has", s.alice.delete(t, "/api/v1/themes/"+uuid.NewString()), http.StatusNotFound, "not_found")
		refuse(t, "uploading with no file part", s.alice.post(t, "/api/v1/themes/"+shared+"/assets", map[string]any{"file": "x"}), http.StatusBadRequest, "bad_request")
		refuse(t, "removing a file the theme lacks", s.alice.delete(t, "/api/v1/themes/"+shared+"/assets/"+uuid.NewString()), http.StatusNotFound, "not_found")
		if got := s.chosen(t, s.bobID); got != shared {
			t.Errorf("bob's choice is %q after the refused one", got)
		}
		if n := s.h.countRows(t, `SELECT count(*) FROM theme_asset WHERE theme_id = $1`, shared); n != 1 {
			t.Errorf("the theme has %d files after the refusals, want 1", n)
		}
	})
}

// Deleting the theme everybody is shown returns them all to the built-in one, and the installation names none (#332).
func TestDeletingTheDefaultThemeLeavesNoDefault(t *testing.T) {
	s := newThemeScene(t)
	s.clearDefault(t)
	id := s.make(t, s.bob, "Default to delete", true)
	want(t, s.alice.put(t, "/api/v1/themes/default", map[string]any{"themeId": id}), http.StatusOK, "alice names it the default")
	if seen, source := activeOf(t, s.carol); seen != id || source != "everyone" {
		t.Fatalf("carol, who chose nothing, sees %q from %v before the delete", seen, source)
	}

	want(t, s.bob.delete(t, "/api/v1/themes/"+id), http.StatusNoContent, "its owner deletes it")
	if got := want(t, s.alice.get(t, "/api/v1/themes/default"), http.StatusOK, "read the default"); got.Body["theme"] != nil {
		t.Errorf("the default reads %s after its theme was deleted", got.Raw)
	}
	if n := s.h.countRows(t, `SELECT count(*) FROM installation WHERE default_theme_id IS NOT NULL`); n != 0 {
		t.Error("the installation still names a default")
	}
	if seen, source := activeOf(t, s.carol); seen != "" || source != "" {
		t.Errorf("carol sees %q from %v after the default was deleted", seen, source)
	}
}

// An administrator tidies away what somebody shared, files and all, but never reaches a private theme (#332).
func TestAnAdministratorDeletesSharedThemesOnly(t *testing.T) {
	s := newThemeScene(t)
	shared := s.make(t, s.bob, "Bob's shared", true)
	s.upload(t, s.bob, shared, "glyph.png", aPicture(t))
	private := s.make(t, s.bob, "Bob's private", false)
	if n := len(s.objects(t, shared)); n != 1 {
		t.Fatalf("the shared theme has %d objects in the bucket, want 1", n)
	}

	t.Run("a shared theme goes, and its files leave the bucket", func(t *testing.T) {
		want(t, s.alice.delete(t, "/api/v1/themes/"+shared), http.StatusNoContent, "alice deletes bob's shared theme")
		if n := s.h.countRows(t, `SELECT count(*) FROM theme WHERE id = $1`, shared); n != 0 {
			t.Error("the theme is still there")
		}
		if keys := s.objects(t, shared); len(keys) != 0 {
			t.Errorf("the bucket still holds %v", keys)
		}
	})

	t.Run("a private theme is not found, and stays as it was", func(t *testing.T) {
		var before, after string
		row := `SELECT name || ' ' || shared || ' ' || updated_at FROM theme WHERE id = $1`
		if err := s.h.super.QueryRow(context.Background(), row, private).Scan(&before); err != nil {
			t.Fatal(err)
		}
		refuse(t, "alice deletes bob's private theme", s.alice.delete(t, "/api/v1/themes/"+private), http.StatusNotFound, "not_found")
		refuse(t, "alice renames bob's private theme", s.alice.patch(t, "/api/v1/themes/"+private, map[string]any{"name": "Tidied"}), http.StatusNotFound, "not_found")
		if err := s.h.super.QueryRow(context.Background(), row, private).Scan(&after); err != nil {
			t.Fatalf("the private theme is gone: %v", err)
		}
		if after != before || !strings.HasPrefix(after, "Bob's private false") {
			t.Errorf("the private theme read %q and reads %q", before, after)
		}
	})
}

// A theme's files are held to who may see and change it, to 64 a theme and 12 MB a file, and fonts are kept as
// fonts (#332).
func TestAThemesFilesAreHeldToTheirLimits(t *testing.T) {
	s := newThemeScene(t)
	private := s.make(t, s.alice, "Files private", false)
	privateAsset := s.upload(t, s.alice, private, "glyph.png", aPicture(t))
	full := s.make(t, s.alice, "Files full", false)
	base := "/api/v1/themes/" + full + "/assets"

	t.Run("somebody else does not reach a private theme's file", func(t *testing.T) {
		refuse(t, "a stranger reads a private file", s.carol.get(t, "/api/v1/themes/"+private+"/assets/"+privateAsset), http.StatusNotFound, "not_found")
	})

	t.Run("a WOFF2 font is kept and served as one", func(t *testing.T) {
		font := append([]byte("wOF2"), make([]byte, 60)...)
		id := s.upload(t, s.alice, full, "face.woff2", font)
		got, data := s.alice.download(t, base+"/"+id)
		if got.StatusCode != http.StatusOK || got.Header.Get("Content-Type") != "font/woff2" || !bytes.Equal(data, font) {
			t.Errorf("the font serves %d as %q with %d bytes", got.StatusCode, got.Header.Get("Content-Type"), len(data))
		}
	})

	t.Run("a file over 12 MB is refused before anything reaches the bucket", func(t *testing.T) {
		before := s.objects(t, full)
		big := append(aPicture(t), make([]byte, theme.MaxAssetBytes)...)
		refuse(t, "a file over the limit", s.alice.upload(t, base, "huge.png", big), http.StatusRequestEntityTooLarge, "too_large")
		if after := s.objects(t, full); !slices.Equal(after, before) {
			t.Errorf("the bucket held %v and holds %v", before, after)
		}
		if n := s.h.countRows(t, `SELECT count(*) FROM theme_asset WHERE theme_id = $1`, full); n != 1 {
			t.Errorf("the theme has %d files, want the font alone", n)
		}
	})

	t.Run("the 65th file is refused", func(t *testing.T) {
		for i := 1; i < theme.MaxAssetsPerSpec; i++ {
			s.upload(t, s.alice, full, "tile.png", aPicture(t))
		}
		r := s.alice.upload(t, base, "one-more.png", aPicture(t))
		refuse(t, "a 65th file", r, http.StatusConflict, "conflict")
		if msg := refusalMessage(t, r); !strings.Contains(msg, "at most 64 files") {
			t.Errorf("the refusal says %q", msg)
		}
		if n := s.h.countRows(t, `SELECT count(*) FROM theme_asset WHERE theme_id = $1`, full); n != theme.MaxAssetsPerSpec {
			t.Errorf("the theme has %d files, want %d", n, theme.MaxAssetsPerSpec)
		}
	})
}

// An import whose files a theme could not take is refused as an upload of them would be, and leaves no theme and
// no object behind, even of the files before the bad one (#332).
func TestAnImportCarryingARefusedFileLeavesNothing(t *testing.T) {
	s := newThemeScene(t)
	safe := theme.PackagedAsset{ID: uuid.New(), Name: "glyph.png", ContentType: "image/png", Data: aPicture(t)}
	cases := []struct {
		what, file string
		bad        []byte
		status     int
		code, says string
	}{
		{"an unsafe SVG", "evil.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"></svg>`), http.StatusBadRequest, "bad_request", "SVG carries script"},
		{"a file over the limit", "huge.png", append(aPicture(t), make([]byte, theme.MaxAssetBytes)...), http.StatusRequestEntityTooLarge, "too_large", "12 MB"},
	}
	for _, tc := range cases {
		t.Run(tc.what, func(t *testing.T) {
			// The type a package claims is never read: the bytes say what a file is.
			bad := theme.PackagedAsset{ID: uuid.New(), Name: tc.file, Data: tc.bad}
			pkg, err := json.Marshal(theme.Package{Format: theme.PackageFormat, Name: "Carries " + tc.file, Spec: theme.Spec{}, Assets: []theme.PackagedAsset{safe, bad}})
			if err != nil {
				t.Fatal(err)
			}
			themes, objects := s.themesOf(t, s.aliceID), s.objects(t, "")
			r := s.alice.upload(t, "/api/v1/themes/import", "carries.armature-theme.json", pkg)
			refuse(t, "an import carrying "+tc.what, r, tc.status, tc.code)
			if r.Status == tc.status && !strings.Contains(refusalMessage(t, r), tc.says) {
				t.Errorf("the refusal says %q", refusalMessage(t, r))
			}
			if n := s.themesOf(t, s.aliceID); n != themes {
				t.Errorf("alice had %d themes and has %d", themes, n)
			}
			if after := s.objects(t, ""); !slices.Equal(after, objects) {
				t.Errorf("the refused import left %d objects behind", len(after)-len(objects))
			}
		})
	}
}

// A chosen theme taken private lapses to the built-in one, not the default, as in Stator; shared again, it comes
// back, and a private theme of somebody else's cannot be chosen at all (#332).
func TestAChosenThemeTakenPrivateLapses(t *testing.T) {
	s := newThemeScene(t)
	s.clearDefault(t)
	chosen := s.make(t, s.alice, "Chosen then private", true)
	want(t, s.bob.put(t, "/api/v1/themes/active", map[string]any{"themeId": chosen}), http.StatusOK, "bob chooses alice's theme")
	if seen, source := activeOf(t, s.bob); seen != chosen || source != "chosen" {
		t.Fatalf("bob sees %q from %v", seen, source)
	}

	t.Run("taken private, bob sees the built-in theme", func(t *testing.T) {
		want(t, s.alice.patch(t, "/api/v1/themes/"+chosen, map[string]any{"shared": false}), http.StatusOK, "alice stops sharing")
		if seen, source := activeOf(t, s.bob); seen != "" || source != "" {
			t.Errorf("bob sees %q from %v", seen, source)
		}
	})

	t.Run("with a default for everyone, bob still sees the built-in theme, not the default", func(t *testing.T) {
		house := s.make(t, s.alice, "House default", true)
		want(t, s.alice.put(t, "/api/v1/themes/default", map[string]any{"themeId": house}), http.StatusOK, "name the default")
		if seen, source := activeOf(t, s.carol); seen != house || source != "everyone" {
			t.Fatalf("carol, who chose nothing, sees %q from %v", seen, source)
		}
		if seen, source := activeOf(t, s.bob); seen != "" || source != "" {
			t.Errorf("bob, whose choice lapsed, sees %q from %v", seen, source)
		}
	})

	t.Run("nobody chooses a private theme of somebody else's, and the choice stays", func(t *testing.T) {
		refuse(t, "bob chooses alice's private theme", s.bob.put(t, "/api/v1/themes/active", map[string]any{"themeId": chosen}), http.StatusNotFound, "not_found")
		if got := s.chosen(t, s.bobID); got != chosen {
			t.Errorf("bob's choice is %q, want the lapsed one kept", got)
		}
	})

	t.Run("shared again, the choice comes back", func(t *testing.T) {
		want(t, s.alice.patch(t, "/api/v1/themes/"+chosen, map[string]any{"shared": true}), http.StatusOK, "alice shares again")
		if seen, source := activeOf(t, s.bob); seen != chosen || source != "chosen" {
			t.Errorf("bob sees %q from %v", seen, source)
		}
	})
}

// renderSpy passes every print to the stack's render service and keeps what it was asked.
type renderSpy struct {
	inner render.Renderer
	mu    sync.Mutex
	asked []render.Request
}

func (r *renderSpy) note(req render.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.asked = append(r.asked, req)
}

func (r *renderSpy) PDF(ctx context.Context, req render.Request) ([]byte, error) {
	r.note(req)
	return r.inner.PDF(ctx, req)
}

func (r *renderSpy) HTML(ctx context.Context, req render.Request) ([]byte, error) {
	r.note(req)
	return r.inner.(render.HTMLRenderer).HTML(ctx, req)
}

func (r *renderSpy) languages() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, each := range r.asked {
		out = append(out, each.Language)
	}
	return out
}

// An export says its own words in the language its reader chose: the site, Word and the print behind the PDF (#332).
func TestExportsSpeakTheReadersLanguage(t *testing.T) {
	s := newDocumentScene(t)
	if s.f.h.cfg.Render.URL == "" {
		t.Fatal("EXCITER_RENDER_URL is not set; run the suite with make test-integration against the running stack")
	}
	want(t, s.f.owner.post(t, "/api/v1/projects/"+s.key+"/terms", map[string]any{"name": "Caliper", "definition": docOf("The part that grips the disc.")}),
		http.StatusCreated, "define a term")
	// bob reads the exports, and read-your-writes holds only for the owner who defined the term.
	s.f.h.settle(t)
	spy := &renderSpy{inner: s.f.h.renderer()}
	api := newAPIServerWith(t, s.f.h, func(_ *apiServer, server *httpapi.Server) { server.Renderer = spy })
	bob, alice := api.as(t, s.f.plain.user), api.as(t, s.f.owner.user)
	want(t, bob.patch(t, "/api/v1/auth/me", map[string]any{"locale": "de"}), http.StatusOK, "bob chooses German")
	project := "/api/v1/projects/" + s.key

	t.Run("the site's own words are German", func(t *testing.T) {
		var all strings.Builder
		for _, page := range unzipped(t, want(t, bob.get(t, project+"/document.html.zip"), http.StatusOK, "bob downloads the site").Raw) {
			all.Write(page)
		}
		site := all.String()
		for _, word := range []string{">Inhalt<", ">Suchen<", "Glossar"} {
			if !strings.Contains(site, word) {
				t.Errorf("the site lacks %q", word)
			}
		}
		if strings.Contains(site, ">Contents<") || strings.Contains(site, ">Search<") {
			t.Error("the site still says Contents or Search")
		}
	})

	t.Run("Word's headings are bob's German and alice's English", func(t *testing.T) {
		words := func(c *client, who string) (body, styles string) {
			parts := unzipped(t, want(t, c.get(t, project+"/document.docx"), http.StatusOK, who+" exports Word").Raw)
			return string(parts["word/document.xml"]), string(parts["word/styles.xml"])
		}
		german, germanStyles := words(bob, "bob")
		english, englishStyles := words(alice, "alice")
		for _, word := range []string{"Spezifikation von Brakes", "Begründung", "Glossar"} {
			if !strings.Contains(german, word) {
				t.Errorf("bob's Word lacks %q", word)
			}
		}
		for _, word := range []string{"Specification of Brakes", "Rationale", "Glossary"} {
			if !strings.Contains(english, word) {
				t.Errorf("alice's Word lacks %q", word)
			}
		}
		if strings.Contains(german, "Rationale") || strings.Contains(english, "Begründung") {
			t.Error("a Word export mixes the two languages")
		}
		if !strings.Contains(germanStyles, `w:lang w:val="de-DE"`) || !strings.Contains(englishStyles, `w:lang w:val="en-US"`) {
			t.Error("a Word export does not name its language for the spelling check")
		}
	})

	t.Run("the print behind bob's PDF is asked for in German, alice's in English", func(t *testing.T) {
		before := len(spy.languages())
		want(t, bob.get(t, project+"/document.pdf"), http.StatusOK, "bob prints")
		want(t, alice.get(t, project+"/document.pdf"), http.StatusOK, "alice prints")
		if got := spy.languages()[before:]; !slices.Equal(got, []string{"de", "en"}) {
			t.Errorf("the prints were asked for in %v", got)
		}
		if got := spy.languages()[0]; got != "de" {
			t.Errorf("bob's site was asked for in %q", got)
		}
	})
}
