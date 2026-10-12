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
)

// widgetCache keeps each widget's last reading for half its refresh interval,
// so several open tabs (or a quick reload) share one request to the host
// instead of each making their own.
type widgetCache struct {
	client  *glances.Client
	mu      sync.Mutex
	entries map[string]*widgetEntry
}

type widgetEntry struct {
	mu     sync.Mutex // held while fetching, so concurrent callers wait for one fetch
	key    string     // what the reading was made for; a changed widget is read again
	result glances.Result
	at     time.Time
}

func newWidgetCache() *widgetCache {
	return &widgetCache{client: glances.New(), entries: map[string]*widgetEntry{}}
}

func targetOf(w config.Widget) glances.Target {
	return glances.Target{
		URL:        w.URL,
		Username:   w.Username,
		Password:   w.Password,
		Metrics:    w.Metrics,
		Mounts:     w.Mounts,
		Interfaces: w.Interfaces,
	}
}

// cacheKey changes whenever anything that affects the reading does.
func cacheKey(w config.Widget) string {
	return strings.Join([]string{
		w.URL, w.Username, w.Password,
		strings.Join(w.Metrics, ","), strings.Join(w.Mounts, "\x00"), strings.Join(w.Interfaces, "\x00"),
	}, "\x01")
}

func (c *widgetCache) get(ctx context.Context, w config.Widget) glances.Result {
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
	e.result = c.client.Fetch(ctx, targetOf(w))
	e.key = key
	e.at = time.Now()
	return e.result
}

// all reads every widget at once and forgets the ones that no longer exist.
func (c *widgetCache) all(ctx context.Context, widgets []config.Widget) map[string]glances.Result {
	out := make(map[string]glances.Result, len(widgets))
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

// testWidget checks a connection from the editor before it is saved. It
// answers only with the Glances version or a short reason, never with what the
// host returned. A blank password falls back to the one stored for the widget,
// since the browser never has it.
func (s *Server) testWidget(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID       string `json:"id"`
		URL      string `json:"url"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	probe := config.Widget{ID: "probe", Name: "probe", Kind: "glances", URL: strings.TrimSpace(req.URL), Metrics: []string{}}
	probe.Refresh, probe.Placement, probe.Width, probe.Opacity = 5, "top", 300, 100
	if err := (config.Config{Settings: config.Default().Settings, Widgets: []config.Widget{probe}}).Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "enter a valid address, starting with http:// or https://")
		return
	}
	pw := req.Password
	if pw == "" && req.ID != "" {
		for _, saved := range s.store.Get().Widgets {
			if saved.ID == req.ID && saved.URL == probe.URL && saved.Username == req.Username {
				pw = saved.Password
			}
		}
	}
	version, err := s.widgets.client.Ping(r.Context(), glances.Target{URL: probe.URL, Username: req.Username, Password: pw})
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": version})
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "unreadable request")
		return false
	}
	return true
}
