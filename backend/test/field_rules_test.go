//go:build integration

package test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// Field rules (#206): a field is required from a status on, a choice narrows by another, a field is changed only by
// the roles its permits name, and a transition asks for trace; the database holds the choices and the permits.

// fieldWorld is a comment world with its statuses by name and a select attribute pair, Vehicle and Brake.
type fieldWorld struct {
	*commentWorld
	statuses                   map[string]string
	attrs                      string
	vehicle, brake             string
	car, bike, disc, drum, rim string
}

func newFieldWorld(t *testing.T, prefix string) *fieldWorld {
	t.Helper()
	w := &fieldWorld{commentWorld: newCommentWorld(t, prefix), statuses: map[string]string{}}
	w.attrs = "/api/v1/projects/" + w.key + "/attributes"
	for _, each := range list(t, want(t, w.owner.get(t, "/api/v1/projects/"+w.key+"/statuses"), http.StatusOK, "list the statuses"), "statuses") {
		st := each.(map[string]any)
		w.statuses[st["name"].(string)] = st["id"].(string)
	}
	options := func(a map[string]any) map[string]string {
		out := map[string]string{}
		for _, o := range a["options"].([]any) {
			out[o.(map[string]any)["name"].(string)] = o.(map[string]any)["id"].(string)
		}
		return out
	}
	vehicle := obj(t, want(t, w.owner.post(t, w.attrs, map[string]any{"name": "Vehicle", "kind": "select",
		"options": []map[string]any{{"name": "Car"}, {"name": "Bike"}}}), http.StatusCreated, "define Vehicle"), "attribute")
	brake := obj(t, want(t, w.owner.post(t, w.attrs, map[string]any{"name": "Brake", "kind": "select",
		"options": []map[string]any{{"name": "Disc"}, {"name": "Drum"}, {"name": "Rim"}}}), http.StatusCreated, "define Brake"), "attribute")
	w.vehicle, w.brake = vehicle["id"].(string), brake["id"].(string)
	v, b := options(vehicle), options(brake)
	w.car, w.bike, w.disc, w.drum, w.rim = v["Car"], v["Bike"], b["Disc"], b["Drum"], b["Rim"]
	return w
}

func (w *fieldWorld) req(n string) string { return "/api/v1/requirements/" + w.key + "-R" + n }

func (w *fieldWorld) submit(t *testing.T, c *client, n string) response {
	t.Helper()
	id, ok := transitionNamed(t, c, w.key+"-R"+n, "Submit for review")
	if !ok {
		t.Fatalf("R%s offers no Submit for review", n)
	}
	return c.post(t, w.req(n)+"/transitions", map[string]any{"transitionId": id})
}

