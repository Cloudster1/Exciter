package trace

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/requirement"
)

func TestShownLevelsFollowOneAnother(t *testing.T) {
	levels := []ModelLevel{{Kind: LevelRequirements}, {Kind: LevelRequirements}, {Kind: LevelRequirements}, {Kind: LevelIssues}}
	for _, c := range []struct {
		asked []int
		want  []int
		field bool
	}{
		{nil, []int{0, 1, 2, 3}, false},
		{[]int{2, 1, 1}, []int{1, 2}, false},
		{[]int{0, 2}, nil, true},
		{[]int{4}, nil, true},
		{[]int{3}, nil, true},
	} {
		got, err := shownLevels(levels, c.asked)
		var field *FieldError
		if c.field != errors.As(err, &field) || (!c.field && len(got) != len(c.want)) {
			t.Errorf("levels %v show %v, %v; want %v, refused %v", c.asked, got, err, c.want, c.field)
		}
	}
}

// node is a requirement on a line, by its key.
func node(key string) ChainNode {
	return ChainNode{Requirement: &requirement.Ref{ID: uuid.NewSHA1(uuid.Nil, []byte(key)), Key: key}}
}

func TestEveryPathIsALineAndOneThatEndsEarlyIsBroken(t *testing.T) {
	need, other := node("N1"), node("N2")
	sys1, sys2 := node("S1"), node("S2")
	c := &Chains{Levels: make([]ModelLevel, 3), Roots: []requirement.Ref{*need.Requirement, *other.Requirement}}
	below := []map[uuid.UUID][]ChainNode{
		nil,
		{need.Requirement.ID: {sys1, sys2}},
		{sys1.Requirement.ID: {{IssueKey: "WEB-1"}}},
	}
	if err := c.lay(below, nil, ChainQuery{MaxLines: MaxChainLines}); err != nil {
		t.Fatal(err)
	}
	var read []string
	for _, l := range c.Lines {
		line := ""
		for _, n := range l.Nodes {
			if n.Requirement != nil {
				line += n.Requirement.Key + " "
			} else {
				line += n.IssueKey + " "
			}
		}
		if l.Broken {
			line += "broken"
		}
		read = append(read, line)
	}
	want := []string{"N1 S1 WEB-1 ", "N1 S2 broken", "N2 broken"}
	if len(read) != len(want) || c.Broken != 2 {
		t.Fatalf("the lines read %q with %d broken, want %q", read, c.Broken, want)
	}
	for i := range want {
		if read[i] != want[i] {
			t.Errorf("line %d reads %q, want %q", i, read[i], want[i])
		}
	}

	broken := &Chains{Levels: c.Levels, Roots: c.Roots}
	if err := broken.lay(below, nil, ChainQuery{MaxLines: MaxChainLines, OnlyBroken: true}); err != nil || len(broken.Lines) != 2 || broken.Broken != 2 {
		t.Errorf("only the broken lines read %v, %v", broken.Lines, err)
	}
	var tooMany *TooManyLinesError
	if err := (&Chains{Levels: c.Levels, Roots: c.Roots}).lay(below, nil, ChainQuery{MaxLines: 2}); !errors.As(err, &tooMany) {
		t.Errorf("three lines past a bound of two read %v", err)
	}
	cut := &Chains{Levels: c.Levels, Roots: c.Roots}
	if err := cut.lay(below, nil, ChainQuery{MaxLines: 2, Truncate: true}); err != nil || !cut.Truncated || len(cut.Lines) != 2 {
		t.Errorf("a file cut at two lines reads %v, truncated %v, %v", cut.Lines, cut.Truncated, err)
	}
}

func TestAModelHoldsItsShape(t *testing.T) {
	typ := uuid.New()
	verifies := uuid.New()
	ok := ModelInput{Levels: []ModelLevelInput{
		{Name: "System", Kind: LevelRequirements, TypeIDs: []uuid.UUID{typ}},
		{Name: "Test", Kind: LevelRequirements, TypeIDs: []uuid.UUID{uuid.New()}, Steps: []ModelStep{{TypeID: &verifies, Direction: Incoming}}},
		{Name: "Issue", Kind: LevelIssues, Steps: []ModelStep{{Relation: "implements"}}},
	}}
	if err := checkModel(&ok); err != nil {
		t.Fatalf("a sound model is refused: %v", err)
	}
	for name, change := range map[string]func(m *ModelInput){
		"levels[1].name":               func(m *ModelInput) { m.Levels[1].Name = " system " },
		"levels[0].kind":               func(m *ModelInput) { m.Levels[0].Kind = LevelPages },
		"levels[0].steps":              func(m *ModelInput) { m.Levels[0].Steps = m.Levels[1].Steps },
		"levels[1].typeIds":            func(m *ModelInput) { m.Levels[1].TypeIDs = []uuid.UUID{typ} },
		"levels[2].steps[0].relation":  func(m *ModelInput) { m.Levels[2].Steps[0].Relation = "specifies" },
		"levels[1].steps[0].direction": func(m *ModelInput) { m.Levels[1].Steps[0].Direction = "both" },
		"levels[1].kind": func(m *ModelInput) {
			m.Levels[1], m.Levels[2] = m.Levels[2], m.Levels[1]
		},
	} {
		m := ModelInput{Levels: append([]ModelLevelInput{}, ok.Levels...)}
		for i := range m.Levels {
			m.Levels[i].Steps = append([]ModelStep{}, m.Levels[i].Steps...)
		}
		change(&m)
		var field *FieldError
		if err := checkModel(&m); !errors.As(err, &field) || field.Field != name {
			t.Errorf("a model refused on %s reads %v", name, err)
		}
	}
}
