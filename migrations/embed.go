// Package migrations embeds the SQL migration files into the binary so the
// API can migrate the database on startup without shipping extra files.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
