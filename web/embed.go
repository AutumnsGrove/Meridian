// Package web embeds the compiled SvelteKit app in build/ so the binary is self-contained.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:build
var build embed.FS

// FS returns the built frontend rooted at the contents of build/, so callers ask for
// "index.html" and not "build/index.html".
func FS() fs.FS {
	rooted, err := fs.Sub(build, "build")
	if err != nil {
		// build/ is guaranteed to exist at compile time by the //go:embed directive, so this
		// branch is defensive only.
		panic("web: embedded build directory is missing: " + err.Error())
	}
	return rooted
}
