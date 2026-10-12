package requirement

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/attribute"
)

func TestAComparisonShowsValuesAsAReaderSeesThem(t *testing.T) {
	low, high := attribute.Option{ID: uuid.New(), Name: "Low"}, attribute.Option{ID: uuid.New(), Name: "High"}
	ada := uuid.NewString()
	people := map[string]string{ada: "Ada Lovelace"}
	for _, c := range []struct {
		def  attribute.Def
		raw  string
		want string
	}{
		{attribute.Def{Kind: attribute.Select, Options: []attribute.Option{low, high}}, `"` + high.ID.String() + `"`, "High"},
		{attribute.Def{Kind: attribute.MultiSelect, Options: []attribute.Option{low, high}}, `["` + low.ID.String() + `","` + high.ID.String() + `"]`, "Low, High"},
		{attribute.Def{Kind: attribute.User}, `"` + ada + `"`, "Ada Lovelace"},
		{attribute.Def{Kind: attribute.Boolean}, `false`, "No"},
		{attribute.Def{Kind: attribute.Date}, `"2026-01-31"`, "2026-01-31"},
		{attribute.Def{Kind: attribute.Number}, `2.5`, "2.5"},
		{attribute.Def{Kind: attribute.RichText}, `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Why"}]}]}`, "Why"},
	} {
		d := c.def
		if got := shown(&d, json.RawMessage(c.raw), people); got != c.want {
			t.Errorf("%s %s reads %q, want %q", d.Kind, c.raw, got, c.want)
		}
	}
	if got := shown(nil, json.RawMessage(`"left behind"`), people); got != `"left behind"` {
		t.Errorf("a value no attribute names reads %q", got)
	}
}
