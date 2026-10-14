//go:build integration

package test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/attachment"
	"github.com/cloudster1/exciter/backend/internal/objectstore"
)

// aPicture is a small PNG, three pixels by two.
func aPicture(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewPaletted(image.Rect(0, 0, 3, 2), color.Palette{color.Black, color.White})); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// showing is a statement that shows a file as a picture.
func showing(id string) map[string]any {
	return map[string]any{"type": "doc", "content": []any{
		map[string]any{"type": "image", "attrs": map[string]any{"attachmentId": id, "alt": "The basket", "width": nil}},
	}}
}

// uploadAs sends a file the way a browser does, naming its type.
func (c *client) uploadAs(t *testing.T, path, name, contentType string, data []byte) response {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{"name": "file", "filename": name}))
	header.Set("Content-Type", contentType)
	part, err := form.CreatePart(header)
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
	return c.do(t, req)
}

// fileKey is where the bytes of a file on a requirement live, under the installation's prefix.
func fileKey(installation uuid.UUID, requirementID, attachmentID string) string {
	return objectstore.InstallationPrefix(installation) + "requirement/" + requirementID + "/" + attachmentID
}

// stored says whether the bucket holds an object.
func stored(t *testing.T, store objectstore.Store, key string) bool {
	t.Helper()
	body, err := store.Get(context.Background(), key)
	if errors.Is(err, objectstore.ErrNoObject) {
		return false
	}
	if err != nil {
		t.Fatalf("read %s: %v", key, err)
	}
	body.Close()
	return true
}

