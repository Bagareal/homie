package server

import (
	"net/http"
	"time"

	"github.com/Bagareal/homie/internal/updates"
)

// getReleases tells the dashboard which versions exist, so it can announce a new
// one and show what changed. It reads GitHub from the server, at most every few
// hours, and not at all when the user has turned the check off or this is a
// development build.
func (s *Server) getReleases(w http.ResponseWriter, r *http.Request) {
	reply := struct {
		Current   string            `json:"current"`
		Enabled   bool              `json:"enabled"`
		CheckedAt *time.Time        `json:"checkedAt,omitempty"`
		Releases  []updates.Release `json:"releases"`
		Error     string            `json:"error,omitempty"`
	}{Current: s.version, Releases: []updates.Release{}}
	if s.store.Get().Settings.NoUpdateCheck || updates.Compare(s.version, "v0.0.0") <= 0 {
		writeJSON(w, http.StatusOK, reply)
		return
	}
	reply.Enabled = true
	rels, at, errMsg := s.updates.Get(r.Context(), r.URL.Query().Get("refresh") == "1")
	if rels != nil {
		reply.Releases = rels
	}
	if !at.IsZero() {
		reply.CheckedAt = &at
	}
	reply.Error = errMsg
	writeJSON(w, http.StatusOK, reply)
}
