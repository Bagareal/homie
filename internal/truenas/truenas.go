// Package truenas reads a TrueNAS system through its JSON-RPC 2.0 API over
// WebSocket (TrueNAS 25.04 and later; the REST API is deprecated and goes away
// in TrueNAS 26) and turns it into the small shape the dashboard widgets use.
package truenas

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Metrics are the groups of values that can be read.
var Metrics = []string{"info", "cpu", "cputemp", "mem", "pools", "drives", "alerts", "apps"}

// Target is one TrueNAS system and which of the Metrics to read.
type Target struct {
	URL      string // https://host[:port]
	Username string // optional; selects the newer login call
	APIKey   string
	Metrics  []string
}

type Usage struct {
	Percent float64 `json:"percent"`
	Used    float64 `json:"used"`
	Total   float64 `json:"total"`
}

type Load struct {
	Min1  float64 `json:"min1"`
	Min5  float64 `json:"min5"`
	Min15 float64 `json:"min15"`
	Cores int     `json:"cores"`
}

// Scan is the last scrub or resilver of a pool.
type Scan struct {
	Function string  `json:"function,omitempty"`
	State    string  `json:"state,omitempty"`
	End      string  `json:"end,omitempty"` // RFC 3339
	Percent  float64 `json:"percent,omitempty"`
	Errors   int     `json:"errors,omitempty"`
}

type Pool struct {
	Name    string  `json:"name"`
	Status  string  `json:"status"`
	Healthy bool    `json:"healthy"`
	Percent float64 `json:"percent"`
	Used    float64 `json:"used"`
	Total   float64 `json:"total"`
	Scan    *Scan   `json:"scan,omitempty"`
}

// Temp is one temperature reading in degrees Celsius.
type Temp struct {
	Label string  `json:"label"`
	Value float64 `json:"value"`
}

// Drive is one disk with its temperature (Temp is nil when it reports none).
type Drive struct {
	Name  string   `json:"name"`
	Model string   `json:"model,omitempty"`
	Pool  string   `json:"pool,omitempty"`
	Temp  *float64 `json:"temp,omitempty"`
}

type Alert struct {
	Level string `json:"level"`
	Text  string `json:"text"`
}

// Alerts counts the alerts nobody has dismissed.
type Alerts struct {
	Count int     `json:"count"`
	Level string  `json:"level,omitempty"` // the most severe
	Items []Alert `json:"items,omitempty"`
}

// Apps counts the installed apps.
type Apps struct {
	Running int `json:"running"`
	Total   int `json:"total"`
	// Stopped names the apps that are not running (up to 10).
	Stopped []string `json:"stopped,omitempty"`
}

// Data holds the values that were asked for and could be read.
type Data struct {
	Version  string   `json:"version,omitempty"`
	Hostname string   `json:"hostname,omitempty"`
	Uptime   *string  `json:"uptime,omitempty"`
	Load     *Load    `json:"load,omitempty"`
	CPU      *float64 `json:"cpu,omitempty"`
	// Temp is the CPU temperature (average of the cores); Sensors adds each core.
	Temp    *Temp   `json:"temp,omitempty"`
	Sensors []Temp  `json:"sensors,omitempty"`
	Mem     *Usage  `json:"mem,omitempty"`
	Pools   []Pool  `json:"pools,omitempty"`
	Drives  []Drive `json:"drives,omitempty"`
	Alerts  *Alerts `json:"alerts,omitempty"`
	Apps    *Apps   `json:"apps,omitempty"`
}

// Result is what the dashboard receives for one widget.
type Result struct {
	Online    bool      `json:"online"`
	Error     string    `json:"error,omitempty"`
	UpdatedAt time.Time `json:"updatedAt"`
	Data      Data      `json:"data"`
	Failed    []string  `json:"failed,omitempty"`
	// Notes explain why a value could not be read (what the system answered
	// instead of what was expected). Meant for troubleshooting.
	Notes map[string]string `json:"notes,omitempty"`
}

// Client reads TrueNAS systems. The zero value is ready to use.
type Client struct{}

func New() *Client { return &Client{} }

func parseHTTPS(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return nil, errors.New("invalid address")
	}
	if u.Scheme != "https" {
		// TrueNAS revokes an API key that is sent without TLS, so never try.
		return nil, errors.New("TrueNAS needs an https:// address (an API key sent over plain http is revoked by TrueNAS)")
	}
	return u, nil
}

const timeout = 12 * time.Second

