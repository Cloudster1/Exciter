package glossary

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/cloudster1/exciter/backend/internal/document"
)

// fieldOf names the field a check refused, or "" when it held.
func fieldOf(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		return ""
	}
	var field *FieldError
	if !errors.As(err, &field) {
		t.Fatalf("a check answered %v, not a field error", err)
	}
	if !strings.HasSuffix(field.Message, ".") {
		t.Errorf("the sentence %q does not end with a period", field.Message)
	}
	return field.Field
}

func TestANameIsItsWordsOnOneLineWithinTheLimit(t *testing.T) {
	for _, c := range []struct{ raw, want, field string }{
		{"  Brake \t pedal\n", "Brake pedal", ""},
		{"", "", "name"},
		{" \t\n", "", "name"},
		{strings.Repeat("x", MaxNameLength), strings.Repeat("x", MaxNameLength), ""},
		{strings.Repeat("x", MaxNameLength+1), "", "name"},
		// The limit counts characters, not bytes.
		{strings.Repeat("ä", MaxNameLength), strings.Repeat("ä", MaxNameLength), ""},
	} {
		got, err := checkName(c.raw)
		if f := fieldOf(t, err); f != c.field || got != c.want {
			t.Errorf("checkName(%q) = %q, refused on %q; want %q on %q", c.raw, got, f, c.want, c.field)
		}
	}
}

func TestWordsDropBlanksAndRepeatsAndHoldTheirLimits(t *testing.T) {
	got, err := checkWords("synonyms", []string{" foot  brake ", "", "  ", "Foot Brake", "pedal", "PEDAL"}, MaxSynonyms)
	if err != nil || !slices.Equal(got, []string{"foot brake", "pedal"}) {
		t.Errorf("the words read %q (%v)", got, err)
	}
	if got, err := checkWords("avoid", nil, MaxAvoid); err != nil || got == nil || len(got) != 0 {
		t.Errorf("no words read %#v (%v), want an empty list", got, err)
	}
	if _, err := checkWords("avoid", []string{strings.Repeat("y", MaxNameLength+1)}, MaxAvoid); fieldOf(t, err) != "avoid" {
		t.Errorf("a word too long is refused on %v", err)
	}
	many := make([]string, MaxSynonyms+1)
	for i := range many {
		many[i] = "word" + strings.Repeat("s", i)
	}
	if _, err := checkWords("synonyms", many, MaxSynonyms); fieldOf(t, err) != "synonyms" {
		t.Errorf("%d synonyms are refused on %v", len(many), err)
	}
	// A repeat does not count against the limit, since it is dropped.
	if got, err := checkWords("synonyms", append(slices.Clone(many[:MaxSynonyms]), many[0]), MaxSynonyms); err != nil || len(got) != MaxSynonyms {
		t.Errorf("the limit with a repeat reads %d words (%v)", len(got), err)
	}
}

