package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Bagareal/homie/internal/config"
)

const haToken = "eyJhbGciOiJIUzI1NiJ9.eyJpc3MiOiJ0ZXN0In0.signature-that-is-long-enough-0123456789"

// fakeHA is a Home Assistant that records the services it is asked to run.
func fakeHA(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var mu sync.Mutex
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+haToken {
			http.Error(w, "{}", http.StatusUnauthorized)
			return
		}
		switch {
		case r.URL.Path == "/api/config":
			io.WriteString(w, `{"version":"2025.9.1","location_name":"Home"}`)
		case r.URL.Path == "/api/states":
			io.WriteString(w, `[{"entity_id":"sensor.power","state":"68.2","attributes":{"friendly_name":"Power","unit_of_measurement":"W"}},{"entity_id":"light.desk","state":"on","attributes":{}}]`)
		case r.URL.Path == "/api/states/sensor.power":
			io.WriteString(w, `{"entity_id":"sensor.power","state":"68.2","attributes":{"unit_of_measurement":"W"}}`)
		case r.URL.Path == "/api/states/light.desk":
			io.WriteString(w, `{"entity_id":"light.desk","state":"off","attributes":{}}`)
		case strings.HasPrefix(r.URL.Path, "/api/services/"):
			body, _ := io.ReadAll(r.Body)
			mu.Lock()
			calls = append(calls, strings.TrimPrefix(r.URL.Path, "/api/services/")+" "+string(body))
			mu.Unlock()
			io.WriteString(w, "[]")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func haConfig(url string) config.Config {
	c := config.Default()
	c.Widgets = []config.Widget{{
		ID: "ha1", Name: "Home", Kind: "homeassistant", URL: url, Password: haToken,
		Rows: []config.Row{
			{ID: "r1", Type: "entity", Key: "sensor.power"},
			{ID: "r2", Type: "entity", Key: "light.desk", Control: true},
		},
		Refresh: 5, Cols: 1, HeightUnits: 1,
	}}
	return c
}

func TestHomeAssistantWidgetReadsAndNeverShowsTheToken(t *testing.T) {
	ha, _ := fakeHA(t)
	s := newTestServer(t)
	if err := s.store.Set(haConfig(ha.URL)); err != nil {
		t.Fatal(err)
	}
	h := authed(s)
	w := doJSON(t, h, "GET", "/api/widgets", nil)
	var got map[string]struct {
		Online bool `json:"online"`
		Data   struct {
			Entities map[string]struct {
				State string `json:"state"`
				Unit  string `json:"unit"`
			} `json:"entities"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	e := got["ha1"]
	if !e.Online || e.Data.Entities["sensor.power"].State != "68.2" || e.Data.Entities["sensor.power"].Unit != "W" || e.Data.Entities["light.desk"].State != "off" {
		t.Fatalf("widget data: %s", w.Body)
	}
	for _, path := range []string{"/api/config", "/api/widgets"} {
		if strings.Contains(doJSON(t, h, "GET", path, nil).Body.String(), haToken) {
			t.Errorf("%s shows the token", path)
		}
	}
}

func TestHomeAssistantTestListsEntities(t *testing.T) {
	ha, _ := fakeHA(t)
	h := authed(newTestServer(t))
	w := doJSON(t, h, "POST", "/api/widgets/test", map[string]string{"kind": "homeassistant", "url": ha.URL, "password": haToken})
	var r struct {
		OK      bool   `json:"ok"`
		Error   string `json:"error"`
		Version string `json:"version"`
		Data    struct {
			EntityList []struct {
				ID string `json:"id"`
			} `json:"entityList"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &r)
	if !r.OK || !strings.Contains(r.Version, "2025.9.1") || len(r.Data.EntityList) != 2 {
		t.Fatalf("test: %s", w.Body)
	}
	w = doJSON(t, h, "POST", "/api/widgets/test", map[string]string{"kind": "homeassistant", "url": ha.URL, "password": "wrong"})
	_ = json.Unmarshal(w.Body.Bytes(), &r)
	if r.OK || r.Error == "" {
		t.Fatalf("a wrong token tested fine: %s", w.Body)
	}
}

func TestHomeAssistantCommands(t *testing.T) {
	ha, calls := fakeHA(t)
	s := newTestServer(t)
	if err := s.store.Set(haConfig(ha.URL)); err != nil {
		t.Fatal(err)
	}
	h := authed(s)
	post := func(id, row, action string) *httptest.ResponseRecorder {
		return doJSON(t, h, "POST", "/api/widgets/"+id+"/command", map[string]string{"row": row, "action": action})
	}
	if w := post("ha1", "r2", "toggle"); w.Code != 200 {
		t.Fatalf("toggle: %d %s", w.Code, w.Body)
	} else if !strings.Contains(w.Body.String(), `"state":"off"`) {
		t.Errorf("the answer does not carry the new state: %s", w.Body)
	}
	if len(*calls) != 1 || (*calls)[0] != `light/toggle {"entity_id":"light.desk"}` {
		t.Fatalf("calls: %v", *calls)
	}
	// A row without a switch, an unknown row, an unknown widget, a wrong action.
	if w := post("ha1", "r1", "toggle"); w.Code != http.StatusForbidden {
		t.Errorf("a read-only row: %d", w.Code)
	}
	if w := post("ha1", "nope", "toggle"); w.Code != http.StatusForbidden {
		t.Errorf("an unknown row: %d", w.Code)
	}
	if w := post("nope", "r2", "toggle"); w.Code != http.StatusNotFound {
		t.Errorf("an unknown widget: %d", w.Code)
	}
	if w := post("ha1", "r2", "trigger"); w.Code != http.StatusBadRequest {
		t.Errorf("a light cannot be triggered: %d", w.Code)
	}
	if w := doJSON(t, h, "POST", "/api/widgets/ha1/command", map[string]string{"row": "r2", "action": "toggle", "entity": "lock.front", "service": "lock/unlock"}); w.Code != 200 {
		t.Errorf("extra fields: %d", w.Code)
	}
	for _, c := range *calls {
		if strings.Contains(c, "lock") {
			t.Errorf("a request chose the entity: %v", *calls)
		}
	}
	// Without a login nothing is switched.
	if w := doJSON(t, s.Handler(), "POST", "/api/widgets/ha1/command", map[string]string{"row": "r2", "action": "toggle"}); w.Code != http.StatusUnauthorized {
		t.Errorf("a command without a login: %d", w.Code)
	}
}

func TestHomeAssistantConfigIsValidated(t *testing.T) {
	for name, mutate := range map[string]func(*config.Config){
		"a lock cannot get a switch": func(c *config.Config) {
			c.Widgets[0].Rows[1] = config.Row{ID: "r2", Type: "entity", Key: "lock.front", Control: true}
		},
		"an id with a slash": func(c *config.Config) { c.Widgets[0].Rows[0].Key = "sensor.power/../x" },
		"an empty id":        func(c *config.Config) { c.Widgets[0].Rows[0].Key = "" },
		"a switch on another kind": func(c *config.Config) {
			c.Widgets[0].Rows[0] = config.Row{ID: "r1", Type: "entity", Key: "scene.x", Source: "nut", Control: true}
		},
		"an upper-case id":                 func(c *config.Config) { c.Widgets[0].Rows[0].Key = "Sensor.Power" },
		"a second value with a switch":     func(c *config.Config) { c.Widgets[0].Rows[1].Key2 = "sensor.power" },
		"a second value that is no entity": func(c *config.Config) { c.Widgets[0].Rows[0].Key2 = "not an id" },
		"a token of a few thousand b.":     func(c *config.Config) { c.Widgets[0].Password = strings.Repeat("a", 2001) },
	} {
		c := haConfig("http://192.168.1.10:8123")
		mutate(&c)
		if err := c.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if err := haConfig("http://192.168.1.10:8123").Validate(); err != nil {
		t.Errorf("a good configuration: %v", err)
	}
}

func TestHomeAssistantRowWithTwoValuesReadsBoth(t *testing.T) {
	if got := metricsOf("homeassistant", []config.Row{
		{ID: "a", Type: "entity", Key: "sensor.temp", Key2: "sensor.humidity"},
		{ID: "b", Type: "entity", Key: "sensor.humidity"},
	}); strings.Join(got, ",") != "sensor.humidity,sensor.temp" {
		t.Errorf("entities asked for: %v", got)
	}
}
