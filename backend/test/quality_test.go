//go:build integration

package test

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/quality"
)

// Writing rules (#207): every revision keeps what the rules found, a project's settings judge it when read, an
// author accepts a finding for good reason, and the score answers queries and holds a transition back.

// findingsOf reads a requirement's findings as rule: match, and its quality.
func findingsOf(t *testing.T, c *client, reqKey string) (map[string]string, map[string]any) {
	t.Helper()
	q := obj(t, want(t, c.get(t, "/api/v1/requirements/"+reqKey+"/quality"), http.StatusOK, "read the findings"), "quality")
	out := map[string]string{}
	for _, each := range q["findings"].([]any) {
		f := each.(map[string]any)
		state := "active"
		switch {
		case f["accepted"] != nil:
			state = "accepted"
		case f["active"] != true:
			state = "off"
		}
		out[f["rule"].(string)+": "+f["match"].(string)] = state
	}
	return out, q
}

func findingKey(t *testing.T, q map[string]any, rule string) string {
	t.Helper()
	for _, each := range q["findings"].([]any) {
		if f := each.(map[string]any); f["rule"] == rule {
			return f["key"].(string)
		}
	}
	t.Fatalf("no %s finding in %v", rule, q["findings"])
	return ""
}

func scoreOf(q map[string]any) float64 {
	if s, ok := q["score"].(float64); ok {
		return s
	}
	return -1
}

