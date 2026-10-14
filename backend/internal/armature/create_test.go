package armature

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestAnIssueIsSummarizedByItsRequirementWithinArmaturesBound(t *testing.T) {
	if got := IssueSummary("CHK-R1", "  Brakes\n engage   within 100 ms "); got != "CHK-R1 Brakes engage within 100 ms" {
		t.Errorf("summary %q", got)
	}
	long := IssueSummary("CHK-R1", strings.Repeat("ä", 2*MaxSummaryLength))
	if utf8.RuneCountInString(long) != MaxSummaryLength || !strings.HasPrefix(long, "CHK-R1 ä") || !strings.HasSuffix(long, "...") {
		t.Errorf("a long title is summarized as %d characters ending %q", utf8.RuneCountInString(long), long[len(long)-6:])
	}
}

func TestAnIssuesDescriptionLeadsBackToItsRequirement(t *testing.T) {
	doc := Description("CHK-R1", "https://exciter.example/r/1")
	paragraph := doc["content"].([]any)[0].(map[string]any)["content"].([]any)
	var text strings.Builder
	for _, node := range paragraph {
		text.WriteString(node.(map[string]any)["text"].(string))
	}
	if text.String() != "From CHK-R1 in Exciter" {
		t.Errorf("the description reads %q", text.String())
	}
	mark := paragraph[1].(map[string]any)["marks"].([]any)[0].(map[string]any)
	if mark["type"] != "link" || mark["attrs"].(map[string]any)["href"] != "https://exciter.example/r/1" {
		t.Errorf("the key links to %v", mark)
	}
}

func TestCheckCreateTidiesAndRefuses(t *testing.T) {
	ok := CreateInput{RequirementKeys: []string{" chk-r1 ", "CHK-R2", "chk-R1"}, ArmatureProject: " cp ", Relation: Implements}
	if err := CheckCreate(&ok); err != nil {
		t.Fatal(err)
	}
	if ok.ArmatureProject != "CP" || strings.Join(ok.RequirementKeys, ",") != "CHK-R1,CHK-R2" {
		t.Errorf("tidied to %q %v", ok.ArmatureProject, ok.RequirementKeys)
	}
	many := make([]string, MaxCreateItems+1)
	for i := range many {
		many[i] = "CHK-R" + strings.Repeat("1", i+1)
	}
	for name, tc := range map[string]struct {
		in    CreateInput
		field string
	}{
		"no project":    {CreateInput{RequirementKeys: []string{"CHK-R1"}, ArmatureProject: " ", Relation: Implements}, "armatureProject"},
		"relates":       {CreateInput{RequirementKeys: []string{"CHK-R1"}, ArmatureProject: "CP", Relation: Relates}, "relation"},
		"no relation":   {CreateInput{RequirementKeys: []string{"CHK-R1"}, ArmatureProject: "CP"}, "relation"},
		"no keys":       {CreateInput{ArmatureProject: "CP", Relation: Verifies}, "requirementKeys"},
		"a blank key":   {CreateInput{RequirementKeys: []string{"CHK-R1", " "}, ArmatureProject: "CP", Relation: Verifies}, "requirementKeys"},
		"too many keys": {CreateInput{RequirementKeys: many, ArmatureProject: "CP", Relation: Verifies}, "requirementKeys"},
	} {
		err := CheckCreate(&tc.in)
		field, isField := err.(*FieldError)
		if !isField || field.Field != tc.field {
			t.Errorf("%s: %v, want a refusal of %s", name, err, tc.field)
		}
	}
}
