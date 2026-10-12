package glances

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Responses captured from a real Glances 4.5.8 (/api/4), trimmed; sensors is
// what a Linux host reports.
var v4 = map[string]string{
	"status":  `{"version": "4.5.8"}`,
	"cpu":     `{"total": 12.5, "user": 8.0, "system": 4.5, "idle": 87.5, "cpucore": 16}`,
	"mem":     `{"total": 34142109696, "available": 21615669248, "percent": 36.7, "used": 12526440448, "free": 21615669248}`,
	"memswap": `{"total": 2952790016, "used": 151412736, "free": 2801377280, "percent": 5.1}`,
	"load":    `{"min1": 0.5, "min5": 0.25, "min15": 0.1, "cpucore": 16}`,
	"uptime":  `"4 days, 4:51:10"`,
	"sensors": `[{"label": "Package id 0", "value": 48, "unit": "C", "type": "temperature_core"},
		{"label": "Core 1", "value": 52, "unit": "C", "type": "temperature_core"},
		{"label": "fan1", "value": 1200, "unit": "R", "type": "fan_speed"},
		{"label": "Battery", "value": 90, "unit": "%", "type": "battery"}]`,
	"fs": `[{"device_name": "/dev/sda1", "fs_type": "ext4", "mnt_point": "/", "size": 498964049920, "used": 194129596416, "percent": 38.9},
		{"device_name": "/dev/sdb1", "fs_type": "ext4", "mnt_point": "/mnt/data", "size": 1000186310656, "used": 891389231104, "percent": 89.1}]`,
	"network": `[{"interface_name": "eno1", "bytes_recv_rate_per_sec": 1000.0, "bytes_sent_rate_per_sec": 200.0},
		{"interface_name": "vmbr0", "bytes_recv_rate_per_sec": 1000.0, "bytes_sent_rate_per_sec": 200.0},
		{"interface_name": "tap100i0", "bytes_recv_rate_per_sec": 50.0, "bytes_sent_rate_per_sec": 50.0},
		{"interface_name": "lo", "bytes_recv_rate_per_sec": 7.0, "bytes_sent_rate_per_sec": 7.0}]`,
}

