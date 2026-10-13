package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Bagareal/homie/internal/config"
	"github.com/Bagareal/homie/internal/glances"
	"github.com/Bagareal/homie/internal/nut"
	"github.com/Bagareal/homie/internal/proxmox"
	"github.com/Bagareal/homie/internal/truenas"
)

// widgetCache keeps each widget's last reading for half its refresh interval,
// so several open tabs (or a quick reload) share one request to the host
// instead of each making their own.
type widgetCache struct {
	glances *glances.Client
	truenas *truenas.Client
	nut     *nut.Client
	proxmox *proxmox.Client
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
	return &widgetCache{glances: glances.New(), truenas: truenas.New(), nut: nut.New(), proxmox: proxmox.New(), entries: map[string]*widgetEntry{}}
}

// metricsOf lists which groups of values the rows of a widget need, so a
// widget that only shows the CPU never asks the host for its disks.
func metricsOf(kind string, rows []config.Row) []string {
	if kind == "nut" {
		return nil // a UPS is read whole, in one short conversation
	}
	if kind == "proxmox" {
		// The cluster resources decide whether the system counts as reachable, so
		// they are always read; the rest only when a row needs it.
		need := map[string]bool{"resources": true}
		for _, r := range rows {
			if r.Source != "" {
				continue
			}
			switch r.Type {
			case "version":
				need["version"] = true
			case "quorum":
				need["cluster"] = true
			case "backup", "tasks":
				need["tasks"] = true
			}
		}
		var out []string
		for _, m := range proxmox.Metrics {
			if need[m] {
				out = append(out, m)
			}
		}
		return out
	}
	need := map[string]bool{}
	for _, r := range rows {
		if r.Source != "" {
			continue // read from the extra source, see glancesNeeds
		}
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
	return c.withExtra(ctx, w, c.fetchMain(ctx, w))
}

func (c *widgetCache) fetchMain(ctx context.Context, w config.Widget) any {
	metrics := metricsOf(w.Kind, w.Rows)
	switch w.Kind {
	case "nut":
		return c.nut.Fetch(ctx, nut.Target{URL: w.URL})
	case "truenas":
		return c.truenas.Fetch(ctx, truenas.Target{URL: w.URL, Username: w.Username, APIKey: w.Password, Metrics: metrics})
	case "proxmox":
		return c.proxmox.Fetch(ctx, proxmox.Target{URL: w.URL, TokenID: w.Username, Secret: w.Password, Metrics: metrics})
	}
	return c.glances.Fetch(ctx, glances.Target{URL: w.URL, Username: w.Username, Password: w.Password, Metrics: metrics})
}

// glancesNeeds lists what the rows of the extra Glances source need from the
// Glances server of one node ("" is the general address).
func glancesNeeds(rows []config.Row, node string) []string {
	var mine []config.Row
	for _, r := range rows {
		if r.Source == "glances" && r.Node == node {
			r.Source = ""
			mine = append(mine, r)
		}
	}
	return metricsOf("glances", mine)
}

// withExtra adds what the widget's extra Glances source reads to the main
// reading: data.glances, and data.glancesNodes[node] for a cluster. A failure
// is explained in notes, and the rows that depend on it show n/a.
func (c *widgetCache) withExtra(ctx context.Context, w config.Widget, main any) any {
	g := w.Glances
	if g == nil {
		return main
	}
	type job struct {
		node, url string
	}
	var jobs []job
	if g.URL != "" {
		jobs = append(jobs, job{"", g.URL})
	}
	for node, u := range g.Nodes {
		jobs = append(jobs, job{node, u})
	}
	results := make([]glances.Result, len(jobs))
	var wg sync.WaitGroup
	for i, j := range jobs {
		metrics := glancesNeeds(w.Rows, j.node)
		if len(metrics) == 0 {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = c.glances.Fetch(ctx, glances.Target{URL: j.url, Username: g.Username, Password: g.Password, Metrics: metrics})
		}()
	}
	wg.Wait()

	out := map[string]any{}
	raw, err := json.Marshal(main)
	if err != nil || json.Unmarshal(raw, &out) != nil {
		return main
	}
	data, _ := out["data"].(map[string]any)
	if data == nil {
		data = map[string]any{}
		out["data"] = data
	}
	notes, _ := out["notes"].(map[string]any)
	if notes == nil {
		notes = map[string]any{}
	}
	nodes := map[string]any{}
	for i, j := range jobs {
		res := results[i]
		key := "glances"
		if j.node != "" {
			key = "glances:" + j.node
		}
		if res.UpdatedAt.IsZero() {
			continue // no row uses it
		}
		if !res.Online {
			notes[key] = "Glances: " + res.Error
			continue
		}
		if j.node == "" {
			data["glances"] = res.Data
		} else {
			nodes[j.node] = res.Data
		}
	}
	if len(nodes) > 0 {
		data["glancesNodes"] = nodes
	}
	if len(notes) > 0 {
		out["notes"] = notes
	}
	return out
}

// cacheKey changes whenever anything that affects the reading does.
func cacheKey(w config.Widget) string {
	parts := []string{w.Kind, w.URL, w.Username, w.Password, strings.Join(metricsOf(w.Kind, w.Rows), ",")}
	if g := w.Glances; g != nil {
		parts = append(parts, g.URL, g.Username, g.Password, strings.Join(glancesNeeds(w.Rows, ""), ","))
		nodes := make([]string, 0, len(g.Nodes))
		for n := range g.Nodes {
			nodes = append(nodes, n)
		}
		sort.Strings(nodes)
		for _, n := range nodes {
			parts = append(parts, n, g.Nodes[n], strings.Join(glancesNeeds(w.Rows, n), ","))
		}
	}
	return strings.Join(parts, "\x01")
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
// everything the host offers, so the editor can list its disks, sensors, pools,
// guests and network cards. A blank password falls back to the one stored for
// the widget, since the browser never has it.
func (s *Server) testWidget(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID       string `json:"id"`
		Kind     string `json:"kind"`
		URL      string `json:"url"`
		Username string `json:"username"`
		Password string `json:"password"`
		// Glances is the extra source of the widget, tested along with the main one.
		Glances *struct {
			URL      string            `json:"url"`
			Username string            `json:"username"`
			Password string            `json:"password"`
			Nodes    map[string]string `json:"nodes"`
		} `json:"glances"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if req.Kind == "" {
		req.Kind = "glances"
	}
	probe := config.Widget{ID: "probe", Name: "probe", Kind: req.Kind, URL: strings.TrimSpace(req.URL), Rows: []config.Row{}}
	probe.Refresh, probe.Cols, probe.HeightUnits = 5, 1, 1
	if req.Kind == "proxmox" {
		probe.Username = "probe@pam!probe" // the token id is checked below, with its own message
	}
	if err := (config.Config{Settings: config.Default().Settings, Widgets: []config.Widget{probe}}).Validate(); err != nil {
		msg := "enter a valid address, starting with http:// or https://"
		switch req.Kind {
		case "truenas":
			msg = "enter a valid address, starting with https:// (TrueNAS revokes an API key sent over plain http)"
		case "nut":
			msg = "enter a valid address, starting with nut:// (for example nut://192.168.1.10)"
		case "proxmox":
			msg = "enter a valid address, starting with https:// (for example https://192.168.1.10:8006)"
		}
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	pw := req.Password
	var stored *config.Widget
	if req.ID != "" {
		for _, saved := range s.store.Get().Widgets {
			if saved.ID == req.ID {
				saved := saved
				stored = &saved
			}
		}
	}
	// Only for the same address and username: otherwise anyone who can open
	// Homie could send the stored password to a server of their own.
	if pw == "" && stored != nil && stored.Kind == req.Kind && stored.URL == probe.URL && stored.Username == req.Username {
		pw = stored.Password
	}

	reply := func(version string, data any) {
		out := map[string]any{"ok": true, "version": version, "data": data}
		if req.Glances != nil && req.Kind != "glances" {
			out["extra"] = s.testGlances(r.Context(), stored, req.Glances.URL, req.Glances.Username, req.Glances.Password, req.Glances.Nodes)
		}
		writeJSON(w, http.StatusOK, out)
	}
	fail := func(err error) { writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()}) }

	switch req.Kind {
	case "nut":
		t := nut.Target{URL: probe.URL}
		list, err := s.widgets.nut.Ping(r.Context(), t)
		if err != nil {
			fail(err)
			return
		}
		res := s.widgets.nut.Fetch(r.Context(), t)
		if !res.Online {
			fail(errors.New(res.Error))
			return
		}
		reply(strings.Join(list, ", "), res.Data)
	case "truenas":
		t := truenas.Target{URL: probe.URL, Username: req.Username, APIKey: pw}
		version, err := s.widgets.truenas.Ping(r.Context(), t)
		if err != nil {
			fail(err)
			return
		}
		t.Metrics = truenas.Metrics
		reply(version, s.widgets.truenas.Fetch(r.Context(), t).Data)
	case "proxmox":
		if !strings.Contains(req.Username, "@") || !strings.Contains(req.Username, "!") {
			fail(errors.New("the API token id looks like user@realm!name, for example root@pam!homie"))
			return
		}
		t := proxmox.Target{URL: probe.URL, TokenID: req.Username, Secret: pw}
		version, err := s.widgets.proxmox.Ping(r.Context(), t)
		if err != nil {
			fail(err)
			return
		}
		t.Metrics = proxmox.Metrics
		res := s.widgets.proxmox.Fetch(r.Context(), t)
		if !res.Online {
			fail(errors.New(res.Error))
			return
		}
		reply("Proxmox VE "+version, res.Data)
	default:
		t := glances.Target{URL: probe.URL, Username: req.Username, Password: pw}
		version, err := s.widgets.glances.Ping(r.Context(), t)
		if err != nil {
			fail(err)
			return
		}
		t.Metrics = glances.Metrics
		reply(version, s.widgets.glances.Fetch(r.Context(), t).Data)
	}
}

// testGlances reads the extra Glances source (the general address and one per
// node) for the editor. A blank password falls back to the stored one, but only
// while every address was already one of the stored ones.
func (s *Server) testGlances(ctx context.Context, stored *config.Widget, rawURL, user, pw string, nodes map[string]string) map[string]any {
	rawURL = strings.TrimSpace(rawURL)
	if pw == "" && stored != nil && stored.Glances != nil && stored.Glances.Username == user {
		known := map[string]bool{stored.Glances.URL: true}
		for _, u := range stored.Glances.Nodes {
			known[u] = true
		}
		ok := rawURL == "" || known[rawURL]
		for _, u := range nodes {
			ok = ok && known[strings.TrimSpace(u)]
		}
		if ok {
			pw = stored.Glances.Password
		}
	}
	one := func(u string) map[string]any {
		u = strings.TrimSpace(u)
		check := config.Config{Settings: config.Default().Settings, Widgets: []config.Widget{{
			ID: "probe", Name: "probe", Kind: "truenas", URL: "https://probe", Rows: []config.Row{}, Refresh: 5, Cols: 1, HeightUnits: 1,
			Glances: &config.Extra{URL: u},
		}}}
		if err := check.Validate(); err != nil {
			return map[string]any{"ok": false, "error": "enter a valid address, starting with http:// or https://"}
		}
		t := glances.Target{URL: u, Username: user, Password: pw}
		version, err := s.widgets.glances.Ping(ctx, t)
		if err != nil {
			return map[string]any{"ok": false, "error": err.Error()}
		}
		t.Metrics = glances.Metrics
		return map[string]any{"ok": true, "version": "Glances " + version, "data": s.widgets.glances.Fetch(ctx, t).Data}
	}
	out := map[string]any{}
	if rawURL != "" {
		out["glances"] = one(rawURL)
	}
	if len(nodes) > 0 {
		perNode := map[string]any{}
		for node, u := range nodes {
			perNode[node] = one(u)
		}
		out["nodes"] = perNode
	}
	return out
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "unreadable request")
		return false
	}
	return true
}