func TestAFieldIsRequiredFromAStatusOn(t *testing.T) {
	w := newFieldWorld(t, "FRQ")
	risk := obj(t, want(t, w.owner.post(t, w.attrs, map[string]any{"name": "Risk", "kind": "text", "requiredFrom": w.statuses["In review"]}),
		http.StatusCreated, "Risk is required from In review"), "attribute")
	riskID := risk["id"].(string)
	if risk["requiredFrom"] != w.statuses["In review"] {
		t.Fatalf("Risk reads %v", risk)
	}
	fieldError(t, w.owner.patch(t, w.attrs+"/"+riskID, map[string]any{"requiredFrom": uuid.NewString()}), "requiredFrom")

	r := w.submit(t, w.bob, "1")
	if msg := fieldError(t, r, "attributes."+riskID); msg != "Give Risk a value; requirements in In review need it." {
		t.Errorf("the refusal says %q", msg)
	}
	want(t, w.bob.patch(t, w.req("1"), map[string]any{"attributes": map[string]any{riskID: "low"}, "baseRevision": 1}), http.StatusOK, "bob gives Risk")
	want(t, w.submit(t, w.bob, "1"), http.StatusOK, "bob submits R1")
	fieldError(t, w.bob.patch(t, w.req("1"), map[string]any{"attributes": map[string]any{riskID: nil}, "baseRevision": 2}), "attributes."+riskID)

	t.Run("a fixed field takes the same rule, and a bulk transition says which requirement lacks it", func(t *testing.T) {
		rules := "/api/v1/projects/" + w.key + "/field-rules"
		fieldError(t, w.owner.put(t, rules+"/title", map[string]any{"requiredFrom": w.statuses["In review"]}), "requiredFrom")
		refuse(t, "an editor sets a field rule", w.bob.put(t, rules+"/owner", map[string]any{"requiredFrom": w.statuses["In review"]}),
			http.StatusForbidden, "forbidden")
		got := obj(t, want(t, w.owner.put(t, rules+"/owner", map[string]any{"requiredFrom": w.statuses["In review"]}), http.StatusOK, "the owner is required in review"), "rule")
		if got["field"] != "owner" || got["requiredFrom"] != w.statuses["In review"] {
			t.Errorf("the rule reads %v", got)
		}
		if all := list(t, want(t, w.carol.get(t, rules), http.StatusOK, "a reader lists the rules"), "rules"); len(all) != 6 {
			t.Errorf("%d fixed fields, want 6", len(all))
		}
		r2 := obj(t, want(t, w.owner.post(t, "/api/v1/projects/"+w.key+"/requirements", map[string]any{"title": "Brake lights follow the pedal"}),
			http.StatusCreated, "write R2"), "requirement")["key"].(string)
		want(t, w.owner.patch(t, "/api/v1/requirements/"+r2, map[string]any{"attributes": map[string]any{riskID: "high"}, "baseRevision": 1}), http.StatusOK, "R2 has Risk")
		submit, _ := transitionNamed(t, w.bob, r2, "Submit for review")
		results := list(t, want(t, w.bob.post(t, "/api/v1/projects/"+w.key+"/transitions", map[string]any{"transitionId": submit, "keys": []string{r2}}),
			http.StatusOK, "submit R2 in bulk"), "results")
		refused := results[0].(map[string]any)
		if refused["outcome"] != "refused" || !strings.Contains(refused["error"].(map[string]any)["message"].(string), "Give the owner a value") {
			t.Errorf("the bulk result reads %v", refused)
		}
		if n := w.h.countRows(t, `SELECT count(*) FROM audit_log WHERE action = 'field_rule.saved' AND target_id = $1`, w.projectID); n != 1 {
			t.Errorf("%d audit entries for the owner's rule, want 1", n)
		}
	})
}

func TestADependentChoiceNarrowsByTheOther(t *testing.T) {
	w := newFieldWorld(t, "FDC")
	brake := w.attrs + "/" + w.brake
	fieldError(t, w.owner.patch(t, brake, map[string]any{"dependsOn": map[string]any{"attributeId": w.brake}}), "dependsOn")
	got := obj(t, want(t, w.owner.patch(t, brake, map[string]any{"dependsOn": map[string]any{"attributeId": w.vehicle,
		"choices": map[string]any{w.car: []string{w.disc, w.drum}}}}), http.StatusOK, "Brake depends on Vehicle"), "attribute")
	if dep := got["dependsOn"].(map[string]any); dep["attributeId"] != w.vehicle {
		t.Fatalf("Brake reads %v", got)
	}
	fieldError(t, w.owner.patch(t, w.attrs+"/"+w.vehicle, map[string]any{"dependsOn": map[string]any{"attributeId": w.brake}}), "dependsOn")

	msg := fieldError(t, w.bob.patch(t, w.req("1"), map[string]any{"attributes": map[string]any{w.vehicle: w.car, w.brake: w.rim}, "baseRevision": 1}),
		"attributes."+w.brake)
	if msg != "Rim is not a choice of Brake while Vehicle is Car. Pick one of: Disc, Drum." {
		t.Errorf("the refusal says %q", msg)
	}
	want(t, w.bob.patch(t, w.req("1"), map[string]any{"attributes": map[string]any{w.vehicle: w.bike, w.brake: w.rim}, "baseRevision": 1}),
		http.StatusOK, "a bike takes a rim brake")
	fieldError(t, w.bob.patch(t, w.req("1"), map[string]any{"attributes": map[string]any{w.vehicle: w.car}, "baseRevision": 2}), "attributes."+w.brake)

	t.Run("a row of a CSV import that breaks it is refused, and says why", func(t *testing.T) {
		file := []byte("title,Vehicle,Brake\nPads wear evenly,Car,Rim\nCables stay taut,Car,Drum\n")
		made := obj(t, want(t, uploadCSV(t, w.owner, w.key, "brakes.csv", file), http.StatusCreated, "upload it"), "import")
		id := made["id"].(string)
		want(t, w.owner.post(t, "/api/v1/imports/"+id+"/start", nil), http.StatusAccepted, "start it")
		done := finished(t, w.owner, id)
		refusedRows := done["refused"].([]any)
		if len(done["imported"].([]any)) != 1 || len(refusedRows) != 1 || !strings.Contains(refusedRows[0].(map[string]any)["reason"].(string), "Rim is not a choice of Brake") {
			t.Errorf("the import: %v", done)
		}
	})
}

