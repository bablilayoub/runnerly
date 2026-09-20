// Package web serves the Runnerly dashboard.
//
// The built assets are embedded, so a release is one binary with the UI
// inside it. A Go build does not require Node: when the dashboard has not
// been built, `dist` holds nothing but a placeholder and the server says so
// instead of serving a blank page.
package web

import (
	"embed"
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// all: is needed so the .gitkeep that holds the empty directory in git is
// embedded too, which is what lets this compile before a dashboard build.
//
//go:embed all:dist
var embedded embed.FS

// notBuiltPage is served when the binary was built without the dashboard.
const notBuiltPage = `<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<title>Runnerly</title>
<meta name="viewport" content="width=device-width,initial-scale=1">
<style>
  :root { color-scheme: light dark }
  body { font: 15px/1.6 ui-sans-serif, system-ui, sans-serif; margin: 0;
         display: grid; place-items: center; min-height: 100vh; padding: 2rem }
  main { max-width: 34rem }
  h1 { font-family: ui-monospace, monospace; font-size: 1.1rem; margin: 0 0 .25rem }
  p { margin: .75rem 0; opacity: .8 }
  code { font-family: ui-monospace, monospace; font-size: .9em }
</style></head>
<body><main>
<h1>runnerly</h1>
<p>This server was built without the dashboard, so there is no interface to show.
The API is unaffected and agents work normally.</p>
<p>To include it, run <code>make web</code> before <code>make build</code>, or use the
release binaries.</p>
</main></body></html>
`

// Available reports whether the dashboard was built into this binary.
func Available() bool {
	_, err := fs.Stat(assets(), "index.html")
	return err == nil
}

// assets returns the built dashboard rooted at its own directory.
func assets() fs.FS {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		// The directory is embedded at compile time, so this cannot happen
		// unless the embed directive above was changed.
		panic("runnerly: dashboard assets are not embedded: " + err.Error())
	}
	return sub
}

// Handler serves the dashboard.
//
// It is a single-page app, so any path that is not a real file falls back to
// index.html and the router in the browser decides what to show. Paths that
// look like API calls are never rewritten: a typo in an API path should be a
// 404 from the API, not a page of HTML that a client cannot parse.
func Handler() http.Handler {
	if !Available() {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(notBuiltPage))
		})
	}

	files := assets()
	fileServer := http.FileServer(http.FS(files))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" || name == "." {
			name = "index.html"
		}

		if _, err := fs.Stat(files, name); err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				http.Error(w, "could not read the dashboard", http.StatusInternalServerError)
				return
			}
			serveIndex(w, r, files)
			return
		}

		// Hashed asset filenames change whenever their contents do, so they
		// can be cached hard. index.html must not be, or a browser keeps
		// loading an old build's script tags after an upgrade.
		if strings.HasPrefix(name, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		fileServer.ServeHTTP(w, r)
	})
}

// serveIndex returns the SPA shell for a path the router will handle.
func serveIndex(w http.ResponseWriter, r *http.Request, files fs.FS) {
	body, err := fs.ReadFile(files, "index.html")
	if err != nil {
		http.Error(w, "could not read the dashboard", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	// 200, not 404: the browser's router decides whether the path exists,
	// and it cannot do that if this returns an error status.
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}
