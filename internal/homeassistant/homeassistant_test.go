package homeassistant

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fake is a Home Assistant with three entities.
func fake(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var mu sync.Mutex
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer good-token" {
			http.Error(w, `{"message":"401: Unauthorized"}`, http.StatusUnauthorized)
			return
		}
		switch {
		case r.URL.Path == "/api/config":
			io.WriteString(w, `{"version":"2025.9.1","location_name":"Home"}`)
		case r.URL.Path == "/api/states":
			io.WriteString(w, `[{"entity_id":"sensor.power","state":"68.2","attributes":{"friendly_name":"Server power","unit_of_measurement":"W","device_class":"power"}},
				{"entity_id":"light.desk","state":"on","attributes":{"friendly_name":"Desk","brightness":128}},
				{"entity_id":"lock.front","state":"locked","attributes":{}}]`)
		case strings.HasPrefix(r.URL.Path, "/api/states/"):
			switch strings.TrimPrefix(r.URL.Path, "/api/states/") {
			case "sensor.power":
				io.WriteString(w, `{"entity_id":"sensor.power","state":"68.2","attributes":{"friendly_name":"Server power","unit_of_measurement":"W","device_class":"power"}}`)
			case "light.desk":
				io.WriteString(w, `{"entity_id":"light.desk","state":"on","attributes":{"friendly_name":"Desk","brightness":128}}`)
			default:
				http.Error(w, `{"message":"Entity not found."}`, http.StatusNotFound)
			}
		case strings.HasPrefix(r.URL.Path, "/api/services/") && r.Method == http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			mu.Lock()
			calls = append(calls, strings.TrimPrefix(r.URL.Path, "/api/services/")+" "+string(body))
			mu.Unlock()
			io.WriteString(w, `[]`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func TestPingAndFetch(t *testing.T) {
	srv, _ := fake(t)
	c := New()
	tg := Target{URL: srv.URL, Token: "good-token", Entities: []string{"sensor.power", "light.desk", "sensor.gone", "sensor.power"}}
	v, loc, err := c.Ping(context.Background(), tg)
	if err != nil || v != "2025.9.1" || loc != "Home" {
		t.Fatalf("ping: %v %v %v", v, loc, err)
	}
	res := c.Fetch(context.Background(), tg)
	if !res.Online {
		t.Fatalf("offline: %s", res.Error)
	}
	p := res.Data.Entities["sensor.power"]
	if p.State != "68.2" || p.Unit != "W" || p.Name != "Server power" || p.Class != "power" {
		t.Errorf("sensor: %+v", p)
	}
	l := res.Data.Entities["light.desk"]
	if l.Brightness == nil || *l.Brightness != 50 {
		t.Errorf("light brightness: %+v", l)
	}
	if !res.Data.Entities["sensor.gone"].Missing {
		t.Errorf("an unknown entity is not marked missing: %+v", res.Data.Entities["sensor.gone"])
	}
	if len(res.Data.Entities) != 3 {
		t.Errorf("entities: %d", len(res.Data.Entities))
	}
}

func TestBadTokenAndAddress(t *testing.T) {
	srv, _ := fake(t)
	c := New()
	if _, _, err := c.Ping(context.Background(), Target{URL: srv.URL, Token: "nope"}); err == nil || !strings.Contains(err.Error(), "token") {
		t.Errorf("a bad token: %v", err)
	}
	res := c.Fetch(context.Background(), Target{URL: srv.URL, Token: "nope", Entities: []string{"sensor.power"}})
	if res.Online || res.Error == "" {
		t.Errorf("a bad token reads as online: %+v", res)
	}
	if _, _, err := c.Ping(context.Background(), Target{URL: "ftp://x", Token: "x"}); err == nil {
		t.Error("a ftp address was accepted")
	}
	if res := c.Fetch(context.Background(), Target{URL: "http://127.0.0.1:1", Token: "x", Entities: []string{"sensor.a"}}); res.Online {
		t.Error("an unreachable server reads as online")
	}
}

func TestNeverFollowsRedirects(t *testing.T) {
	var leaked bool
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked = r.Header.Get("Authorization") != ""
	}))
	defer other.Close()
	redir := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/api/config", http.StatusFound)
	}))
	defer redir.Close()
	if _, _, err := New().Ping(context.Background(), Target{URL: redir.URL, Token: "secret"}); err == nil {
		t.Error("a redirect was followed")
	}
	if leaked {
		t.Error("the token was sent to the redirect target")
	}
}

func TestList(t *testing.T) {
	srv, _ := fake(t)
	list, err := New().List(context.Background(), Target{URL: srv.URL, Token: "good-token"})
	if err != nil || len(list) != 3 || list[0].ID != "light.desk" {
		t.Fatalf("list: %v %+v", err, list)
	}
}

func TestServicesAreAllowListed(t *testing.T) {
	for _, c := range []struct {
		id, action string
		want       Service
		ok         bool
	}{
		{"light.desk", "toggle", Service{"light", "toggle"}, true},
		{"switch.plug", "on", Service{"switch", "turn_on"}, true},
		{"fan.bedroom", "off", Service{"fan", "turn_off"}, true},
		{"scene.movie", "trigger", Service{"scene", "turn_on"}, true},
		{"button.doorbell", "trigger", Service{"button", "press"}, true},
		{"scene.movie", "toggle", Service{}, false}, // a scene is run, not toggled
		{"light.desk", "trigger", Service{}, false},
		{"lock.front", "toggle", Service{}, false},
		{"alarm_control_panel.home", "on", Service{}, false},
		{"cover.garage", "toggle", Service{}, false},
		{"climate.heat", "on", Service{}, false},
		{"light.desk/../../x", "toggle", Service{}, false},
		{"light", "toggle", Service{}, false},
	} {
		got, err := ServiceFor(c.id, c.action)
		if (err == nil) != c.ok || got != c.want {
			t.Errorf("%s %s: %+v %v", c.id, c.action, got, err)
		}
	}
}

func TestCallSendsOnlyTheEntity(t *testing.T) {
	srv, calls := fake(t)
	c := New()
	tg := Target{URL: srv.URL, Token: "good-token"}
	if err := c.Call(context.Background(), tg, Service{"light", "toggle"}, "light.desk"); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 || (*calls)[0] != `light/toggle {"entity_id":"light.desk"}` {
		t.Fatalf("calls: %v", *calls)
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(strings.TrimPrefix((*calls)[0], "light/toggle ")), &m); err != nil || len(m) != 1 {
		t.Errorf("body: %v", m)
	}
	// An entity of another domain than the service is refused before any request.
	if err := c.Call(context.Background(), tg, Service{"light", "toggle"}, "lock.front"); err == nil {
		t.Error("a light service was sent for a lock")
	}
	if err := c.Call(context.Background(), tg, Service{"light", "toggle"}, "light.desk/x"); err == nil {
		t.Error("a malformed id was sent")
	}
	if len(*calls) != 1 {
		t.Errorf("refused calls reached the server: %v", *calls)
	}
}
