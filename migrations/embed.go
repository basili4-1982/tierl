// Package migrations embeds the SQL migrations so the binary can apply them.
package migrations

import "embed"

// FS holds the *.up.sql and *.down.sql files.
//
//go:embed *.sql
var FS embed.FS
