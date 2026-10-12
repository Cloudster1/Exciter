package requirement

import (
	"encoding/json"
	"regexp"
	"testing"
)

func TestKeysParseWithoutRegardToCase(t *testing.T) {
	for in, want := range map[string]struct {
		project string
		number  int
	}{"CHK-R42": {"CHK", 42}, " chk-r7 ": {"CHK", 7}, "SYS2-R1": {"SYS2", 1}} {
		p, n, ok := ParseKey(in)
		if !ok || p != want.project || n != want.number {
			t.Errorf("ParseKey(%q) = %q %d %v", in, p, n, ok)
		}
	}
	for _, bad := range []string{"CHK-42", "CHK-R0", "CHK-R07", "C-R1", "CHK-R", "2X-R1", "CHK-R1x"} {
		if _, _, ok := ParseKey(bad); ok {
			t.Errorf("ParseKey(%q) took a key that is none", bad)
		}
	}
	if Key("CHK", 42) != "CHK-R42" {
		t.Error("Key does not write CHK-R42")
	}
}

// Text with an Exciter key sits beside issue keys, which Armature and Stator find with these patterns, copied from
// Armature's issue/model.go and Stator's armature/armature.go; neither may take an Exciter key for an issue.
func TestNoIssueKeyPatternReadsAnExciterKey(t *testing.T) {
	patterns := []string{`^([A-Z][A-Z0-9]{1,9})-([1-9][0-9]{0,17})$`, `^[A-Z][A-Z0-9]{1,9}-[1-9][0-9]{0,17}$`}
	for _, p := range patterns {
		anchored := regexp.MustCompile(p)
		// Found in running text, the same pattern between word boundaries.
		inText := regexp.MustCompile(`\b` + p[1:len(p)-1] + `\b`)
		for _, key := range []string{"CHK-R1", "CHK-R42", "SYS2-R999"} {
			if anchored.MatchString(key) || inText.MatchString("see "+key+" first") {
				t.Errorf("%s reads as an issue key under %s", key, p)
			}
		}
	}
}

func TestTheHashIsStableUnderKeyOrderAndWhiteSpace(t *testing.T) {
	a := Normative{Title: "Pay  the basket ", Statement: json.RawMessage(`{"type":"doc","content":[
		{"type":"paragraph"},
		{"type":"paragraph","content":[{"type":"text","text":"  Pay   now "},{"type":"text","marks":[{"type":"bold"}],"text":"quickly  "}]},
		{"type":"paragraph"}]}`)}
	b := Normative{Title: "Pay the basket", Statement: json.RawMessage(`{"content":[
		{"content":[{"text":"Pay now ","type":"text"},{"marks":[{"type":"bold"}],"type":"text","text":"quickly"}],"type":"paragraph"}],"type":"doc"}`)}
	if a.Hash() != b.Hash() {
		t.Error("the same words with other spacing and key order hash differently")
	}
	c := b
	c.Statement = json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Pay now quickly"}]}]}`)
	if c.Hash() == b.Hash() {
		t.Error("taking the bold away is no change to the hash")
	}
	d := b
	d.Rationale = json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Because."}]}]}`)
	if d.Hash() == b.Hash() {
		t.Error("a rationale is no change to the hash")
	}
}
