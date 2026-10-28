package testexec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/review"
)

func TestAFileIsWhatItSaysOnlyWhenItSaysSoPlainly(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	for name, tc := range map[string]struct {
		declared string
		data     []byte
		want     string
	}{
		"a plain type":                     {"image/png", png, "image/png"},
		"a plain type with parameters":     {"text/plain; charset=utf-8", []byte("notes"), "text/plain; charset=utf-8"},
		"padded with spaces":               {"  application/pdf  ", []byte("%PDF-1.7"), "application/pdf"},
		"nothing declared":                 {"", png, "image/png"},
		"the browser's shrug":              {"application/octet-stream", []byte("plain words"), "text/plain; charset=utf-8"},
		"a line break smuggled in":         {"text/plain\r\nX-Evil: 1", png, "image/png"},
		"a bare line feed":                 {"image/png\nX-Evil: 1", []byte("plain words"), "text/plain; charset=utf-8"},
		"a type that does not parse":       {"image/", png, "image/png"},
		"a parameter that does not parse":  {"text/plain; =broken", []byte("plain words"), "text/plain; charset=utf-8"},
		"a declared type wins over sniffs": {"text/html", png, "text/html"},
	} {
		if got := contentTypeOf(tc.declared, tc.data); got != tc.want {
			t.Errorf("%s: %q, want %q", name, got, tc.want)
		}
	}
}

func TestADueDateLiesAheadWithinAYear(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	at := func(d time.Duration) *time.Time { v := now.Add(d); return &v }
	for name, tc := range map[string]struct {
		due *time.Time
		ok  bool
	}{
		"none":                {nil, true},
		"now":                 {at(0), true},
		"tomorrow":            {at(24 * time.Hour), true},
		"at the furthest":     {at(MaxDueAhead), true},
		"a second gone by":    {at(-time.Second), false},
		"a second too far":    {at(MaxDueAhead + time.Second), false},
		"two years ahead":     {at(2 * MaxDueAhead), false},
		"a week in the past":  {at(-7 * 24 * time.Hour), false},
		"an hour before then": {at(-time.Hour), false},
	} {
		err := checkDue(tc.due, now)
		var field *FieldError
		switch {
		case tc.ok && err != nil:
			t.Errorf("%s: refused with %v", name, err)
		case !tc.ok && (!errors.As(err, &field) || field.Field != "dueAt" || !strings.HasSuffix(field.Message, ".")):
			t.Errorf("%s: %v, want a sentence on dueAt", name, err)
		}
	}
}

// mayComment answers every existence check of checkTesters with yes, so only what it checks before asking the
// database decides; the integration tests ask the real one.
type mayComment struct{ asked int }

func (m *mayComment) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("checkTesters writes nothing")
}

func (m *mayComment) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("checkTesters lists nothing")
}

func (m *mayComment) QueryRow(context.Context, string, ...any) pgx.Row {
	m.asked++
	return yes{}
}

type yes struct{}

func (yes) Scan(dest ...any) error {
	*dest[0].(*bool) = true
	return nil
}

func TestTestersAreOneToFiftyPeopleOrGroupsEachNamedOnce(t *testing.T) {
	person := func() review.Subject { id := uuid.New(); return review.Subject{Type: review.SubjectUser, UserID: &id} }
	group := func() review.Subject {
		id := uuid.New()
		return review.Subject{Type: review.SubjectGroup, GroupID: &id}
	}
	many := func(n int) []review.Subject {
		out := make([]review.Subject, n)
		for i := range out {
			out[i] = person()
		}
		return out
	}
	count := fmt.Sprintf("Name 1 to %d people or groups who test.", MaxTesters)
	twice := person()
	sameGroup := group()
	someone := uuid.New()
	for name, tc := range map[string]struct {
		in   []review.Subject
		want string
	}{
		"one person":                       {[]review.Subject{person()}, ""},
		"a person and a group":             {[]review.Subject{person(), group()}, ""},
		"as many as allowed":               {many(MaxTesters), ""},
		"nobody":                           {nil, count},
		"one too many":                     {many(MaxTesters + 1), count},
		"a person twice":                   {[]review.Subject{twice, person(), twice}, "Name each tester once."},
		"a group twice":                    {[]review.Subject{sameGroup, sameGroup}, "Name each tester once."},
		"a role":                           {[]review.Subject{{Type: "role", UserID: &someone}}, "A tester is a person or a group."},
		"no kind":                          {[]review.Subject{{UserID: &someone}}, "A tester is a person or a group."},
		"a person without an id":           {[]review.Subject{{Type: review.SubjectUser}}, "Name each tester by the id of the person or the group."},
		"a group named by a person's id":   {[]review.Subject{{Type: review.SubjectGroup, UserID: &someone}}, "Name each tester by the id of the person or the group."},
		"the same id as person and group":  {[]review.Subject{{Type: review.SubjectUser, UserID: &someone}, {Type: review.SubjectGroup, GroupID: &someone}}, ""},
		"a person after one named already": {[]review.Subject{person(), {Type: review.SubjectUser}}, "Name each tester by the id of the person or the group."},
	} {
		db := &mayComment{}
		err := checkTesters(context.Background(), db, uuid.New(), tc.in)
		if tc.want == "" {
			if err != nil {
				t.Errorf("%s: refused with %v", name, err)
			} else if db.asked != len(tc.in) {
				t.Errorf("%s: asked the database %d times for %d testers", name, db.asked, len(tc.in))
			}
			continue
		}
		var field *FieldError
		if !errors.As(err, &field) || field.Field != "testers" || field.Message != tc.want {
			t.Errorf("%s: %v, want %q on testers", name, err, tc.want)
		}
	}
}

