package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Bagareal/homie/internal/config"
	"github.com/Bagareal/homie/internal/glances"
	"github.com/Bagareal/homie/internal/nut"
	"github.com/Bagareal/homie/internal/truenas"
)

// widgetCache keeps each widget's last reading for half its refresh interval,
// so several open tabs (or a quick reload) share one request to the host
// instead of each making their own.
type widgetCache struct {
	glances *glances.Client
	truenas *truenas.Client
	nut     *nut.Client
	mu      sync.Mutex
	entries map[string]*widgetEntry
}

type widgetEntry struct {
	mu     sync.Mutex // held while fetching, so concurrent callers wait for one fetch
	key    string     // what the reading was made for; a changed widget is read again
	result any        // glances, truenas or nut Result: the same JSON envelope, different data
	at     time.Time
}

func newWidgetCache() *widgetCache {
	return &widgetCache{glances: glances.New(), truenas: truenas.New(), nut: nut.New(), entries: map[string]*widgetEntry{}}
}

// metricsOf lists which groups of values the rows of a widget need, so a
// widget that only shows the CPU never asks the host for its disks.
func metricsOf(kind string, rows []config.Row) []string {
	if kind == "nut" {
		return nil // a UPS is read whole, in one short conversation
	}
	need := map[string]bool{}
	for _, r := range rows {
		switch {
		case kind == "truenas":
			switch r.Type {
			case "uptime", "load", "version":
				need["info"] = true
			case "mem":
				need["mem"], need["info"] = true, true // the installed memory comes from system.info
			case "pool":
				need["pools"] = true
			case "drive":
				need["drives"] = true
			case "cpu":
				need["cpu"] = r.Load || need["cpu"]
				need["cputemp"] = r.Temp || need["cputemp"]
			default: // alerts, apps
				need[r.Type] = true
			}
		case r.Type == "cpu":
			need["cpu"] = r.Load || need["cpu"]
			need["sensors"] = r.Temp || need["sensors"]
		case r.Type == "temp":
			need["sensors"] = true
		case r.Type == "disk":
			need["disks"] = true
		default: // mem, swap, load, uptime, network
			need[r.Type] = true
		}
	}
	all := glances.Metrics
	if kind == "truenas" {
		all = truenas.Metrics
	}
	var out []string
	for _, m := range all { // a stable order
		if need[m] {
			out = append(out, m)
		}
	}
	return out
}

// fetch reads a widget's host once, whatever its kind.
func (c *widgetCache) fetch(ctx context.Context, w config.Widget) any {
	metrics := metricsOf(w.Kind, w.Rows)
	if w.Kind == "nut" {
		return c.nut.Fetch(ctx, nut.Target{URL: w.URL})
	}
	if w.Kind == "truenas" {
		return c.truenas.Fetch(ctx, truenas.Target{URL: w.URL, Username: w.Username, APIKey: w.Password, Metrics: metrics})
	}
	return c.glances.Fetch(ctx, glances.Target{URL: w.URL, Username: w.Username, Password: w.Password, Metrics: metrics})
}

// cacheKey changes whenever anything that affects the reading does.
func cacheKey(w config.Widget) string {
	return strings.Join([]string{w.Kind, w.URL, w.Username, w.Password, strings.Join(metricsOf(w.Kind, w.Rows), ",")}, "\x01")
}

func (c *widgetCache) get(ctx context.Context, w config.Widget) any {
	c.mu.Lock()
	e := c.entries[w.ID]
	if e == nil {
		e = &widgetEntry{}
		c.entries[w.ID] = e
	}
	c.mu.Unlock()

	e.mu.Lock()
	defer e.mu.Unlock()
	key := cacheKey(w)
	if e.key == key && time.Since(e.at) < time.Duration(w.Refresh)*time.Second/2 {
		return e.result
	}
	e.result = c.fetch(ctx, w)
	e.key = key
	e.at = time.Now()
	return e.result
}

// all reads every widget at once and forgets the ones that no longer exist.
func (c *widgetCache) all(ctx context.Context, widgets []config.Widget) map[string]any {
	out := make(map[string]any, len(widgets))
	var mu sync.Mutex
	var wg sync.WaitGroup
	live := make(map[string]bool, len(widgets))
	for _, w := range widgets {
		live[w.ID] = true
		wg.Add(1)
		go func(w config.Widget) {
			defer wg.Done()
			r := c.get(ctx, w)
			mu.Lock()
			out[w.ID] = r
			mu.Unlock()
		}(w)
	}
	wg.Wait()

	c.mu.Lock()
	for id := range c.entries {
		if !live[id] {
			delete(c.entries, id)
		}
	}
	c.mu.Unlock()
	return out
}

func (s *Server) getWidgets(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.widgets.all(r.Context(), s.store.Get().Widgets))
}

// testWidget checks a connection from the editor before it is saved and reads
// everything the host offers, so the editor can list its disks, sensors, pools
// and network cards. A blank password falls back to the one stored for the
// widget, since the browser never has it.
func (s *Server) testWidget(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID       string `json:"id"`
		Kind     string `json:"kind"`
		URL      string `json:"url"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if req.Kind == "" {
		req.Kind = "glances"
	}
	probe := config.Widget{ID: "probe", Name: "probe", Kind: req.Kind, URL: strings.TrimSpace(req.URL), Rows: []config.Row{}}
	probe.Refresh, probe.Cols, probe.HeightUnits = 5, 1, 1
	if err := (config.Config{Settings: config.Default().Settings, Widgets: []config.Widget{probe}}).Validate(); err != nil {
		msg := "enter a valid address, starting with http:// or https://"
		if req.Kind == "truenas" {
			msg = "enter a valid address, starting with https:// (TrueNAS revokes an API key sent over plain http)"
		}
		if req.Kind == "nut" {
			msg = "enter a valid address, starting with nut:// (for example nut://192.168.1.10)"
		}
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	pw := req.Password
	if pw == "" && req.ID != "" {
		// Only for the same address and username: otherwise anyone who can open
		// Homie could send the stored password to a server of their own.
		for _, saved := range s.store.Get().Widgets {
			if saved.ID == req.ID && saved.Kind == req.Kind && saved.URL == probe.URL && saved.Username == req.Username {
				pw = saved.Password
			}
		}
	}

	if req.Kind == "nut" {
		t := nut.Target{URL: probe.URL}
		list, err := s.widgets.nut.Ping(r.Context(), t)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		res := s.widgets.nut.Fetch(r.Context(), t)
		if !res.Online {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": res.Error})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": strings.Join(list, ", "), "data": res.Data})
		return
	}
	if req.Kind == "truenas" {
		t := truenas.Target{URL: probe.URL, Username: req.Username, APIKey: pw}
		version, err := s.widgets.truenas.Ping(r.Context(), t)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		t.Metrics = truenas.Metrics
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": version, "data": s.widgets.truenas.Fetch(r.Context(), t).Data})
		return
	}
	t := glances.Target{URL: probe.URL, Username: req.Username, Password: pw}
	version, err := s.widgets.glances.Ping(r.Context(), t)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	t.Metrics = glances.Metrics
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": version, "data": s.widgets.glances.Fetch(r.Context(), t).Data})
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "unreadable request")
		return false
	}
	return true
}
