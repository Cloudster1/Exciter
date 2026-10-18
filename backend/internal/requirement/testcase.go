package requirement

import "encoding/json"

// A test case is a requirement whose type has the test-case role (#250): it carries preconditions, steps and
// parameters besides. See docs/api-contract-m12.md.

// Agreed limits of a test case (#253).
const (
	MaxSteps                = 100
	MaxParameters           = 10
	MaxParameterValues      = 50
	MaxParameterValueLength = 200
)

// TypeRole is what a type's requirements are: requirements, or test cases.
type TypeRole string

const (
	RoleRequirement TypeRole = "requirement"
	RoleTestCase    TypeRole = "testCase"
)

// TypeRoles lists every TypeRole, for the API document.
var TypeRoles = []TypeRole{RoleRequirement, RoleTestCase}

// Step is one step of a test case: what to do, and what should follow, a rich text document each.
type Step struct {
	Action json.RawMessage `json:"action"`
	// Expected is null when the step expects nothing to be checked.
	Expected json.RawMessage `json:"expected"`
}

// Parameter is a value a test case is run with; the n-th values of all its parameters make its n-th iteration.
type Parameter struct {
	Name   string   `json:"name"`
	Values []string `json:"values"`
}
