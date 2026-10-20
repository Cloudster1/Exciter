//go:build integration

package test

import (
	"net/http"
	"strings"
	"testing"
)

const pumpDiagram = "@startuml\ntitle Pump control\n[Pump] --> [Valve] : opens\n@enduml"

// A diagram (#284) is kept as its PlantUML source in a requirement's text, drawn by the stack's Kroki, found by its
// words, compared as its source, carried by Markdown and drawn into Word; a comment holds none.
func TestADiagramIsKeptAsItsSourceAndDrawnByKroki(t *testing.T) {
	f := newProjectFixture(t)
	if f.h.cfg.Kroki.URL == "" {
		t.Fatal("EXCITER_KROKI_URL is not set; run the suite with make test-integration against the running stack")
	}
	key := projectKey(t, "DGM")
	want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": key}), http.StatusCreated, "make "+key)
	written := "The parts:\n\n```plantuml\n" + pumpDiagram + "\n```"
	made := obj(t, want(t, f.owner.post(t, "/api/v1/projects/"+key+"/requirements", map[string]any{"title": "Pump", "statementMarkdown": written}),
		http.StatusCreated, "write a diagram in Markdown"), "requirement")
	path := "/api/v1/requirements/" + made["key"].(string)

	t.Run("it is stored as its source and read back as the fence it was written as", func(t *testing.T) {
		got := obj(t, want(t, f.plain.get(t, path), http.StatusOK, "read it"), "requirement")
		block := got["statement"].(map[string]any)["content"].([]any)[1].(map[string]any)
		attrs, _ := block["attrs"].(map[string]any)
		if block["type"] != "diagram" || attrs["language"] != "plantuml" || attrs["source"] != pumpDiagram {
			t.Errorf("the statement holds %v", block)
		}
		md := obj(t, want(t, f.plain.get(t, path+"?format=markdown"), http.StatusOK, "read Markdown"), "requirement")
		if md["statementMarkdown"] != written {
			t.Errorf("the Markdown reads %q", md["statementMarkdown"])
		}
	})

	t.Run("search finds the words of its source", func(t *testing.T) {
		hits := hitKeys(t, want(t, f.plain.get(t, "/api/v1/search?q=Valve"), http.StatusOK, "search for a label"))
		found := false
		for _, hit := range hits {
			found = found || hit == made["key"]
		}
		if !found {
			t.Errorf("a label of the diagram does not find it: %v", hits)
		}
	})

	t.Run("anybody signed in has it drawn, and broken source is refused in the renderer's words", func(t *testing.T) {
		drawn := want(t, f.plain.post(t, "/api/v1/diagrams", map[string]any{"language": "plantuml", "source": pumpDiagram}), http.StatusOK, "draw it")
		if svg, _ := drawn.Body["svg"].(string); !strings.Contains(svg, "<svg") || !strings.Contains(svg, "Valve") {
			t.Errorf("the drawing is %.200q", svg)
		}
		broken := want(t, f.plain.post(t, "/api/v1/diagrams", map[string]any{"language": "plantuml", "source": "@startuml\nclass A {\nfoo(\n@enduml"}),
			http.StatusUnprocessableEntity, "draw broken source")
		refusal, _ := broken.Body["error"].(map[string]any)
		fields, _ := refusal["fields"].(map[string]any)
		if refusal["code"] != "diagram_source" || fields["source"] == "" || fields["source"] == nil {
			t.Errorf("broken source is refused as %v", broken.Body)
		}
		refuse(t, "another language", f.plain.post(t, "/api/v1/diagrams", map[string]any{"language": "mermaid", "source": "graph TD"}),
			http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "blank source", f.plain.post(t, "/api/v1/diagrams", map[string]any{"language": "plantuml", "source": " "}),
			http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "nobody signed in", f.api.anonymous().post(t, "/api/v1/diagrams", map[string]any{"language": "plantuml", "source": pumpDiagram}),
			http.StatusUnauthorized, "unauthorized")
	})

	t.Run("a diagram reads no file of the renderer's", func(t *testing.T) {
		got := want(t, f.plain.post(t, "/api/v1/diagrams", map[string]any{"language": "plantuml", "source": "@startuml\n!include /etc/passwd\nA -> B\n@enduml"}),
			http.StatusOK, "draw an include")
		if svg, _ := got.Body["svg"].(string); strings.Contains(svg, "root:") {
			t.Error("the renderer read a file a diagram named")
		}
	})

	t.Run("a changed diagram compares as its source", func(t *testing.T) {
		changed := strings.Replace(written, "[Valve]", "[Tank]", 1)
		want(t, f.owner.patch(t, path, map[string]any{"baseRevision": 1, "statementMarkdown": changed}), http.StatusOK, "change the diagram")
		c := obj(t, want(t, f.plain.get(t, path+"/diff"), http.StatusOK, "compare"), "comparison")
		block := c["statement"].([]any)[1].(map[string]any)
		node, _ := block["node"].(map[string]any)
		if block["change"] != "modified" || node["type"] != "codeBlock" || !strings.Contains(stringOf(node), "Tank") {
			t.Errorf("the changed diagram compares as %v", block)
		}
	})

	t.Run("Word draws it as a picture described by its title", func(t *testing.T) {
		body := unzipped(t, want(t, f.plain.get(t, "/api/v1/projects/"+key+"/document.docx"), http.StatusOK, "export Word").Raw)
		if body["word/media/picture1.png"] == nil || !strings.Contains(string(body["word/document.xml"]), `descr="Pump control"`) {
			t.Error("the diagram is not drawn into the Word file")
		}
	})

	t.Run("a comment keeps a plantuml fence as code", func(t *testing.T) {
		thread := obj(t, want(t, f.owner.post(t, path+"/comments", map[string]any{"bodyMarkdown": "```plantuml\n" + pumpDiagram + "\n```"}),
			http.StatusCreated, "comment with a fence"), "thread")
		read := obj(t, want(t, f.owner.get(t, "/api/v1/comments/"+thread["id"].(string)), http.StatusOK, "read the thread"), "thread")
		first := read["comments"].([]any)[0].(map[string]any)["body"].(map[string]any)["content"].([]any)[0].(map[string]any)
		if first["type"] != "codeBlock" {
			t.Errorf("a comment's fence became %v", first["type"])
		}
	})
}

// stringOf is a node's text, its children's joined.
func stringOf(n map[string]any) string {
	var b strings.Builder
	if text, ok := n["text"].(string); ok {
		b.WriteString(text)
	}
	children, _ := n["content"].([]any)
	for _, c := range children {
		if child, ok := c.(map[string]any); ok {
			b.WriteString(stringOf(child))
		}
	}
	return b.String()
}
