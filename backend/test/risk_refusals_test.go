//go:build integration

package test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/risk"
)

// projectID reads a project's id past every policy.
func (s *riskScene) projectID(t *testing.T) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := s.f.h.super.QueryRow(t.Context(), `SELECT id FROM project WHERE key = $1`, s.key).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// writeRisk writes a hazard of the scene rated before mitigation, answering its key.
func (s *riskScene) writeRisk(t *testing.T, title string, severity, probability any) string {
	t.Helper()
	body := map[string]any{"title": title, "typeId": s.hazard}
	if severity != nil {
		body["risk"] = map[string]any{"before": side(severity, probability)}
	}
	return obj(t, want(t, s.f.owner.post(t, "/api/v1/projects/"+s.key+"/requirements", body), http.StatusCreated, "write "+title),
		"requirement")["key"].(string)
}

// cellCount is how many risks a matrix counts in one cell, -1 for a cell it lacks.
func cellCount(m map[string]any, severity, probability int) int {
	for _, c := range m["cells"].([]any) {
		cell := c.(map[string]any)
		if cell["severity"] == float64(severity) && cell["probability"] == float64(probability) {
			return int(cell["count"].(float64))
		}
	}
	return -1
}

// riskKeysOf are the keys of the risks a page of the FMEA table or a report lists.
func riskKeysOf(t *testing.T, rows []any) []string {
	t.Helper()
	keys := make([]string, len(rows))
	for i, each := range rows {
		keys[i] = each.(map[string]any)["risk"].(map[string]any)["key"].(string)
	}
	return keys
}

// A scheme is set only while its project is open, each save is audited with its actor, and detection stays on
// while a rating gives it (#325).
func TestASchemeIsSavedOnlyWhileItsProjectIsOpen(t *testing.T) {
	s := newRiskScene(t, "RSA")
	owner := s.f.owner
	scheme := "/api/v1/projects/" + s.key + "/risk-scheme"
	saved := func() int {
		return s.f.h.countRows(t, `SELECT count(*) FROM audit_log WHERE action = 'risk_scheme.saved' AND target_id = $1 AND actor_user_id = $2
			AND data->>'projectKey' = $3`, s.projectID(t), owner.user, s.key)
	}

	t.Run("a save writes one audit row naming its actor", func(t *testing.T) {
		before := saved()
		want(t, owner.put(t, scheme, riskScheme(5, "matrix", true)), http.StatusOK, "rate detection too")
		if got := saved(); got != before+1 {
			t.Errorf("a save wrote %d audit rows", got-before)
		}
	})

	t.Run("detection stays on while a rating gives it", func(t *testing.T) {
		want(t, owner.patch(t, "/api/v1/requirements/"+s.riskKey, map[string]any{"baseRevision": 1,
			"risk": map[string]any{"before": map[string]any{"severity": 4, "probability": 3, "detection": 2}}}), http.StatusOK, "rate detection")
		r := owner.put(t, scheme, riskScheme(5, "matrix", false))
		refuse(t, "detection turned off", r, http.StatusConflict, "risk_scale_held")
		if msg := refusalMessage(t, r); !strings.HasPrefix(msg, "Risks are rated on detection.") {
			t.Errorf("the refusal says %q", msg)
		}
		got := obj(t, want(t, owner.get(t, scheme), http.StatusOK, "read the scheme"), "scheme")
		rating := ratingOf(t, obj(t, want(t, owner.get(t, "/api/v1/requirements/"+s.riskKey), http.StatusOK, "read the risk"), "requirement"))
		if got["detection"] != true || rating["before"].(map[string]any)["detection"] != float64(2) {
			t.Errorf("after the refusal the scheme rates detection %v and the risk %v", got["detection"], rating["before"])
		}
	})

	t.Run("an archived project keeps its scheme", func(t *testing.T) {
		kept := want(t, owner.get(t, scheme), http.StatusOK, "read the scheme").Raw
		audited := saved()
		want(t, owner.put(t, "/api/v1/projects/"+s.key+"/archive", nil), http.StatusOK, "archive the project")
		refuse(t, "a scheme set in an archived project", owner.put(t, scheme, riskScheme(5, "rpn", true)), http.StatusConflict, "project_archived")
		if got := want(t, owner.get(t, scheme), http.StatusOK, "read it again").Raw; !bytes.Equal(got, kept) {
			t.Errorf("the scheme changed in the archive: %s", got)
		}
		if got := saved(); got != audited {
			t.Errorf("a refused save wrote %d audit rows", got-audited)
		}
	})
}

