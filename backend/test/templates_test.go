//go:build integration

package test

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// Every built-in template makes a valid project: each type has a workflow and the first is the default, each
// attribute shows on types the project has, each section stands in the tree, and the link types are the ones it names.
func TestEveryTemplateMakesAValidProject(t *testing.T) {
	f := newProjectFixture(t)
	refuse(t, "nobody signed in", f.api.anonymous().get(t, "/api/v1/project-templates"), http.StatusUnauthorized, "unauthorized")
	templates := list(t, want(t, f.plain.get(t, "/api/v1/project-templates"), http.StatusOK, "list the templates"), "templates")
	if len(templates) != 3 {
		t.Fatalf("%d templates, want the three built in", len(templates))
	}
	for _, each := range templates {
		tpl := each.(map[string]any)
		key := projectKey(t, strings.ToUpper(tpl["key"].(string))[:4])
		t.Run(tpl["key"].(string), func(t *testing.T) {
			made := obj(t, want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": tpl["name"], "template": tpl["key"]}),
				http.StatusCreated, "make a project from it"), "project")
			if made["template"] != tpl["key"] {
				t.Errorf("the project does not keep its template: %v", made["template"])
			}
			projectID := uuid.MustParse(made["id"].(string))

			types := list(t, want(t, f.owner.get(t, "/api/v1/projects/"+key+"/types"), http.StatusOK, "its types"), "types")
			var names []string
			for _, typ := range types {
				names = append(names, typ.(map[string]any)["name"].(string))
			}
			sameList(t, "the types", names, toStrings(tpl["types"])...)
			first := types[0].(map[string]any)["id"]
			if made["settings"].(map[string]any)["defaultTypeId"] != first {
				t.Errorf("the default type is not the template's first: %v", made["settings"])
			}
			if n := f.h.countRows(t, `SELECT count(*) FROM requirement_type t WHERE t.project_id = $1 AND requirement_workflow(t.id, t.project_id) IS NULL`, projectID); n != 0 {
				t.Errorf("%d types have no workflow", n)
			}

			attrs := list(t, want(t, f.owner.get(t, "/api/v1/projects/"+key+"/attributes"), http.StatusOK, "its attributes"), "attributes")
			names = nil
			for _, a := range attrs {
				names = append(names, a.(map[string]any)["name"].(string))
			}
			sameList(t, "the attributes", names, toStrings(tpl["attributes"])...)
			if n := f.h.countRows(t, `SELECT count(*) FROM attribute_def_type at JOIN attribute_def a ON a.id = at.attribute_id
				LEFT JOIN requirement_type rt ON rt.id = at.type_id WHERE a.project_id = $1 AND rt.id IS NULL`, projectID); n != 0 {
				t.Errorf("%d attributes show on types the project lacks", n)
			}

			var sections []string
			for _, n := range list(t, want(t, f.owner.get(t, "/api/v1/projects/"+key+"/tree"), http.StatusOK, "its tree"), "nodes") {
				node := n.(map[string]any)
				if node["kind"] != "section" || node["revision"] != float64(1) {
					t.Errorf("the tree holds %v, want sections at revision 1", node)
				}
				sections = append(sections, node["title"].(string))
			}
			sameList(t, "the sections", sections, toStrings(tpl["sections"])...)

			var used []string
			for _, lt := range list(t, want(t, f.owner.get(t, "/api/v1/projects/"+key+"/link-types"), http.StatusOK, "its link types"), "linkTypes") {
				if l := lt.(map[string]any); l["enabled"] == true {
					used = append(used, l["key"].(string))
				}
			}
			if wanted := toStrings(tpl["linkTypes"]); len(wanted) > 0 {
				slices.Sort(used)
				slices.Sort(wanted)
				sameList(t, "the link types it uses", used, wanted...)
			} else if len(used) != 6 {
				t.Errorf("a template that names no link types uses %v, want all six", used)
			}
			want(t, f.owner.post(t, "/api/v1/projects/"+key+"/requirements", map[string]any{"title": "Works at once"}), http.StatusCreated,
				"write a requirement of the default type")
			if tpl["key"] == "test" {
				// Its first type takes test cases with their steps straight away (#263).
				if first := types[0].(map[string]any); first["name"] != "Test case" || first["role"] != "testCase" {
					t.Errorf("the first type is %v, want a type of test cases", first)
				}
				made := obj(t, want(t, f.owner.post(t, "/api/v1/projects/"+key+"/requirements", map[string]any{"title": "Brake on a wet road",
					"testCase": map[string]any{"steps": []any{map[string]any{"action": statementSaying("Press the pedal.")}}}}),
					http.StatusCreated, "write a test case with steps"), "requirement")
				sameList(t, "the steps", stepTexts(t, testCaseOf(t, made)), "Press the pedal.")
			}
		})
	}
}
