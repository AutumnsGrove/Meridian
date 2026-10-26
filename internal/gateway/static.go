package gateway

import (
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/autumnsgrove/meridian/web"
)

// static serves the embedded app: a real file when the path names one, the app shell for any other
// page (the SPA routes itself), and a JSON 404 for an unknown API path.
func static(deps Deps) http.Handler {
	root := deps.Static
	if root == nil {
		root = web.FS()
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isAPIPath(r.URL.Path) {
			writeError(w, http.StatusNotFound, "no such endpoint")
			return
		}
		name, found := appFile(root, r.URL.Path)
		if !found {
			serveShell(w, r, root)
			return
		}
		w.Header().Set("Cache-Control", cacheControlFor(name))
		// name came out of appFile, which cleaned it and found it with fs.Stat inside the app root.
		http.ServeFileFS(w, r, root, name) //nolint:gosec // the name is a cleaned path that exists in root

	})
}

// appFile resolves a request path to a file in the app, if the app has one. A directory, a missing
// file and a path that tries to climb out all report false, so the caller falls back to the shell.
func appFile(root fs.FS, urlPath string) (string, bool) {
	name := strings.TrimPrefix(path.Clean("/"+urlPath), "/")
	if name == "" {
		return "index.html", true
	}
	info, err := fs.Stat(root, name)
	if err != nil || info.IsDir() {
		return "", false
	}
	return name, true
}

// serveShell answers with the app's index.html, which is what makes /control work without a server
// route. The shell is never cached: it names the asset build.
func serveShell(w http.ResponseWriter, r *http.Request, root fs.FS) {
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFileFS(w, r, root, "index.html")
}

// cacheControlFor gives a fingerprinted asset a year and everything else no cache at all. The shell
// names the build, so a stale copy would pin an old one.
func cacheControlFor(name string) string {
	if strings.HasPrefix(name, "_app/immutable/") {
		return "public, max-age=31536000, immutable"
	}
	return "no-cache"
}