func TestWritingRulesFindScoreAndAccept(t *testing.T) {
	w := newCommentWorld(t, "QRS")
	reqs := "/api/v1/projects/" + w.key + "/requirements"
	vague := obj(t, want(t, w.bob.post(t, reqs, map[string]any{"title": "Response", "statement": docOf("The system shall respond quickly.")}),
		http.StatusCreated, "bob writes a vague requirement"), "requirement")
	key := vague["key"].(string)

	t.Run("a revision keeps its findings, and the requirement shows the score they leave", func(t *testing.T) {
		found, q := findingsOf(t, w.carol, key)
		if found["vague: quickly"] != "active" {
			t.Errorf("the findings read %v", found)
		}
		score := scoreOf(q)
		if score < 0 || score >= quality.FullScore {
			t.Errorf("the score is %v", q["score"])
		}
		r := obj(t, want(t, w.carol.get(t, "/api/v1/requirements/"+key), http.StatusOK, "read it"), "requirement")
		if got := r["quality"].(map[string]any); scoreOf(got) != score || got["active"] != q["active"] || got["stale"] != false {
			t.Errorf("the requirement's quality reads %v, its findings %v", got, q)
		}
		var sql int
		if err := w.h.super.QueryRow(context.Background(), `SELECT score FROM requirement_quality_of($1)`, vague["id"]).Scan(&sql); err != nil || float64(sql) != score {
			t.Errorf("the database scores %d (%v), Go %v", sql, err, score)
		}
	})

	t.Run("the rules are listed for whoever is signed in", func(t *testing.T) {
		rules := list(t, want(t, w.carol.get(t, "/api/v1/quality-rules"), http.StatusOK, "list the rules"), "rules")
		if len(rules) != len(quality.Rules) || rules[0].(map[string]any)["label"] == "" {
			t.Errorf("the rules read %v", rules)
		}
		refuse(t, "nobody lists the rules", w.carol.api.anonymous().get(t, "/api/v1/quality-rules"), http.StatusUnauthorized, "unauthorized")
	})

	t.Run("a draft is checked as it is written, by its editors", func(t *testing.T) {
		draft := map[string]any{"title": "Speed", "statement": docOf("The display shall be fast and/or bright.")}
		check := obj(t, want(t, w.bob.post(t, "/api/v1/projects/"+w.key+"/quality-check", draft), http.StatusOK, "bob checks a draft"), "check")
		rules := []string{}
		for _, each := range check["findings"].([]any) {
			f := each.(map[string]any)
			rules = append(rules, f["rule"].(string))
			if f["field"] != "statement" || f["block"] != float64(0) || f["to"].(float64) <= f["from"].(float64) || f["message"] == "" || f["fix"] == "" {
				t.Errorf("a finding reads %v", f)
			}
		}
		if !slices.Contains(rules, "vague") || !slices.Contains(rules, "and_or") || check["language"] != "en" {
			t.Errorf("the check reads %v", check)
		}
		md := "The brake shall engage TBD."
		check = obj(t, want(t, w.bob.post(t, "/api/v1/projects/"+w.key+"/quality-check", map[string]any{"title": "Brake", "statementMarkdown": md}),
			http.StatusOK, "bob checks Markdown"), "check")
		if !strings.Contains(fmtFindings(check), "placeholder") {
			t.Errorf("a Markdown draft finds %v", check["findings"])
		}
		refuse(t, "carol, who reads only", w.carol.post(t, "/api/v1/projects/"+w.key+"/quality-check", draft), http.StatusForbidden, "forbidden")
		fieldError(t, w.bob.post(t, "/api/v1/projects/"+w.key+"/quality-check", map[string]any{"title": "x", "statement": map[string]any{"type": "nonsense"}}), "statement")
	})

	t.Run("an accepted finding stops counting, stays with the revision, and comes forward while its words do", func(t *testing.T) {
		_, q := findingsOf(t, w.bob, key)
		before, fk := scoreOf(q), findingKey(t, q, "vague")
		refuse(t, "carol accepting", w.carol.post(t, "/api/v1/requirements/"+key+"/quality/accepted", map[string]any{"revision": 1, "key": fk, "reason": "Fine."}),
			http.StatusForbidden, "forbidden")
		fieldError(t, w.bob.post(t, "/api/v1/requirements/"+key+"/quality/accepted", map[string]any{"revision": 1, "key": fk, "reason": " "}), "reason")
		fieldError(t, w.bob.post(t, "/api/v1/requirements/"+key+"/quality/accepted", map[string]any{"revision": 1, "key": "nothing", "reason": "Fine."}), "key")
		got := obj(t, want(t, w.bob.post(t, "/api/v1/requirements/"+key+"/quality/accepted",
			map[string]any{"revision": 1, "key": fk, "reason": "Quickly is defined in the glossary of the contract."}), http.StatusOK, "bob accepts"), "quality")
		if scoreOf(got) <= before || got["accepted"] != float64(1) {
			t.Errorf("after accepting, the quality reads %v", got)
		}
		want(t, w.bob.patch(t, "/api/v1/requirements/"+key, map[string]any{"title": "Response time", "baseRevision": 1}), http.StatusOK, "bob renames it")
		found, q := findingsOf(t, w.carol, key)
		if found["vague: quickly"] != "accepted" || q["revision"] != float64(2) {
			t.Errorf("revision 2 reads %v", found)
		}
		for _, each := range q["findings"].([]any) {
			if f := each.(map[string]any); f["accepted"] != nil && f["accepted"].(map[string]any)["carriedFrom"] != float64(1) {
				t.Errorf("the carried acceptance reads %v", f["accepted"])
			}
		}
		refuse(t, "accepting on revision 1", w.bob.post(t, "/api/v1/requirements/"+key+"/quality/accepted",
			map[string]any{"revision": 1, "key": fk, "reason": "Old."}), http.StatusConflict, "stale_revision")
		older := obj(t, want(t, w.carol.get(t, "/api/v1/requirements/"+key+"/quality?revision=1"), http.StatusOK, "read revision 1"), "quality")
		if older["revision"] != float64(1) || older["accepted"] != float64(1) {
			t.Errorf("revision 1 reads %v", older)
		}
		want(t, w.bob.delete(t, "/api/v1/requirements/"+key+"/quality/accepted/"+url.PathEscape(fk)+"?revision=2"), http.StatusOK, "bob takes it back")
		if found, _ := findingsOf(t, w.carol, key); found["vague: quickly"] != "active" {
			t.Errorf("taken back, the findings read %v", found)
		}
		want(t, w.bob.patch(t, "/api/v1/requirements/"+key, map[string]any{"statement": docOf("The system shall respond within 2 s."), "baseRevision": 2}),
			http.StatusOK, "bob says how fast")
		if found, _ := findingsOf(t, w.carol, key); found["vague: quickly"] != "" {
			t.Errorf("fixed, the findings read %v", found)
		}
	})

	t.Run("switching a rule off counts at once; a word list has the worker check again", func(t *testing.T) {
		loose := obj(t, want(t, w.bob.post(t, reqs, map[string]any{"title": "Feel", "statement": docOf("The screen shall feel snappy and quick.")}),
			http.StatusCreated, "bob writes another"), "requirement")["key"].(string)
		_, q := findingsOf(t, w.carol, loose)
		before := scoreOf(q)
		settings := "/api/v1/projects/" + w.key + "/quality-settings"
		off := map[string]any{"rules": []map[string]any{{"rule": "vague", "enabled": false, "severity": "warning", "typeIds": []string{}}}}
		refuse(t, "bob setting rules", w.bob.put(t, settings, off), http.StatusForbidden, "forbidden")
		fieldError(t, w.owner.put(t, settings, map[string]any{"rules": []map[string]any{{"rule": "nonsense", "enabled": true, "severity": "error"}}}), "rules[0].rule")
		fieldError(t, w.owner.put(t, settings, map[string]any{"words": map[string]any{"passive": map[string]any{"add": []string{"x"}}}}), "words.passive")
		want(t, w.owner.put(t, settings, off), http.StatusOK, "the owner switches vague off")
		found, q := findingsOf(t, w.carol, loose)
		if found["vague: quick"] != "off" || scoreOf(q) <= before {
			t.Errorf("with vague off the findings read %v, the score %v after %v", found, q["score"], before)
		}
		if keys := rowKeys(t, want(t, w.carol.get(t, reqs+"?query="+url.QueryEscape("qualityFinding = vague")), http.StatusOK, "query")); len(keys) != 0 {
			t.Errorf("with vague off, qualityFinding = vague keeps %v", keys)
		}
		on := map[string]any{"rules": []map[string]any{}, "words": map[string]any{"vague": map[string]any{"add": []string{"snappy"}, "except": []string{"quick"}}}}
		want(t, w.owner.put(t, settings, on), http.StatusOK, "the owner adds snappy and excepts quick")
		w.h.drained(t)
		found, q = findingsOf(t, w.carol, loose)
		if found["vague: snappy"] != "active" || found["vague: quick"] != "" || q["stale"] != false {
			t.Errorf("checked again, the findings read %v, stale %v", found, q["stale"])
		}
		got := obj(t, want(t, w.carol.get(t, settings), http.StatusOK, "read the settings"), "settings")
		if words := got["words"].(map[string]any)["vague"].(map[string]any); toStrings(words["add"])[0] != "snappy" {
			t.Errorf("the settings read %v", got)
		}
	})

	t.Run("the query language and the grid read the score", func(t *testing.T) {
		clean := obj(t, want(t, w.bob.post(t, reqs, map[string]any{"title": "Stop distance", "statement": docOf("The vehicle shall stop within 40 m.")}),
			http.StatusCreated, "bob writes a clean one"), "requirement")["key"].(string)
		keep := func(q string) []string {
			return rowKeys(t, want(t, w.carol.get(t, reqs+"?kind=requirement&query="+url.QueryEscape(q)), http.StatusOK, q))
		}
		if got := keep("qualityFinding = vague"); !slices.Contains(got, w.key+"-R3") || slices.Contains(got, clean) {
			t.Errorf("qualityFinding = vague keeps %v", got)
		}
		if got := keep("qualityFinding IS EMPTY"); !slices.Contains(got, clean) {
			t.Errorf("qualityFinding IS EMPTY keeps %v", got)
		}
		if got := keep("quality < 100"); slices.Contains(got, clean) || !slices.Contains(got, w.key+"-R3") {
			t.Errorf("quality < 100 keeps %v", got)
		}
		rows := list(t, want(t, w.carol.get(t, reqs+"?kind=requirement&sort=quality"), http.StatusOK, "sort by score"), "rows")
		if len(rows) == 0 || rows[0].(map[string]any)["quality"] == nil {
			t.Errorf("sorted by score, the grid reads %v", rows)
		}
		summary := obj(t, want(t, w.carol.get(t, "/api/v1/projects/"+w.key+"/quality"), http.StatusOK, "the project's quality"), "quality")
		if summary["checked"].(float64) < 3 || len(summary["bands"].([]any)) != 5 || summary["requirements"].([]any)[0].(map[string]any)["key"] == clean {
			t.Errorf("the project's quality reads %v", summary)
		}
	})
}