// Editors attach files to a requirement and viewers read them, through the API alone; a file some revision
// shows is only taken off the list, and any other goes with its bytes.
func TestAttachmentsOverTheAPI(t *testing.T) {
	attKey := projectKey(t, "ATT")
	f := newProjectFixture(t)
	project := obj(t, want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": attKey, "name": "Attached"}), http.StatusCreated, "make a project"), "project")
	editor := f.plain
	viewerID := f.h.addPerson(t, "guest")
	f.h.grantAs(t, uuid.MustParse(project["id"].(string)), viewerID, "viewer")
	viewer := f.api.as(t, viewerID)
	first := obj(t, want(t, editor.post(t, "/api/v1/projects/"+attKey+"/requirements", map[string]any{"title": "Show the basket"}), http.StatusCreated, "write "+attKey+"-R1"), "requirement")
	want(t, editor.post(t, "/api/v1/projects/"+attKey+"/requirements", map[string]any{"title": "Pay"}), http.StatusCreated, "write "+attKey+"-R2")
	picture := aPicture(t)

	file := obj(t, want(t, editor.upload(t, "/api/v1/requirements/"+attKey+"-R1/attachments", "basket.png", picture), http.StatusCreated, "attach a picture"), "attachment")
	if file["fileName"] != "basket.png" || file["contentType"] != "image/png" || file["width"] != float64(3) || file["height"] != float64(2) ||
		file["size"] != float64(len(picture)) || file["referenced"] != false || file["requirementId"] != first["id"] {
		t.Fatalf("the picture as stored: %v", file)
	}
	pictureID := file["id"].(string)
	pictureKey := fileKey(f.h.installation(t).ID, first["id"].(string), pictureID)
	if !stored(t, f.api.store, pictureKey) {
		t.Fatal("the picture's bytes are not under the requirement's prefix")
	}
	note := obj(t, want(t, editor.uploadAs(t, "/api/v1/requirements/"+attKey+"-R1/attachments", "dir/note.svg", "image/svg+xml",
		[]byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)), http.StatusCreated, "attach a drawing"), "attachment")
	if note["fileName"] != "note.svg" || note["contentType"] != "image/svg+xml" || note["width"] != nil {
		t.Errorf("the drawing keeps the base of its name and the type sent, unmeasured: %v", note)
	}
	noteID := note["id"].(string)

	t.Run("only editors attach, and only what fits", func(t *testing.T) {
		refuse(t, "a viewer attaches", viewer.upload(t, "/api/v1/requirements/"+attKey+"-R1/attachments", "a.txt", []byte("a")), http.StatusForbidden, "forbidden")
		refuse(t, "a guest who cannot see it", f.guest.upload(t, "/api/v1/requirements/"+attKey+"-R1/attachments", "a.txt", []byte("a")), http.StatusNotFound, "not_found")
		refuse(t, "a guest no grant names", f.outside.upload(t, "/api/v1/requirements/"+attKey+"-R1/attachments", "a.txt", []byte("a")), http.StatusNotFound, "not_found")
		refuse(t, "nobody signed in", f.api.anonymous().upload(t, "/api/v1/requirements/"+attKey+"-R1/attachments", "a.txt", []byte("a")), http.StatusUnauthorized, "unauthorized")
		refuse(t, "a file over the limit", editor.upload(t, "/api/v1/requirements/"+attKey+"-R1/attachments", "big.bin", make([]byte, testUploadLimit+1)),
			http.StatusRequestEntityTooLarge, "too_large")
		refuse(t, "an empty file", editor.upload(t, "/api/v1/requirements/"+attKey+"-R1/attachments", "empty.txt", nil), http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "no such requirement", editor.upload(t, "/api/v1/requirements/"+attKey+"-R9/attachments", "a.txt", []byte("a")), http.StatusNotFound, "not_found")
	})

	t.Run("viewers list and read them, as downloads unless safe in place", func(t *testing.T) {
		files := list(t, want(t, viewer.get(t, "/api/v1/requirements/"+attKey+"-R1/attachments"), http.StatusOK, "list them"), "attachments")
		if len(files) != 2 || files[0].(map[string]any)["id"] != noteID {
			t.Fatalf("the latest first: %v", files)
		}
		refuse(t, "a guest lists them", f.guest.get(t, "/api/v1/requirements/"+attKey+"-R1/attachments"), http.StatusNotFound, "not_found")

		resp, body := viewer.download(t, "/api/v1/attachments/"+pictureID)
		if resp.StatusCode != http.StatusOK || !bytes.Equal(body, picture) {
			t.Fatalf("the picture came back as %d with %d bytes", resp.StatusCode, len(body))
		}
		if got := resp.Header.Get("Content-Disposition"); got != `attachment; filename=basket.png` {
			t.Errorf("a plain download: %q", got)
		}
		if resp.Header.Get("X-Content-Type-Options") != "nosniff" || !strings.HasPrefix(resp.Header.Get("Cache-Control"), "private") {
			t.Errorf("served without nosniff or private caching: %v", resp.Header)
		}
		if resp, _ := viewer.download(t, "/api/v1/attachments/"+pictureID+"?inline=true"); !strings.HasPrefix(resp.Header.Get("Content-Disposition"), "inline") {
			t.Errorf("a picture asked for in place: %q", resp.Header.Get("Content-Disposition"))
		}
		if resp, _ := viewer.download(t, "/api/v1/attachments/"+noteID+"?inline=true"); !strings.HasPrefix(resp.Header.Get("Content-Disposition"), "attachment") {
			t.Errorf("a drawing that can run scripts is shown in place: %q", resp.Header.Get("Content-Disposition"))
		}
		refuse(t, "inline that is no yes or no", viewer.get(t, "/api/v1/attachments/"+pictureID+"?inline=maybe"), http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "a guest reads one", f.guest.get(t, "/api/v1/attachments/"+pictureID), http.StatusNotFound, "not_found")
		refuse(t, "no such file", viewer.get(t, "/api/v1/attachments/"+uuid.NewString()), http.StatusNotFound, "not_found")
	})

	t.Run("a file nothing shows is deleted with its bytes", func(t *testing.T) {
		refuse(t, "a viewer deletes", viewer.delete(t, "/api/v1/attachments/"+noteID), http.StatusForbidden, "forbidden")
		want(t, editor.delete(t, "/api/v1/attachments/"+noteID), http.StatusNoContent, "delete the drawing")
		if stored(t, f.api.store, fileKey(f.h.installation(t).ID, first["id"].(string), noteID)) {
			t.Error("the drawing's bytes stayed")
		}
		if n := f.h.countRows(t, `SELECT count(*) FROM attachment_tombstone WHERE object_key = $1`, fileKey(f.h.installation(t).ID, first["id"].(string), noteID)); n != 0 {
			t.Errorf("%d tombstones stayed after the bytes went", n)
		}
		refuse(t, "it is gone", editor.delete(t, "/api/v1/attachments/"+noteID), http.StatusNotFound, "not_found")
		refuse(t, "and unreadable", viewer.get(t, "/api/v1/attachments/"+noteID), http.StatusNotFound, "not_found")
	})

	t.Run("a file a revision shows is only taken off the list", func(t *testing.T) {
		want(t, editor.patch(t, "/api/v1/requirements/"+attKey+"-R1", map[string]any{"statement": showing(pictureID), "baseRevision": 1}), http.StatusOK, "show it")
		files := list(t, want(t, editor.get(t, "/api/v1/requirements/"+attKey+"-R1/attachments"), http.StatusOK, "list"), "attachments")
		if files[0].(map[string]any)["referenced"] != true {
			t.Fatalf("the shown picture is not marked: %v", files)
		}
		want(t, editor.delete(t, "/api/v1/attachments/"+pictureID), http.StatusNoContent, "delete the shown picture")
		if n := len(list(t, want(t, editor.get(t, "/api/v1/requirements/"+attKey+"-R1/attachments"), http.StatusOK, "list"), "attachments")); n != 0 {
			t.Errorf("the list still holds %d files", n)
		}
		if resp, body := viewer.download(t, "/api/v1/attachments/"+pictureID); resp.StatusCode != http.StatusOK || !bytes.Equal(body, picture) {
			t.Errorf("history can no longer show the picture: %d", resp.StatusCode)
		}
		refuse(t, "taking it off twice", editor.delete(t, "/api/v1/attachments/"+pictureID), http.StatusNotFound, "not_found")
		if n := f.h.countRows(t, `SELECT count(*) FROM requirement_change WHERE field = 'attachment'`); n != 4 {
			t.Errorf("the history logs %d file changes, want two uploads and two deletions", n)
		}
	})

	t.Run("a trashed requirement takes no files", func(t *testing.T) {
		want(t, editor.delete(t, "/api/v1/requirements/"+attKey+"-R2"), http.StatusNoContent, "trash "+attKey+"-R2")
		refuse(t, "an upload to the trash", editor.upload(t, "/api/v1/requirements/"+attKey+"-R2/attachments", "a.txt", []byte("a")), http.StatusConflict, "requirement_trashed")
	})

	t.Run("a copy shows files of its own, and a purge takes them with their bytes", func(t *testing.T) {
		copied := obj(t, want(t, editor.post(t, "/api/v1/requirements/"+attKey+"-R1/copy", map[string]any{"place": map[string]any{}}), http.StatusCreated, "copy"),
			"requirement")
		files := list(t, want(t, editor.get(t, "/api/v1/requirements/"+copied["key"].(string)+"/attachments"), http.StatusOK, "the copy's files"), "attachments")
		if len(files) != 1 {
			t.Fatalf("the copy holds %d files, want the picture its statement shows", len(files))
		}
		copyFile := files[0].(map[string]any)
		shown := copied["statement"].(map[string]any)["content"].([]any)[0].(map[string]any)["attrs"].(map[string]any)["attachmentId"]
		if copyFile["id"] == pictureID || shown != copyFile["id"] || copyFile["referenced"] != true {
			t.Fatalf("the copy shows %v and holds %v", shown, copyFile)
		}
		copyKey := fileKey(f.h.installation(t).ID, copied["id"].(string), copyFile["id"].(string))
		if resp, body := viewer.download(t, "/api/v1/attachments/"+copyFile["id"].(string)); resp.StatusCode != http.StatusOK || !bytes.Equal(body, picture) {
			t.Fatalf("the copy's picture came back as %d", resp.StatusCode)
		}

		want(t, editor.delete(t, "/api/v1/requirements/"+copied["key"].(string)), http.StatusNoContent, "trash the copy")
		want(t, f.owner.delete(t, "/api/v1/projects/"+attKey+"/trash/"+copied["id"].(string)), http.StatusNoContent, "purge it")
		if n := f.h.countRows(t, `SELECT count(*) FROM attachment_tombstone WHERE object_key = $1`, copyKey); n != 1 {
			t.Fatal("the purged copy's file left no tombstone")
		}
		if _, err := attachment.NewReaper(f.api.files, discard(), 0).Once(context.Background()); err != nil {
			t.Fatal(err)
		}
		if stored(t, f.api.store, copyKey) || !stored(t, f.api.store, pictureKey) {
			t.Error("the reaper took the wrong bytes: the copy's should go and the original's stay")
		}
	})
}