func TestOnlyThePermittedChangeAField(t *testing.T) {
	w := newFieldWorld(t, "FPM")
	want(t, w.owner.put(t, "/api/v1/projects/"+w.key+"/field-rules/priority", map[string]any{"editableBy": []map[string]any{{"roles": []string{"admin"}}}}),
		http.StatusOK, "only admins change the priority")
	if msg := fieldError(t, w.bob.patch(t, w.req("1"), map[string]any{"priority": "must"}), "priority"); msg != "Only admins change the priority in Draft." {
		t.Errorf("the refusal says %q", msg)
	}
	want(t, w.owner.patch(t, w.req("1"), map[string]any{"priority": "must"}), http.StatusOK, "the owner, an admin, sets it")
	fieldError(t, w.owner.patch(t, w.attrs+"/"+w.vehicle, map[string]any{"editableBy": []map[string]any{{"roles": []string{"boss"}}}}), "editableBy")
	want(t, w.owner.patch(t, w.attrs+"/"+w.vehicle, map[string]any{"editableBy": []map[string]any{{"roles": []string{"editor"}, "statuses": []string{w.statuses["Draft"]}}}}),
		http.StatusOK, "editors change Vehicle in Draft")
	want(t, w.bob.patch(t, w.req("1"), map[string]any{"attributes": map[string]any{w.vehicle: w.car}, "baseRevision": 1}), http.StatusOK, "bob sets Vehicle in Draft")
	want(t, w.submit(t, w.bob, "1"), http.StatusOK, "bob submits R1")
	fieldError(t, w.bob.patch(t, w.req("1"), map[string]any{"attributes": map[string]any{w.vehicle: w.bike}, "baseRevision": 2}), "attributes."+w.vehicle)
	if p := obj(t, want(t, w.bob.get(t, "/api/v1/projects/"+w.key), http.StatusOK, "bob reads the project"), "project"); !strings.Contains(strings.Join(toStrings(p["roles"]), ","), "editor") {
		t.Errorf("bob's project reads roles %v", p["roles"])
	}
}

