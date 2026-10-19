// Package server exposes the HTTP API and the web interface.
package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg" // registers the JPEG decoder used by image.DecodeConfig
	_ "image/png"  // registers the PNG decoder used by image.DecodeConfig
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Bagareal/homie/internal/auth"
	"github.com/Bagareal/homie/internal/config"
	"github.com/Bagareal/homie/internal/status"
	"github.com/Bagareal/homie/internal/updates"
)

const maxBody = 1 << 20

// maxUploadBody and maxImageDim bound background image uploads: a generous
// size for a photo, and a sanity cap on pixel dimensions so a crafted file
// with a tiny byte size but an enormous declared resolution can't force the
// browser (or a future server-side decode) into excessive memory use.
const (
	maxUploadBody = 10 << 20
	maxImageDim   = 8000
)

// startTime acts as Last-Modified for the files embedded in the binary.
var startTime = time.Now()

type Server struct {
	store   *config.Store
	checker *status.Checker
	ui      fs.FS
	dataDir string
	version string
	widgets *widgetCache
	icons   *iconCache
	updates *updates.Checker
	// auth is the login. If its file cannot be read the server stays closed
	// (authErr says why) instead of open.
	auth    *auth.Manager
	authErr error
	// bgMu keeps the clean-up of background images from racing an upload.
	bgMu sync.Mutex
}

func New(store *config.Store, checker *status.Checker, ui fs.FS, dataDir, version string) *Server {
	s := &Server{store: store, checker: checker, ui: ui, dataDir: dataDir, version: version, widgets: newWidgetCache(), icons: newIconCache(dataDir), updates: updates.New("")}
	s.auth, s.authErr = auth.Open(dataDir)
	return s
}

// AuthError is why the login could not be set up, if it could not.
func (s *Server) AuthError() error { return s.authErr }

// WithReleasesURL reads the versions from another address than GitHub's (for tests).
func (s *Server) WithReleasesURL(url string) *Server {
	if url != "" {
		s.updates = updates.New(url)
	}
	return s
}

// Handler is the whole site, behind the login.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/auth/status", s.authStatus)
	mux.HandleFunc("POST /api/auth/setup", s.authSetup)
	mux.HandleFunc("POST /api/auth/login", s.authLogin)
	mux.HandleFunc("POST /api/auth/logout", s.authLogout)
	mux.HandleFunc("POST /api/auth/active", s.authActive)
	mux.HandleFunc("POST /api/auth/hidden", s.authHidden)
	mux.HandleFunc("POST /api/auth/password", s.authPassword)
	mux.HandleFunc("GET /api/auth/devices", s.authDevices)
	mux.HandleFunc("DELETE /api/auth/devices/{id}", s.authRevoke)
	mux.HandleFunc("POST /api/auth/devices/others/revoke", s.authRevokeOthers)
	mux.HandleFunc("POST /api/auth/passkey/register/begin", s.passkeyRegisterBegin)
	mux.HandleFunc("POST /api/auth/passkey/register/finish", s.passkeyRegisterFinish)
	mux.HandleFunc("POST /api/auth/passkey/assert/begin", s.passkeyAssertBegin)
	mux.HandleFunc("POST /api/auth/passkey/assert/finish", s.passkeyAssertFinish)
	mux.HandleFunc("DELETE /api/auth/passkeys/{id}", s.passkeyRemove)
	mux.HandleFunc("POST /api/auth/lock/off", s.lockOff)
	mux.HandleFunc("POST /api/auth/lock/after", s.lockAfter)
	mux.HandleFunc("GET /api/config", s.getConfig)
	mux.HandleFunc("PUT /api/config", s.putConfig)
	mux.HandleFunc("GET /api/status", s.getStatus)
	mux.HandleFunc("GET /api/widgets", s.getWidgets)
	mux.HandleFunc("POST /api/widgets/test", s.testWidget)
	mux.HandleFunc("POST /api/widgets/{id}/command", s.widgetCommand)
	mux.HandleFunc("GET /api/version", s.getVersion)
	mux.HandleFunc("GET /api/releases", s.getReleases)
	mux.HandleFunc("GET /icons/{name}", s.icon)
	mux.HandleFunc("GET /cdn/{collection}/{dir}/{file}", s.cdnIcon)
	mux.HandleFunc("POST /api/background", s.uploadBackground)
	mux.HandleFunc("DELETE /api/background", s.deleteBackground)
	mux.HandleFunc("GET /backgrounds/{name}", s.background)
	mux.HandleFunc("GET /", s.static)
	return s.guard(mux)
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
	writeJSON(w, http.StatusOK, s.store.Get().Redacted())
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
	s.gcBackgrounds() // a theme may have been deleted
	writeJSON(w, http.StatusOK, s.store.Get().Redacted())
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
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeFile(w, r, filepath.Join(s.dataDir, "icons", name))
}

