package requirement

import (
	"encoding/json"
	"strings"
	"testing"
)

func doc(blocks ...string) json.RawMessage {
	return json.RawMessage(`{"type":"doc","content":[` + strings.Join(blocks, ",") + `]}`)
}

func para(text string) string {
	if text == "" {
		return `{"type":"paragraph"}`
	}
	raw, _ := json.Marshal(text)
	return `{"type":"paragraph","content":[{"type":"text","text":` + string(raw) + `}]}`
}

func changes(blocks []DiffBlock) []DiffChange {
	out := make([]DiffChange, len(blocks))
	for i, b := range blocks {
		out[i] = b.Change
	}
	return out
}

func sameChanges(t *testing.T, got []DiffBlock, want ...DiffChange) {
	t.Helper()
	g := changes(got)
	if len(g) != len(want) {
		t.Fatalf("changes %v, want %v", g, want)
	}
	for i := range g {
		if g[i] != want[i] {
			t.Fatalf("changes %v, want %v", g, want)
		}
	}
}

// runs reads a block's text as runs of plain, inserted and deleted text:
// "+word" inserted, "-word" deleted, "word" kept.
func runs(t *testing.T, raw json.RawMessage) []string {
	t.Helper()
	var n node
	if err := json.Unmarshal(raw, &n); err != nil {
		t.Fatal(err)
	}
	var out []string
	var walk func(*node)
	walk = func(n *node) {
		if n.Type == "text" {
			prefix := ""
			for _, m := range n.Marks {
				switch m["type"] {
				case MarkDiffInsert:
					prefix = "+"
				case MarkDiffDelete:
					prefix = "-"
				}
			}
			out = append(out, prefix+n.Text)
		}
		for _, c := range n.Content {
			walk(c)
		}
	}
	walk(&n)
	return out
}

func sameRuns(t *testing.T, got []string, want ...string) {
	t.Helper()
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("runs %q, want %q", got, want)
	}
}

func mustDiff(t *testing.T, from, to json.RawMessage) []DiffBlock {
	t.Helper()
	blocks, err := Diff(from, to)
	if err != nil {
		t.Fatal(err)
	}
	return blocks
}

func TestIdenticalDocumentsAreAllEqual(t *testing.T) {
	d := doc(para("One"), para("Two"))
	sameChanges(t, mustDiff(t, d, d), DiffEqual, DiffEqual)
}

func TestTheEmptyPageMakesEveryBlockInserted(t *testing.T) {
	sameChanges(t, mustDiff(t, nil, doc(para("One"), `{"type":"horizontalRule"}`)), DiffInserted, DiffInserted)
	sameChanges(t, mustDiff(t, doc(para("One")), nil), DiffDeleted)
}

func TestBlocksAddedAndRemovedKeepReadingOrder(t *testing.T) {
	got := mustDiff(t, doc(para("A"), para("B"), para("C")), doc(para("A"), para("C"), para("D")))
	sameChanges(t, got, DiffEqual, DiffDeleted, DiffEqual, DiffInserted)
	sameRuns(t, runs(t, got[1].Node), "B")
}

func TestAChangedParagraphMarksWordsInline(t *testing.T) {
	got := mustDiff(t, doc(para("The quick brown fox")), doc(para("The slow brown fox jumps")))
	sameChanges(t, got, DiffModified)
	sameRuns(t, runs(t, got[0].Node), "The ", "-quick", "+slow", " brown fox", "+ jumps")
}

func TestAMarkChangeIsAWordReplaced(t *testing.T) {
	bold := `{"type":"paragraph","content":[{"type":"text","text":"a "},{"type":"text","text":"b","marks":[{"type":"bold"}]}]}`
	got := mustDiff(t, doc(para("a b")), doc(bold))
	sameChanges(t, got, DiffModified)
	sameRuns(t, runs(t, got[0].Node), "a ", "-b", "+b")
	var n node
	_ = json.Unmarshal(got[0].Node, &n)
	last := n.Content[len(n.Content)-1]
	if len(last.Marks) != 2 || last.Marks[0]["type"] != "bold" || last.Marks[1]["type"] != MarkDiffInsert {
		t.Fatalf("the inserted word keeps its own marks first: %v", last.Marks)
	}
}

func TestBlocksOfAnotherTypeAreReplacedWhole(t *testing.T) {
	heading := `{"type":"heading","attrs":{"level":2,"id":"a"},"content":[{"type":"text","text":"A"}]}`
	sameChanges(t, mustDiff(t, doc(para("A")), doc(heading)), DiffDeleted, DiffInserted)
}

func TestAHeadingWhoseAnchorFollowsItsWordsIsModified(t *testing.T) {
	h := func(id, text string) string {
		return `{"type":"heading","attrs":{"level":2,"id":"` + id + `"},"content":[{"type":"text","text":"` + text + `"}]}`
	}
	got := mustDiff(t, doc(h("setup", "Setup")), doc(h("setup-guide", "Setup guide")))
	sameChanges(t, got, DiffModified)
	sameRuns(t, runs(t, got[0].Node), "Setup", "+ guide")
}