func fakeGlances(t *testing.T, prefix string, data map[string]string, user, pass string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user != "" {
			u, p, ok := r.BasicAuth()
			if !ok || u != user || p != pass {
				http.Error(w, "no", http.StatusUnauthorized)
				return
			}
		}
		body, ok := data[strings.TrimPrefix(r.URL.Path, prefix+"/")]
		if !strings.HasPrefix(r.URL.Path, prefix+"/") || !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

var allMetrics = Metrics

func TestFetchReadsEveryMetric(t *testing.T) {
	srv := fakeGlances(t, "/api/4", v4, "", "")
	r := New().Fetch(context.Background(), Target{URL: srv.URL + "/", Metrics: allMetrics})
	if !r.Online || r.Error != "" || len(r.Failed) != 0 {
		t.Fatalf("online=%v error=%q failed=%v", r.Online, r.Error, r.Failed)
	}
	d := r.Data
	if d.CPU == nil || *d.CPU != 12.5 {
		t.Errorf("cpu = %v", d.CPU)
	}
	if d.Mem == nil || d.Mem.Percent != 36.7 || d.Mem.Total != 34142109696 {
		t.Errorf("mem = %+v", d.Mem)
	}
	if d.Swap == nil || d.Swap.Used != 151412736 {
		t.Errorf("swap = %+v", d.Swap)
	}
	if d.Load == nil || d.Load.Min5 != 0.25 || d.Load.Cores != 16 {
		t.Errorf("load = %+v", d.Load)
	}
	if d.Uptime == nil || *d.Uptime != "4 days, 4:51:10" {
		t.Errorf("uptime = %v", d.Uptime)
	}
	if d.Temp == nil || d.Temp.Value != 48 || d.Temp.Label != "Package id 0" {
		t.Errorf("temp = %+v (want the CPU package, not the hottest sensor)", d.Temp)
	}
	if len(d.Sensors) != 2 || d.Sensors[1].Label != "Core 1" {
		t.Errorf("sensors = %+v (want the two temperatures, no fan or battery)", d.Sensors)
	}
	if len(d.Disks) != 2 || d.Disks[1].Mount != "/mnt/data" || d.Disks[1].Percent != 89.1 {
		t.Errorf("disks = %+v", d.Disks)
	}
	if len(d.Network) != 4 {
		t.Fatalf("network = %+v", d.Network)
	}
	virtual := map[string]bool{}
	for _, n := range d.Network {
		virtual[n.Name] = n.Virtual
	}
	if virtual["eno1"] || virtual["vmbr0"] || !virtual["tap100i0"] || !virtual["lo"] {
		t.Errorf("virtual flags = %v (VM tap and loopback are virtual, the card and the bridge are not)", virtual)
	}
	if !d.Network[1].Bridge || d.Network[0].Bridge {
		t.Errorf("bridge flags: %+v", d.Network)
	}
	if d.Network[0].Rx != 1000 || d.Network[0].Tx != 200 {
		t.Errorf("eno1 = %+v", d.Network[0])
	}
}

func TestPickTemp(t *testing.T) {
	// Real sensors from a Dell host: dell_smm 6 reads a bogus 126.
	temps := temperatures([]sensor{
		{Label: "Composite", Value: 35.0, Unit: "C", Type: "temperature_core"},
		{Label: "Package id 0", Value: 42.0, Unit: "C", Type: "temperature_core"},
		{Label: "dell_smm 6", Value: 126.0, Unit: "C", Type: "temperature_core"},
		{Label: "dell_smm 0", Value: 1811.0, Unit: "R", Type: "fan_speed"},
	})
	if len(temps) != 3 {
		t.Fatalf("temperatures = %+v, want the fan left out", temps)
	}
	if got := pickTemp(temps); got == nil || got.Label != "Package id 0" {
		t.Errorf("auto = %+v", got)
	}
	if got := pickTemp(temps[2:]); got == nil || got.Value != 126 {
		t.Errorf("no CPU sensor: want the warmest, got %+v", got)
	}
}

func TestFetchOnlyAsksForTheChosenMetrics(t *testing.T) {
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.Path)
		body, ok := v4[strings.TrimPrefix(r.URL.Path, "/api/4/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	r := New().Fetch(context.Background(), Target{URL: srv.URL, Metrics: []string{"cpu"}})
	if !r.Online || r.Data.Mem != nil || r.Data.Disks != nil {
		t.Fatalf("result = %+v", r)
	}
	for _, p := range asked {
		if p != "/api/4/status" && p != "/api/4/cpu" {
			t.Errorf("unexpected request %s", p)
		}
	}
}

func TestFetchKeepsWorkingWhenOneMetricIsMissing(t *testing.T) {
	data := map[string]string{"status": v4["status"], "cpu": v4["cpu"]} // no sensors endpoint
	srv := fakeGlances(t, "/api/4", data, "", "")
	r := New().Fetch(context.Background(), Target{URL: srv.URL, Metrics: []string{"cpu", "sensors"}})
	if !r.Online || r.Data.CPU == nil {
		t.Fatalf("result = %+v", r)
	}
	if len(r.Failed) != 1 || r.Failed[0] != "sensors" {
		t.Errorf("failed = %v", r.Failed)
	}
}

func TestFetchFallsBackToTheOlderAPI(t *testing.T) {
	v3 := map[string]string{
		"status":  `"Active"`,
		"uptime":  `"1 day, 2:00:00"`,
		"network": `[{"interface_name": "eth0", "time_since_update": 2.0, "rx": 4000, "tx": 1000}]`,
	}
	srv := fakeGlances(t, "/api/3", v3, "", "")
	r := New().Fetch(context.Background(), Target{URL: srv.URL, Metrics: []string{"uptime", "network"}})
	if !r.Online {
		t.Fatalf("error = %q", r.Error)
	}
	if len(r.Data.Network) != 1 || r.Data.Network[0].Rx != 2000 || r.Data.Network[0].Tx != 500 {
		t.Errorf("network = %+v (Glances 3 reports bytes per interval)", r.Data.Network)
	}
}

func TestUptimeAcceptsAnObject(t *testing.T) {
	s, ok := parseUptime([]byte(`{"uptime": "3:00:00"}`))
	if !ok || s != "3:00:00" {
		t.Errorf("got %q, %v", s, ok)
	}
}

func TestBasicAuth(t *testing.T) {
	srv := fakeGlances(t, "/api/4", v4, "glances", "s3cret")
	c := New()

	r := c.Fetch(context.Background(), Target{URL: srv.URL, Metrics: []string{"cpu"}})
	if r.Online || !strings.Contains(r.Error, "authentication") {
		t.Errorf("without credentials: online=%v error=%q", r.Online, r.Error)
	}
	r = c.Fetch(context.Background(), Target{URL: srv.URL, Metrics: []string{"cpu"}, Username: "glances", Password: "wrong"})
	if r.Online {
		t.Error("a wrong password was accepted")
	}
	r = c.Fetch(context.Background(), Target{URL: srv.URL, Metrics: []string{"cpu"}, Username: "glances", Password: "s3cret"})
	if !r.Online {
		t.Errorf("with credentials: error = %q", r.Error)
	}
}

func TestErrorsNeverRevealTheAddressOrPassword(t *testing.T) {
	r := New().Fetch(context.Background(), Target{URL: "http://127.0.0.1:1", Password: "s3cret", Metrics: []string{"cpu"}})
	if r.Online || r.Error == "" {
		t.Fatalf("result = %+v", r)
	}
	if strings.Contains(r.Error, "127.0.0.1") || strings.Contains(r.Error, "s3cret") {
		t.Errorf("error leaks details: %q", r.Error)
	}
}

func TestNotGlances(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	if _, err := New().Ping(context.Background(), Target{URL: srv.URL}); err == nil || !strings.Contains(err.Error(), "Glances") {
		t.Errorf("err = %v", err)
	}
}

func TestPing(t *testing.T) {
	srv := fakeGlances(t, "/api/4", v4, "", "")
	v, err := New().Ping(context.Background(), Target{URL: srv.URL})
	if err != nil || v != "4.5.8" {
		t.Errorf("version = %q, err = %v", v, err)
	}
}
