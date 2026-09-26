package server

import (
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"strings"
)

// webDistDir is the directory inside the embedded filesystem that holds the
// compiled SPA.
const webDistDir = "webdist"

// indexFile is the SPA entry document served for client-side routes.
const indexFile = "index.html"

// assetsDir is the directory of content-hashed build artifacts. Its contents
// are immutable and can be cached aggressively.
const assetsDir = "assets/"

// Dev-mode configuration. When WEB_DEV=1 the SPA handler proxies every
// non-API request to the Vite dev server, which defaults to
// http://localhost:5173 and can be overridden with WEB_DEV_URL.
const (
	webDevEnv        = "WEB_DEV"
	webDevURLEnv     = "WEB_DEV_URL"
	defaultWebDevURL = "http://localhost:5173"
)

// newSPAHandler builds the handler that serves the embedded SPA. In dev mode
// (WEB_DEV=1) it instead returns a reverse proxy to the Vite dev server.
func newSPAHandler() (http.Handler, error) {
	if os.Getenv(webDevEnv) == "1" {
		return newWebDevProxy()
	}

	dist, err := fs.Sub(webDist, webDistDir)
	if err != nil {
		return nil, fmt.Errorf("spa: embedded %s: %w", webDistDir, err)
	}

	index, err := fs.ReadFile(dist, indexFile)
	if err != nil {
		return nil, fmt.Errorf("spa: embedded %s: %w", indexFile, err)
	}

	return &spaHandler{files: dist, index: index}, nil
}

// newWebDevProxy returns a reverse proxy to the Vite dev server.
func newWebDevProxy() (http.Handler, error) {
	target := os.Getenv(webDevURLEnv)
	if target == "" {
		target = defaultWebDevURL
	}

	u, err := url.Parse(target)
	if err != nil {
		return nil, fmt.Errorf("spa: invalid %s %q: %w", webDevURLEnv, target, err)
	}

	return httputil.NewSingleHostReverseProxy(u), nil
}

// spaHandler serves static files from the embedded build and falls back to
// index.html so client-side routes resolve on a full page load.
type spaHandler struct {
	files fs.FS
	index []byte
}

// ServeHTTP serves an existing file verbatim; anything else without a file
// extension is treated as a client-side route and answered with index.html.
func (h *spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if name != "" {
		if info, err := fs.Stat(h.files, name); err == nil && !info.IsDir() {
			if strings.HasPrefix(name, assetsDir) {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			http.ServeFileFS(w, r, h.files, name)
			return
		}

		// A missing path that looks like a file is a genuine 404 rather than a
		// client-side route.
		if path.Ext(name) != "" {
			http.NotFound(w, r)
			return
		}
	}

	h.serveIndex(w, r)
}

// serveIndex writes the embedded index.html.
func (h *spaHandler) serveIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(h.index)
}
