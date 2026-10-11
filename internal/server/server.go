// Package server exposes the HTTP API and the web interface.
package server

import (
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/Bagareal/homie/internal/config"
	"github.com/Bagareal/homie/internal/status"
)

const maxBody = 1 << 20

// startTime acts as Last-Modified for the files embedded in the binary.
var startTime = time.Now()

type Server struct {
	store   *config.Store
	checker *status.Checker
	ui      fs.FS
	dataDir string
	version string
}

func New(store *config.Store, checker *status.Checker, ui fs.FS, dataDir, version string) *Server {
	return &Server{store: store, checker: checker, ui: ui, dataDir: dataDir, version: version}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/config", s.getConfig)
	mux.HandleFunc("PUT /api/config", s.putConfig)
	mux.HandleFunc("GET /api/status", s.getStatus)
	mux.HandleFunc("GET /api/version", s.getVersion)
	mux.HandleFunc("GET /icons/{name}", s.icon)
	mux.HandleFunc("GET /", s.static)
	return mux
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func (s *Server) getConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.store.Get())
}

func (s *Server) putConfig(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		writeError(w, http.StatusBadRequest, "unreadable configuration: invalid JSON or too large")
		return
	}
	// UnmarshalConfig also accepts an export taken from an earlier Homie
	// version (a top-level "groups" array with widget/color fields no
	// longer in use, or the brief v0.6.x "blocks" shape) so an old
	// exported file can still be imported without silently emptying the
	// dashboard.
	c, err := config.UnmarshalConfig(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "unreadable configuration: invalid JSON or too large")
		return
	}
	if err := s.store.Set(c); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.checker.Wake()
	writeJSON(w, http.StatusOK, s.store.Get())
}

func (s *Server) getStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.checker.Results())
}

func (s *Server) getVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"version": s.version})
}

// icon serves local icons placed in <data>/icons.
func (s *Server) icon(w http.ResponseWriter, r *http.Request) {
	name := filepath.Base(r.PathValue("name"))
	if name == "." || name == "/" || strings.HasPrefix(name, ".") {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, r, filepath.Join(s.dataDir, "icons", name))
}

// static serves the interface; unknown paths get index.html.
func (s *Server) static(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if p == "" {
		p = "index.html"
	}
	if _, err := fs.Stat(s.ui, p); err != nil {
		p = "index.html"
	}
	data, err := fs.ReadFile(s.ui, p)
	if err != nil {
		http.Error(w, "Interface not built: run 'make ui' before building.", http.StatusInternalServerError)
		return
	}
	if strings.HasPrefix(p, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	http.ServeContent(w, r, p, startTime, bytes.NewReader(data))
}
