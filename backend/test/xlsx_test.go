//go:build integration

package test

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"
)

// workbookText reads a downloaded workbook's sheet and its strings, as one text to look for words in.
func workbookText(t *testing.T, r response) string {
	t.Helper()
	if got := r.Header.Get("Content-Type"); got != "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" {
		t.Errorf("a workbook came as %q", got)
	}
	z, err := zip.NewReader(bytes.NewReader(r.Raw), int64(len(r.Raw)))
	if err != nil {
		t.Fatalf("a workbook is no zip: %v", err)
	}
	var b strings.Builder
	for _, f := range z.File {
		if f.Name != "xl/sharedStrings.xml" && f.Name != "xl/worksheets/sheet1.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(rc)
		rc.Close()
		b.Write(body)
	}
	return b.String()
}

// The matrix, the chains and a baseline's matrix as workbooks (#154), each holding what its CSV file holds.
func TestTheMatrixAndItsChainsDownloadAsWorkbooks(t *testing.T) {
	s := newTraceScene(t, "XLS")
	want(t, s.owner.put(t, "/api/v1/projects/"+s.key+"/trace-model", s.model(t)), http.StatusOK, "set the trace model")
	t.Run("the matrix", func(t *testing.T) {
		text := workbookText(t, want(t, s.carol.get(t, "/api/v1/projects/"+s.key+"/matrix.xlsx?linkType=derives"), http.StatusOK, "the matrix workbook"))
		for _, word := range []string{s.keys["S1"], "Derives", "Links", `state="frozen"`, "<autoFilter"} {
			if !strings.Contains(text, word) {
				t.Errorf("the matrix workbook lacks %q", word)
			}
		}
		fieldError(t, s.carol.get(t, "/api/v1/projects/"+s.key+"/matrix.xlsx?columns=everything"), "columns")
	})
	t.Run("the chains", func(t *testing.T) {
		text := workbookText(t, want(t, s.carol.get(t, "/api/v1/projects/"+s.key+"/trace-chains.xlsx"), http.StatusOK, "the chains workbook"))
		for _, word := range []string{"Need", "Breaks after", "WEB-9", s.keys["N2"]} {
			if !strings.Contains(text, word) {
				t.Errorf("the chains workbook lacks %q", word)
			}
		}
	})
	t.Run("a baseline's matrix", func(t *testing.T) {
		taken := obj(t, want(t, s.owner.post(t, "/api/v1/projects/"+s.key+"/baselines", map[string]any{"name": "Workbook"}), http.StatusCreated, "take a baseline"), "baseline")
		text := workbookText(t, want(t, s.carol.get(t, "/api/v1/baselines/"+taken["id"].(string)+"/matrix.xlsx?with=now"), http.StatusOK, "the baseline workbook"))
		if !strings.Contains(text, "Derives") || !strings.Contains(text, s.keys["S1"]) {
			t.Errorf("the baseline workbook reads %s", text)
		}
	})
}
