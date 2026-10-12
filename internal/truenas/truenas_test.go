package truenas

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeNAS is a TrueNAS stand-in: TLS, a WebSocket at /api/current and the
// JSON-RPC methods Homie calls, answering with canned results.
type fakeNAS struct {
	*httptest.Server
	key       string // accepted API key
	username  string // expected by auth.login_ex ("" = not required)
	noLoginEx bool   // pretend auth.login_ex does not exist
	results   map[string]string
	calls     []string
}

var defaults = map[string]string{
	"system.version": `"TrueNAS-25.10.1"`,
	"system.info": `{"version":"TrueNAS-25.10.1","hostname":"truenas","physmem":34359738368,"cores":8,
		"uptime_seconds":97531.5,"loadavg":[0.5,0.25,0.1]}`,
	"pool.query": `[
		{"name":"tank","status":"ONLINE","healthy":true,"size":4000000000000,"allocated":1000000000000,
		 "scan":{"function":"SCRUB","state":"FINISHED","end_time":{"$date":1760000000000},"percentage":100,"errors":0}},
		{"name":"boot-pool","status":"DEGRADED","healthy":false,"size":100,"allocated":50,"scan":null}]`,
	"disk.query":        `[{"name":"sda","model":"WDC","pool":"tank"},{"name":"nvme0n1","model":"Samsung","pool":null}]`,
	"disk.temperatures": `{"sda":38,"nvme0n1":null}`,
	"app.query":         `[{"name":"plex","state":"RUNNING"},{"name":"nextcloud","state":"STOPPED"},{"name":"immich","state":"RUNNING"},{"name":"adguard","state":"DEPLOYING"}]`,
	"alert.list": `[
		{"level":"WARNING","text":"Scrub overdue","dismissed":false},
		{"level":"CRITICAL","text":"Pool degraded","dismissed":false},
		{"level":"ERROR","text":"old","dismissed":true}]`,
	"reporting.netdata_get_data": `[
		{"name":"cpu","legend":["time","user","system","iowait"],"data":[[1760000000,3,2,1],[1760000001,5,3,null],[1760000002,4,2,1.5]]},
		{"name":"memory","legend":["time","free","used","cached","buffers"],"data":[[1760000002,8000,16000,6000,2000]]}]`,
}

func newFakeNAS(t *testing.T) *fakeNAS {
	t.Helper()
	f := &fakeNAS{key: "s3cret", results: map[string]string{}}
	for k, v := range defaults {
		f.results[k] = v
	}
	f.Server = httptest.NewTLSServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeNAS) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/current" || !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		http.NotFound(w, r)
		return
	}
	sum := sha1.Sum([]byte(r.Header.Get("Sec-WebSocket-Key") + wsGUID))
	conn, rw, err := w.(http.Hijacker).Hijack()
	if err != nil {
		return
	}
	defer conn.Close()
	_, _ = rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: " +
		base64.StdEncoding.EncodeToString(sum[:]) + "\r\n\r\n")
	_ = rw.Flush()

	for {
		msg, err := readClientFrame(rw.Reader)
		if err != nil {
			return
		}
		var req struct {
			ID     int             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		_ = json.Unmarshal(msg, &req)
		f.calls = append(f.calls, req.Method)
		reply := f.answer(req.Method, req.Params)
		out := `{"jsonrpc":"2.0","id":` + itoa(req.ID) + `,` + reply + `}`
		// Big replies are sent in two fragments with a ping in between, as a
		// real server may.
		if len(out) > 200 {
			half := len(out) / 2
			writeServerFrame(rw.Writer, 0x1, false, []byte(out[:half]))
			writeServerFrame(rw.Writer, 0x9, true, []byte("ping"))
			writeServerFrame(rw.Writer, 0x0, true, []byte(out[half:]))
		} else {
			writeServerFrame(rw.Writer, 0x1, true, []byte(out))
		}
		_ = rw.Flush()
	}
}

func (f *fakeNAS) answer(method string, params json.RawMessage) string {
	switch method {
	case "auth.login_ex":
		if f.noLoginEx {
			return `"error":{"code":-32601,"message":"Method not found"}`
		}
		var p []struct {
			Mechanism string `json:"mechanism"`
			Username  string `json:"username"`
			APIKey    string `json:"api_key"`
		}
		_ = json.Unmarshal(params, &p)
		if len(p) == 1 && p[0].Mechanism == "API_KEY_PLAIN" && p[0].APIKey == f.key && p[0].Username == f.username {
			return `"result":{"response_type":"SUCCESS"}`
		}
		return `"result":{"response_type":"AUTH_ERR"}`
	case "auth.login_with_api_key":
		var p []string
		_ = json.Unmarshal(params, &p)
		return `"result":` + map[bool]string{true: "true", false: "false"}[len(p) == 1 && p[0] == f.key]
	}
	if res, ok := f.results[method]; ok {
		if res == "ERR" {
			return `"error":{"code":-32001,"message":"Not authorized"}`
		}
		return `"result":` + res
	}
	return `"error":{"code":-32601,"message":"Method not found"}`
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

// readClientFrame reads one masked client text frame.
func readClientFrame(r *bufio.Reader) ([]byte, error) {
	for {
		p, pong, err := readOne(r)
		if err != nil || !pong {
			return p, err
		}
	}
}

// readOne reads one frame; pong reports a pong (the answer to our ping).
func readOne(r *bufio.Reader) (p []byte, pong bool, err error) {
	var h [2]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return nil, false, err
	}
	if h[0]&0x0f == 0x8 {
		return nil, false, io.EOF
	}
	n := int(h[1] & 0x7f)
	switch n {
	case 126:
		var b [2]byte
		_, _ = io.ReadFull(r, b[:])
		n = int(binary.BigEndian.Uint16(b[:]))
	case 127:
		var b [8]byte
		_, _ = io.ReadFull(r, b[:])
		n = int(binary.BigEndian.Uint64(b[:]))
	}
	var mask [4]byte
	_, _ = io.ReadFull(r, mask[:])
	p = make([]byte, n)
	if _, err := io.ReadFull(r, p); err != nil {
		return nil, false, err
	}
	for i := range p {
		p[i] ^= mask[i%4]
	}
	return p, h[0]&0x0f == 0xA, nil
}

