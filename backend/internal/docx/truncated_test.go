package docx

import (
	"context"
	"strings"
	"testing"
)

// A document cut short says so after its last entry, in its reader's language, and a whole one says nothing (#310).
func TestADocumentCutShortSaysSoAfterItsLastEntry(t *testing.T) {
	for _, c := range []struct {
		lang, note string
	}{
		{"en", "This document holds the first 2 of the 3 requirements. Export a section to hold the rest."},
		{"de", "Dieses Dokument enthält die ersten 2 der 3 Anforderungen."},
	} {
		data, err := Bytes(context.Background(), aDocument(), c.lang, Reader{})
		if err != nil {
			t.Fatal(err)
		}
		body := unzip(t, data)["word/document.xml"]
		if at := strings.Index(body, c.note); at < 0 || at < strings.LastIndex(body, "derives from") {
			t.Errorf("%s: the note %q is missing or not after the last entry", c.lang, c.note)
		}
	}
	whole := aDocument()
	whole.Total = len(whole.Entries)
	data, err := Bytes(context.Background(), whole, "en", Reader{})
	if err != nil {
		t.Fatal(err)
	}
	if body := unzip(t, data)["word/document.xml"]; strings.Contains(body, "This document holds the first") {
		t.Error("a whole document says it is cut short")
	}
}