// A transition may ask for a least score, and refuses one below it with the score and what it asks for.
func TestATransitionAsksForAScore(t *testing.T) {
	w := newFieldWorld(t, "QTS")
	ctx := context.Background()
	want(t, w.bob.patch(t, w.req("1"), map[string]any{"statement": docOf("The brake shall engage quickly, if possible."), "baseRevision": 1}),
		http.StatusOK, "bob writes a weak statement")
	var submit string
	if err := w.h.super.QueryRow(ctx, `
		SELECT t.id FROM workflow_transition t JOIN workflow f ON f.id = t.workflow_id WHERE f.project_id = $1 AND t.name = 'Submit for review'`,
		w.projectID).Scan(&submit); err != nil {
		t.Fatal(err)
	}
	if _, err := w.h.super.Exec(ctx, `INSERT INTO transition_rule (transition_id, kind, type, config, position)
		VALUES ($1, 'validator', 'validator.quality_min', '{"minimum": 100}', 10)`, submit); err != nil {
		t.Fatal(err)
	}
	_, q := findingsOf(t, w.bob, w.key+"-R1")
	score := int(scoreOf(q))
	msg := fieldError(t, w.submit(t, w.bob, "1"), "transition")
	if msg != w.key+"-R1 scores "+strconv.Itoa(score)+"; this transition asks for 100. Fix or accept its findings, then take it again." {
		t.Errorf("the refusal says %q", msg)
	}
	if _, err := w.h.super.Exec(ctx, `UPDATE transition_rule SET config = jsonb_build_object('minimum', $2::int) WHERE transition_id = $1 AND type = 'validator.quality_min'`,
		submit, score); err != nil {
		t.Fatal(err)
	}
	want(t, w.submit(t, w.bob, "1"), http.StatusOK, "bob submits R1 at the score asked for")
}