// session connects and logs in.
func (c *Client) session(ctx context.Context, t Target) (*wsConn, error) {
	host, path, err := hostPort(t.URL)
	if err != nil {
		return nil, err
	}
	// The deadline is copied onto the connection, so it outlives this context.
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, err := dialWS(ctx, host, path)
	if err != nil {
		return nil, describe(err)
	}
	if err := login(conn, t); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

var errAuth = errors.New("authentication failed: check the API key and its user, that the key is not revoked in TrueNAS, and that the address is the NAS itself, not a reverse proxy")

// login uses the newer call when a username is given, the older otherwise.
// If that one fails it tries the other too (a mistyped username must not
// block a key that is good on its own), and only then gives up.
func login(c *wsConn, t Target) error {
	t.APIKey = strings.TrimSpace(t.APIKey)
	t.Username = strings.TrimSpace(t.Username)
	tryEx := func() (bool, error) {
		var r struct {
			ResponseType string `json:"response_type"`
		}
		err := c.call("auth.login_ex", []any{map[string]any{
			"mechanism": "API_KEY_PLAIN", "username": t.Username, "api_key": t.APIKey,
			"login_options": map[string]any{"user_info": false},
		}}, &r)
		return r.ResponseType == "SUCCESS", err
	}
	tryKey := func() (bool, error) {
		var ok bool
		err := c.call("auth.login_with_api_key", []any{t.APIKey}, &ok)
		return ok, err
	}
	first, second := tryKey, tryEx
	if t.Username != "" {
		first, second = tryEx, tryKey
	}
	for _, try := range []func() (bool, error){first, second} {
		ok, err := try()
		if ok {
			return nil
		}
		var re *rpcError
		if err != nil && !errors.As(err, &re) {
			return describe(err) // the connection broke: no point trying again
		}
	}
	return errAuth
}

// Ping connects, logs in and returns the TrueNAS version.
func (c *Client) Ping(ctx context.Context, t Target) (string, error) {
	conn, err := c.session(ctx, t)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	var v string
	if err := conn.call("system.version", []any{}, &v); err != nil {
		return "", nil // connected and logged in; the version is a nicety
	}
	return v, nil
}

// Fetch reads the requested metrics. One that fails is listed in Failed
// without hiding the others; if the connection or login fails the result is
// offline with the reason in Error.
func (c *Client) Fetch(ctx context.Context, t Target) Result {
	r := Result{UpdatedAt: time.Now()}
	conn, err := c.session(ctx, t)
	if err != nil {
		r.Error = err.Error()
		return r
	}
	defer conn.Close()

	want := map[string]bool{}
	for _, m := range t.Metrics {
		want[m] = true
	}
	failed := map[string]bool{}
	var info sysInfo
	// "mem" needs the installed memory, which system.info reports.
	if want["info"] || want["mem"] {
		if err := conn.call("system.info", []any{}, &info); err != nil {
			failed["info"] = true
			if want["mem"] {
				failed["mem"] = true
			}
		} else {
			r.Data.Version, r.Data.Hostname = info.Version, info.Hostname
			up := formatUptime(info.UptimeSeconds)
			r.Data.Uptime = &up
			r.Data.Load = info.load()
		}
	}
	if want["cpu"] || (want["mem"] && !failed["mem"]) {
		notes := map[string]string{}
		cpu, mem, err := readReporting(conn, want["cpu"], want["mem"], info.PhysMem, notes)
		if len(notes) > 0 {
			r.Notes = notes
		}
		if err != nil {
			if want["cpu"] {
				failed["cpu"] = true
			}
			if want["mem"] {
				failed["mem"] = true
			}
		} else {
			r.Data.CPU, r.Data.Mem = cpu, mem
			if want["cpu"] && cpu == nil {
				failed["cpu"] = true
			}
			if want["mem"] && mem == nil {
				failed["mem"] = true
			}
		}
	}
	if want["cputemp"] {
		notes := map[string]string{}
		temps, err := readCPUTemp(conn, notes)
		for k, v := range notes {
			if r.Notes == nil {
				r.Notes = map[string]string{}
			}
			r.Notes[k] = v
		}
		if err != nil || len(temps) == 0 {
			failed["cputemp"] = true
		} else {
			r.Data.Sensors = temps
			r.Data.Temp = &r.Data.Sensors[0]
		}
	}
	if want["pools"] {
		if pools, err := readPools(conn); err != nil {
			failed["pools"] = true
		} else {
			r.Data.Pools = pools
		}
	}
	if want["drives"] {
		if drives, err := readDrives(conn); err != nil {
			failed["drives"] = true
		} else {
			r.Data.Drives = drives
		}
	}
	if want["alerts"] {
		if alerts, err := readAlerts(conn); err != nil {
			failed["alerts"] = true
		} else {
			r.Data.Alerts = alerts
		}
	}

	if want["apps"] {
		if apps, err := readApps(conn); err != nil {
			failed["apps"] = true
		} else {
			r.Data.Apps = apps
		}
	}

	for _, m := range t.Metrics {
		if failed[m] {
			r.Failed = append(r.Failed, m)
		}
	}
	r.Online = true
	return r
}

// ---- system.info

type sysInfo struct {
	Version       string  `json:"version"`
	Hostname      string  `json:"hostname"`
	PhysMem       float64 `json:"physmem"`
	Cores         int     `json:"cores"`
	UptimeSeconds float64 `json:"uptime_seconds"`
	Loadavg       []any   `json:"loadavg"`
}

func (s sysInfo) load() *Load {
	if len(s.Loadavg) < 3 {
		return nil
	}
	var v [3]float64
	for i := range v {
		f, ok := s.Loadavg[i].(float64)
		if !ok {
			return nil
		}
		v[i] = f
	}
	return &Load{Min1: v[0], Min5: v[1], Min15: v[2], Cores: s.Cores}
}

func formatUptime(secs float64) string {
	s := int(secs)
	d, h, m, sec := s/86400, s%86400/3600, s%3600/60, s%60
	if d > 0 {
		unit := "days"
		if d == 1 {
			unit = "day"
		}
		return fmt.Sprintf("%d %s, %d:%02d:%02d", d, unit, h, m, sec)
	}
	return fmt.Sprintf("%d:%02d:%02d", h, m, sec)
}

// ---- pools

func readPools(c *wsConn) ([]Pool, error) {
	var raw []struct {
		Name      string          `json:"name"`
		Status    string          `json:"status"`
		Healthy   bool            `json:"healthy"`
		Size      *float64        `json:"size"`
		Allocated *float64        `json:"allocated"`
		Scan      json.RawMessage `json:"scan"`
	}
	opts := map[string]any{"select": []string{"name", "status", "healthy", "size", "allocated", "scan"}}
	if err := c.call("pool.query", []any{[]any{}, opts}, &raw); err != nil {
		return nil, err
	}
	pools := make([]Pool, 0, len(raw))
	for _, p := range raw {
		out := Pool{Name: p.Name, Status: p.Status, Healthy: p.Healthy, Scan: parseScan(p.Scan)}
		if p.Size != nil && p.Allocated != nil && *p.Size > 0 {
			out.Total, out.Used = *p.Size, *p.Allocated
			out.Percent = math.Round(*p.Allocated / *p.Size * 1000) / 10
		}
		pools = append(pools, out)
	}
	sort.SliceStable(pools, func(i, j int) bool { return pools[i].Name < pools[j].Name })
	return pools, nil
}

// parseScan reads the scrub/resilver object. TrueNAS sends dates as
// {"$date": <milliseconds>}; a plain number or string is accepted too.
func parseScan(raw json.RawMessage) *Scan {
	var s struct {
		Function   *string         `json:"function"`
		State      *string         `json:"state"`
		EndTime    json.RawMessage `json:"end_time"`
		Percentage *float64        `json:"percentage"`
		Errors     *float64        `json:"errors"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &s) != nil || (s.Function == nil && s.State == nil) {
		return nil
	}
	out := &Scan{}
	if s.Function != nil {
		out.Function = *s.Function
	}
	if s.State != nil {
		out.State = *s.State
	}
	if s.Percentage != nil {
		out.Percent = *s.Percentage
	}
	if s.Errors != nil {
		out.Errors = int(*s.Errors)
	}
	if t, ok := parseDate(s.EndTime); ok {
		out.End = t.UTC().Format(time.RFC3339)
	}
	return out
}

func parseDate(raw json.RawMessage) (time.Time, bool) {
	var o struct {
		Date float64 `json:"$date"`
	}
	if json.Unmarshal(raw, &o) == nil && o.Date > 0 {
		return time.UnixMilli(int64(o.Date)), true
	}
	var n float64
	if json.Unmarshal(raw, &n) == nil && n > 0 {
		return time.Unix(int64(n), 0), true
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// ---- drives

func readDrives(c *wsConn) ([]Drive, error) {
	var disks []struct {
		Name  string  `json:"name"`
		Model *string `json:"model"`
		Pool  *string `json:"pool"`
	}
	// TrueNAS leaves "pool" empty unless the pools are asked for.
	opts := map[string]any{"select": []string{"name", "model", "pool"}, "extra": map[string]any{"pools": true}}
	if err := c.call("disk.query", []any{[]any{}, opts}, &disks); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(disks))
	for _, d := range disks {
		names = append(names, d.Name)
	}
	temps := map[string]any{}
	if len(names) > 0 {
		// Temperatures are a bonus: a system without them still lists its disks.
		_ = c.call("disk.temperatures", []any{names, false}, &temps)
	}
	out := make([]Drive, 0, len(disks))
	for _, d := range disks {
		dr := Drive{Name: d.Name}
		if d.Model != nil {
			dr.Model = *d.Model
		}
		if d.Pool != nil {
			dr.Pool = *d.Pool
		}
		if v, ok := temps[d.Name].(float64); ok {
			dr.Temp = &v
		}
		out = append(out, dr)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// ---- alerts

var levelRank = map[string]int{"INFO": 0, "NOTICE": 1, "WARNING": 2, "ERROR": 3, "CRITICAL": 4, "ALERT": 5, "EMERGENCY": 6}

func readAlerts(c *wsConn) (*Alerts, error) {
	var raw []struct {
		Level     string `json:"level"`
		Text      string `json:"text"`
		Formatted string `json:"formatted"`
		Dismissed bool   `json:"dismissed"`
	}
	if err := c.call("alert.list", []any{}, &raw); err != nil {
		return nil, err
	}
	out := &Alerts{}
	for _, a := range raw {
		if a.Dismissed {
			continue
		}
		out.Count++
		if out.Level == "" || levelRank[a.Level] > levelRank[out.Level] {
			out.Level = a.Level
		}
		if len(out.Items) < 5 {
			text := a.Text
			if text == "" {
				text = a.Formatted
			}
			if len(text) > 200 {
				text = text[:200] + "…"
			}
			out.Items = append(out.Items, Alert{Level: a.Level, Text: text})
		}
	}
	sort.SliceStable(out.Items, func(i, j int) bool { return levelRank[out.Items[i].Level] > levelRank[out.Items[j].Level] })
	return out, nil
}

// ---- apps

func readApps(c *wsConn) (*Apps, error) {
	var raw []struct {
		Name  string `json:"name"`
		State string `json:"state"`
	}
	if err := c.call("app.query", []any{[]any{}, map[string]any{"select": []string{"name", "state"}}}, &raw); err != nil {
		return nil, err
	}
	out := &Apps{Total: len(raw)}
	for _, a := range raw {
		if strings.EqualFold(a.State, "RUNNING") {
			out.Running++
		} else if len(out.Stopped) < 10 {
			out.Stopped = append(out.Stopped, a.Name)
		}
	}
	sort.Strings(out.Stopped)
	return out, nil
}

// ---- cpu and memory (reporting graphs)

// readReporting reads the latest CPU load and memory use from the reporting
// graphs of the last two minutes. The graph layout is not formally documented,
// so it is read by the names of the series and any failure just leaves the
// value out.
func readReporting(c *wsConn, wantCPU, wantMem bool, physmem float64, notes map[string]string) (cpu *float64, mem *Usage, err error) {
	var graphs []map[string]any
	if wantCPU {
		graphs = append(graphs, map[string]any{"name": "cpu"})
	}
	if wantMem {
		graphs = append(graphs, map[string]any{"name": "memory"})
	}
	now := time.Now().Unix()
	query := map[string]any{"start": now - 120, "end": now, "aggregate": false}
	var res []struct {
		Name   string   `json:"name"`
		Legend []string `json:"legend"`
		Data   [][]any  `json:"data"`
	}
	if err := c.call("reporting.netdata_get_data", []any{graphs, query}, &res); err != nil {
		return nil, nil, err
	}
	for _, g := range res {
		series, ok := latest(g.Legend, g.Data)
		if !ok {
			notes[g.Name] = fmt.Sprintf("no usable data point: legend=%v, %d rows", g.Legend, len(g.Data))
			continue
		}
		switch g.Name {
		case "cpu":
			var sum float64
			for name, v := range series {
				if name != "idle" {
					sum += v
				}
			}
			sum = math.Round(math.Min(100, math.Max(0, sum))*10) / 10
			cpu = &sum
		case "memory":
			if u := memoryUsage(series, physmem); u != nil {
				mem = u
			} else {
				notes["memory"] = fmt.Sprintf("unrecognised memory graph: series=%v, installed=%.0f", series, physmem)
			}
		}
	}
	return cpu, mem, nil
}

// readCPUTemp reads the newest CPU temperatures from the "cputemp" graph (one
// series per core). The first result is the average of the cores, followed by
// each core. A system without sensors (a virtual machine) reports nothing.
func readCPUTemp(c *wsConn, notes map[string]string) ([]Temp, error) {
	now := time.Now().Unix()
	query := map[string]any{"start": now - 120, "end": now, "aggregate": false}
	var res []struct {
		Name   string   `json:"name"`
		Legend []string `json:"legend"`
		Data   [][]any  `json:"data"`
	}
	if err := c.call("reporting.netdata_get_data", []any{[]any{map[string]any{"name": "cputemp"}}, query}, &res); err != nil {
		return nil, err
	}
	for _, g := range res {
		if g.Name != "cputemp" {
			continue
		}
		series, ok := latest(g.Legend, g.Data)
		if !ok {
			notes["cputemp"] = fmt.Sprintf("no usable data point: legend=%v, %d rows", g.Legend, len(g.Data))
			return nil, nil
		}
		names := make([]string, 0, len(series))
		var sum float64
		for name, v := range series {
			names = append(names, name)
			sum += v
		}
		sort.Slice(names, func(i, j int) bool { return coreOrder(names[i]) < coreOrder(names[j]) })
		out := []Temp{{Label: "CPU (average)", Value: math.Round(sum/float64(len(series))*10) / 10}}
		for _, n := range names {
			out = append(out, Temp{Label: "Core " + strings.TrimPrefix(n, "cpu"), Value: math.Round(series[n]*10) / 10})
		}
		return out, nil
	}
	notes["cputemp"] = "the system returned no cputemp graph (no temperature sensors?)"
	return nil, nil
}

// coreOrder makes "0", "2", "10" and "cpu2" sort by their number.
func coreOrder(name string) string {
	n := strings.TrimPrefix(name, "cpu")
	return strings.Repeat("0", 6-min(6, len(n))) + n
}

// memoryUsage turns the memory graph into a usage figure. TrueNAS documents it
// as the physical memory *available*, so used = installed - available; a graph
// that lists used memory next to free/cached/buffers is read as a share. The
// values are mebibytes (as in netdata) or bytes; which one is told by comparing
// them with the installed memory.
func memoryUsage(series map[string]float64, physmem float64) *Usage {
	if used, ok := series["used"]; ok {
		var sum float64
		for _, v := range series {
			sum += v
		}
		if sum <= 0 || physmem <= 0 {
			return nil
		}
		pct := math.Round(used/sum*1000) / 10
		return &Usage{Percent: pct, Used: physmem * pct / 100, Total: physmem}
	}
	avail, ok := series["available"]
	if !ok {
		avail, ok = series["free"]
	}
	if !ok || physmem <= 0 {
		return nil
	}
	if avail*(1<<20) <= physmem*1.05 {
		avail *= 1 << 20 // mebibytes
	}
	if avail > physmem {
		avail = physmem
	}
	used := physmem - avail
	return &Usage{Percent: math.Round(used/physmem*1000) / 10, Used: used, Total: physmem}
}

// latest returns the newest data point as series name -> value. A point is a
// row of numbers, usually led by the timestamp ("time" in the legend).
func latest(legend []string, rows [][]any) (map[string]float64, bool) {
	for i := len(rows) - 1; i >= 0; i-- {
		row := rows[i]
		if len(row) != len(legend) {
			continue
		}
		out := map[string]float64{}
		complete := true
		for j, name := range legend {
			if strings.EqualFold(name, "time") || strings.EqualFold(name, "timestamp") {
				continue
			}
			v, ok := row[j].(float64)
			if !ok {
				complete = false
				break
			}
			out[strings.ToLower(name)] = v
		}
		if complete && len(out) > 0 {
			return out, true
		}
	}
	return nil, false
}

func describe(err error) error {
	var timeout interface{ Timeout() bool }
	switch {
	case errors.Is(err, context.Canceled):
		return errors.New("cancelled")
	case errors.As(err, &timeout) && timeout.Timeout():
		return errors.New("no response in time")
	case errors.Is(err, errNotWebSocket):
		return errors.New("not a TrueNAS 25.04+ system (no WebSocket API at /api/current)")
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "no such host"):
		return errors.New("host name not found")
	case strings.Contains(msg, "tls:") || strings.Contains(msg, "certificate"):
		return errors.New("TLS connection failed (is the address https://?)")
	}
	return errors.New("connection failed")
}
