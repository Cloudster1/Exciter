//go:build integration

package test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// optionID finds an option of an attribute answer by its name.
func optionID(t *testing.T, attr map[string]any, name string) string {
	t.Helper()
	for _, each := range attr["options"].([]any) {
		o := each.(map[string]any)
		if o["name"] == name {
			return o["id"].(string)
		}
	}
	t.Fatalf("no option %s in %v", name, attr["options"])
	return ""
}

// A project's administrators shape its types and attributes, editors give requirements values that fit them,
// and what was ever used is retired rather than deleted.
func TestTypesAndAttributesOverTheAPI(t *testing.T) {
	attKey := projectKey(t, "ATT")
	f := newProjectFixture(t)
	project := obj(t, want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": attKey, "name": "Attributed"}), http.StatusCreated, "make a project"), "project")
	defaultType := project["settings"].(map[string]any)["defaultTypeId"].(string)
	editor := f.plain
	types, attrs := "/api/v1/projects/"+attKey+"/types", "/api/v1/projects/"+attKey+"/attributes"

	t.Run("a project starts with one type, its default", func(t *testing.T) {
		got := list(t, want(t, editor.get(t, types), http.StatusOK, "list types"), "types")
		if len(got) != 1 || got[0].(map[string]any)["id"] != defaultType || got[0].(map[string]any)["name"] != "Requirement" {
			t.Fatalf("the first type is the default Requirement: %v", got)
		}
		if kinds := list(t, want(t, editor.get(t, "/api/v1/attribute-kinds"), http.StatusOK, "list kinds"), "kinds"); len(kinds) != 9 {
			t.Errorf("%d kinds, want 9", len(kinds))
		}
		refuse(t, "nobody asks for kinds", f.api.anonymous().get(t, "/api/v1/attribute-kinds"), http.StatusUnauthorized, "unauthorized")
		refuse(t, "a guest reads types", f.guest.get(t, types), http.StatusNotFound, "not_found")
		refuse(t, "a guest reads attributes", f.guest.get(t, attrs), http.StatusNotFound, "not_found")
	})

	iface := obj(t, want(t, f.owner.post(t, types, map[string]any{"name": "Interface", "icon": "interface", "colour": "#AA00FF", "position": 0}),
		http.StatusCreated, "make a type"), "type")
	ifaceID := iface["id"].(string)
	if iface["colour"] != "#aa00ff" || iface["position"] != float64(0) {
		t.Fatalf("the type is kept in lower case, first: %v", iface)
	}
	verification := obj(t, want(t, f.owner.post(t, attrs, map[string]any{
		"name": "Verification", "kind": "select", "required": true, "typeIds": []string{defaultType},
		"options": []any{map[string]any{"name": "Test"}, map[string]any{"name": "Analysis"}},
	}), http.StatusCreated, "define a select"), "attribute")
	verificationID := verification["id"].(string)
	test, analysis := optionID(t, verification, "Test"), optionID(t, verification, "Analysis")
	notes := obj(t, want(t, f.owner.post(t, attrs, map[string]any{"name": "Notes", "kind": "text"}), http.StatusCreated, "define a text"), "attribute")
	notesID := notes["id"].(string)

	t.Run("only administrators shape types and attributes, and only well", func(t *testing.T) {
		refuse(t, "an editor makes a type", editor.post(t, types, map[string]any{"name": "Mine", "icon": "x", "colour": "#000000"}), http.StatusForbidden, "forbidden")
		refuse(t, "an editor defines an attribute", editor.post(t, attrs, map[string]any{"name": "Mine", "kind": "text"}), http.StatusForbidden, "forbidden")
		for what, body := range map[string]map[string]any{
			"a type named twice":    {"name": "interface", "icon": "x", "colour": "#000000"},
			"an icon that is none":  {"name": "Odd", "icon": "Not An Icon", "colour": "#000000"},
			"a colour that is none": {"name": "Odd", "icon": "odd", "colour": "red"},
		} {
			refuse(t, what, f.owner.post(t, types, body), http.StatusUnprocessableEntity, "validation_failed")
		}
		for what, body := range map[string]map[string]any{
			"an attribute named twice": {"name": "notes", "kind": "text"},
			"no kind":                  {"name": "Kindless"},
			"a select without options": {"name": "Empty", "kind": "select"},
			"options on a text":        {"name": "Odd", "kind": "text", "options": []any{map[string]any{"name": "a"}}},
			"a type of nowhere":        {"name": "Odd", "kind": "text", "typeIds": []string{uuid.NewString()}},
		} {
			refuse(t, what, f.owner.post(t, attrs, body), http.StatusUnprocessableEntity, "validation_failed")
		}
		refuse(t, "a new kind", f.owner.patch(t, attrs+"/"+notesID, map[string]any{"kind": "number"}), http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "a type that is none", f.owner.patch(t, types+"/"+uuid.NewString(), map[string]any{"name": "X"}), http.StatusNotFound, "not_found")
		refuse(t, "an id that is none", f.owner.patch(t, types+"/nope", map[string]any{"name": "X"}), http.StatusBadRequest, "bad_request")
	})

	var reqID string
	t.Run("a requirement takes the default type and values that fit its attributes", func(t *testing.T) {
		refuse(t, "a required value left out", editor.post(t, "/api/v1/projects/"+attKey+"/requirements", map[string]any{"title": "Pay"}),
			http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "a value of the wrong shape", editor.post(t, "/api/v1/projects/"+attKey+"/requirements", map[string]any{
			"title": "Pay", "attributes": map[string]any{verificationID: "Test"}}), http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "an attribute not on that type", editor.post(t, "/api/v1/projects/"+attKey+"/requirements", map[string]any{
			"title": "Pay", "typeId": ifaceID, "attributes": map[string]any{verificationID: test}}), http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "an attribute of nowhere", editor.post(t, "/api/v1/projects/"+attKey+"/requirements", map[string]any{
			"title": "Pay", "attributes": map[string]any{uuid.NewString(): "x"}}), http.StatusUnprocessableEntity, "validation_failed")
		made := obj(t, want(t, editor.post(t, "/api/v1/projects/"+attKey+"/requirements", map[string]any{
			"title": "Pay", "attributes": map[string]any{verificationID: test, notesID: "  fast  "},
		}), http.StatusCreated, "write a requirement with values"), "requirement")
		reqID = made["id"].(string)
		values := made["attributes"].(map[string]any)
		if made["type"].(map[string]any)["id"] != defaultType || values[verificationID] != test || values[notesID] != "fast" {
			t.Fatalf("the requirement keeps the default type and its values: %v", made)
		}
		rows := list(t, want(t, editor.get(t, "/api/v1/projects/"+attKey+"/requirements"), http.StatusOK, "list"), "rows")
		if rows[0].(map[string]any)["type"].(map[string]any)["name"] != "Requirement" {
			t.Errorf("the list shows the type: %v", rows[0])
		}
	})

	t.Run("a new type is a revision, and values that do not show on it leave the requirement", func(t *testing.T) {
		got := obj(t, want(t, editor.patch(t, "/api/v1/requirements/"+attKey+"-R1", map[string]any{"typeId": ifaceID, "baseRevision": 1}),
			http.StatusOK, "change the type"), "requirement")
		values := got["attributes"].(map[string]any)
		if got["revision"] != float64(2) || values[verificationID] != nil || values[notesID] != "fast" {
			t.Errorf("the type change is revision 2 and keeps only the values that show: %v", got)
		}
		want(t, editor.patch(t, "/api/v1/requirements/"+attKey+"-R1", map[string]any{"typeId": defaultType, "attributes": map[string]any{verificationID: analysis}, "baseRevision": 2}),
			http.StatusOK, "change it back with a value")
	})

	t.Run("options keep their ids; one left out is retired and stays where it was chosen", func(t *testing.T) {
		renamed := obj(t, want(t, f.owner.patch(t, attrs+"/"+verificationID, map[string]any{
			"options": []any{map[string]any{"id": test, "name": "Testing"}, map[string]any{"name": "Inspection"}},
		}), http.StatusOK, "rename and replace options"), "attribute")
		if optionID(t, renamed, "Testing") != test || optionID(t, renamed, "Analysis") != analysis {
			t.Fatalf("the options kept their ids: %v", renamed["options"])
		}
		want(t, editor.patch(t, "/api/v1/requirements/"+attKey+"-R1", map[string]any{"attributes": map[string]any{verificationID: analysis}, "title": "Pay now", "baseRevision": 3}),
			http.StatusOK, "keep a retired option")
		want(t, editor.post(t, "/api/v1/projects/"+attKey+"/requirements", map[string]any{"title": "Second", "attributes": map[string]any{verificationID: test}}),
			http.StatusCreated, "a second requirement")
		refuse(t, "a retired option chosen anew", editor.patch(t, "/api/v1/requirements/"+attKey+"-R2", map[string]any{
			"attributes": map[string]any{verificationID: analysis}, "baseRevision": 1}), http.StatusUnprocessableEntity, "validation_failed")
	})

	t.Run("what was ever used is retired, and only what never was is deleted", func(t *testing.T) {
		refuse(t, "deleting a used attribute", f.owner.delete(t, attrs+"/"+verificationID), http.StatusConflict, "in_use")
		refuse(t, "deleting the default type", f.owner.delete(t, types+"/"+defaultType), http.StatusConflict, "default_type")
		refuse(t, "retiring the default type", f.owner.patch(t, types+"/"+defaultType, map[string]any{"retired": true}), http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "deleting a type a revision had", f.owner.delete(t, types+"/"+ifaceID), http.StatusConflict, "in_use")
		retired := obj(t, want(t, f.owner.patch(t, types+"/"+ifaceID, map[string]any{"retired": true}), http.StatusOK, "retire a type"), "type")
		if retired["retired"] != true {
			t.Errorf("the type was not retired: %v", retired)
		}
		refuse(t, "a retired type newly taken", editor.patch(t, "/api/v1/requirements/"+attKey+"-R2", map[string]any{"typeId": ifaceID, "baseRevision": 1}),
			http.StatusUnprocessableEntity, "validation_failed")
		spare := obj(t, want(t, f.owner.post(t, types, map[string]any{"name": "Spare", "icon": "spare", "colour": "#123456"}), http.StatusCreated, "a spare type"), "type")
		refuse(t, "an editor deletes a type", editor.delete(t, types+"/"+spare["id"].(string)), http.StatusForbidden, "forbidden")
		want(t, f.owner.delete(t, types+"/"+spare["id"].(string)), http.StatusNoContent, "delete an unused type")
		unused := obj(t, want(t, f.owner.post(t, attrs, map[string]any{"name": "Unused", "kind": "boolean"}), http.StatusCreated, "an unused attribute"), "attribute")
		refuse(t, "an editor deletes an attribute", editor.delete(t, attrs+"/"+unused["id"].(string)), http.StatusForbidden, "forbidden")
		want(t, f.owner.delete(t, attrs+"/"+unused["id"].(string)), http.StatusNoContent, "delete an unused attribute")
		refuse(t, "an editor changes an attribute", editor.patch(t, attrs+"/"+notesID, map[string]any{"name": "Mine"}), http.StatusForbidden, "forbidden")
	})

	t.Run("the attributes are listed in order, retired ones too", func(t *testing.T) {
		names := []string{}
		for _, each := range list(t, want(t, editor.get(t, attrs), http.StatusOK, "list attributes"), "attributes") {
			names = append(names, each.(map[string]any)["name"].(string))
		}
		sameList(t, "the attributes", names, "Verification", "Notes")
	})

	t.Run("the default type is one of the project's and in use", func(t *testing.T) {
		refuse(t, "a retired default", f.owner.patch(t, "/api/v1/projects/"+attKey, map[string]any{"settings": map[string]any{"defaultTypeId": ifaceID}}),
			http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "a default of nowhere", f.owner.patch(t, "/api/v1/projects/"+attKey, map[string]any{"settings": map[string]any{"defaultTypeId": uuid.NewString()}}),
			http.StatusUnprocessableEntity, "validation_failed")
		if n := f.h.countRows(t, `SELECT count(*) FROM requirement_revision WHERE requirement_id = $1 AND type_id IS NOT NULL`, reqID); n != 4 {
			t.Errorf("%d revisions of "+attKey+"-R1 name their type, want 4", n)
		}
	})
}
