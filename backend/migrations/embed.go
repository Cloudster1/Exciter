// Package migrations embeds the SQL migration files so that the migrate binary
// is self contained: the container image needs no bind mount to run them.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