func fmtFindings(check map[string]any) string {
	var rules []string
	for _, each := range check["findings"].([]any) {
		rules = append(rules, each.(map[string]any)["rule"].(string))
	}
	return strings.Join(rules, ",")
}

// Every writer of a revision checks it in the same transaction: create, save, move, copy, restore and reuse.
func TestEveryRevisionWriterRecordsQuality(t *testing.T) {
	w := newCommentWorld(t, "QRW")
	ctx := context.Background()
	checked := func(what, key string) {
		t.Helper()
		var ok bool
		if err := w.h.super.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM requirement r JOIN requirement_quality c ON c.requirement_id = r.id AND c.revision = r.revision
			               WHERE r.id = (SELECT requirement_id FROM requirement_old_key WHERE key = $1
			                             UNION ALL SELECT r2.id FROM requirement r2 JOIN project p ON p.id = r2.project_id
			                             WHERE p.key || '-R' || r2.number = $1 LIMIT 1))`, key).Scan(&ok); err != nil || !ok {
			t.Errorf("after %s, %s's current revision is not checked (%v)", what, key, err)
		}
	}
	reqs := "/api/v1/projects/" + w.key + "/requirements"
	checked("making the world", w.reqKey)
	made := obj(t, want(t, w.owner.post(t, reqs, map[string]any{"title": "Brake lights shall be bright"}), http.StatusCreated, "write R2"), "requirement")
	r2 := made["key"].(string)
	checked("a create", r2)
	want(t, w.owner.patch(t, "/api/v1/requirements/"+r2, map[string]any{"title": "Brake lights shall be visible", "baseRevision": 1}), http.StatusOK, "save")
	checked("a save", r2)
	section := obj(t, want(t, w.owner.post(t, reqs, map[string]any{"kind": "section", "title": "Lights"}), http.StatusCreated, "a section"), "requirement")
	want(t, w.owner.post(t, "/api/v1/requirements/"+r2+"/move", map[string]any{"place": place(section["id"], nil)}), http.StatusOK, "move")
	checked("a move", r2)
	copied := obj(t, want(t, w.owner.post(t, "/api/v1/requirements/"+r2+"/copy", map[string]any{"place": map[string]any{}}), http.StatusCreated, "copy"), "requirement")
	checked("a copy", copied["key"].(string))
	want(t, w.owner.post(t, "/api/v1/requirements/"+r2+"/revisions/1/restore", map[string]any{"baseRevision": 2}), http.StatusOK, "restore a revision")
	checked("restoring a revision", r2)
	want(t, w.owner.delete(t, "/api/v1/requirements/"+r2), http.StatusNoContent, "trash")
	want(t, w.owner.post(t, "/api/v1/projects/"+w.key+"/trash/"+made["id"].(string)+"/restore", nil), http.StatusOK, "restore from the trash")
	checked("restoring from the trash", r2)
	if _, err := w.h.super.Exec(ctx, `UPDATE project SET settings = settings || '{"library": true}' WHERE id = $1`, w.projectID); err != nil {
		t.Fatal(err)
	}
	other := projectKey(t, "QRU")
	want(t, w.owner.post(t, "/api/v1/projects", map[string]any{"key": other, "name": "Reusing"}), http.StatusCreated, "make another project")
	reused := obj(t, want(t, w.owner.post(t, "/api/v1/requirements/"+w.reqKey+"/reuse", map[string]any{"projectKey": other}), http.StatusCreated, "reuse"), "requirement")
	checked("a reuse", reused["key"].(string))
}

// Straight through SQL as exciter_app: findings are written for the current revision by its editors alone, an
// acceptance names a finding and the person giving it, and only administrators set the rules.
func TestRawSQLKeepsFindingsToTheirRevision(t *testing.T) {
	w := newCommentWorld(t, "QRL")
	ctx := context.Background()
	id := uuid.MustParse(w.reqID)
	want(t, w.bob.patch(t, "/api/v1/requirements/"+w.reqKey, map[string]any{"statement": docOf("The brake shall engage quickly."), "baseRevision": 1}),
		http.StatusOK, "bob writes revision 2")
	var key string
	if err := w.h.super.QueryRow(ctx, `SELECT key FROM quality_finding WHERE requirement_id = $1 AND revision = 2 AND rule = 'vague'`, id).Scan(&key); err != nil {
		t.Fatal(err)
	}
	want(t, w.bob.patch(t, "/api/v1/requirements/"+w.reqKey, map[string]any{"statement": docOf("The brake shall engage fast."), "baseRevision": 2}),
		http.StatusOK, "bob writes revision 3")
	var current string
	if err := w.h.super.QueryRow(ctx, `SELECT key FROM quality_finding WHERE requirement_id = $1 AND revision = 3 AND rule = 'vague'`, id).Scan(&current); err != nil {
		t.Fatal(err)
	}
	const config = "0000000000000000000000000000000000000000000000000000000000000000"

	viewer := w.h.appConn(t, w.carolID)
	refusedOutright(t, viewer, "a viewer writes a finding", `INSERT INTO quality_finding (requirement_id, revision, key, rule, field, block, start_at, end_at, match)
		VALUES ($1, 3, 'x', 'vague', 'title', 0, 0, 1, 'x')`, id)
	refusedOutright(t, viewer, "a viewer accepts", `INSERT INTO quality_acceptance (requirement_id, revision, key, reason, accepted_by) VALUES ($1, 3, $2, 'Fine.', $3)`,
		id, current, w.carolID)
	refusedOutright(t, viewer, "a viewer sets a rule", `INSERT INTO quality_rule_setting (project_id, rule, enabled, severity, updated_by) VALUES ($1, 'vague', false, 'hint', $2)`,
		w.projectID, w.carolID)

	editor := w.h.appConn(t, w.bobID)
	refusedOutright(t, editor, "an editor checks an old revision", `INSERT INTO requirement_quality (requirement_id, revision, config) VALUES ($1, 1, $2)
		ON CONFLICT DO NOTHING`, id, config)
	refusedOutright(t, editor, "an editor rewrites a finding", `UPDATE quality_finding SET rule = 'hint' WHERE requirement_id = $1`, id)
	refusedOutright(t, editor, "an editor removes a finding", `DELETE FROM quality_finding WHERE requirement_id = $1`, id)
	refusedOutright(t, editor, "an editor accepts in another's name", `INSERT INTO quality_acceptance (requirement_id, revision, key, reason, accepted_by)
		VALUES ($1, 3, $2, 'Fine.', $3)`, id, current, w.carolID)
	refusedOutright(t, editor, "an editor accepts what was not found", `INSERT INTO quality_acceptance (requirement_id, revision, key, reason, accepted_by)
		VALUES ($1, 3, 'invented', 'Fine.', $2)`, id, w.bobID)
	refusedOutright(t, editor, "an editor accepts on an old revision", `INSERT INTO quality_acceptance (requirement_id, revision, key, reason, accepted_by)
		VALUES ($1, 2, $2, 'Fine.', $3)`, id, key, w.bobID)
	refusedOutright(t, editor, "an editor forges a carried acceptance", `INSERT INTO quality_acceptance (requirement_id, revision, key, reason, carried_from, accepted_by)
		VALUES ($1, 3, $2, 'Fine.', 2, $3)`, id, current, w.daveID)
	refusedOutright(t, editor, "an editor sets a rule", `INSERT INTO quality_rule_setting (project_id, rule, enabled, severity, updated_by) VALUES ($1, 'vague', false, 'hint', $2)`,
		w.projectID, w.bobID)
	if _, err := editor.Exec(ctx, `INSERT INTO quality_acceptance (requirement_id, revision, key, reason, accepted_by) VALUES ($1, 3, $2, 'Defined elsewhere.', $3)`,
		id, current, w.bobID); err != nil {
		t.Errorf("an editor may accept a finding of the current revision: %v", err)
	}

	admin := w.h.appConn(t, w.ownerID)
	refusedOutright(t, admin, "an administrator sets a rule in another's name", `INSERT INTO quality_rule_setting (project_id, rule, enabled, severity, updated_by)
		VALUES ($1, 'vague', false, 'hint', $2)`, w.projectID, w.bobID)
	refusedOutright(t, admin, "an administrator adds words to a rule that takes none", `INSERT INTO quality_rule_setting (project_id, rule, enabled, severity, add_words, updated_by)
		VALUES ($1, 'passive', true, 'hint', '{by}', $2)`, w.projectID, w.ownerID)
	refusedOutright(t, admin, "an administrator writes the config", `INSERT INTO project_quality (project_id, config) VALUES ($1, $2)`, w.projectID, config)
	if _, err := admin.Exec(ctx, `INSERT INTO quality_rule_setting (project_id, rule, enabled, severity, updated_by) VALUES ($1, 'vague', false, 'hint', $2)`,
		w.projectID, w.ownerID); err != nil {
		t.Errorf("an administrator may set a rule: %v", err)
	}
}

// The rules the database knows are the rules the code has.
func TestTheDatabaseKnowsEveryRule(t *testing.T) {
	h := newHarness(t)
	rows, err := h.super.Query(context.Background(), `SELECT rule, default_severity, default_enabled FROM quality_rule ORDER BY rule`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string]string{}
	for rows.Next() {
		var rule, severity string
		var enabled bool
		if err := rows.Scan(&rule, &severity, &enabled); err != nil {
			t.Fatal(err)
		}
		got[rule] = severity + " " + map[bool]string{true: "on", false: "off"}[enabled]
	}
	for _, r := range quality.Rules {
		if want := string(r.DefaultSeverity) + " " + map[bool]string{true: "on", false: "off"}[r.DefaultEnabled]; got[string(r.ID)] != want {
			t.Errorf("the database has %s as %q, the code as %q", r.ID, got[string(r.ID)], want)
		}
	}
	if len(got) != len(quality.Rules) {
		t.Errorf("the database has %d rules, the code %d", len(got), len(quality.Rules))
	}
}