func TestATransitionAsksForTrace(t *testing.T) {
	w := newFieldWorld(t, "FTR")
	ctx := context.Background()
	var submit, verifies string
	if err := w.h.super.QueryRow(ctx, `
		SELECT t.id FROM workflow_transition t JOIN workflow f ON f.id = t.workflow_id WHERE f.project_id = $1 AND t.name = 'Submit for review'`,
		w.projectID).Scan(&submit); err != nil {
		t.Fatal(err)
	}
	if err := w.h.super.QueryRow(ctx, `SELECT id FROM link_type WHERE key = 'derives'`).Scan(&verifies); err != nil {
		t.Fatal(err)
	}
	if _, err := w.h.super.Exec(ctx, `INSERT INTO transition_rule (transition_id, kind, type, config, position) VALUES
		($1, 'validator', 'validator.links_exist', jsonb_build_object('linkTypeId', $2::text, 'direction', 'incoming'), 10),
		($1, 'validator', 'validator.threads_resolved', '{}', 11), ($1, 'validator', 'validator.no_suspect', '{}', 12)`, submit, verifies); err != nil {
		t.Fatal(err)
	}
	if _, ok := transitionNamed(t, w.bob, w.key+"-R1", "Submit for review"); !ok {
		t.Fatal("a transition asking for trace was not offered")
	}
	r := w.submit(t, w.bob, "1")
	if msg := fieldError(t, r, "transition"); !strings.HasPrefix(msg, w.key+"-R1 has no Derives link to it yet.") {
		t.Errorf("the refusal says %q", msg)
	}
	r2 := obj(t, want(t, w.owner.post(t, "/api/v1/projects/"+w.key+"/requirements", map[string]any{"title": "Brake lights follow the pedal"}),
		http.StatusCreated, "write R2"), "requirement")["key"].(string)
	want(t, w.bob.post(t, "/api/v1/requirements/"+r2+"/links", map[string]any{"typeKey": "derives", "otherKey": w.key + "-R1"}), http.StatusCreated, "R2 derives from R1")
	thread := obj(t, want(t, w.dave.post(t, w.req("1")+"/comments", commentBody("Is 100 ms enough?")), http.StatusCreated, "dave asks"), "thread")["id"].(string)
	if msg := fieldError(t, w.submit(t, w.bob, "1"), "transition"); msg != w.key+"-R1 has 1 open comment thread. Resolve them, then take this transition." {
		t.Errorf("the refusal says %q", msg)
	}
	want(t, w.bob.put(t, "/api/v1/comment-threads/"+thread+"/resolution", nil), http.StatusOK, "bob resolves it")
	want(t, w.submit(t, w.bob, "1"), http.StatusOK, "bob submits R1 with its trace in order")
}

func TestRawSQLHoldsFieldRules(t *testing.T) {
	w := newFieldWorld(t, "FSQ")
	ctx := context.Background()
	want(t, w.owner.patch(t, w.attrs+"/"+w.brake, map[string]any{"dependsOn": map[string]any{"attributeId": w.vehicle,
		"choices": map[string]any{w.car: []string{w.disc}}}}), http.StatusOK, "Brake depends on Vehicle")
	want(t, w.owner.put(t, "/api/v1/projects/"+w.key+"/field-rules/priority", map[string]any{"editableBy": []map[string]any{{"roles": []string{"admin"}}}}),
		http.StatusOK, "only admins change the priority")
	bob, owner := w.h.appConn(t, w.bobID), w.h.appConn(t, w.ownerID)
	if _, err := bob.Exec(ctx, `UPDATE requirement SET attributes = jsonb_build_object($2::text, $3::text, $4::text, $5::text) WHERE id = $1`,
		w.reqID, w.vehicle, w.car, w.brake, w.rim); !refusedBy(err, "attribute_value_choice") {
		t.Errorf("a rim brake on a car through SQL: %v", err)
	}
	if _, err := bob.Exec(ctx, `UPDATE requirement SET priority = 'must' WHERE id = $1`, w.reqID); !refusedBy(err, "field_not_yours") {
		t.Errorf("an editor set a priority only admins change: %v", err)
	}
	if _, err := owner.Exec(ctx, `UPDATE requirement SET priority = 'must' WHERE id = $1`, w.reqID); err != nil {
		t.Errorf("the owner set the priority: %v", err)
	}
	if _, err := bob.Exec(ctx, `INSERT INTO field_rule (project_id, field) VALUES ($1, 'owner')`, w.projectID); !isPolicyViolation(err) {
		t.Errorf("an editor wrote a field rule: %v", err)
	}
}
