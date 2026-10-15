package rql

import (
	"strings"
	"testing"
)

func TestCompleteKnowsWhereTheCaretIs(t *testing.T) {
	cat := sample()
	cases := []struct {
		text   string
		slot   Slot
		field  string
		prefix string
		from   int
		want   string // a word that must be offered
		not    string // a word that must not be
	}{
		{"", SlotField, "", "", 0, "status", ""},
		{"", SlotField, "", "", 0, `"Story points"`, ""},
		{"sta", SlotField, "", "sta", 0, "status", "title"},
		{"Sto", SlotField, "", "Sto", 0, `"Story points"`, "status"},
		{"verif", SlotField, "", "verif", 0, "verifiedBy", ""},
		{"status ", SlotOperator, "status", "", 7, "IN", "<"},
		{"priority ", SlotOperator, "priority", "", 9, ">=", ""},
		{"status = ", SlotValue, "status", "", 9, "", ""},
		{"category = ", SlotValue, "category", "", 11, "review", ""},
		{"approval = ou", SlotValue, "approval", "ou", 11, "outdated", "none"},
		{"verified = ", SlotValue, "verified", "", 11, "partial", ""},
		{"owner = ", SlotValue, "owner", "", 8, "currentUser()", ""},
		{"status = Done ", SlotKeyword, "", "", 14, "AND", ""},
		{"status = Done AND ", SlotField, "", "", 18, "owner", ""},
		{"status IN (", SlotValue, "status", "", 11, "", ""},
		{"status IN (Done, ", SlotValue, "status", "", 17, "", ""},
		{"owner ", SlotOperator, "owner", "", 6, "IS EMPTY", ""},
		{"owner IS ", SlotKeyword, "", "", 9, "NOT EMPTY", ""},
		{"owner IS EMPTY ", SlotKeyword, "", "", 15, "ORDER BY", ""},
		{"status = Done ORDER ", SlotKeyword, "", "", 20, "BY", ""},
		{"status = Done ORDER BY ", SlotField, "", "", 23, "updated", "statement"},
		{"status = Done ORDER BY updated ", SlotKeyword, "", "", 31, "DESC", ""},
		{`"Story points" `, SlotOperator, "Story points", "", 15, ">=", "~"},
		{`"Risk" = `, SlotValue, "Risk", "", 9, "High", ""},
		{`"Safety" = `, SlotValue, "Safety", "", 11, "true", ""},
		{`title ~ "print`, SlotValue, "title", "print", 8, "", ""},
		{`(status = Done) `, SlotKeyword, "", "", 16, "OR", ""},
	}
	for _, c := range cases {
		got := cat.Complete(c.text, len([]rune(c.text)))
		if got.Slot != c.slot || got.Field != c.field || got.Prefix != c.prefix || got.From != c.from || got.To != len([]rune(c.text)) {
			t.Errorf("%q: slot %s field %q prefix %q from %d to %d", c.text, got.Slot, got.Field, got.Prefix, got.From, got.To)
		}
		if c.want != "" && !hasWord(got.Words, c.want) {
			t.Errorf("%q: %q missing from %v", c.text, c.want, got.Words)
		}
		if c.not != "" && hasWord(got.Words, c.not) {
			t.Errorf("%q: %q offered in %v", c.text, c.not, got.Words)
		}
	}
	if got := cat.Complete(`title ~ "print`, 14); !got.Quoted || got.From != 8 {
		t.Errorf("an open quote is the word being typed: %+v", got)
	}
	if got := cat.Complete(`"Sto`, 4); !got.Quoted || got.Slot != SlotField || !hasWord(got.Words, "Story points") {
		t.Errorf("a quote opened where a field goes starts an attribute: %+v", got)
	}
	// The caret in the middle of the text completes what is before it.
	if got := cat.Complete("status = Done AND owner = x", 3); got.Slot != SlotField || got.Prefix != "sta" {
		t.Errorf("mid-text caret: %+v", got)
	}
}

func TestTakesSaysWhatNamesToLookUp(t *testing.T) {
	cat := sample()
	for field, want := range map[Field]string{
		{Name: "status"}: "status", {Name: "type"}: "type", {Name: "owner"}: "person", {Name: "labels"}: "label",
		{Name: "parent"}: "requirement", {Name: "verifiedBy"}: "requirement", {Name: "Reviewer", Custom: true}: "person",
		{Name: "approval"}: "", {Name: "nonsense"}: "",
	} {
		if got := cat.Takes(field); got != want {
			t.Errorf("%v: got %q want %q", field, got, want)
		}
	}
}

func hasWord(words []string, w string) bool {
	for _, each := range words {
		if strings.EqualFold(each, w) {
			return true
		}
	}
	return false
}