func list(items ...string) string {
	var parts []string
	for _, item := range items {
		parts = append(parts, `{"type":"listItem","content":[`+para(item)+`]}`)
	}
	return `{"type":"bulletList","content":[` + strings.Join(parts, ",") + `]}`
}

func TestAListWithAnItemMoreIsModifiedWithTheItemMarked(t *testing.T) {
	got := mustDiff(t, doc(list("one", "two")), doc(list("one", "one and a half", "two")))
	sameChanges(t, got, DiffModified)
	sameRuns(t, runs(t, got[0].Node), "one", "+one and a half", "two")
	got = mustDiff(t, doc(list("one", "two")), doc(list("two")))
	sameRuns(t, runs(t, got[0].Node), "-one", "two")
}

func table(rows ...[]string) string {
	var rs []string
	for _, row := range rows {
		var cells []string
		for _, c := range row {
			cells = append(cells, `{"type":"tableCell","attrs":{"colspan":1,"rowspan":1},"content":[`+para(c)+`]}`)
		}
		rs = append(rs, `{"type":"tableRow","content":[`+strings.Join(cells, ",")+`]}`)
	}
	return `{"type":"table","content":[` + strings.Join(rs, ",") + `]}`
}

func TestATableOfTheSameShapeIsComparedCellByCell(t *testing.T) {
	got := mustDiff(t, doc(table([]string{"a", "b"}, []string{"c", "d"})), doc(table([]string{"a", "B"}, []string{"c", "d"})))
	sameChanges(t, got, DiffModified)
	sameRuns(t, runs(t, got[0].Node), "a", "-b", "+B", "c", "d")
}

func TestATableWhoseShapeChangedIsReplacedWhole(t *testing.T) {
	got := mustDiff(t, doc(table([]string{"a", "b"})), doc(table([]string{"a", "b"}, []string{"c", "d"})))
	sameChanges(t, got, DiffDeleted, DiffInserted)
	got = mustDiff(t, doc(table([]string{"a", "b"})), doc(table([]string{"a", "b", "c"})))
	sameChanges(t, got, DiffDeleted, DiffInserted)
}

func TestInlineNodesThatAreNotTextAreComparedWhole(t *testing.T) {
	mention := func(id string) string {
		return `{"type":"paragraph","content":[{"type":"text","text":"Ask "},{"type":"mention","attrs":{"id":"` + id + `","label":"x"}}]}`
	}
	got := mustDiff(t, doc(mention("a")), doc(mention("b")))
	sameChanges(t, got, DiffModified)
	var n node
	_ = json.Unmarshal(got[0].Node, &n)
	if len(n.Content) != 3 || n.Content[1].Marks[0]["type"] != MarkDiffDelete || n.Content[2].Marks[0]["type"] != MarkDiffInsert {
		t.Fatalf("the mention swap reads %s", got[0].Node)
	}
}

func TestAHugeRewriteFallsBackToReplacingTheMiddle(t *testing.T) {
	var a, b []string
	for i := range 3000 {
		a = append(a, "a"+strings.Repeat("x", i%7))
		b = append(b, "b"+strings.Repeat("y", i%5))
	}
	got := mustDiff(t, doc(para("start"), para(strings.Join(a, " ")), para("end")), doc(para("start"), para(strings.Join(b, " ")), para("end")))
	sameChanges(t, got, DiffEqual, DiffModified, DiffEqual)
}

func TestAlignFindsTheLongestCommonRun(t *testing.T) {
	a, b := []rune("ABCBDAB"), []rune("BDCABA")
	got := align(len(a), len(b), func(i, j int) bool { return a[i] == b[j] })
	if len(got) != 4 {
		t.Fatalf("align matched %v, want four", got)
	}
	for k := 1; k < len(got); k++ {
		if got[k][0] <= got[k-1][0] || got[k][1] <= got[k-1][1] {
			t.Fatalf("align is out of order: %v", got)
		}
	}
}

func TestAChangedDiagramIsItsSourceWithWordsMarked(t *testing.T) {
	d := func(source string) string {
		raw, _ := json.Marshal(source)
		return `{"type":"diagram","attrs":{"language":"plantuml","source":` + string(raw) + `}}`
	}
	sameChanges(t, mustDiff(t, doc(d("A -> B")), doc(d("A -> B"))), DiffEqual)
	got := mustDiff(t, doc(d("[Pump] --> [Valve]")), doc(d("[Pump] --> [Tank]")))
	sameChanges(t, got, DiffModified)
	sameRuns(t, runs(t, got[0].Node), "[Pump] --> [", "-Valve", "+Tank", "]")
	var n node
	_ = json.Unmarshal(got[0].Node, &n)
	if n.Type != "codeBlock" || n.Attrs["language"] != "plantuml" {
		t.Errorf("a changed diagram reads as its source in its language: %s %v", n.Type, n.Attrs)
	}
	sameChanges(t, mustDiff(t, doc(para("A")), doc(d("A -> B"))), DiffDeleted, DiffInserted)
}
