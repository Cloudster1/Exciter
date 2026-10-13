package document

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestRewriteReferencesPointsCopiesAtTheirOwnFiles(t *testing.T) {
	old1, old2, other := uuid.New(), uuid.New(), uuid.New()
	new1, new2 := uuid.New(), uuid.New()
	body := `{"type":"doc","content":[
		{"type":"image","attrs":{"attachmentId":"` + old1.String() + `","alt":"x","width":480}},
		{"type":"paragraph","content":[{"type":"text","text":"see "},{"type":"attachment","attrs":{"attachmentId":"` + old2.String() + `","fileName":"a.pdf"}}]},
		{"type":"image","attrs":{"attachmentId":"` + other.String() + `","alt":null,"width":null}},
		{"type":"mention","attrs":{"attachmentId":"` + old1.String() + `"}}]}`
	out, changed, err := RewriteReferences(json.RawMessage(body), map[uuid.UUID]uuid.UUID{old1: new1, old2: new2})
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	text := string(out)
	for _, want := range []string{new1.String(), new2.String(), other.String(), `"width":480`} {
		if !strings.Contains(text, want) {
			t.Errorf("%s lacks %s", text, want)
		}
	}
	if strings.Count(text, old1.String()) != 1 {
		t.Errorf("only file nodes change, and every one of them: %s", text)
	}

	same := `{"type":"doc","content":[{"type":"paragraph"}]}`
	out, changed, err = RewriteReferences(json.RawMessage(same), map[uuid.UUID]uuid.UUID{old1: new1})
	if err != nil || changed || string(out) != same {
		t.Fatalf("a body without files changed: %s %v %v", out, changed, err)
	}
}

func TestRewriteReferencesReachesRichTextValues(t *testing.T) {
	old, fresh := uuid.New(), uuid.New()
	value := `{"type":"doc","content":[{"type":"image","attrs":{"attachmentId":"` + old.String() + `","alt":null,"width":null}}]}`
	out, changed, err := RewriteReferences(json.RawMessage(value), map[uuid.UUID]uuid.UUID{old: fresh})
	if err != nil || !changed || !strings.Contains(string(out), fresh.String()) {
		t.Fatalf("a rich text value kept the original's file: %s %v %v", out, changed, err)
	}
	if _, err := Parse(out); err != nil {
		t.Fatalf("the rewritten value is no longer a document: %v", err)
	}
}

func TestAStatementShowsImagesAndFiles(t *testing.T) {
	id := uuid.New().String()
	doc := `{"type":"doc","content":[{"type":"image","attrs":{"attachmentId":"` + id + `","alt":"The basket","width":320}},
		{"type":"paragraph","content":[{"type":"text","text":"See "},{"type":"attachment","attrs":{"attachmentId":"` + id + `","fileName":"spec.pdf"}}]}]}`
	if _, err := Parse(json.RawMessage(doc)); err != nil {
		t.Fatalf("a statement with an image and a file was refused: %v", err)
	}
	for name, bad := range map[string]string{
		"an image in a line":     `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"image","attrs":{"attachmentId":"` + id + `","alt":null,"width":null}}]}]}`,
		"a file without an id":   `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"attachment","attrs":{"attachmentId":"x","fileName":"a"}}]}]}`,
		"an image too wide":      `{"type":"doc","content":[{"type":"image","attrs":{"attachmentId":"` + id + `","alt":null,"width":` + strconv.Itoa(MaxImageWidth+1) + `}}]}`,
		"a description too long": `{"type":"doc","content":[{"type":"image","attrs":{"attachmentId":"` + id + `","alt":"` + strings.Repeat("a", MaxAltLength+1) + `","width":null}}]}`,
	} {
		if _, err := Parse(json.RawMessage(bad)); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}
