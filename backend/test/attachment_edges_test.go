//go:build integration

package test

import (
	"io"
	"net/http"
	"testing"
)

// Files refused (#312): in the trash and in an archived project a file is neither attached nor deleted, and both
// the row and its bytes stay; a locked requirement still takes one.

func TestFilesRefusedLeaveTheFilesAsTheyWere(t *testing.T) {
	w := newCommentWorld(t, "FRF")
	h := w.h
	store := w.owner.api.store
	files := "/api/v1/requirements/" + w.reqKey + "/attachments"
	id := obj(t, want(t, w.bob.upload(t, files, "spec.txt", []byte("Brakes engage within 100 ms.")), http.StatusCreated, "bob attaches"), "attachment")["id"].(string)
	var objectKey string
	if err := h.super.QueryRow(t.Context(), `SELECT object_key FROM attachment WHERE id = $1`, id).Scan(&objectKey); err != nil {
		t.Fatal(err)
	}
	listed := `SELECT count(*) FROM attachment WHERE requirement_id = $1 AND deleted_at IS NULL`
	stays := func(t *testing.T, want int) {
		t.Helper()
		if n := h.countRows(t, listed, w.reqID); n != want {
			t.Errorf("%d files listed on R1, want %d", n, want)
		}
		body, err := store.Get(t.Context(), objectKey)
		if err != nil {
			t.Fatalf("the file's bytes are gone: %v", err)
		}
		defer body.Close()
		if data, _ := io.ReadAll(body); string(data) != "Brakes engage within 100 ms." {
			t.Errorf("the file now holds %q", data)
		}
	}

	t.Run("a file of a requirement in the trash is not deleted", func(t *testing.T) {
		want(t, w.bob.delete(t, "/api/v1/requirements/"+w.reqKey), http.StatusNoContent, "trash R1")
		refuse(t, "deleting a file in the trash", w.bob.delete(t, "/api/v1/attachments/"+id), http.StatusConflict, "requirement_trashed")
		stays(t, 1)
		want(t, w.bob.post(t, "/api/v1/projects/"+w.key+"/trash/"+w.reqID+"/restore", nil), http.StatusOK, "restore R1")
	})

	t.Run("an archived project takes no file and gives none up", func(t *testing.T) {
		want(t, w.owner.put(t, "/api/v1/projects/"+w.key+"/archive", nil), http.StatusOK, "archive the project")
		refuse(t, "attaching in an archived project", w.bob.upload(t, files, "late.txt", []byte("Late.")), http.StatusConflict, "project_archived")
		refuse(t, "deleting in an archived project", w.bob.delete(t, "/api/v1/attachments/"+id), http.StatusConflict, "project_archived")
		stays(t, 1)
		want(t, w.owner.delete(t, "/api/v1/projects/"+w.key+"/archive"), http.StatusOK, "unarchive the project")
	})

	t.Run("an id that is no id is a bad request, for there is no field to refuse", func(t *testing.T) {
		refuse(t, "deleting no id", w.bob.delete(t, "/api/v1/attachments/nothing"), http.StatusBadRequest, "bad_request")
		stays(t, 1)
	})

	t.Run("a locked requirement takes a file, which changes none of its words", func(t *testing.T) {
		want(t, w.owner.post(t, "/api/v1/requirements/"+w.reqKey+"/lock", map[string]any{"reason": "Released"}), http.StatusOK, "lock R1")
		want(t, w.bob.upload(t, files, "test-report.txt", []byte("Passed.")), http.StatusCreated, "bob attaches to the locked R1")
		got := obj(t, want(t, w.bob.get(t, "/api/v1/requirements/"+w.reqKey), http.StatusOK, "read R1"), "requirement")
		if got["revision"] != float64(1) || got["lock"] == nil {
			t.Errorf("R1 reads revision %v, lock %v", got["revision"], got["lock"])
		}
		stays(t, 2)
	})
}
