// Package glances reads system metrics from a Glances web server
// (`glances -w`) through its REST API and turns them into one small,
// version-independent shape for the dashboard widgets.
package glances

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

// maxBody bounds a single API response; the largest ones (fs, network) are a
// few kilobytes.
const maxBody = 2 << 20

// maxDisks keeps a host with dozens of ZFS datasets from flooding a widget.
const maxDisks = 16

// virtualPrefixes are interface names that carry traffic already counted on a
// physical card (bridges, VM taps, container links), so adding them up would
// count the same bytes several times. Used only when no filter is given.
var virtualPrefixes = []string{"lo", "veth", "tap", "fwbr", "fwpr", "fwln", "docker", "br-", "virbr", "vmbr"}

// Target is one Glances server and what to read from it.
type Target struct {
	URL        string
	Username   string
	Password   string
	Metrics    []string
	Mounts     []string
	Interfaces []string
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

type Temp struct {
	Label string  `json:"label"`
	Value float64 `json:"value"`
}

type Disk struct {
	Mount string `json:"mount"`
	Usage
}

type Net struct {
	Rx float64 `json:"rx"` // bytes per second received
	Tx float64 `json:"tx"` // bytes per second sent
}

// Data holds the metrics that were asked for and could be read.
type Data struct {
	CPU     *float64 `json:"cpu,omitempty"`
	Mem     *Usage   `json:"mem,omitempty"`
	Swap    *Usage   `json:"swap,omitempty"`
	Load    *Load    `json:"load,omitempty"`
	Uptime  *string  `json:"uptime,omitempty"`
	Temp    *Temp    `json:"temp,omitempty"`
	Disks   []Disk   `json:"disks,omitempty"`
	Network *Net     `json:"network,omitempty"`
}

// Result is what the dashboard receives for one widget.
type Result struct {
	Online    bool      `json:"online"`
	Error     string    `json:"error,omitempty"`
	UpdatedAt time.Time `json:"updatedAt"`
	Data      Data      `json:"data"`
	// Failed lists the requested metrics the server could not provide (for
	// example sensors on a host without them).
	Failed []string `json:"failed,omitempty"`
}

// Client talks to Glances servers. The zero value is not usable; use New.
type Client struct {
	http *http.Client
	mu   sync.Mutex
	api  map[string]string // base URL -> working API prefix ("/api/4" or "/api/3")
}

func New() *Client {
	return &Client{
		api: map[string]string{},
		http: &http.Client{
			Timeout: 5 * time.Second,
			Transport: &http.Transport{
				// In a homelab, self-signed certificates are the norm.
				TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
				MaxIdleConnsPerHost: 2,
				IdleConnTimeout:     60 * time.Second,
			},
			// Never follow a redirect: it could carry the credentials elsewhere.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

// Ping checks that the target is a reachable Glances server and returns its
// version.
func (c *Client) Ping(ctx context.Context, t Target) (string, error) {
	c.forget(t)
	prefix, version, err := c.detect(ctx, t)
	if err != nil {
		return "", err
	}
	c.remember(t, prefix)
	return version, nil
}

// Fetch reads the requested metrics. Metrics are read concurrently; one that
// fails is listed in Result.Failed without hiding the others. If none can be
// read the result is offline, with the reason in Error.
func (c *Client) Fetch(ctx context.Context, t Target) Result {
	r := Result{UpdatedAt: time.Now()}
	prefix, err := c.prefix(ctx, t)
	if err != nil {
		r.Error = err.Error()
		return r
	}

	var (
		mu   sync.Mutex
		wg   sync.WaitGroup
		errs = map[string]error{}
	)
	for _, m := range t.Metrics {
		wg.Add(1)
		go func(m string) {
			defer wg.Done()
			err := c.read(ctx, t, prefix, m, &r.Data, &mu)
			if err != nil {
				mu.Lock()
				errs[m] = err
				mu.Unlock()
			}
		}(m)
	}
	wg.Wait()

	for _, m := range t.Metrics { // keep the configured order
		if errs[m] != nil {
			r.Failed = append(r.Failed, m)
		}
	}
	if len(t.Metrics) > 0 && len(r.Failed) == len(t.Metrics) {
		// Everything failed: the server may have changed (e.g. upgraded), so
		// detect the API version again next time.
		c.forget(t)
		r.Error = errs[t.Metrics[0]].Error()
		return r
	}
	r.Online = true
	return r
}

func (c *Client) read(ctx context.Context, t Target, prefix, metric string, d *Data, mu *sync.Mutex) error {
	switch metric {
	case "cpu":
		var v struct {
			Total *float64 `json:"total"`
		}
		if err := c.get(ctx, t, prefix, "cpu", &v); err != nil {
			return err
		}
		if v.Total == nil {
			return errors.New("unexpected response")
		}
		mu.Lock()
		d.CPU = v.Total
		mu.Unlock()
	case "mem", "swap":
		endpoint := "mem"
		if metric == "swap" {
			endpoint = "memswap"
		}
		var v Usage
		if err := c.get(ctx, t, prefix, endpoint, &v); err != nil {
			return err
		}
		mu.Lock()
		if metric == "mem" {
			d.Mem = &v
		} else {
			d.Swap = &v
		}
		mu.Unlock()
	case "load":
		var v struct {
			Min1  float64 `json:"min1"`
			Min5  float64 `json:"min5"`
			Min15 float64 `json:"min15"`
			Cores int     `json:"cpucore"`
		}
		if err := c.get(ctx, t, prefix, "load", &v); err != nil {
			return err
		}
		mu.Lock()
		d.Load = &Load{Min1: v.Min1, Min5: v.Min5, Min15: v.Min15, Cores: v.Cores}
		mu.Unlock()
	case "uptime":
		var raw json.RawMessage
		if err := c.get(ctx, t, prefix, "uptime", &raw); err != nil {
			return err
		}
		s, ok := parseUptime(raw)
		if !ok {
			return errors.New("unexpected response")
		}
		mu.Lock()
		d.Uptime = &s
		mu.Unlock()
	case "temp":
		var v []sensor
		if err := c.get(ctx, t, prefix, "sensors", &v); err != nil {
			return err
		}
		temp := hottest(v)
		if temp == nil {
			return errors.New("no temperature sensors")
		}
		mu.Lock()
		d.Temp = temp
		mu.Unlock()
	case "disks":
		var v []fsEntry
		if err := c.get(ctx, t, prefix, "fs", &v); err != nil {
			return err
		}
		disks := pickDisks(v, t.Mounts)
		mu.Lock()
		d.Disks = disks
		mu.Unlock()
	case "network":
		var v []netEntry
		if err := c.get(ctx, t, prefix, "network", &v); err != nil {
			return err
		}
		n := sumNetwork(v, t.Interfaces)
		mu.Lock()
		d.Network = &n
		mu.Unlock()
	default:
		return fmt.Errorf("unknown metric %q", metric)
	}
	return nil
}

type sensor struct {
	Label string `json:"label"`
	Value any    `json:"value"`
	Unit  string `json:"unit"`
	Type  string `json:"type"`
}

// hottest returns the warmest temperature sensor, or nil if there is none.
func hottest(sensors []sensor) *Temp {
	var best *Temp
	for _, s := range sensors {
		v, ok := s.Value.(float64)
		if !ok || !(strings.HasPrefix(s.Type, "temperature") || s.Unit == "C") {
			continue
		}
		if best == nil || v > best.Value {
			best = &Temp{Label: s.Label, Value: v}
		}
	}
	return best
}

type fsEntry struct {
	Mount   string  `json:"mnt_point"`
	Size    float64 `json:"size"`
	Used    float64 `json:"used"`
	Percent float64 `json:"percent"`
}

func pickDisks(entries []fsEntry, mounts []string) []Disk {
	var out []Disk
	for _, e := range entries {
		if len(mounts) > 0 && !slices.Contains(mounts, e.Mount) {
			continue
		}
		out = append(out, Disk{Mount: e.Mount, Usage: Usage{Percent: e.Percent, Used: e.Used, Total: e.Size}})
		if len(out) == maxDisks {
			break
		}
	}
	return out
}

type netEntry struct {
	Name     string   `json:"interface_name"`
	RxRate   *float64 `json:"bytes_recv_rate_per_sec"`
	TxRate   *float64 `json:"bytes_sent_rate_per_sec"`
	Rx       float64  `json:"rx"` // Glances 3: bytes since the last update
	Tx       float64  `json:"tx"`
	Interval float64  `json:"time_since_update"`
}

func (n netEntry) rates() (rx, tx float64) {
	if n.RxRate != nil && n.TxRate != nil {
		return *n.RxRate, *n.TxRate
	}
	if n.Interval > 0 {
		return n.Rx / n.Interval, n.Tx / n.Interval
	}
	return 0, 0
}

func sumNetwork(entries []netEntry, only []string) Net {
	var out Net
	for _, e := range entries {
		if len(only) > 0 {
			if !slices.Contains(only, e.Name) {
				continue
			}
		} else if isVirtual(e.Name) {
			continue
		}
		rx, tx := e.rates()
		out.Rx += rx
		out.Tx += tx
	}
	return out
}

func isVirtual(name string) bool {
	n := strings.ToLower(name)
	for _, p := range virtualPrefixes {
		if strings.HasPrefix(n, p) {
			return true
		}
	}
	return false
}

// parseUptime accepts both shapes Glances has used: a bare string, or an
// object with an "uptime" field.
func parseUptime(raw json.RawMessage) (string, bool) {
	var s string
	if json.Unmarshal(raw, &s) == nil && s != "" {
		return s, true
	}
	var o struct {
		Uptime string `json:"uptime"`
	}
	if json.Unmarshal(raw, &o) == nil && o.Uptime != "" {
		return o.Uptime, true
	}
	return "", false
}

func baseURL(t Target) string { return strings.TrimRight(t.URL, "/") }

func (c *Client) forget(t Target) {
	c.mu.Lock()
	delete(c.api, baseURL(t))
	c.mu.Unlock()
}

func (c *Client) remember(t Target, prefix string) {
	c.mu.Lock()
	c.api[baseURL(t)] = prefix
	c.mu.Unlock()
}

func (c *Client) prefix(ctx context.Context, t Target) (string, error) {
	c.mu.Lock()
	p := c.api[baseURL(t)]
	c.mu.Unlock()
	if p != "" {
		return p, nil
	}
	p, _, err := c.detect(ctx, t)
	if err != nil {
		return "", err
	}
	c.remember(t, p)
	return p, nil
}

// detect finds which API version the server speaks (Glances 4 or 3).
func (c *Client) detect(ctx context.Context, t Target) (prefix, version string, err error) {
	for _, p := range []string{"/api/4", "/api/3"} {
		var raw json.RawMessage
		err = c.get(ctx, t, p, "status", &raw)
		if err == nil {
			var o struct {
				Version string `json:"version"`
			}
			_ = json.Unmarshal(raw, &o)
			return p, o.Version, nil
		}
		if !errors.Is(err, errNotFound) {
			return "", "", err
		}
	}
	return "", "", errors.New("not a Glances server (is it started with “glances -w”?)")
}

var errNotFound = errors.New("not found")

// get reads one endpoint and decodes its JSON into v. Errors are phrased for
// the person looking at the dashboard and never contain the address or the
// credentials.
func (c *Client) get(ctx context.Context, t Target, prefix, endpoint string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL(t)+prefix+"/"+endpoint, nil)
	if err != nil {
		return errors.New("invalid address")
	}
	req.Header.Set("User-Agent", "homie-widget")
	req.Header.Set("Accept", "application/json")
	if t.Username != "" || t.Password != "" {
		req.SetBasicAuth(t.Username, t.Password)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return describe(err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return errors.New("authentication failed (check the username and password)")
	case resp.StatusCode == http.StatusNotFound:
		return errNotFound
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("the server answered HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(v); err != nil {
		return errors.New("unexpected response (not Glances JSON)")
	}
	return nil
}

func describe(err error) error {
	var timeout interface{ Timeout() bool }
	switch {
	case errors.Is(err, context.Canceled):
		return errors.New("cancelled")
	case errors.As(err, &timeout) && timeout.Timeout():
		return errors.New("no response within 5 seconds")
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return errors.New("host name not found")
	}
	return errors.New("connection failed")
}
