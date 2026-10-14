package httpapi

import (
	"encoding/json"
	"strings"
	"testing"
)

// An installation is one team's, so what the document and the assistant's tools say to a reader speaks of
// everyone, all projects or Exciter, never an organization. Adapted from Armature's.
func TestTheDocumentSaysEveryoneNotTheOrganization(t *testing.T) {
	encoded, err := Spec().MarshalIndent()
	if err != nil {
		t.Fatal(err)
	}
	var doc any
	if err := json.Unmarshal(encoded, &doc); err != nil {
		t.Fatal(err)
	}
	for _, words := range readWords(doc, "") {
		if strings.Contains(strings.ToLower(words), "organization") {
			t.Errorf("the document says %q", words)
		}
	}
	for _, op := range operations {
		if strings.Contains(strings.ToLower(op.toolHelp), "organization") {
			t.Errorf("the tool %s says %q", op.tool, op.toolHelp)
		}
	}
}

// readWords collects what a reader of the document reads: summaries, descriptions, tags and the values an enum
// allows, but not names or ids.
func readWords(node any, key string) []string {
	var out []string
	switch v := node.(type) {
	case map[string]any:
		for k, child := range v {
			out = append(out, readWords(child, k)...)
		}
	case []any:
		for _, child := range v {
			out = append(out, readWords(child, key)...)
		}
	case string:
		if key == "summary" || key == "description" || key == "tags" || key == "enum" {
			out = append(out, v)
		}
	}
	return out
}
