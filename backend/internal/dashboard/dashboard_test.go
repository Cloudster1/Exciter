package dashboard

import (
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/search"
)

func TestABucketsConditionJoinsTheSourcesQueryWithoutItsOrder(t *testing.T) {
	for _, tc := range []struct{ source, cond, want string }{
		{"", "status = \"Draft\"", "status = \"Draft\""},
		{"type = Functional", "suspect > 0", "(type = Functional) AND suspect > 0"},
		{"owner = currentUser() ORDER BY updated DESC", "approval = pending", "(owner = currentUser()) AND approval = pending"},
	} {
		if got := and(search.Filter{Query: tc.source}, tc.cond).Query; got != tc.want {
			t.Errorf("%q and %q = %q, want %q", tc.source, tc.cond, got, tc.want)
		}
	}
	if got := quote(`Say "hi" \ there`); got != `"Say \"hi\" \\ there"` {
		t.Errorf("quoted as %s", got)
	}
}

func TestAWidgetIsCheckedAndFilledIn(t *testing.T) {
	w, err := checkWidget(WidgetInput{Kind: Changes, Title: " Changes "})
	if err != nil || w.Title != "Changes" || w.Width != 2 || w.Config.Interval != Week || w.Config.Days != DefaultDays || len(w.Config.Series) != 2 {
		t.Errorf("filled in as %+v, %v", w, err)
	}
	for name, in := range map[string]WidgetInput{
		"an unknown kind":        {Kind: "gauge", Title: "G"},
		"no title":               {Kind: Count},
		"too wide":               {Kind: Count, Title: "C", Width: MaxWidth + 1},
		"an unknown grouping":    {Kind: Breakdown, Title: "B", Config: Config{GroupBy: "colour"}},
		"an attribute unnamed":   {Kind: Breakdown, Title: "B", Config: Config{GroupBy: ByAttribute}},
		"too many rows":          {Kind: List, Title: "L", Config: Config{Rows: MaxListRows + 1}},
		"a query and a view":     {Kind: Count, Title: "C", Config: Config{Source: Source{Query: "x", ViewID: &uuid.Nil}}},
		"an unknown series":      {Kind: Changes, Title: "C", Config: Config{Series: []Series{"deleted"}}},
		"a window past the most": {Kind: Changes, Title: "C", Config: Config{Days: MaxDays + 1}},
	} {
		if _, err := checkWidget(in); err == nil {
			t.Errorf("%s was taken", name)
		}
	}
}

func TestEveryTemplatesWidgetHolds(t *testing.T) {
	for _, tpl := range Templates() {
		if len(tpl.Widgets) == 0 {
			t.Errorf("%s has no widgets", tpl.Key)
		}
		for _, w := range tpl.Widgets {
			if _, err := checkWidget(w); err != nil {
				t.Errorf("%s: %s: %v", tpl.Key, w.Title, err)
			}
		}
	}
	if _, ok := templateOf("overview"); !ok {
		t.Error("there is no overview")
	}
}

func TestAnAttributesBucketOpensWithItsValue(t *testing.T) {
	low := attribute.Option{ID: uuid.New(), Name: "Low"}
	risk := attribute.Def{ID: uuid.New(), Name: "Risk", Kind: attribute.Select, Options: []attribute.Option{low}}
	cost := attribute.Def{ID: uuid.New(), Name: "Cost", Kind: attribute.Number}
	notes := attribute.Def{ID: uuid.New(), Name: "Notes", Kind: attribute.RichText}
	defs := []attribute.Def{risk, cost, notes}
	g, _, err := groupingOf(defs, Config{GroupBy: ByAttribute, AttributeID: &risk.ID})
	if err != nil || g.cond(low.ID.String()) != `"Risk" = "Low"` || g.none != `"Risk" IS EMPTY` {
		t.Errorf("a select reads %q, %q, %v", g.cond(low.ID.String()), g.none, err)
	}
	if g, _, _ := groupingOf(defs, Config{GroupBy: ByAttribute, AttributeID: &cost.ID}); g.cond("12.5") != `"Cost" = 12.5` {
		t.Errorf("a number reads %q", g.cond("12.5"))
	}
	if _, _, err := groupingOf(defs, Config{GroupBy: ByAttribute, AttributeID: &notes.ID}); err == nil {
		t.Error("formatted text was counted by value")
	}
	if g, _, _ := groupingOf(defs, Config{GroupBy: ByStatus}); g.cond("In review") != `status = "In review"` || g.none != "status IS EMPTY" {
		t.Errorf("a status reads %q", g.cond("In review"))
	}
}
