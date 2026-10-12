package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Bagareal/homie/internal/config"
	"github.com/Bagareal/homie/internal/status"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	store, err := config.Open(dir)
	if err != nil {
		t.Fatalf("config.Open: %v", err)
	}
	checker := status.New(store)
	return New(store, checker, nil, dir, "test")
}

func doJSON(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		r = httptest.NewRequest(method, path, bytes.NewReader(b))
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestGetConfigReturnsDefault(t *testing.T) {
	s := newTestServer(t)
	w := doJSON(t, s.Handler(), "GET", "/api/config", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d, body %s", w.Code, w.Body.String())
	}
}

func TestPutConfigSavesAndReturnsIt(t *testing.T) {
	s := newTestServer(t)
	h := s.Handler()
	body := map[string]any{
		"title":    "My dashboard",
		"settings": map[string]any{"theme": "auto", "density": "comfortable", "checkInterval": 60, "openInNewTab": true},
		"groups": []map[string]any{{
			"id": "g1", "name": "Services",
			"items": []map[string]any{{"id": "i1", "name": "Site", "url": "http://example.com"}},
		}},
	}
	w := doJSON(t, h, "PUT", "/api/config", body)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d, body %s", w.Code, w.Body.String())
	}
	if s.store.Get().Title != "My dashboard" {
		t.Fatalf("expected the title to be saved, got %q", s.store.Get().Title)
	}
}

func TestPutConfigMigratesLegacyGroupsShapeWithWidgets(t *testing.T) {
	s := newTestServer(t)
	h := s.Handler()

	legacy := map[string]any{
		"title":    "Old export",
		"settings": map[string]any{"theme": "auto", "density": "comfortable", "checkInterval": 60, "openInNewTab": true},
		"groups": []map[string]any{{
			"id": "g1", "name": "Group", "color": "#e9b949",
			"items": []map[string]any{{
				"id": "i1", "name": "Site", "url": "http://example.com",
				"widget": map[string]any{"integrationId": "x", "kind": "customapi", "size": "1x1"},
			}},
		}},
	}
	w := doJSON(t, h, "PUT", "/api/config", legacy)
	if w.Code != http.StatusOK {
		t.Fatalf("put legacy config: got %d, body %s", w.Code, w.Body.String())
	}

	got := s.store.Get()
	if len(got.Groups) != 1 || got.Groups[0].ID != "g1" || len(got.Groups[0].Items) != 1 {
		t.Fatalf("expected the legacy group to survive, got %+v", got.Groups)
	}
}

func TestPutConfigMigratesV06BlocksShape(t *testing.T) {
	s := newTestServer(t)
	h := s.Handler()

	legacy := map[string]any{
		"title":    "v0.6.x export",
		"settings": map[string]any{"theme": "auto", "density": "comfortable", "checkInterval": 60, "openInNewTab": true},
		"blocks": []map[string]any{{
			"type": "item",
			"item": map[string]any{
				"id": "px", "name": "Proxmox", "url": "https://pve.local:8006",
				"widget": map[string]any{"integrationId": "x", "kind": "proxmoxve", "size": "2x2"},
			},
		}},
	}
	w := doJSON(t, h, "PUT", "/api/config", legacy)
	if w.Code != http.StatusOK {
		t.Fatalf("put v0.6.x config: got %d, body %s", w.Code, w.Body.String())
	}

	got := s.store.Get()
	if len(got.Groups) != 1 || got.Groups[0].ID != "other" || len(got.Groups[0].Items) != 1 || got.Groups[0].Items[0].Name != "Proxmox" {
		t.Fatalf("expected the floating item to land in a synthetic Other group, got %+v", got.Groups)
	}
}

func putWidget(t *testing.T, s *Server, glancesURL, password string) {
	t.Helper()
	cfg := s.store.Get().Redacted()
	cfg.Widgets = []config.Widget{{
		ID: "w1", Name: "Proxmox", URL: glancesURL, Username: "glances", Password: password,
		Rows: []config.Row{{ID: "r1", Type: "cpu", Load: true}}, Refresh: 5, Width: 300, Opacity: 100,
	}}
	if w := doJSON(t, s.Handler(), "PUT", "/api/config", cfg); w.Code != http.StatusOK {
		t.Fatalf("put widget: %d %s", w.Code, w.Body.String())
	}
}

