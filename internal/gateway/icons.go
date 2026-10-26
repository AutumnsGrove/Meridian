package gateway

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
)

// iconPattern is the closed grammar of an icon file name: a plain name and a lower case image
// extension. A name that does not match is a 404, never a cleaned path.
var iconPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+\.(png|webp|jpg|jpeg|gif)$`)

// IconNameAllowed reports whether name is a file name this gateway will serve out of the icons
// directory (decision #43). It is a name, not a path: no separators, nothing that can climb out.
func IconNameAllowed(name string) bool {
	if name == "." || name == ".." {
		return false
	}
	return iconPattern.MatchString(name)
}

// icons serves a local image out of <config dir>/icons/. The directory is opened as an os.Root, so a
// symlink or a name that points outside it cannot be read, even if the name passed the grammar.
func icons(deps Deps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("file")
		if !IconNameAllowed(name) {
			writeError(w, http.StatusNotFound, "no such icon")
			return
		}

		root, err := os.OpenRoot(filepath.Join(deps.ConfigDir, "icons"))
		if err != nil {
			writeError(w, http.StatusNotFound, "no such icon")
			return
		}
		defer func() { _ = root.Close() }()

		file, err := root.Open(name)
		if err != nil {
			writeError(w, http.StatusNotFound, "no such icon")
			return
		}
		defer func() { _ = file.Close() }()

		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() {
			writeError(w, http.StatusNotFound, "no such icon")
			return
		}

		w.Header().Set("Cache-Control", "public, max-age=3600")
		http.ServeContent(w, r, name, info.ModTime(), file)
	})
}
