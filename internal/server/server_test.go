package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