// The FMEA table pages without repeating a risk and refuses a page or a cell off its bounds, naming the field (#325).
func TestTheFMEATablePagesAndRefusesWhatIsOffItsBounds(t *testing.T) {
	const pageSize = 2
	s := newRiskScene(t, "RSB")
	for i := range 4 {
		s.writeRisk(t, fmt.Sprintf("Hazard %d", i), i+1, 1)
	}
	risks := "/api/v1/projects/" + s.key + "/risks"

	t.Run("limit and offset walk every risk once, the total unchanged", func(t *testing.T) {
		seen := map[string]bool{}
		for offset := 0; offset < 5; offset += pageSize {
			page := want(t, s.f.owner.get(t, fmt.Sprintf("%s?limit=%d&offset=%d", risks, pageSize, offset)), http.StatusOK, "read a page").Body
			if page["total"] != float64(5) {
				t.Errorf("at %d the total is %v", offset, page["total"])
			}
			keys := riskKeysOf(t, page["risks"].([]any))
			if len(keys) != min(pageSize, 5-offset) {
				t.Errorf("at %d the page holds %v", offset, keys)
			}
			for _, k := range keys {
				if seen[k] {
					t.Errorf("%s comes twice", k)
				}
				seen[k] = true
			}
		}
		if len(seen) != 5 {
			t.Errorf("the pages hold %d risks of 5", len(seen))
		}
	})

	t.Run("a page or a cell off its bounds is refused on its field", func(t *testing.T) {
		for _, c := range []struct{ query, field string }{
			{fmt.Sprintf("limit=%d", risk.MaxRows+1), "limit"},
			{"severity=0", "severity"},
			{"severity=abc", "severity"},
			{fmt.Sprintf("probability=%d", risk.MaxScaleValue+1), "probability"},
		} {
			r := s.f.owner.get(t, risks+"?"+c.query)
			if r.Status != http.StatusUnprocessableEntity {
				t.Errorf("%s: %d %s", c.query, r.Status, r.Raw)
				continue
			}
			fieldError(t, r, c.field)
		}
	})
}

// Without a scheme every risk is unrated and the matrix has no cells, and no report of any kind is made (#325).
func TestRisksWithoutASchemeAreUnratedAndReportNothing(t *testing.T) {
	f := newProjectFixture(t)
	key := projectKey(t, "RSC")
	want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": "No scheme"}), http.StatusCreated, "make the project")
	hazard := obj(t, want(t, f.owner.post(t, "/api/v1/projects/"+key+"/types", map[string]any{"name": "Hazard", "icon": "risk", "colour": "#dc2626", "role": "risk"}),
		http.StatusCreated, "make a type of risks"), "type")["id"].(string)
	want(t, f.owner.post(t, "/api/v1/projects/"+key+"/requirements", map[string]any{"title": "Brakes fail on ice", "typeId": hazard}), http.StatusCreated, "write a risk")
	base := "/api/v1/projects/" + key

	table := want(t, f.owner.get(t, base+"/risks"), http.StatusOK, "read the table").Body
	rows := table["risks"].([]any)
	if table["total"] != float64(1) || table["unrated"] != float64(1) || len(rows) != 1 {
		t.Fatalf("the table reads %v", table)
	}
	if rating := rows[0].(map[string]any)["rating"].(map[string]any); rating["level"] != nil || rating["beforeLevel"] != nil || rating["beforeRpn"] != nil {
		t.Errorf("an unrated risk reads %v", rating)
	}
	matrix := obj(t, want(t, f.owner.get(t, base+"/risk-matrix"), http.StatusOK, "read the matrix"), "matrix")
	if len(matrix["cells"].([]any)) != 0 || matrix["unrated"] != float64(1) || matrix["severity"].(map[string]any)["dimension"] != "severity" ||
		matrix["probability"].(map[string]any)["dimension"] != "probability" {
		t.Errorf("the matrix reads %v", matrix)
	}
	for _, format := range []string{"risk-report", "risk-report.docx", "risk-report.pdf"} {
		refuse(t, format+" without a scheme", f.owner.get(t, base+"/"+format), http.StatusConflict, "risk_scheme_missing")
	}
	if n := f.h.countRows(t, `SELECT count(*) FROM audit_log WHERE action = 'specification.exported' AND data->>'project' = $1`, key); n != 0 {
		t.Errorf("%d exports were audited that never left", n)
	}
}

