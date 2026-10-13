package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
		Rows: []config.Row{{ID: "r1", Type: "cpu", Load: true}}, Refresh: 5, Cols: 1, HeightUnits: 1,
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
	got := metricsOf("truenas", []config.Row{{Type: "pool"}, {Type: "mem"}, {Type: "alerts"}, {Type: "drive"}, {Type: "version"}, {Type: "apps"}})
	if want := "info,mem,pools,drives,alerts,apps"; strings.Join(got, ",") != want {
		t.Errorf("got %v, want %s", got, want)
	}
	got = metricsOf("truenas", []config.Row{{Type: "cpu", Temp: true}})
	if want := "cputemp"; strings.Join(got, ",") != want {
		t.Errorf("temperature only: got %v, want %s", got, want)
	}
	got = metricsOf("truenas", []config.Row{{Type: "cpu", Load: true, Temp: true}})
	if want := "cpu,cputemp"; strings.Join(got, ",") != want {
		t.Errorf("load and temperature: got %v, want %s", got, want)
	}
}

func TestBackgroundsStayWhileAThemeUsesThem(t *testing.T) {
	s := newTestServer(t)
	h := s.Handler()
	dir := filepath.Join(s.dataDir, "backgrounds")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"bg-current.jpg", "bg-theme.jpg", "bg-orphan.jpg"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	cfg := s.store.Get().Redacted()
	cfg.Settings.BackgroundImage = "bg-current.jpg"
	cfg.Themes = []config.Theme{{ID: "t1", Name: "Night", Appearance: config.Appearance{Theme: "dark", Density: "comfortable", Layout: "list", BackgroundImage: "bg-theme.jpg"}}}
	if w := doJSON(t, h, "PUT", "/api/config", cfg); w.Code != http.StatusOK {
		t.Fatalf("put: %d %s", w.Code, w.Body.String())
	}
	exists := func(n string) bool { _, err := os.Stat(filepath.Join(dir, n)); return err == nil }
	if !exists("bg-current.jpg") || !exists("bg-theme.jpg") {
		t.Error("the current image and the one a theme uses must stay")
	}
	if exists("bg-orphan.jpg") {
		t.Error("an image nothing refers to should be removed")
	}

	// Deleting the theme frees its image.
	cfg.Themes = nil
	if w := doJSON(t, h, "PUT", "/api/config", cfg); w.Code != http.StatusOK {
		t.Fatalf("put: %d %s", w.Code, w.Body.String())
	}
	if exists("bg-theme.jpg") || !exists("bg-current.jpg") {
		t.Error("after deleting the theme only the current image should remain")
	}

	// Removing the background keeps what a theme still needs.
	cfg.Themes = []config.Theme{{ID: "t2", Name: "Keep", Appearance: config.Appearance{Theme: "dark", Density: "comfortable", Layout: "list", BackgroundImage: "bg-current.jpg"}}}
	if w := doJSON(t, h, "PUT", "/api/config", cfg); w.Code != http.StatusOK {
		t.Fatalf("put: %d %s", w.Code, w.Body.String())
	}
	if w := doJSON(t, h, "DELETE", "/api/background", nil); w.Code != http.StatusOK {
		t.Fatalf("delete: %d", w.Code)
	}
	if !exists("bg-current.jpg") {
		t.Error("the image a theme uses must survive removing the background")
	}
}

