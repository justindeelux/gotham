package store

import (
	"embed"
	"io/fs"
)

// migrationsFS embeds the forward-only SQL migrations into the binary. Neither
// CI nor production needs the goose CLI: the embedded files are executed by the
// goose library at runtime.
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations returns the embedded migration filesystem rooted at the migrations
// directory, which is the layout goose expects.
func Migrations() fs.FS {
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		// The embed directive guarantees this subtree exists, so reaching here
		// indicates a programming error rather than a runtime condition.
		panic(err)
	}
	return sub
}