func fakeGlances(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, p, _ := r.BasicAuth(); p != "s3cret" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/4/status":
			_, _ = w.Write([]byte(`{"version":"4.5.8"}`))
		case "/api/4/cpu":
			_, _ = w.Write([]byte(`{"total": 21.5}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestWidgetPasswordNeverReachesTheBrowser(t *testing.T) {
	s := newTestServer(t)
	g := fakeGlances(t)
	putWidget(t, s, g.URL, "s3cret")

	for _, path := range []string{"/api/config"} {
		w := doJSON(t, s.Handler(), "GET", path, nil)
		if bytes.Contains(w.Body.Bytes(), []byte("s3cret")) {
			t.Fatalf("GET %s leaks the password: %s", path, w.Body.String())
		}
		if !bytes.Contains(w.Body.Bytes(), []byte(`"hasPassword":true`)) {
			t.Fatalf("GET %s should tell the editor a password exists: %s", path, w.Body.String())
		}
	}
	// The response to the save itself must not leak it either.
	w := doJSON(t, s.Handler(), "PUT", "/api/config", s.store.Get().Redacted())
	if bytes.Contains(w.Body.Bytes(), []byte("s3cret")) {
		t.Fatal("PUT response leaks the password")
	}
	if s.store.Get().Widgets[0].Password != "s3cret" {
		t.Fatal("saving without a password must keep the stored one")
	}
}

func TestGetWidgetsReadsTheHost(t *testing.T) {
	s := newTestServer(t)
	g := fakeGlances(t)
	putWidget(t, s, g.URL, "s3cret")

	w := doJSON(t, s.Handler(), "GET", "/api/widgets", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d, body %s", w.Code, w.Body.String())
	}
	var got map[string]struct {
		Online bool `json:"online"`
		Data   struct {
			CPU float64 `json:"cpu"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got["w1"].Online || got["w1"].Data.CPU != 21.5 {
		t.Fatalf("unexpected result: %s", w.Body.String())
	}
}

func TestTestWidgetUsesTheStoredPasswordOnlyForTheSameAddress(t *testing.T) {
	s := newTestServer(t)
	g := fakeGlances(t)
	putWidget(t, s, g.URL, "s3cret")
	h := s.Handler()

	type reply struct {
		OK      bool   `json:"ok"`
		Version string `json:"version"`
		Error   string `json:"error"`
		Data    struct {
			CPU float64 `json:"cpu"`
		} `json:"data"`
	}
	call := func(body map[string]any) reply {
		w := doJSON(t, h, "POST", "/api/widgets/test", body)
		var r reply
		_ = json.Unmarshal(w.Body.Bytes(), &r)
		return r
	}

	if r := call(map[string]any{"id": "w1", "url": g.URL, "username": "glances"}); !r.OK || r.Version != "4.5.8" || r.Data.CPU != 21.5 {
		t.Fatalf("saved widget, blank password: %+v", r)
	}
	// Pointing the stored credentials at another address must not work.
	other := fakeGlances(t)
	if r := call(map[string]any{"id": "w1", "url": other.URL, "username": "glances"}); r.OK {
		t.Fatal("the stored password was sent to a different address")
	}
	if r := call(map[string]any{"url": g.URL, "username": "glances", "password": "wrong"}); r.OK {
		t.Fatal("a wrong password was accepted")
	}
	if w := doJSON(t, h, "POST", "/api/widgets/test", map[string]any{"url": "not a url"}); w.Code != http.StatusBadRequest {
		t.Fatalf("invalid address: got %d", w.Code)
	}
}

func TestMetricsOfAsksOnlyForWhatTheRowsNeed(t *testing.T) {
	got := metricsOf("glances", []config.Row{
		{Type: "cpu", Temp: true}, // temperature only: no CPU reading needed
		{Type: "disk", Key: "/"},
		{Type: "disk"},
		{Type: "network"},
	})
	if want := "sensors,disks,network"; strings.Join(got, ",") != want {
		t.Errorf("got %v, want %s", got, want)
	}
	got = metricsOf("glances", []config.Row{{Type: "cpu", Load: true, Temp: true}, {Type: "uptime"}})
	if want := "cpu,uptime,sensors"; strings.Join(got, ",") != want {
		t.Errorf("got %v, want %s", got, want)
	}
}

func TestMetricsOfForTrueNAS(t *testing.T) {
	got := metricsOf("truenas", []config.Row{{Type: "pool"}, {Type: "mem"}, {Type: "alerts"}, {Type: "drive"}, {Type: "version"}})
	if want := "info,mem,pools,drives,alerts"; strings.Join(got, ",") != want {
		t.Errorf("got %v, want %s", got, want)
	}
}