// uploadBackground accepts a multipart "file" field, validated as a real
// JPEG or PNG (not just by extension or Content-Type header) before it's
// written to disk, and records it as the dashboard's background image.
func (s *Server) uploadBackground(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBody)
	if err := r.ParseMultipartForm(maxUploadBody); err != nil {
		writeError(w, http.StatusBadRequest, "upload too large or malformed (10 MB max)")
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "no file provided")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not read the uploaded file")
		return
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "jpeg" && format != "png") {
		writeError(w, http.StatusBadRequest, "the file must be a JPEG or PNG image")
		return
	}
	if cfg.Width > maxImageDim || cfg.Height > maxImageDim {
		writeError(w, http.StatusBadRequest, "image is too large (8000px max per side)")
		return
	}

	s.bgMu.Lock()
	defer s.bgMu.Unlock()
	ext := ".jpg"
	if format == "png" {
		ext = ".png"
	}
	name := fmt.Sprintf("bg-%d%s", time.Now().UnixNano(), ext)
	dir := filepath.Join(s.dataDir, "backgrounds")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save the image")
		return
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o640); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save the image")
		return
	}

	c := s.store.Get()
	c.Settings.BackgroundImage = name
	if err := s.store.Set(c); err != nil {
		os.Remove(filepath.Join(dir, name))
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.gcBackgroundsLocked() // the previous image goes unless a theme still uses it
	writeJSON(w, http.StatusOK, s.store.Get().Redacted())
}

// gcBackgrounds deletes the image files nothing refers to any more: the ones in
// <data>/backgrounds that are neither the current background nor used by a
// saved theme.
func (s *Server) gcBackgrounds() {
	s.bgMu.Lock()
	defer s.bgMu.Unlock()
	s.gcBackgroundsLocked()
}

func (s *Server) gcBackgroundsLocked() {
	c := s.store.Get()
	keep := map[string]bool{c.Settings.BackgroundImage: true}
	for _, t := range c.Themes {
		keep[t.Appearance.BackgroundImage] = true
	}
	dir := filepath.Join(s.dataDir, "backgrounds")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() && !keep[e.Name()] {
			os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

func (s *Server) deleteBackground(w http.ResponseWriter, _ *http.Request) {
	s.bgMu.Lock()
	defer s.bgMu.Unlock()
	c := s.store.Get()
	old := c.Settings.BackgroundImage
	if old == "" {
		writeJSON(w, http.StatusOK, c.Redacted())
		return
	}
	c.Settings.BackgroundImage = ""
	if err := s.store.Set(c); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.gcBackgroundsLocked()
	writeJSON(w, http.StatusOK, s.store.Get().Redacted())
}

// background serves the uploaded background image from <data>/backgrounds.
func (s *Server) background(w http.ResponseWriter, r *http.Request) {
	name := filepath.Base(r.PathValue("name"))
	if name == "." || name == "/" || strings.HasPrefix(name, ".") {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	http.ServeFile(w, r, filepath.Join(s.dataDir, "backgrounds", name))
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
	if strings.HasSuffix(p, ".webmanifest") {
		// Not in every system's MIME table, so set it explicitly.
		w.Header().Set("Content-Type", "application/manifest+json")
	}
	if strings.HasPrefix(p, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	http.ServeContent(w, r, p, startTime, bytes.NewReader(data))
}
