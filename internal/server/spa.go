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

const webDistDir = "webdist"

const indexFile = "index.html"

const assetsDir = "assets/"

const (
	webDevEnv        = "WEB_DEV"
	webDevURLEnv     = "WEB_DEV_URL"
	defaultWebDevURL = "http://localhost:5173"
)

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

type spaHandler struct {
	files fs.FS
	index []byte
}

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

		if path.Ext(name) != "" {
			http.NotFound(w, r)
			return
		}
	}

	h.serveIndex(w, r)
}

func (h *spaHandler) serveIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(h.index)
}
