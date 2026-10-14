package audit

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/config"
)

func TestEveryActionIsNamedOnceAsDomainDotVerb(t *testing.T) {
	shape := regexp.MustCompile(`^[a-z][a-z_]*\.[a-z_]+$`)
	seen := map[string]bool{}
	for _, a := range Actions {
		if !shape.MatchString(a) {
			t.Errorf("action %q is not domain.verb", a)
		}
		if seen[a] {
			t.Errorf("action %q is listed twice", a)
		}
		seen[a] = true
	}
}

func TestACredentialNeverReachesTheRecord(t *testing.T) {
	data := map[string]any{
		"name":            "Deploy bot",
		"clientSecret":    "s3cr3t-value",
		"webhookSecret":   "kept",
		"apiToken":        "anything at all",
		"password":        "",
		"tokensForgotten": 3,
		"enabled":         true,
		"note":            "exciter_pat_abcdef",
		"grants": []any{
			map[string]any{"id": "x", "token": "armature_pat_123"},
			"armature_whs_abc",
		},
		"nested": map[string]any{"Password": "hunter2", "secretChange": "set"},
	}
	raw, err := Scrub(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{"s3cr3t-value", "anything at all", "exciter_pat_", "armature_pat_", "armature_whs_", "hunter2"} {
		if strings.Contains(string(raw), leaked) {
			t.Errorf("the record holds %q: %s", leaked, raw)
		}
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["name"] != "Deploy bot" || got["webhookSecret"] != "kept" || got["tokensForgotten"] != float64(3) || got["enabled"] != true {
		t.Errorf("what is no secret was changed: %s", raw)
	}
	if got["clientSecret"] != Redacted || got["note"] != Redacted {
		t.Errorf("a secret is not marked redacted: %s", raw)
	}
	if nested := got["nested"].(map[string]any); nested["secretChange"] != "set" || nested["Password"] != Redacted {
		t.Errorf("the nested data reads %v", nested)
	}
}

func TestTheFilterIsBoundNotWritten(t *testing.T) {
	actor, target := uuid.New(), uuid.New()
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 1, 0)
	where, args := Filter{Action: "space.created'; DROP TABLE audit_log; --", ActorID: &actor, TargetType: "space", TargetID: &target, From: &from, To: &to}.where()
	if strings.Contains(where, "DROP") || len(args) != 6 {
		t.Errorf("the filter wrote %q with %d arguments", where, len(args))
	}
	if !strings.HasPrefix(where, "a.action = $1 AND ") {
		t.Errorf("the filter does not start with the action: %q", where)
	}
	if where, args := (Filter{}).where(); where != "true" || len(args) != 0 {
		t.Errorf("no filter is %q with %v", where, args)
	}
}

func TestAnExportNotesTheFilterItWasMadeWith(t *testing.T) {
	actor := uuid.New()
	from := time.Date(2026, 9, 1, 22, 0, 0, 0, time.UTC)
	got := Filter{Action: ActionEventDeadLettered, ActorID: &actor, From: &from}.Describe()
	if got["action"] != ActionEventDeadLettered || got["actorId"] != actor || got["from"] != "2026-09-01T22:00:00Z" || len(got) != 3 {
		t.Errorf("the export's filter reads %v", got)
	}
}

func TestASpreadsheetReadsNoFormulaFromTheLog(t *testing.T) {
	for in, want := range map[string]string{"": "", "Ann": "Ann", "=SUM(A1)": "'=SUM(A1)", "+1": "'+1", "-1": "'-1", "@x": "'@x"} {
		if got := Cell(in); got != want {
			t.Errorf("Cell(%q) = %q, want %q", in, got, want)
		}
	}
	target := uuid.New()
	line := CSVLine(AuditEntry{
		Action: ActionEventDeadLettered, TargetType: "event", TargetID: &target, ActorName: "=Mallory",
		Data: json.RawMessage(`{"topic":"requirement.revised"}`), CreatedAt: time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC),
	})
	if len(line) != len(CSVHeader) || line[0] != "2026-10-01T08:00:00Z" || line[2] != "" || line[3] != "'=Mallory" || line[5] != target.String() {
		t.Errorf("the line reads %q", line)
	}
}

func TestTheRetentionTheConfigurationNamesIsThisPackages(t *testing.T) {
	if config.DefaultRetainAudit != DefaultRetention || config.MinRetainAudit != MinRetention {
		t.Errorf("config keeps %s at least %s, the log %s at least %s",
			config.DefaultRetainAudit, config.MinRetainAudit, DefaultRetention, MinRetention)
	}
}
