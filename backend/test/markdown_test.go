//go:build integration

package test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// fieldSays is the sentence a refusal gives a field.
func fieldSays(t *testing.T, r response, field string) string {
	t.Helper()
	return fieldError(t, want(t, r, http.StatusUnprocessableEntity, "refuse "+field), field)
}

// Requirements and comments are written in Markdown and read back as the document the editor shows and as the same
// Markdown, by a script or an assistant alike (#186).
func TestRequirementsAreWrittenAndReadInMarkdown(t *testing.T) {
	key := projectKey(t, "MDN")
	f := newProjectFixture(t)
	want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": key}), http.StatusCreated, "make "+key)
	written := "Brakes **engage** within `100 ms`.\n\n- front\n- rear"
	made := obj(t, want(t, f.owner.post(t, "/api/v1/projects/"+key+"/requirements", map[string]any{
		"title": "Brakes", "statementMarkdown": written, "rationaleMarkdown": "Asked by *the norm*.",
	}), http.StatusCreated, "write in Markdown"), "requirement")
	reqKey := made["key"].(string)
	path := "/api/v1/requirements/" + reqKey

	t.Run("it is stored as the editor's document and read back as written", func(t *testing.T) {
		got := obj(t, want(t, f.owner.get(t, path), http.StatusOK, "read the document"), "requirement")
		statement := got["statement"].(map[string]any)["content"].([]any)
		if len(statement) != 2 || statement[1].(map[string]any)["type"] != "bulletList" || got["statementMarkdown"] != nil {
			t.Errorf("the document reads %v, Markdown %v", got["statement"], got["statementMarkdown"])
		}
		md := obj(t, want(t, f.owner.get(t, path+"?format=markdown"), http.StatusOK, "read Markdown"), "requirement")
		if md["statementMarkdown"] != written || md["rationaleMarkdown"] != "Asked by *the norm*." {
			t.Errorf("the Markdown reads %q and %q", md["statementMarkdown"], md["rationaleMarkdown"])
		}
		revision := obj(t, want(t, f.owner.get(t, path+"/revisions/1?format=markdown"), http.StatusOK, "read revision 1"), "revision")
		if revision["statementMarkdown"] != written {
			t.Errorf("revision 1 reads %v", revision["statementMarkdown"])
		}
		entries := list(t, want(t, f.owner.get(t, "/api/v1/projects/"+key+"/spec?format=markdown"), http.StatusOK, "read the specification"), "entries")
		if len(entries) != 1 || entries[0].(map[string]any)["statementMarkdown"] != written {
			t.Errorf("the specification reads %v", entries)
		}
		refuse(t, "a format nobody has", f.owner.get(t, path+"?format=html"), http.StatusUnprocessableEntity, "validation_failed")
	})

	t.Run("what Markdown cannot hold is left out rather than read short", func(t *testing.T) {
		cell := func(kind string, colspan int) map[string]any {
			return map[string]any{"type": kind, "attrs": map[string]any{"colspan": colspan, "rowspan": 1, "colwidth": nil},
				"content": []any{map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": "Wide"}}}}}
		}
		spanning := map[string]any{"type": "doc", "content": []any{map[string]any{"type": "table", "content": []any{
			map[string]any{"type": "tableRow", "content": []any{cell("tableHeader", 2)}}}}}}
		wide := obj(t, want(t, f.owner.post(t, "/api/v1/projects/"+key+"/requirements", map[string]any{"title": "Wide", "statement": spanning}),
			http.StatusCreated, "write a spanning table"), "requirement")
		got := obj(t, want(t, f.owner.get(t, "/api/v1/requirements/"+wide["key"].(string)+"?format=markdown"), http.StatusOK, "read it"), "requirement")
		if _, answered := got["statementMarkdown"]; answered {
			t.Errorf("a spanning cell is answered as Markdown: %v", got["statementMarkdown"])
		}
	})

	t.Run("a change in Markdown may show the requirement's own files and mention its people", func(t *testing.T) {
		file := obj(t, want(t, f.owner.uploadAs(t, path+"/attachments", "pedal.png", "image/png", aPicture(t)), http.StatusCreated, "attach a picture"), "attachment")
		picture := "![The pedal](exciter:attachment/" + file["id"].(string) + ")"
		mention := `Ask <span data-exciter="mention" data-id="` + f.plain.user.String() + `">@Someone</span>.`
		changed := obj(t, want(t, f.owner.patch(t, path, map[string]any{"baseRevision": 1, "statementMarkdown": picture + "\n\n" + mention}),
			http.StatusOK, "change it in Markdown"), "requirement")
		if changed["revision"] != float64(2) {
			t.Errorf("the change wrote revision %v", changed["revision"])
		}
		md := obj(t, want(t, f.owner.get(t, path+"?format=markdown"), http.StatusOK, "read it back"), "requirement")
		if md["statementMarkdown"] != picture+"\n\n"+mention {
			t.Errorf("it reads back %q", md["statementMarkdown"])
		}
		cleared := obj(t, want(t, f.owner.patch(t, path, map[string]any{"baseRevision": 2, "rationaleMarkdown": " "}), http.StatusOK, "clear the rationale"), "requirement")
		if cleared["rationale"] != nil {
			t.Errorf("a rationale of no words is kept: %v", cleared["rationale"])
		}
	})

	t.Run("what would not come back as written is refused, a sentence on the Markdown field", func(t *testing.T) {
		write := func(body map[string]any) response {
			return f.owner.post(t, "/api/v1/projects/"+key+"/requirements", body)
		}
		empty := map[string]any{"type": "doc", "content": []any{map[string]any{"type": "paragraph"}}}
		cases := map[string]struct {
			body       map[string]any
			field, say string
		}{
			"both forms":               {map[string]any{"title": "X", "statement": empty, "statementMarkdown": "x"}, "statementMarkdown", "not both"},
			"HTML":                     {map[string]any{"title": "X", "statementMarkdown": "<div>x</div>"}, "statementMarkdown", "no HTML"},
			"a picture from elsewhere": {map[string]any{"title": "X", "statementMarkdown": "![x](https://example.com/x.png)"}, "statementMarkdown", "only its own pictures"},
			"a file before it has any": {map[string]any{"title": "X", "rationaleMarkdown": "[x](exciter:attachment/" + uuid.NewString() + ")"}, "rationaleMarkdown", "not a file"},
			"a mention of nobody": {map[string]any{"title": "X", "statementMarkdown": `<span data-exciter="mention" data-id="` + uuid.NewString() + `">@Nobody</span>`},
				"statementMarkdown", "names nobody"},
			"a heading too deep": {map[string]any{"title": "X", "statementMarkdown": "#### Deep"}, "statementMarkdown", "level 3"},
		}
		for name, tc := range cases {
			if says := fieldSays(t, write(tc.body), tc.field); !strings.Contains(says, tc.say) {
				t.Errorf("%s: %q, want it to say %q", name, says, tc.say)
			}
		}
	})

	t.Run("comments are written and read in Markdown", func(t *testing.T) {
		thread := obj(t, want(t, f.owner.post(t, path+"/comments", map[string]any{"bodyMarkdown": "Why **100 ms**?"}), http.StatusCreated, "comment in Markdown"), "thread")
		threadID := thread["id"].(string)
		want(t, f.owner.post(t, "/api/v1/comments/"+threadID+"/replies", map[string]any{"bodyMarkdown": "- the norm\n- the test"}), http.StatusCreated, "reply in Markdown")
		read := obj(t, want(t, f.owner.get(t, "/api/v1/comments/"+threadID+"?format=markdown"), http.StatusOK, "read the thread"), "thread")
		comments := read["comments"].([]any)
		if comments[0].(map[string]any)["bodyMarkdown"] != "Why **100 ms**?" || comments[1].(map[string]any)["bodyMarkdown"] != "- the norm\n- the test" {
			t.Errorf("the thread reads %v", comments)
		}
		listed := list(t, want(t, f.owner.get(t, path+"/comments?format=markdown"), http.StatusOK, "list the threads"), "threads")
		if listed[0].(map[string]any)["comments"].([]any)[0].(map[string]any)["bodyMarkdown"] != "Why **100 ms**?" {
			t.Errorf("the list reads %v", listed)
		}
		if says := fieldSays(t, f.owner.post(t, path+"/comments", map[string]any{"bodyMarkdown": "| a |\n| - |\n| b |"}), "bodyMarkdown"); says == "" {
			t.Error("a table, which a comment holds none of, was refused without a sentence")
		}
		if says := fieldSays(t, f.owner.post(t, path+"/comments", map[string]any{"bodyMarkdown": "x", "body": commentBody("x")["body"]}), "bodyMarkdown"); !strings.Contains(says, "not both") {
			t.Errorf("both forms of a comment: %q", says)
		}
	})

	t.Run("an assistant reads and writes Markdown without asking", func(t *testing.T) {
		_, secret := makeToken(t, f.owner, map[string]any{"name": "assistant"})
		f.h.settle(t)
		a := connectAssistant(t, f.api, secret)
		made := a.call(t, "create_requirement", map[string]any{"projectKey": key, "title": "Lights", "statementMarkdown": "Lights *blink* twice."})["requirement"].(map[string]any)
		got := a.call(t, "get_requirement", map[string]any{"reqKey": made["key"]})["requirement"].(map[string]any)
		if got["statementMarkdown"] != "Lights *blink* twice." {
			t.Errorf("get_requirement reads %v", got["statementMarkdown"])
		}
		if plain := a.call(t, "get_requirement", map[string]any{"reqKey": made["key"], "format": "document"})["requirement"].(map[string]any); plain["statementMarkdown"] != nil {
			t.Errorf("asked for the document alone, the assistant reads %v", plain["statementMarkdown"])
		}
	})
}
