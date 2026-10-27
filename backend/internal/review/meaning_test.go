package review

import "testing"

// The sentence a signer confirms names the decision, the revision and the requirement in their language, English
// unless it is German.
func TestTheMeaningOfADecisionIsInTheSignersLanguage(t *testing.T) {
	for _, c := range []struct {
		lang     string
		decision Decision
		want     string
	}{
		{"en", DecisionApprove, "I approve revision 3 of CHK-R7, Pay the basket, as one of its approvers."},
		{"en", DecisionReject, "I reject revision 3 of CHK-R7, Pay the basket, as one of its approvers."},
		{"de", DecisionApprove, "Ich genehmige Revision 3 von CHK-R7, Pay the basket, als eine der genehmigenden Personen."},
		{"de", DecisionReject, "Ich lehne Revision 3 von CHK-R7, Pay the basket, als eine der genehmigenden Personen ab."},
		{"", DecisionReject, "I reject revision 3 of CHK-R7, Pay the basket, as one of its approvers."},
		{"fr", DecisionApprove, "I approve revision 3 of CHK-R7, Pay the basket, as one of its approvers."},
	} {
		if got := meaning(c.lang, c.decision, 3, "CHK-R7", "Pay the basket"); got != c.want {
			t.Errorf("%s in %q: %q, want %q", c.decision, c.lang, got, c.want)
		}
	}
}
