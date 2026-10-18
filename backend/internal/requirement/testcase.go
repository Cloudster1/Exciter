package requirement

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/markdown"
)

// A test case is a requirement whose type has the test-case role (#250): it carries preconditions, steps and
// parameters besides. See docs/api-contract-m12.md.

// Agreed limits of a test case (#253).
const (
	MaxSteps                = 100
	MaxParameters           = 10
	MaxParameterValues      = 50
	MaxParameterValueLength = 200
	MaxParameterNameLength  = 50
)

// TypeRole is what a type's requirements are, as the attribute package keeps types.
type TypeRole = attribute.TypeRole

const (
	RoleRequirement = attribute.RoleRequirement
	RoleTestCase    = attribute.RoleTestCase
)

// Step is one step of a test case: what to do, and what should follow, a rich text document each.
type Step struct {
	Action json.RawMessage `json:"action"`
	// Expected is null when the step expects nothing to be checked.
	Expected json.RawMessage `json:"expected"`
	// ActionMarkdown and ExpectedMarkdown are the same as Markdown, sent instead of the documents or read beside them.
	ActionMarkdown   *string `json:"actionMarkdown,omitempty"`
	ExpectedMarkdown *string `json:"expectedMarkdown,omitempty"`
}

// Parameter is a value a test case is run with; the n-th values of all its parameters make its n-th iteration.
type Parameter struct {
	Name   string   `json:"name"`
	Values []string `json:"values"`
}

// TestCase is what a test case carries besides a requirement's content.
type TestCase struct {
	// Preconditions is a document, null for none.
	Preconditions         json.RawMessage `json:"preconditions"`
	PreconditionsMarkdown *string         `json:"preconditionsMarkdown,omitempty"`
	Steps                 []Step          `json:"steps"`
	Parameters            []Parameter     `json:"parameters"`
}

// Empty says it holds nothing, as every requirement that is not a test case.
func (t TestCase) Empty() bool {
	return (len(t.Preconditions) == 0 || isNull(t.Preconditions)) && len(t.Steps) == 0 && len(t.Parameters) == 0
}

// Iterations is how often the case runs: once per value its parameters line up, once without any.
func (t TestCase) Iterations() int {
	if len(t.Parameters) == 0 {
		return 1
	}
	return len(t.Parameters[0].Values)
}

// Values are the parameters' values of iteration n, from 1, by name.
func (t TestCase) Values(n int) map[string]string {
	out := map[string]string{}
	for _, p := range t.Parameters {
		if n >= 1 && n <= len(p.Values) {
			out[p.Name] = p.Values[n-1]
		}
	}
	return out
}

// settled is the case with its slices never nil, as the API answers it.
func (t TestCase) settled() TestCase {
	if t.Steps == nil {
		t.Steps = []Step{}
	}
	if t.Parameters == nil {
		t.Parameters = []Parameter{}
	}
	if isNull(t.Preconditions) {
		t.Preconditions = nil
	}
	// A step kept without an expected result reads back as JSON null, which is none.
	for i := range t.Steps {
		if isNull(t.Steps[i].Expected) {
			t.Steps[i].Expected = nil
		}
	}
	return t
}

// checkTestCase reads what a writer sent, Markdown included, into what is kept; the refusal names the field.
func checkTestCase(in TestCase, resolve markdown.Resolve) (TestCase, error) {
	out := TestCase{}
	var err error
	if out.Preconditions, err = FromMarkdown("preconditions", in.PreconditionsMarkdown, in.Preconditions, resolve, checkStatement); err != nil {
		return out, err
	}
	if len(out.Preconditions) > 0 && !isNull(out.Preconditions) {
		if out.Preconditions, err = checkDocument("preconditions", out.Preconditions); err != nil {
			return out, err
		}
	} else {
		out.Preconditions = nil
	}
	if len(in.Steps) > MaxSteps {
		return out, &FieldError{"steps", fmt.Sprintf("A test case has at most %d steps. Split it into more cases.", MaxSteps)}
	}
	out.Steps = make([]Step, 0, len(in.Steps))
	for i, s := range in.Steps {
		field := fmt.Sprintf("steps[%d]", i)
		step := Step{}
		if step.Action, err = FromMarkdown(field+".action", s.ActionMarkdown, s.Action, resolve, checkStatement); err != nil {
			return out, err
		}
		if len(step.Action) == 0 || isNull(step.Action) || blank(step.Action) {
			return out, &FieldError{field + ".action", "Say what to do in this step, or remove it."}
		}
		if step.Action, err = checkDocument(field+".action", step.Action); err != nil {
			return out, err
		}
		if step.Expected, err = FromMarkdown(field+".expected", s.ExpectedMarkdown, s.Expected, resolve, checkStatement); err != nil {
			return out, err
		}
		if len(step.Expected) == 0 || isNull(step.Expected) || blank(step.Expected) {
			step.Expected = nil
		} else if step.Expected, err = checkDocument(field+".expected", step.Expected); err != nil {
			return out, err
		}
		out.Steps = append(out.Steps, step)
	}
	if out.Parameters, err = checkParameters(in.Parameters); err != nil {
		return out, err
	}
	return out.settled(), nil
}

