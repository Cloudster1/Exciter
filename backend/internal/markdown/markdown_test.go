package markdown

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudster1/exciter/backend/internal/document"
)

const (
	fileID    = "0190a8f0-0000-7000-8000-000000000001"
	pictureID = "0190a8f0-0000-7000-8000-000000000002"
	personID  = "0190a8f0-0000-7000-8000-0000000000aa"
)

var known = Resolve{
	File: func(id string) (string, bool) {
		switch id {
		case fileID:
			return "brake plan.pdf", true
		case pictureID:
			return "pedal.png", true
		}
		return "", false
	},
	Person: func(id string) bool { return id == personID },
}

// doc reads a document as the editor writes it, every attribute it sets in place.
func doc(t *testing.T, blocks string) document.Node {
	t.Helper()
	var n document.Node
	if err := json.Unmarshal([]byte(`{"type":"doc","content":[`+blocks+`]}`), &n); err != nil {
		t.Fatalf("%v: %s", err, blocks)
	}
	if _, err := document.Parse(mustJSON(t, n)); err != nil {
		t.Fatalf("the sample is not a document the allowlist takes: %v", err)
	}
	return n
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// same compares two documents as JSON reads them, so an int and a float64 of one number are one number.
func same(t *testing.T, a, b document.Node) bool {
	t.Helper()
	var x, y any
	_ = json.Unmarshal(mustJSON(t, a), &x)
	_ = json.Unmarshal(mustJSON(t, b), &y)
	return reflect.DeepEqual(x, y)
}

const (
	link   = `{"type":"link","attrs":{"href":"https://example.com/brakes","target":"_blank","rel":"noopener noreferrer nofollow","class":null,"title":%s}}`
	cell   = `{"colspan":1,"rowspan":1,"colwidth":null%s}`
	single = `{"type":"paragraph","content":[{"type":"text","text":%q}]}`
)

func linked(title string) string { return strings.Replace(link, "%s", title, 1) }
func cellAttrs(align string) string {
	if align == "" {
		return strings.Replace(cell, "%s", "", 1)
	}
	return strings.Replace(cell, "%s", `,"align":"`+align+`"`, 1)
}

// Every node and mark a requirement holds goes to Markdown and back as it was, which is what makes a statement
// read as Markdown safe to write back.
func TestEveryNodeAndMarkComesBackAsItLeft(t *testing.T) {
	samples := map[string]string{
		"marks": `{"type":"paragraph","content":[
			{"type":"text","text":"Brakes "},{"type":"text","text":"engage","marks":[{"type":"bold"}]},
			{"type":"text","text":" "},{"type":"text","text":"within","marks":[{"type":"italic"}]},{"type":"text","text":" "},
			{"type":"text","text":"100 ms","marks":[{"type":"code"}]},
			{"type":"text","text":", never","marks":[{"type":"strike"}]},{"type":"text","text":" later than "},
			{"type":"text","text":"the norm","marks":[` + linked(`null`) + `]},{"type":"text","text":" or "},
			{"type":"text","text":"its annex","marks":[` + linked(`"Annex B"`) + `,{"type":"bold"}]}]}`,
		"a break and what looks like Markdown": `{"type":"paragraph","content":[
			{"type":"text","text":"1. not a list * _ [x] <b> &amp; # | ~ \\ back"},{"type":"hardBreak"},{"type":"text","text":"- nor this"}]}`,
		"headings": `{"type":"heading","attrs":{"level":1},"content":[{"type":"text","text":"Scope"}]},
			{"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"Braking # 2"}]},
			{"type":"heading","attrs":{"level":3},"content":[{"type":"text","text":"Pedal","marks":[{"type":"italic"}]}]}`,
		"lists": `{"type":"bulletList","content":[
			{"type":"listItem","content":[` + strings.Replace(single, "%q", `"Front"`, 1) + `,
				{"type":"orderedList","attrs":{"start":3,"type":null},"content":[
					{"type":"listItem","content":[` + strings.Replace(single, "%q", `"Third"`, 1) + `]},
					{"type":"listItem","content":[` + strings.Replace(single, "%q", `"Fourth"`, 1) + `]}]}]},
			{"type":"listItem","content":[` + strings.Replace(single, "%q", `"Rear"`, 1) + `]}]}`,
		"a loose list": `{"type":"orderedList","attrs":{"start":1,"type":null},"content":[
			{"type":"listItem","content":[` + strings.Replace(single, "%q", `"First"`, 1) + `,` + strings.Replace(single, "%q", `"More of it"`, 1) + `]},
			{"type":"listItem","content":[` + strings.Replace(single, "%q", `"Second"`, 1) + `]}]}`,
		"two lists in a row": `{"type":"bulletList","content":[{"type":"listItem","content":[` + strings.Replace(single, "%q", `"One"`, 1) + `]}]},
			{"type":"bulletList","content":[{"type":"listItem","content":[` + strings.Replace(single, "%q", `"Two"`, 1) + `]}]}`,
		"a quote": `{"type":"blockquote","content":[` + strings.Replace(single, "%q", `"Quoted"`, 1) + `,
			{"type":"bulletList","content":[{"type":"listItem","content":[` + strings.Replace(single, "%q", `"In it"`, 1) + `]}]}]}`,
		"code": `{"type":"codeBlock","attrs":{"language":"go"},"content":[{"type":"text","text":"if engaged {\n\treturn ` + "```" + `\n}"}]},
			{"type":"codeBlock","attrs":{"language":null},"content":[{"type":"text","text":"plain"}]}`,
		"a rule": strings.Replace(single, "%q", `"Above"`, 1) + `,{"type":"horizontalRule"},` + strings.Replace(single, "%q", `"Below"`, 1),
		"a table": `{"type":"table","content":[
			{"type":"tableRow","content":[
				{"type":"tableHeader","attrs":` + cellAttrs("left") + `,"content":[` + strings.Replace(single, "%q", `"Signal"`, 1) + `]},
				{"type":"tableHeader","attrs":` + cellAttrs("right") + `,"content":[` + strings.Replace(single, "%q", `"Within | ms"`, 1) + `]}]},
			{"type":"tableRow","content":[
				{"type":"tableCell","attrs":` + cellAttrs("left") + `,"content":[{"type":"paragraph","content":[
					{"type":"text","text":"Pedal","marks":[{"type":"bold"}]},{"type":"hardBreak"},{"type":"text","text":"pressed"}]}]},
				{"type":"tableCell","attrs":` + cellAttrs("right") + `,"content":[` + strings.Replace(single, "%q", `"100"`, 1) + `]}]}]}`,
		"files and people": `{"type":"paragraph","content":[{"type":"text","text":"Ask "},
			{"type":"mention","attrs":{"id":"` + personID + `","label":"Ada Lovelace","mentionSuggestionChar":"@"}},
			{"type":"text","text":" about "},{"type":"attachment","attrs":{"attachmentId":"` + fileID + `","fileName":"brake plan.pdf"}}]},
			{"type":"image","attrs":{"attachmentId":"` + pictureID + `","alt":"The pedal","width":null}},
			{"type":"image","attrs":{"attachmentId":"` + pictureID + `","alt":"Smaller","width":320}}`,
	}
	for name, blocks := range samples {
		t.Run(name, func(t *testing.T) {
			want := doc(t, blocks)
			if !Holds(want) {
				t.Fatal("Holds says Markdown cannot hold it")
			}
			written := Render(want)
			got, err := Convert(written, known)
			if err != nil {
				t.Fatalf("%v\n%s", err, written)
			}
			if !same(t, got, want) {
				t.Errorf("it came back otherwise:\n%s\ngot  %s\nwant %s", written, mustJSON(t, got), mustJSON(t, want))
			}
		})
	}
}

// Where Markdown would read back less than the document holds, a reader is told so by Holds rather than handed it.
func TestHoldsSaysWhereMarkdownWouldLoseSomething(t *testing.T) {
	header := `{"type":"tableHeader","attrs":` + cellAttrs("") + `,"content":[` + strings.Replace(single, "%q", `"H"`, 1) + `]}`
	body := `{"type":"tableCell","attrs":` + cellAttrs("") + `,"content":[` + strings.Replace(single, "%q", `"C"`, 1) + `]}`
	cases := map[string]string{
		"a list numbered by letters": `{"type":"orderedList","attrs":{"start":1,"type":"a"},"content":[{"type":"listItem","content":[` +
			strings.Replace(single, "%q", `"A"`, 1) + `]}]}`,
		"a cell that spans": `{"type":"table","content":[{"type":"tableRow","content":[{"type":"tableHeader","attrs":{"colspan":2,"rowspan":1,"colwidth":null},"content":[` +
			strings.Replace(single, "%q", `"Wide"`, 1) + `]}]}]}`,
		"a table without a header row": `{"type":"table","content":[{"type":"tableRow","content":[` + body + `]}]}`,
		"a header below the first row": `{"type":"table","content":[{"type":"tableRow","content":[` + header + `]},{"type":"tableRow","content":[` + header + `]}]}`,
		"a cell of two paragraphs": `{"type":"table","content":[{"type":"tableRow","content":[{"type":"tableHeader","attrs":` + cellAttrs("") + `,"content":[` +
			strings.Replace(single, "%q", `"One"`, 1) + `,` + strings.Replace(single, "%q", `"Two"`, 1) + `]}]}]}`,
		"a break that ends a paragraph": `{"type":"paragraph","content":[{"type":"text","text":"End"},{"type":"hardBreak"}]}`,
	}
	for name, blocks := range cases {
		if Holds(doc(t, blocks)) {
			t.Errorf("%s: Holds says Markdown holds it", name)
		}
	}
	if !Holds(doc(t, strings.Replace(single, "%q", `"Plain"`, 1)+`,{"type":"paragraph"}`)) {
		t.Error("an empty paragraph, which is only spacing, keeps a document from Markdown")
	}
}

// What would not come back as written is refused with a sentence that says what to write instead.
func TestWhatWouldNotComeAcrossIsRefusedWithASentence(t *testing.T) {
	cases := map[string]struct{ src, says string }{
		"an HTML block":              {"<div>Brakes</div>", "holds no HTML"},
		"an inline tag":              {"Brakes <b>engage</b>", "holds no HTML"},
		"a heading too deep":         {"#### Detail", "level 3"},
		"a picture from elsewhere":   {"![x](https://example.com/x.png)", "only its own pictures"},
		"a file it does not have":    {"[x](" + FileScheme + "0190a8f0-0000-7000-8000-00000000ffff)", "not a file of this requirement"},
		"a picture it does not have": {"![x](" + FileScheme + "0190a8f0-0000-7000-8000-00000000ffff)", "not a file of this requirement"},
		"a mention of nobody":        {`<span data-exciter="mention" data-id="0190a8f0-0000-7000-8000-0000000000bb">@Nobody</span>`, "names nobody"},
		"a link that runs code":      {"[x](javascript:alert(1))", "not a web, mail or site address"},
		"a width that is none":       {`<img src="` + FileScheme + pictureID + `" alt="x" width="wide">`, "number of pixels"},
		"too deep a nest":            {strings.Repeat("> ", document.MaxDepth+2) + "deep", "too deeply"},
		"too much":                   {strings.Repeat("a", MaxSourceBytes+1), "over 1 MB"},
	}
	for name, tc := range cases {
		_, err := Convert(tc.src, known)
		var refusal *Refusal
		if !errors.As(err, &refusal) || !errors.Is(err, ErrRefused) || !strings.Contains(refusal.Message, tc.says) {
			t.Errorf("%s: %v, want a refusal saying %q", name, err, tc.says)
		}
	}
	if got, err := Convert("", known); err != nil || len(got.Content) != 1 || got.Content[0].Type != "paragraph" {
		t.Errorf("nothing reads as the empty document: %v %+v", err, got)
	}
}
