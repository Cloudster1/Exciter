package quality

import "strings"

// Explain is the English message and fix of a rule's hit, for the API and MCP.
func Explain(rule RuleID, match string) (message, fix string) {
	quoted := `"` + match + `"`
	switch rule {
	case RuleVague:
		return quoted + " means what each reader wants it to mean.",
			"Say how much, how often or how well, with a number and a unit a test can measure."
	case RuleUnbounded:
		return quoted + " has no end a test can reach.",
			"Name the cases it covers, or give the limit."
	case RuleEscape:
		return quoted + " lets the requirement be skipped.",
			"Say exactly when it applies, or take the clause out."
	case RulePlaceholder:
		return quoted + " is text left open.",
			"Fill in what is meant before the requirement is reviewed."
	case RuleAndOr:
		return quoted + " leaves open whether both or one is meant.",
			`Write "and" or "or", or split it into two requirements.`
	case RuleMultipleShall:
		return "A second " + quoted + " in one sentence makes it two requirements.",
			"Split it into one requirement for each shall."
	case RuleNegative:
		return quoted + " says what shall not be, which a test cannot cover in full.",
			"Say what shall be instead, where you can."
	case RulePassive:
		return quoted + " does not say who does it.",
			`Name who does it as the subject, as in "The gateway shall encrypt", or add "by".`
	case RulePronoun:
		return quoted + " stands for something the requirement has not named before it.",
			"Name the thing itself."
	case RuleEARS:
		if w := strings.ToLower(match); w == "if" || w == "falls" {
			return "This condition starts with " + quoted + " but has no then.",
				`Write "If <trigger>, then the <system> shall <response>".`
		}
		return quoted + " fits none of the EARS shapes.",
			"Write The <system> shall <response>, with any condition first: When, While, If ... then or Where."
	case RuleAvoidTerm:
		return "The glossary says not to use " + quoted + ".",
			"Use the term the glossary names instead."
	}
	return quoted + " breaks a writing rule.", "Rewrite it."
}
