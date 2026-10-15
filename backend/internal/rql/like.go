package rql

import "strings"

// likeEscaper backslashes what ILIKE would read as a pattern, so a search for
// "50%" finds those three characters. Backslash is ILIKE's default escape.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

func escapeLike(text string) string { return likeEscaper.Replace(text) }