func doc(text string) json.RawMessage {
	raw, _ := json.Marshal(map[string]any{"type": "doc", "content": []any{
		map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": text}}}}})
	return raw
}

func TestAFailureIsFiledWithTheStepItFailedAt(t *testing.T) {
	failed := Failed
	e := &executionRow{Execution: Execution{Title: "Release 2 on wet roads", Version: "2.3", Configuration: "Winter tyres"}}
	c := &Case{
		Run: CaseRun{Case: CaseRef{Key: "BRK-R2", Title: "Brake on a wet road"}, Iteration: 2, Revision: 3, Outcome: &failed, Note: "Tyres were worn."},
		Steps: []requirement.Step{
			{Action: doc("Drive at the speed.")},
			{Action: doc("Press the pedal."), Expected: doc("The car stops within 40 m.")},
		},
		Results: []StepResult{{Step: 1, Note: "Fine."}, {Step: 2, Note: "It slid on."}},
	}
	step := func(n int) *int { return &n }
	const link = "http://exciter.test/p/BRK/tests/executions/x/run/BRK-R2"

	t.Run("without a summary it says which case failed where", func(t *testing.T) {
		d := failureDraft(e, c, FailureIssueInput{ArmatureProject: "CP"}, []string{"BRK-R1"}, link)
		if d.Summary != "BRK-R2 Brake on a wet road failed in Release 2 on wet roads" {
			t.Errorf("the summary reads %q", d.Summary)
		}
		want := []string{"BRK-R2 failed in the test execution Release 2 on wet roads, at revision 3, iteration 2.",
			"Version: 2.3. Configuration: Winter tyres.", "Tyres were worn."}
		if !slices.Equal(d.Paragraphs, want) {
			t.Errorf("the description reads %q", d.Paragraphs)
		}
		if d.ArmatureProject != "CP" || d.Link != link || !slices.Equal(d.RequirementKeys, []string{"BRK-R2", "BRK-R1"}) {
			t.Errorf("the draft is %+v", d)
		}
	})

	t.Run("a summary given is kept, trimmed", func(t *testing.T) {
		if d := failureDraft(e, c, FailureIssueInput{Summary: "  Brakes slide  "}, nil, link); d.Summary != "Brakes slide" {
			t.Errorf("the summary reads %q", d.Summary)
		}
	})

	t.Run("the step's action, expected result and actual note come from that step alone", func(t *testing.T) {
		d := failureDraft(e, c, FailureIssueInput{Step: step(2)}, nil, link)
		for _, wanted := range []string{"Step 2: Press the pedal.", "Expected: The car stops within 40 m.", "Actual: It slid on."} {
			if !slices.Contains(d.Paragraphs, wanted) {
				t.Errorf("the description lacks %q: %q", wanted, d.Paragraphs)
			}
		}
		if slices.Contains(d.Paragraphs, "Actual: Fine.") {
			t.Errorf("the note of step 1 is in the description of step 2: %q", d.Paragraphs)
		}
		first := failureDraft(e, c, FailureIssueInput{Step: step(1)}, nil, link)
		for _, wanted := range []string{"Step 1: Drive at the speed.", "Actual: Fine."} {
			if !slices.Contains(first.Paragraphs, wanted) {
				t.Errorf("the description lacks %q: %q", wanted, first.Paragraphs)
			}
		}
		for _, p := range first.Paragraphs {
			if strings.HasPrefix(p, "Expected:") {
				t.Errorf("a step that expects nothing says %q", p)
			}
		}
	})

	t.Run("a step the case does not have is left out", func(t *testing.T) {
		for _, n := range []int{0, 3, 101} {
			d := failureDraft(e, c, FailureIssueInput{Step: step(n)}, nil, link)
			for _, p := range d.Paragraphs {
				if strings.HasPrefix(p, "Step ") || strings.HasPrefix(p, "Actual:") {
					t.Errorf("step %d: the description says %q", n, p)
				}
			}
		}
	})

	t.Run("without a version or configuration that line is left out", func(t *testing.T) {
		bare := &executionRow{Execution: Execution{Title: "Smoke"}}
		for _, p := range failureDraft(bare, c, FailureIssueInput{}, nil, link).Paragraphs {
			if strings.HasPrefix(p, "Version:") {
				t.Errorf("the description says %q", p)
			}
		}
	})
}
