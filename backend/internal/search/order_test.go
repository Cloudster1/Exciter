package search

import (
	"strconv"
	"strings"
	"testing"

	"github.com/cloudster1/exciter/backend/internal/rql"
)

// binder is a stub of the grid's bind, numbering what it is given from $1 and keeping it.
type binder struct{ args []any }

func (b *binder) bind(v any) string {
	b.args = append(b.args, v)
	return "$" + strconv.Itoa(len(b.args))
}

func compiled(t *testing.T, text string) *rql.Compiled {
	t.Helper()
	q, err := rql.Parse(text)
	if err != nil {
		t.Fatalf("%q: %v", text, err)
	}
	c, err := q.Compile(rql.NewCatalog(gridDef, nil), rql.Env{})
	if err != nil {
		t.Fatalf("%q: %v", text, err)
	}
	return c
}

// The grid orders by a query's ORDER BY over the column sort, by the column otherwise with empty values last, and
// by the outline in the end, so a window never shows a row twice (#311).
func TestOrderByPutsTheQueryFirstAndTheOutlineLast(t *testing.T) {
	t.Run("no sort is outline order", func(t *testing.T) {
		var b binder
		if got := orderBy(nil, Sort{}, gridDef, b.bind); got != `w.path` || len(b.args) != 0 {
			t.Errorf("got %s binding %v", got, b.args)
		}
		if got := orderBy(compiled(t, `kind = section`), Sort{}, gridDef, b.bind); got != `w.path` {
			t.Errorf("a query without ORDER BY orders by %s", got)
		}
	})

	t.Run("a column sort ends in its empty values, then the outline", func(t *testing.T) {
		var b binder
		if got := orderBy(nil, Sort{Column: "title"}, gridDef, b.bind); got != `lower(r.title) ASC NULLS LAST, w.path` {
			t.Errorf("by title: %s", got)
		}
		if got := orderBy(nil, Sort{Column: "updated", Desc: true}, gridDef, b.bind); got != `r.updated_at DESC NULLS LAST, w.path` {
			t.Errorf("by the latest change, backwards: %s", got)
		}
		got := orderBy(nil, Sort{Column: costID.String()}, gridDef, b.bind)
		if got != `(r.attributes ->> $1)::numeric ASC NULLS LAST, w.path` || len(b.args) != 1 || b.args[0] != costID.String() {
			t.Errorf("by a number attribute: %s binding %v", got, b.args)
		}
	})

	t.Run("a query's ORDER BY outranks the column sort", func(t *testing.T) {
		query := compiled(t, `kind = requirement ORDER BY priority DESC, key`)
		var want binder
		keys := query.PlaceOrder(want.bind)
		if len(keys) != 2 {
			t.Fatalf("the query orders by %v", keys)
		}
		var b binder
		got := orderBy(query, Sort{Column: "title"}, gridDef, b.bind)
		expected := keys[0].SQL + ` DESC NULLS LAST, ` + keys[1].SQL + ` ASC NULLS LAST, w.path`
		if got != expected {
			t.Errorf("got %s\nwant %s", got, expected)
		}
		if strings.Contains(got, "lower(r.title)") {
			t.Errorf("the column sort is in %s", got)
		}
	})

	t.Run("an attribute in a query's ORDER BY binds its own arguments", func(t *testing.T) {
		var b binder
		got := orderBy(compiled(t, `ORDER BY "Cost" DESC`), Sort{}, gridDef, b.bind)
		if !strings.HasSuffix(got, ` DESC NULLS LAST, w.path`) || !strings.Contains(got, "$1") || len(b.args) == 0 {
			t.Errorf("got %s binding %v", got, b.args)
		}
	})
}