func newIconTestServer(t *testing.T) (*Server, *int) {
	t.Helper()
	hits := 0
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		switch r.URL.Path {
		case "/png/proxmox.png":
			_, _ = w.Write([]byte("PNGDATA"))
		case "/tree.json":
			_, _ = w.Write([]byte(`{"png":["proxmox.png"]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(up.Close)
	s := newTestServer(t)
	s.icons.origins = map[string][]string{"dashboard-icons": {"http://127.0.0.1:1", up.URL}}
	return s, &hits
}

func TestIconsAreDownloadedOnceAndKept(t *testing.T) {
	s, hits := newIconTestServer(t)
	h := s.Handler()
	for i := 0; i < 3; i++ {
		w := doJSON(t, h, "GET", "/cdn/dashboard-icons/png/proxmox.png", nil)
		if w.Code != http.StatusOK || w.Body.String() != "PNGDATA" || w.Header().Get("Content-Type") != "image/png" {
			t.Fatalf("round %d: %d %q %q", i, w.Code, w.Body.String(), w.Header().Get("Content-Type"))
		}
	}
	if *hits != 1 {
		t.Errorf("the CDN was asked %d times, want 1", *hits)
	}
	if _, err := os.Stat(filepath.Join(s.icons.dir, "dashboard-icons", "png", "proxmox.png")); err != nil {
		t.Errorf("not kept in the data folder: %v", err)
	}
}

func TestIconThatDoesNotExistIsNotAskedAgain(t *testing.T) {
	s, hits := newIconTestServer(t)
	h := s.Handler()
	for i := 0; i < 3; i++ {
		if w := doJSON(t, h, "GET", "/cdn/dashboard-icons/png/nothing.png", nil); w.Code != http.StatusNotFound {
			t.Fatalf("got %d", w.Code)
		}
	}
	if *hits != 1 {
		t.Errorf("the CDN was asked %d times, want 1", *hits)
	}
}

func TestIconIndexIsCachedAndOnlyKnownFilesAreFetched(t *testing.T) {
	s, hits := newIconTestServer(t)
	h := s.Handler()
	if w := doJSON(t, h, "GET", "/cdn/dashboard-icons/index/tree.json", nil); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "proxmox") {
		t.Fatalf("index: %d %q", w.Code, w.Body.String())
	}
	before := *hits
	for _, p := range []string{
		"/cdn/dashboard-icons/index/secrets.json", // not one of the index files
		"/cdn/dashboard-icons/png/..%2f..%2fconfig.json",
		"/cdn/dashboard-icons/png/Proxmox.png", // names are lowercase
		"/cdn/dashboard-icons/svg/proxmox.png", // format must match the folder
		"/cdn/elsewhere/png/proxmox.png",       // unknown collection
		"/cdn/dashboard-icons/png/a.png.exe",
	} {
		if w := doJSON(t, h, "GET", p, nil); w.Code != http.StatusNotFound {
			t.Errorf("%s: got %d, want 404", p, w.Code)
		}
	}
	if *hits != before {
		t.Errorf("a refused path reached the CDN (%d requests)", *hits-before)
	}
}

func TestIconUnreachableCDNGivesBadGatewayThenRecovers(t *testing.T) {
	s := newTestServer(t)
	s.icons.origins = map[string][]string{"dashboard-icons": {"http://127.0.0.1:1"}}
	if w := doJSON(t, s.Handler(), "GET", "/cdn/dashboard-icons/png/proxmox.png", nil); w.Code != http.StatusBadGateway {
		t.Errorf("got %d, want 502", w.Code)
	}
}

func TestMetricsOfForProxmox(t *testing.T) {
	got := metricsOf("proxmox", []config.Row{{Type: "lxc"}, {Type: "cpu", Load: true}})
	if want := "resources"; strings.Join(got, ",") != want {
		t.Errorf("got %v, want %s", got, want)
	}
	got = metricsOf("proxmox", []config.Row{{Type: "quorum"}, {Type: "backup"}, {Type: "version"}, {Type: "temp", Source: "glances"}})
	if want := "resources,version,cluster,tasks"; strings.Join(got, ",") != want {
		t.Errorf("got %v, want %s", got, want)
	}
	// The rows of the extra Glances source are asked of Glances, per node.
	rows := []config.Row{{Type: "temp", Source: "glances"}, {Type: "disk", Source: "glances", Node: "pve2"}, {Type: "lxc"}}
	if got := strings.Join(glancesNeeds(rows, ""), ","); got != "sensors" {
		t.Errorf("general Glances: %q", got)
	}
	if got := strings.Join(glancesNeeds(rows, "pve2"), ","); got != "disks" {
		t.Errorf("pve2 Glances: %q", got)
	}
}

func TestExtraGlancesSourceIsMergedAndItsPasswordKept(t *testing.T) {
	s := newTestServer(t)
	g := fakeGlances(t)
	h := s.Handler()
	put := func(extraPassword string) *httptest.ResponseRecorder {
		cfg := s.store.Get().Redacted()
		cfg.Widgets = []config.Widget{{
			ID: "w1", Name: "PVE", Kind: "proxmox", URL: "https://127.0.0.1:1", Username: "root@pam!homie",
			Glances: &config.Extra{URL: g.URL, Username: "glances", Password: extraPassword},
			Rows:    []config.Row{{ID: "r1", Type: "cpu", Load: true, Source: "glances"}}, Refresh: 5, Cols: 1, HeightUnits: 1,
		}}
		return doJSON(t, h, "PUT", "/api/config", cfg)
	}
	if w := put("s3cret"); w.Code != http.StatusOK || strings.Contains(w.Body.String(), "s3cret") {
		t.Fatalf("saving: %d %s", w.Code, w.Body.String())
	}
	// Saved again with no password (the browser never has it): the stored one stays.
	if w := put(""); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"hasPassword": true`) && !strings.Contains(w.Body.String(), `"hasPassword":true`) {
		t.Fatalf("resaving: %d %s", w.Code, w.Body.String())
	}
	w := doJSON(t, h, "GET", "/api/widgets", nil)
	var got map[string]struct {
		Online bool `json:"online"`
		Data   struct {
			Glances struct {
				CPU float64 `json:"cpu"`
			} `json:"glances"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	// The Proxmox side is unreachable, the Glances side is not.
	if got["w1"].Online || got["w1"].Data.Glances.CPU != 21.5 {
		t.Fatalf("unexpected result: %s", w.Body.String())
	}
	// The stored extra password is never sent to a different address.
	cfg := s.store.Get().Redacted()
	other := fakeGlances(t)
	cfg.Widgets[0].Glances.URL = other.URL
	if r := doJSON(t, h, "PUT", "/api/config", cfg); r.Code != http.StatusOK {
		t.Fatal(r.Body.String())
	}
	if p := s.store.Get().Widgets[0].Glances.Password; p != "" {
		t.Fatalf("the Glances password followed a changed address: %q", p)
	}
}