func TestADefinitionIsADocumentOrNothing(t *testing.T) {
	for _, raw := range []json.RawMessage{nil, json.RawMessage("null")} {
		if got, err := checkDefinition(raw); err != nil || string(got) != string(document.Empty) {
			t.Errorf("no definition %q reads %s (%v)", raw, got, err)
		}
	}
	doc := json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"The lever."}]}]}`)
	if got, err := checkDefinition(doc); err != nil || string(got) != string(doc) {
		t.Errorf("a document reads %s (%v)", got, err)
	}
	for _, raw := range []string{`{"type":"nonsense"}`, `{"type":"doc","content":[{"type":"script"}]}`, `"words"`} {
		if _, err := checkDefinition(json.RawMessage(raw)); fieldOf(t, err) != "definition" {
			t.Errorf("%s is refused on %v", raw, err)
		}
	}
}

func TestAStatusIsOneOfThree(t *testing.T) {
	for _, s := range Statuses {
		if err := checkStatus(s); err != nil {
			t.Errorf("%s is refused: %v", s, err)
		}
	}
	for _, s := range []Status{"", "Approved", "retired"} {
		if fieldOf(t, checkStatus(s)) != "status" {
			t.Errorf("%q is taken", s)
		}
	}
}

func TestACommentIsTrimmedWithinTheLimit(t *testing.T) {
	if got, err := checkComment("  Holding counts too.\n"); err != nil || got != "Holding counts too." {
		t.Errorf("the comment reads %q (%v)", got, err)
	}
	if got, err := checkComment(""); err != nil || got != "" {
		t.Errorf("no comment reads %q (%v)", got, err)
	}
	if _, err := checkComment(strings.Repeat("ü", MaxCommentChars)); err != nil {
		t.Errorf("a comment at the limit is refused: %v", err)
	}
	if _, err := checkComment(strings.Repeat("ü", MaxCommentChars+1)); fieldOf(t, err) != "comment" {
		t.Errorf("a comment over the limit is refused on %v", err)
	}
}

func TestAPatchChangesOnlyWhatItNames(t *testing.T) {
	cur := &Term{TermRef: TermRef{Name: "Brake pedal", Status: StatusProposed}, Definition: json.RawMessage(`{"type":"doc","content":[]}`),
		Synonyms: []string{"foot brake"}, Avoid: []string{"stopper"}}

	t.Run("a patch that names nothing keeps every field", func(t *testing.T) {
		next, err := patched(cur, TermPatch{BaseRevision: 1})
		if err != nil || next.name != cur.Name || string(next.definition) != string(cur.Definition) || !slices.Equal(next.synonyms, cur.Synonyms) ||
			!slices.Equal(next.avoid, cur.Avoid) || next.status != cur.Status || next.comment != "" {
			t.Errorf("the patch reads %+v (%v)", next, err)
		}
	})

	t.Run("each named field is checked as when the term is made", func(t *testing.T) {
		name, status, synonyms := "  Pedal ", StatusApproved, []string{"lever", "Lever"}
		next, err := patched(cur, TermPatch{Name: &name, Status: &status, Synonyms: &synonyms, Comment: " Shorter. "})
		if err != nil || next.name != "Pedal" || next.status != StatusApproved || !slices.Equal(next.synonyms, []string{"lever"}) ||
			!slices.Equal(next.avoid, cur.Avoid) || next.comment != "Shorter." {
			t.Errorf("the patch reads %+v (%v)", next, err)
		}
	})

	blank, bad, junk := " ", Status("gone"), json.RawMessage(`{"type":"nonsense"}`)
	long, tooMany := strings.Repeat("c", MaxCommentChars+1), make([]string, MaxAvoid+1)
	for i := range tooMany {
		tooMany[i] = strings.Repeat("a", i+1)
	}
	for _, c := range []struct {
		what  string
		patch TermPatch
		field string
	}{
		{"a blank name", TermPatch{Name: &blank}, "name"},
		{"an unknown status", TermPatch{Status: &bad}, "status"},
		{"a definition that is no document", TermPatch{Definition: &junk}, "definition"},
		{"too many words to avoid", TermPatch{Avoid: &tooMany}, "avoid"},
		{"a comment too long", TermPatch{Comment: long}, "comment"},
	} {
		if _, err := patched(cur, c.patch); fieldOf(t, err) != c.field {
			t.Errorf("%s is refused on %v, want %s", c.what, err, c.field)
		}
	}
}

func TestTheSameDocumentIsWhatItSaysNotHowItIsSpelled(t *testing.T) {
	a := json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"The lever."}]}]}`)
	spaced := json.RawMessage(`{ "content": [ {"content":[{"text":"The lever.","type":"text"}], "type":"paragraph"} ], "type": "doc" }`)
	other := json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"The pedal."}]}]}`)
	if !sameDocument(a, spaced) {
		t.Error("the same document spelled otherwise reads as another")
	}
	if sameDocument(a, other) {
		t.Error("another text reads as the same document")
	}
	// What does not read as JSON is compared as it is written.
	if !sameDocument(json.RawMessage("{"), json.RawMessage("{")) || sameDocument(json.RawMessage("{"), a) {
		t.Error("broken JSON is not compared byte for byte")
	}
}
