// Package via says what a row was written through (#190): a personal token or an OAuth grant, by its name at that
// moment, or nothing for a browser session. The database writes it; this package only reads it.
package via

// Kind is what a row was written through.
type Kind string

const (
	KindToken Kind = "token"
	KindOAuth Kind = "oauth"
)

// Kinds lists every Kind, for the API document.
var Kinds = []Kind{KindToken, KindOAuth}

// Via is what a row was written through; a row written in a session has none.
type Via struct {
	Kind Kind   `json:"kind"`
	Name string `json:"name"`
}

// Column reads a row's via as one JSON value, null for a session, so a query adds one column and a scan one
// *Via, which pgx fills from the JSON.
func Column(alias string) string {
	return `CASE WHEN ` + alias + `.via_kind IS NULL THEN NULL ELSE jsonb_build_object('kind', ` + alias + `.via_kind, 'name', ` + alias + `.via_name) END`
}
