// Package migrations embeds the versioned SQL migrations so they ship inside
// the binary.
package migrations

import "embed"

// FS contains all versioned migration files.
//
//go:embed *.sql
var FS embed.FS