func checkParameters(in []Parameter) ([]Parameter, error) {
	if len(in) > MaxParameters {
		return nil, &FieldError{"parameters", fmt.Sprintf("A test case has at most %d parameters.", MaxParameters)}
	}
	out := make([]Parameter, 0, len(in))
	seen := map[string]bool{}
	for i, p := range in {
		field := fmt.Sprintf("parameters[%d]", i)
		name := strings.TrimSpace(p.Name)
		switch {
		case name == "":
			return nil, &FieldError{field + ".name", "Name the parameter."}
		case utf8.RuneCountInString(name) > MaxParameterNameLength:
			return nil, &FieldError{field + ".name", fmt.Sprintf("Keep the parameter's name to %d characters.", MaxParameterNameLength)}
		case seen[strings.ToLower(name)]:
			return nil, &FieldError{field + ".name", "Another parameter is called that. Give each its own name."}
		}
		seen[strings.ToLower(name)] = true
		if len(p.Values) == 0 || len(p.Values) > MaxParameterValues {
			return nil, &FieldError{field + ".values", fmt.Sprintf("Give the parameter 1 to %d values.", MaxParameterValues)}
		}
		if i > 0 && len(p.Values) != len(out[0].Values) {
			return nil, &FieldError{field + ".values", fmt.Sprintf("Give every parameter as many values as %s has, %d: each iteration takes one value of each.",
				out[0].Name, len(out[0].Values))}
		}
		values := make([]string, len(p.Values))
		for j, v := range p.Values {
			if utf8.RuneCountInString(v) > MaxParameterValueLength {
				return nil, &FieldError{field + ".values", fmt.Sprintf("Keep each value to %d characters.", MaxParameterValueLength)}
			}
			values[j] = strings.TrimSpace(v)
		}
		out = append(out, Parameter{Name: name, Values: values})
	}
	return out, nil
}

// blank says a document holds no words.
func blank(doc json.RawMessage) bool {
	c, ok := canonical(doc).(map[string]any)
	if !ok {
		return false
	}
	blocks, _ := c["content"].([]any)
	return len(blocks) == 0
}

// columns are the case as the database keeps it: preconditions, steps and parameters.
func (t TestCase) columns() (any, []byte, []byte, error) {
	t = t.settled()
	steps := make([]Step, len(t.Steps))
	for i, s := range t.Steps {
		steps[i] = Step{Action: s.Action, Expected: s.Expected}
	}
	encodedSteps, err := json.Marshal(steps)
	if err != nil {
		return nil, nil, nil, err
	}
	params, err := json.Marshal(t.Parameters)
	if err != nil {
		return nil, nil, nil, err
	}
	var pre any
	if len(t.Preconditions) > 0 {
		pre = t.Preconditions
	}
	return pre, encodedSteps, params, nil
}

// scanTestCase reads the columns a select gave.
func scanTestCase(pre, steps, params []byte) (TestCase, error) {
	var t TestCase
	if len(pre) > 0 && string(pre) != "null" {
		t.Preconditions = pre
	}
	if len(steps) > 0 {
		if err := json.Unmarshal(steps, &t.Steps); err != nil {
			return t, fmt.Errorf("read the steps: %w", err)
		}
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &t.Parameters); err != nil {
			return t, fmt.Errorf("read the parameters: %w", err)
		}
	}
	return t.settled(), nil
}