func writeServerFrame(w *bufio.Writer, opcode byte, fin bool, p []byte) {
	b0 := opcode
	if fin {
		b0 |= 0x80
	}
	_ = w.WriteByte(b0)
	switch {
	case len(p) < 126:
		_ = w.WriteByte(byte(len(p)))
	case len(p) < 1<<16:
		_ = w.WriteByte(126)
		_ = binary.Write(w, binary.BigEndian, uint16(len(p)))
	default:
		_ = w.WriteByte(127)
		_ = binary.Write(w, binary.BigEndian, uint64(len(p)))
	}
	_, _ = w.Write(p)
}

func target(f *fakeNAS, key, user string, metrics ...string) Target {
	return Target{URL: f.URL, Username: user, APIKey: key, Metrics: metrics}
}

func TestFetchReadsEverything(t *testing.T) {
	f := newFakeNAS(t)
	r := New().Fetch(context.Background(), target(f, "s3cret", "", Metrics...))
	if !r.Online || r.Error != "" || len(r.Failed) != 0 {
		t.Fatalf("online=%v error=%q failed=%v", r.Online, r.Error, r.Failed)
	}
	d := r.Data
	if d.Version != "TrueNAS-25.10.1" || d.Hostname != "truenas" {
		t.Errorf("version/hostname = %q %q", d.Version, d.Hostname)
	}
	if d.Uptime == nil || *d.Uptime != "1 day, 3:05:31" {
		t.Errorf("uptime = %v", d.Uptime)
	}
	if d.Load == nil || d.Load.Min5 != 0.25 || d.Load.Cores != 8 {
		t.Errorf("load = %+v", d.Load)
	}
	// Latest complete point: the row with a null is skipped only if it is the newest.
	if d.CPU == nil || *d.CPU != 7.5 {
		t.Errorf("cpu = %v, want 4+2+1.5", d.CPU)
	}
	if d.Mem == nil || d.Mem.Percent != 50 || d.Mem.Total != 34359738368 {
		t.Errorf("mem = %+v (used 16000 of 32000)", d.Mem)
	}
	if len(d.Pools) != 2 || d.Pools[1].Name != "tank" || d.Pools[1].Percent != 25 || !d.Pools[1].Healthy {
		t.Fatalf("pools = %+v", d.Pools)
	}
	if s := d.Pools[1].Scan; s == nil || s.Function != "SCRUB" || s.End != "2025-10-09T08:53:20Z" {
		t.Errorf("scan = %+v", s)
	}
	if d.Pools[0].Name != "boot-pool" || d.Pools[0].Healthy || d.Pools[0].Scan != nil {
		t.Errorf("degraded pool = %+v", d.Pools[0])
	}
	if len(d.Drives) != 2 || d.Drives[1].Name != "sda" || d.Drives[1].Temp == nil || *d.Drives[1].Temp != 38 || d.Drives[0].Temp != nil {
		t.Errorf("drives = %+v", d.Drives)
	}
	if d.Apps == nil || d.Apps.Running != 2 || d.Apps.Total != 4 || strings.Join(d.Apps.Stopped, ",") != "adguard,nextcloud" {
		t.Errorf("apps = %+v", d.Apps)
	}
	if d.Alerts == nil || d.Alerts.Count != 2 || d.Alerts.Level != "CRITICAL" || d.Alerts.Items[0].Text != "Pool degraded" {
		t.Errorf("alerts = %+v (the dismissed one must not count)", d.Alerts)
	}
}

