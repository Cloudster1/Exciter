package requirement

// A risk is a requirement whose type has the risk role (#259): it carries a rating before and after mitigation
// besides. See docs/api-contract-m13.md.

// Side is a risk's rating on one side of its mitigation: the value of each scale, null while unknown.
type Side struct {
	Severity    *int `json:"severity"`
	Probability *int `json:"probability"`
	// Detection is null while unknown, and always when the project's scheme does not rate it.
	Detection *int `json:"detection"`
}

// Rating is what a risk is rated, before and after it is mitigated.
type Rating struct {
	Before Side `json:"before"`
	After  Side `json:"after"`
}

// RiskLevel is a level of the project's risk scheme, as a side of a rating gives it.
type RiskLevel struct {
	Name       string `json:"name"`
	Colour     string `json:"colour"`
	Acceptable bool   `json:"acceptable"`
}

// RiskState says how far a risk is mitigated (#260): nothing mitigates it, a mitigation is not in effect, or all are.
type RiskState string

const (
	Unmitigated RiskState = "unmitigated"
	Claimed     RiskState = "claimed"
	Mitigated   RiskState = "mitigated"
)

// RiskStates lists every RiskState, for the API document.
var RiskStates = []RiskState{Unmitigated, Claimed, Mitigated}

// RiskView is a risk's rating as it is read: each side with the priority number and the level the scheme gives it.
type RiskView struct {
	Rating
	BeforeRPN   *int       `json:"beforeRpn"`
	AfterRPN    *int       `json:"afterRpn"`
	BeforeLevel *RiskLevel `json:"beforeLevel"`
	AfterLevel  *RiskLevel `json:"afterLevel"`
	// State and Level, the level of the side in effect, arrive with mitigations (#260).
	State RiskState  `json:"state"`
	Level *RiskLevel `json:"level"`
}

// RiskMark is how the trace graph and the chain report mark a risk (#261).
type RiskMark struct {
	Level *RiskLevel `json:"level"`
	State RiskState  `json:"state"`
}