// hashed adds the case to the content a hash covers, and nothing when it holds nothing, so every other requirement
// keeps the hash it had.
func (t TestCase) hashed(content map[string]any) {
	if len(t.Preconditions) > 0 && !isNull(t.Preconditions) {
		content["preconditions"] = canonical(t.Preconditions)
	}
	if len(t.Steps) > 0 {
		steps := make([]any, len(t.Steps))
		for i, s := range t.Steps {
			steps[i] = map[string]any{"action": canonical(s.Action), "expected": canonical(s.Expected)}
		}
		content["steps"] = steps
	}
	if len(t.Parameters) > 0 {
		params := make([]any, len(t.Parameters))
		for i, p := range t.Parameters {
			params[i] = map[string]any{"name": p.Name, "values": p.Values}
		}
		content["parameters"] = params
	}
}

// WithMarkdown adds the preconditions and each step as Markdown.
func (t *TestCase) WithMarkdown() {
	t.PreconditionsMarkdown = markdown.Of(t.Preconditions)
	for i := range t.Steps {
		t.Steps[i].ActionMarkdown, t.Steps[i].ExpectedMarkdown = markdown.Of(t.Steps[i].Action), markdown.Of(t.Steps[i].Expected)
	}
}

// roleOf is the role of a type, requirement for none.
func roleOf(ctx context.Context, tx db.DBTX, typeID *uuid.UUID) (TypeRole, error) {
	if typeID == nil {
		return RoleRequirement, nil
	}
	var role TypeRole
	err := tx.QueryRow(ctx, `SELECT role FROM requirement_type WHERE id = $1`, *typeID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return RoleRequirement, nil
	}
	return role, err
}

// testFor keeps what a test case carries when its type is a test-case type and drops it otherwise, as a change of
// type, a move or a copy to another project does.
func testFor(ctx context.Context, tx db.DBTX, typeID *uuid.UUID, t TestCase) (TestCase, error) {
	if t.Empty() {
		return TestCase{}.settled(), nil
	}
	role, err := roleOf(ctx, tx, typeID)
	if err != nil || role != RoleTestCase {
		return TestCase{}.settled(), err
	}
	return t, nil
}

// errNotTestCase refuses what only a test case carries on anything else.
var errNotTestCase = &FieldError{"testCase", "Only a requirement of a type marked as test cases has preconditions, steps and parameters. Pick such a type first."}

// testOf checks what a writer sent as a test case for a requirement of typeID; nil sends nothing.
func testOf(ctx context.Context, tx db.DBTX, typeID *uuid.UUID, in *TestCase, resolve markdown.Resolve) (TestCase, error) {
	if in == nil {
		return TestCase{}.settled(), nil
	}
	checked, err := checkTestCase(*in, resolve)
	if err != nil || checked.Empty() {
		return checked, err
	}
	role, err := roleOf(ctx, tx, typeID)
	if err != nil {
		return checked, err
	}
	if role != RoleTestCase {
		return checked, errNotTestCase
	}
	return checked, nil
}

// testCaseRefusal says what the database refused when a requirement that is no test case was given steps.
func testCaseRefusal(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.ConstraintName == "requirement_test_case_fields" {
		return errNotTestCase
	}
	return err
}

// ReadTestCase reads a test case as the database keeps it, for whoever selects its columns elsewhere.
func ReadTestCase(preconditions, steps, parameters []byte) (TestCase, error) {
	return scanTestCase(preconditions, steps, parameters)
}

// mergeTest lays what a change sends over what a test case carries: a field it leaves out stays.
func mergeTest(cur, in TestCase) TestCase {
	out := cur
	if len(in.Preconditions) > 0 || in.PreconditionsMarkdown != nil {
		out.Preconditions, out.PreconditionsMarkdown = in.Preconditions, in.PreconditionsMarkdown
	}
	if in.Steps != nil {
		out.Steps = in.Steps
	}
	if in.Parameters != nil {
		out.Parameters = in.Parameters
	}
	return out
}