func TestMemoryFromAvailableOnly(t *testing.T) {
	// TrueNAS documents the memory graph as "physical memory available".
	physmem := 34359738368.0 // 32768 MiB
	mib := physmem / (1 << 20)
	u := memoryUsage(map[string]float64{"available": mib / 2}, physmem)
	if u == nil || u.Percent != 50 || u.Used != physmem/2 {
		t.Errorf("available in MiB: %+v", u)
	}
	u = memoryUsage(map[string]float64{"available": physmem / 4}, physmem)
	if u == nil || u.Percent != 75 {
		t.Errorf("available in bytes: %+v", u)
	}
	if memoryUsage(map[string]float64{"weird": 1}, physmem) != nil {
		t.Error("an unknown graph must not produce a figure")
	}

	f := newFakeNAS(t)
	f.results["reporting.netdata_get_data"] = `[{"name":"memory","legend":["time","available"],"data":[[1760000002,16384]]}]`
	r := New().Fetch(context.Background(), target(f, "s3cret", "", "mem"))
	if r.Data.Mem == nil || r.Data.Mem.Percent != 50 {
		t.Errorf("mem = %+v notes=%v", r.Data.Mem, r.Notes)
	}
	f.results["reporting.netdata_get_data"] = `[{"name":"memory","legend":["time","strange"],"data":[[1760000002,5]]}]`
	r = New().Fetch(context.Background(), target(f, "s3cret", "", "mem"))
	if r.Data.Mem != nil || r.Notes["memory"] == "" || len(r.Failed) != 1 {
		t.Errorf("unrecognised graph: mem=%+v notes=%v failed=%v", r.Data.Mem, r.Notes, r.Failed)
	}
}

func TestFetchAsksOnlyForWhatItNeeds(t *testing.T) {
	f := newFakeNAS(t)
	r := New().Fetch(context.Background(), target(f, "s3cret", "", "alerts"))
	if !r.Online {
		t.Fatal(r.Error)
	}
	for _, m := range f.calls {
		if m != "auth.login_with_api_key" && m != "alert.list" {
			t.Errorf("unexpected call %s", m)
		}
	}
}

func TestLoginVariants(t *testing.T) {
	f := newFakeNAS(t)
	f.username = "homie"
	c := New()
	if r := c.Fetch(context.Background(), target(f, "s3cret", "homie", "alerts")); !r.Online {
		t.Errorf("login_ex with a username: %q", r.Error)
	}
	// A system without login_ex falls back to the older call.
	f.noLoginEx = true
	if r := c.Fetch(context.Background(), target(f, "s3cret", "homie", "alerts")); !r.Online {
		t.Errorf("fallback to login_with_api_key: %q", r.Error)
	}
	f.noLoginEx = false
	// A mistyped username does not block a good key.
	if r := c.Fetch(context.Background(), target(f, "s3cret", "homie_typo", "alerts")); !r.Online {
		t.Errorf("good key, wrong username: %q", r.Error)
	}
	// Whitespace around a pasted key is ignored.
	if r := c.Fetch(context.Background(), target(f, "  s3cret\n", "homie", "alerts")); !r.Online {
		t.Errorf("padded key: %q", r.Error)
	}
	for _, key := range []string{"wrong", ""} {
		r := c.Fetch(context.Background(), target(f, key, "homie", "alerts"))
		if r.Online || !strings.Contains(r.Error, "authentication") {
			t.Errorf("key %q: online=%v error=%q", key, r.Online, r.Error)
		}
	}
}

func TestOneFailingMetricDoesNotHideTheOthers(t *testing.T) {
	f := newFakeNAS(t)
	f.results["alert.list"] = "ERR"
	delete(f.results, "reporting.netdata_get_data")
	r := New().Fetch(context.Background(), target(f, "s3cret", "", "pools", "alerts", "cpu"))
	if !r.Online || len(r.Data.Pools) != 2 {
		t.Fatalf("result = %+v", r)
	}
	if strings.Join(r.Failed, ",") != "alerts,cpu" {
		t.Errorf("failed = %v", r.Failed)
	}
}

func TestPlainHTTPIsRefused(t *testing.T) {
	r := New().Fetch(context.Background(), Target{URL: "http://truenas.local", APIKey: "k", Metrics: []string{"alerts"}})
	if r.Online || !strings.Contains(r.Error, "https") {
		t.Errorf("result = %+v: an API key must never be sent without TLS", r)
	}
}

func TestErrorsNeverRevealTheKey(t *testing.T) {
	r := New().Fetch(context.Background(), Target{URL: "https://127.0.0.1:1", APIKey: "s3cret", Metrics: []string{"alerts"}})
	if r.Online || r.Error == "" || strings.Contains(r.Error, "s3cret") || strings.Contains(r.Error, "127.0.0.1") {
		t.Errorf("result = %+v", r)
	}
}

func TestPing(t *testing.T) {
	f := newFakeNAS(t)
	v, err := New().Ping(context.Background(), target(f, "s3cret", ""))
	if err != nil || v != "TrueNAS-25.10.1" {
		t.Errorf("version = %q, err = %v", v, err)
	}
	if _, err := New().Ping(context.Background(), target(f, "nope", "")); err == nil {
		t.Error("a wrong key was accepted")
	}
}

func TestNotATrueNAS(t *testing.T) {
	srv := httptest.NewTLSServer(http.NotFoundHandler())
	defer srv.Close()
	_, err := New().Ping(context.Background(), Target{URL: srv.URL, APIKey: "k"})
	if err == nil || !strings.Contains(err.Error(), "WebSocket") {
		t.Errorf("err = %v", err)
	}
}
