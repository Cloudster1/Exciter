package via

import "testing"

func TestTheColumnNamesTheAliasAndIsNullForASession(t *testing.T) {
	want := `CASE WHEN r.via_kind IS NULL THEN NULL ELSE jsonb_build_object('kind', r.via_kind, 'name', r.via_name) END`
	if got := Column("r"); got != want {
		t.Errorf("the column reads %s", got)
	}
}