// A query narrows the matrix, the report and the workbook as it narrows the table, and every download is audited
// with its format and how many risks it holds (#325).
func TestAQueryNarrowsTheMatrixTheReportAndTheDownloads(t *testing.T) {
	s := newRiskScene(t, "RSD")
	owner := s.f.owner
	s.writeRisk(t, "Brakes squeal", 1, 1)
	fade := s.writeRisk(t, "Brakes fade on a descent", 4, 3)
	base := "/api/v1/projects/" + s.key
	onIce := "?query=" + url.QueryEscape("title ~ ice")

	t.Run("the matrix counts only the risks kept", func(t *testing.T) {
		all := obj(t, want(t, owner.get(t, base+"/risk-matrix"), http.StatusOK, "the whole matrix"), "matrix")
		kept := obj(t, want(t, owner.get(t, base+"/risk-matrix"+onIce), http.StatusOK, "the matrix narrowed"), "matrix")
		if cellCount(all, 4, 3) != 2 || cellCount(all, 1, 1) != 1 || cellCount(kept, 4, 3) != 1 || cellCount(kept, 1, 1) != 0 {
			t.Errorf("the cells 4:3 and 1:1 count %d and %d, narrowed %d and %d",
				cellCount(all, 4, 3), cellCount(all, 1, 1), cellCount(kept, 4, 3), cellCount(kept, 1, 1))
		}
	})

	t.Run("the report lists only the risks kept, counted at their levels", func(t *testing.T) {
		report := obj(t, want(t, owner.get(t, base+"/risk-report"+onIce), http.StatusOK, "the report narrowed"), "report")
		sameList(t, "the report's risks", riskKeysOf(t, report["risks"].([]any)), s.riskKey)
		counts := map[string]float64{}
		for _, l := range report["levels"].([]any) {
			level := l.(map[string]any)
			counts[level["level"].(map[string]any)["name"].(string)] = level["before"].(float64)
		}
		if counts["High"] != 1 || counts["Low"] != 0 || counts["Medium"] != 0 {
			t.Errorf("the narrowed report counts %v before mitigation", counts)
		}
	})

	exported := func(format string, risks int) int {
		return s.f.h.countRows(t, `SELECT count(*) FROM audit_log WHERE action = 'specification.exported' AND actor_user_id = $1
			AND data->>'project' = $2 AND data->>'kind' = 'risks' AND data->>'format' = $3 AND (data->>'risks')::int = $4`, owner.user, s.key, format, risks)
	}

	t.Run("the workbook holds the cell's risks the query keeps, and is audited", func(t *testing.T) {
		got := want(t, owner.get(t, base+"/risks.xlsx"+onIce+"&severity=4&probability=3"), http.StatusOK, "download a cell narrowed")
		strs := string(unzipped(t, got.Raw)["xl/sharedStrings.xml"])
		if !strings.Contains(strs, "Brakes fail on ice") || strings.Contains(strs, "Brakes fade on a descent") || strings.Contains(strs, "Brakes squeal") {
			t.Errorf("the workbook holds %s", strs)
		}
		if n := exported("xlsx", 1); n != 1 {
			t.Errorf("the workbook of one risk was audited %d times", n)
		}
	})

	t.Run("the Word and PDF reports are audited with the risks they hold", func(t *testing.T) {
		word := unzipped(t, want(t, owner.get(t, base+"/risk-report.docx"+onIce), http.StatusOK, "the Word report narrowed").Raw)["word/document.xml"]
		if !bytes.Contains(word, []byte("Brakes fail on ice")) || bytes.Contains(word, []byte(fade)) {
			t.Errorf("the narrowed Word report holds the wrong risks")
		}
		pdf := want(t, owner.get(t, base+"/risk-report.pdf"), http.StatusOK, "the PDF report")
		if !bytes.HasPrefix(pdf.Raw, []byte("%PDF-")) {
			t.Errorf("the PDF report starts %q", pdf.Raw[:min(len(pdf.Raw), 8)])
		}
		if docx, printed := exported("docx", 1), exported("pdf", 3); docx != 1 || printed != 1 {
			t.Errorf("the Word report was audited %d times and the PDF %d", docx, printed)
		}
	})
}

