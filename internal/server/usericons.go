package server

import (
	"bytes"
	"encoding/xml"
	"image"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// maxIconUpload bounds an uploaded icon: icons are small, so 1 MB is generous.
const maxIconUpload = 1 << 20

var iconNameChars = regexp.MustCompile(`[^a-z0-9._-]+`)

// iconsDir is where the owner's own icons live (also where /icons/ serves from).
func (s *Server) iconsDir() string { return filepath.Join(s.dataDir, "icons") }

// validIconName is true for a plain file name of a PNG or SVG icon.
func validIconName(name string) bool {
	if name == "" || name != filepath.Base(name) || strings.HasPrefix(name, ".") || iconNameChars.MatchString(name) {
		return false
	}
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".png" || ext == ".svg"
}

// listIcons returns the names of the uploaded icons.
func (s *Server) listIcons(w http.ResponseWriter, _ *http.Request) {
	names := []string{}
	if entries, err := os.ReadDir(s.iconsDir()); err == nil {
		for _, e := range entries {
			if !e.IsDir() && validIconName(e.Name()) {
				names = append(names, e.Name())
			}
		}
	}
	sort.Strings(names)
	writeJSON(w, http.StatusOK, map[string][]string{"icons": names})
}

// isSVG reports whether data is a well-formed XML document whose root is <svg>.
func isSVG(data []byte) bool {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	for {
		tok, err := dec.Token()
		if err != nil {
			return false
		}
		if se, ok := tok.(xml.StartElement); ok {
			return strings.EqualFold(se.Name.Local, "svg")
		}
	}
}

// uploadIcon accepts a multipart "file" field that must really be a PNG or an
// SVG (checked by content, not by extension), and stores it in <data>/icons
// under a safe, unique name. The reply holds the address to use as the icon.
func (s *Server) uploadIcon(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxIconUpload+(64<<10))
	if err := r.ParseMultipartForm(maxIconUpload); err != nil {
		writeError(w, http.StatusBadRequest, "upload too large or malformed (1 MB max)")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "no file provided")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxIconUpload+1))
	if err != nil || len(data) > maxIconUpload {
		writeError(w, http.StatusBadRequest, "the icon is too large (1 MB max)")
		return
	}

	var ext string
	if cfg, format, err := image.DecodeConfig(bytes.NewReader(data)); err == nil && format == "png" {
		if cfg.Width > maxImageDim || cfg.Height > maxImageDim {
			writeError(w, http.StatusBadRequest, "image is too large (8000px max per side)")
			return
		}
		ext = ".png"
	} else if isSVG(data) {
		ext = ".svg"
	} else {
		writeError(w, http.StatusBadRequest, "the file must be a PNG or SVG image")
		return
	}

	stem := strings.TrimSuffix(filepath.Base(header.Filename), filepath.Ext(header.Filename))
	stem = strings.Trim(iconNameChars.ReplaceAllString(strings.ToLower(stem), "-"), "-._")
	if stem == "" {
		stem = "icon"
	}
	if len(stem) > 60 {
		stem = stem[:60]
	}

	dir := s.iconsDir()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save the icon")
		return
	}
	// An existing name is never overwritten (the browser caches icons for a day,
	// and a tile may still use the old one): pick the next free "-2", "-3"...
	name := stem + ext
	for i := 2; ; i++ {
		f, err := os.OpenFile(filepath.Join(dir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
		if err == nil {
			_, werr := f.Write(data)
			if cerr := f.Close(); werr != nil || cerr != nil {
				os.Remove(filepath.Join(dir, name))
				writeError(w, http.StatusInternalServerError, "could not save the icon")
				return
			}
			break
		}
		if !os.IsExist(err) || i > 999 {
			writeError(w, http.StatusInternalServerError, "could not save the icon")
			return
		}
		name = stem + "-" + strconv.Itoa(i) + ext
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": name, "value": "/icons/" + name})
}

// deleteIcon removes an uploaded icon. Tiles that still point at it fall back
// to their initials.
func (s *Server) deleteIcon(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !validIconName(name) {
		writeError(w, http.StatusBadRequest, "invalid icon name")
		return
	}
	if err := os.Remove(filepath.Join(s.iconsDir(), name)); err != nil && !os.IsNotExist(err) {
		writeError(w, http.StatusInternalServerError, "could not delete the icon")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
