// Package migrations embeds the goose SQL migration files.
package migrations

import "embed"

// FS holds all *.sql migration files in this directory. It is handed to
// goose via SetBaseFS; migration files live at the root of this FS.
//
//go:embed *.sql
var FS embed.FS