// A mitigation from a project the reader may not view counts for the risk's state but names nothing, and the
// mitigations of a risk are not found outside its project (#325).
func TestAHiddenMitigationCountsButNamesNothing(t *testing.T) {
	s := newRiskScene(t, "RSE")
	owner, reader := s.f.owner, s.f.plain
	hidden := projectKey(t, "RSF")
	made := obj(t, want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": hidden, "name": "Hidden controls"}), http.StatusCreated, "make a hidden project"), "project")
	if _, err := s.f.h.super.Exec(t.Context(), `DELETE FROM project_grant WHERE project_id = $1 AND subject_type = 'everyone'`, made["id"]); err != nil {
		t.Fatal(err)
	}
	s.f.h.settle(t)
	want(t, owner.post(t, "/api/v1/projects/"+hidden+"/requirements", map[string]any{"title": "Secret brake control"}), http.StatusCreated, "write the control")
	want(t, owner.post(t, "/api/v1/requirements/"+hidden+"-R1/links", map[string]any{"typeKey": "mitigates", "otherKey": s.riskKey}),
		http.StatusCreated, "mitigate the risk from the hidden project")
	s.f.h.settle(t)

	nameless := func(what string, m map[string]any) {
		t.Helper()
		source := m["source"].(map[string]any)
		if source["hidden"] != true || source["key"] != "" || source["title"] != "" || source["projectKey"] != "" || m["inEffect"] != true {
			t.Errorf("%s: the hidden mitigation reads %v", what, m)
		}
	}
	got := want(t, reader.get(t, "/api/v1/requirements/"+s.riskKey+"/mitigations"), http.StatusOK, "the reader reads the mitigations").Body
	if ms := got["mitigations"].([]any); len(ms) != 1 || got["state"] != "mitigated" {
		t.Fatalf("the reader reads %v", got)
	} else {
		nameless("the mitigations", ms[0].(map[string]any))
	}
	table := want(t, reader.get(t, "/api/v1/projects/"+s.key+"/risks"), http.StatusOK, "the reader reads the table").Body
	row := table["risks"].([]any)[0].(map[string]any)
	nameless("the table", row["mitigations"].([]any)[0].(map[string]any))
	if row["rating"].(map[string]any)["state"] != "mitigated" {
		t.Errorf("the table's risk stands %v", row["rating"])
	}
	if strs := string(unzipped(t, want(t, reader.get(t, "/api/v1/projects/"+s.key+"/risks.xlsx"), http.StatusOK, "the reader downloads the table").Raw)["xl/sharedStrings.xml"]); strings.Contains(strs, "Secret brake control") ||
		!strings.Contains(strs, "A requirement you may not view") {
		t.Errorf("the reader's workbook names the hidden mitigation as %s", strs)
	}
	ownerView := want(t, owner.get(t, "/api/v1/requirements/"+s.riskKey+"/mitigations"), http.StatusOK, "the owner reads them").Body
	if src := ownerView["mitigations"].([]any)[0].(map[string]any)["source"].(map[string]any); src["key"] != hidden+"-R1" || src["hidden"] != false {
		t.Errorf("the owner reads the source as %v", src)
	}
	refuse(t, "the mitigations outside the project", s.f.outside.get(t, "/api/v1/requirements/"+s.riskKey+"/mitigations"), http.StatusNotFound, "not_found")
}

// sameJSON says whether two answers hold the same JSON.
func sameJSON(t *testing.T, a, b any) bool {
	t.Helper()
	var x, y any
	for _, each := range []struct {
		from any
		into *any
	}{{a, &x}, {b, &y}} {
		raw, err := json.Marshal(each.from)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, each.into); err != nil {
			t.Fatal(err)
		}
	}
	return reflect.DeepEqual(x, y)
}

// The five risk tools answer what their HTTP calls answer, and refuse in the result what those refuse (#325).
func TestTheRiskToolsAnswerAsTheirCallsDo(t *testing.T) {
	s := newRiskScene(t, "RSG")
	owner := s.f.owner
	want(t, owner.post(t, "/api/v1/requirements/"+s.plainKey+"/links", map[string]any{"typeKey": "mitigates", "otherKey": s.riskKey}),
		http.StatusCreated, "mitigate the risk")
	bare := projectKey(t, "RSH")
	want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": bare, "name": "No scheme"}), http.StatusCreated, "make a project without a scheme")
	_, secret := makeToken(t, owner, map[string]any{"name": "assistant"})
	s.f.h.settle(t)
	a := connectAssistant(t, s.f.api, secret)
	base := "/api/v1/projects/" + s.key

	for _, c := range []struct {
		tool string
		args map[string]any
		path string
	}{
		{"get_risk_scheme", map[string]any{"projectKey": s.key}, base + "/risk-scheme"},
		{"list_risks", map[string]any{"projectKey": s.key, "side": "before", "severity": 4, "probability": 3, "limit": 10}, base + "/risks?side=before&severity=4&probability=3&limit=10"},
		{"get_risk_matrix", map[string]any{"projectKey": s.key, "side": "before"}, base + "/risk-matrix?side=before"},
		{"get_risk_mitigations", map[string]any{"reqKey": s.riskKey}, "/api/v1/requirements/" + s.riskKey + "/mitigations"},
		{"get_risk_report", map[string]any{"projectKey": s.key, "query": "rpn >= 10"}, base + "/risk-report?query=" + url.QueryEscape("rpn >= 10")},
	} {
		got := a.call(t, c.tool, c.args)
		if direct := want(t, owner.get(t, c.path), http.StatusOK, c.path).Body; !sameJSON(t, got, direct) {
			t.Errorf("%s answers %v, the call %v", c.tool, got, direct)
		}
	}
	if msg := a.refused(t, "get_risk_report", map[string]any{"projectKey": bare}); !strings.Contains(msg, "no risk scheme") {
		t.Errorf("a report without a scheme is refused with %q", msg)
	}
	if msg := a.refused(t, "get_risk_mitigations", map[string]any{"reqKey": s.plainKey}); !strings.Contains(msg, "no risk") {
		t.Errorf("the mitigations of a requirement are refused with %q", msg)
	}
}
