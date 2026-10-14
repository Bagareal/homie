// Package pbs reads a Proxmox Backup Server through its REST API with an API
// token and turns it into the small shape the dashboard widgets use.
//
// The server answers for itself only (it is not a cluster): the datastores and
// their backup groups, the host's own load and the recent tasks.
package pbs

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// Metrics are the groups of values that can be read.
var Metrics = []string{"version", "usage", "groups", "host", "tasks"}

// Target is one Proxmox Backup Server and which of the Metrics to read.
type Target struct {
	URL     string // https://host:8007
	TokenID string // user@realm!name
	Secret  string
	Metrics []string
}

type Usage struct {
	Percent float64 `json:"percent"`
	Used    float64 `json:"used"`
	Total   float64 `json:"total"`
}

// Datastore is one datastore with its space. FullAt is when PBS expects it to
// be full, from the growth of the last months (empty when it is not growing).
type Datastore struct {
	Name   string     `json:"name"`
	Error  string     `json:"error,omitempty"`
	FullAt *time.Time `json:"fullAt,omitempty"`
	Usage
}

// Group is one backup group (a VM, a container or a host) and its latest backup.
type Group struct {
	Store   string    `json:"store"`
	Type    string    `json:"type"` // vm, ct or host
	ID      string    `json:"id"`
	Comment string    `json:"comment,omitempty"`
	Last    time.Time `json:"last,omitempty"`
	Count   int       `json:"count"`
}

// Backup is the most recent backup task.
type Backup struct {
	Status  string    `json:"status"` // ok, warnings, failed, running
	End     time.Time `json:"end,omitempty"`
	Node    string    `json:"node,omitempty"` // what it backed up, store:vm/100
	Message string    `json:"message,omitempty"`
}

type Task struct {
	Type   string    `json:"type"`
	Node   string    `json:"node"` // the worker id: the datastore, or store:group
	Status string    `json:"status"`
	End    time.Time `json:"end"`
}

// Tasks counts the tasks that failed in the last 24 hours.
type Tasks struct {
	Failed int    `json:"failed"`
	Items  []Task `json:"items,omitempty"`
}

// Data holds the values that were asked for and could be read.
type Data struct {
	Version    string      `json:"version,omitempty"`
	CPU        *float64    `json:"cpu,omitempty"` // percent
	Mem        *Usage      `json:"mem,omitempty"`
	Uptime     *string     `json:"uptime,omitempty"`
	Datastores []Datastore `json:"datastores,omitempty"`
	Groups     []Group     `json:"groups,omitempty"`
	Backup     *Backup     `json:"backup,omitempty"`
	Tasks      *Tasks      `json:"tasks,omitempty"`
}

// Result is what the dashboard receives for one widget.
type Result struct {
	Online    bool              `json:"online"`
	Error     string            `json:"error,omitempty"`
	UpdatedAt time.Time         `json:"updatedAt"`
	Data      Data              `json:"data"`
	Failed    []string          `json:"failed,omitempty"`
	Notes     map[string]string `json:"notes,omitempty"`
}

// Client reads Proxmox Backup Servers.
type Client struct{ http *http.Client }

func New() *Client {
	return &Client{http: &http.Client{
		Timeout: 8 * time.Second,
		Transport: &http.Transport{
			// Proxmox Backup Server ships with a self-signed certificate.
			TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
			MaxIdleConnsPerHost: 2,
			IdleConnTimeout:     60 * time.Second,
		},
		// Never follow a redirect: it could carry the token elsewhere.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

var errAuth = errors.New("authentication failed: check the token id (user@realm!name) and its secret")
var errPerm = errors.New("permission denied: give the user, and the token itself, the DatastoreAudit role on /datastore and the Audit role on /system")

func (c *Client) get(ctx context.Context, t Target, path string, v any) error {
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(t.URL), "/") + "/api2/json" + path)
	if err != nil || u.Host == "" {
		return errors.New("invalid address")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	// Unlike Proxmox VE, the secret follows a colon.
	req.Header.Set("Authorization", "PBSAPIToken="+strings.TrimSpace(t.TokenID)+":"+strings.TrimSpace(t.Secret))
	resp, err := c.http.Do(req)
	if err != nil {
		return describe(err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return errAuth
	case resp.StatusCode == http.StatusForbidden:
		return errPerm
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		// A plain http address is answered with a redirect to https. It is not followed
		// (it could carry the token elsewhere), but the user is told where it goes.
		if loc := resp.Header.Get("Location"); loc != "" {
			return fmt.Errorf("the address redirects to %s: Proxmox Backup Server speaks https, so use https:// and its port (8007)", loc)
		}
		return fmt.Errorf("the server answered %s: Proxmox Backup Server speaks https on port 8007", resp.Status)
	case resp.StatusCode >= 300:
		return fmt.Errorf("the server answered %s (is this the Proxmox Backup Server address, port 8007?)", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return err
	}
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return errors.New("this does not look like the Proxmox Backup Server API (is the port 8007?)")
	}
	return json.Unmarshal(env.Data, v)
}

func describe(err error) error {
	var ne interface{ Timeout() bool }
	if errors.As(err, &ne) && ne.Timeout() {
		return errors.New("timed out: the server did not answer")
	}
	msg := err.Error()
	if i := strings.LastIndex(msg, ": "); i >= 0 {
		msg = msg[i+2:]
	}
	return fmt.Errorf("cannot reach the server: %s", msg)
}

type versionInfo struct {
	Version string `json:"version"`
	Release string `json:"release"`
}

func (v versionInfo) String() string {
	if v.Release != "" {
		return v.Version + "-" + v.Release
	}
	return v.Version
}

// Ping checks the address and the token, and returns the server's version.
func (c *Client) Ping(ctx context.Context, t Target) (string, error) {
	var v versionInfo
	if err := c.get(ctx, t, "/version", &v); err != nil {
		return "", err
	}
	return v.String(), nil
}

// Fetch reads what the metrics ask for. The version decides whether the server
// counts as online (it is always read, and it needs no permission beyond the
// token); the other groups fail on their own.
func (c *Client) Fetch(ctx context.Context, t Target) Result {
	res := Result{UpdatedAt: time.Now().UTC()}
	want := map[string]bool{"version": true}
	for _, m := range t.Metrics {
		want[m] = true
	}
	var v versionInfo
	if err := c.get(ctx, t, "/version", &v); err != nil {
		res.Error = err.Error()
		return res
	}
	res.Online = true
	res.Data.Version = v.String()

	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		failed  = map[string]error{}
		usage   []usageEntry
		host    hostEntry
		tasks   []taskEntry
		groups  []Group
		perName = map[string]error{}
	)
	run := func(name string, f func() error) {
		if !want[name] {
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := f(); err != nil {
				mu.Lock()
				failed[name] = err
				mu.Unlock()
			}
		}()
	}
	// The groups need the datastores' names, so they wait for the usage.
	var usageErr error
	if want["usage"] || want["groups"] {
		usageErr = c.get(ctx, t, "/status/datastore-usage", &usage)
		if usageErr != nil {
			if want["usage"] {
				failed["usage"] = usageErr
			}
			if want["groups"] {
				failed["groups"] = usageErr
			}
		}
	}
	run("host", func() error { return c.get(ctx, t, "/nodes/localhost/status", &host) })
	run("tasks", func() error { return c.get(ctx, t, "/nodes/localhost/tasks?limit=500", &tasks) })
	if want["groups"] && usageErr == nil {
		for _, u := range usage {
			if u.Error != "" {
				continue
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				var list []groupEntry
				err := c.get(ctx, t, "/admin/datastore/"+url.PathEscape(u.Store)+"/groups", &list)
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					perName[u.Store] = err
					return
				}
				for _, g := range list {
					groups = append(groups, Group{Store: u.Store, Type: g.Type, ID: g.ID, Comment: g.Comment, Last: epoch(g.Last), Count: g.Count})
				}
			}()
		}
	}
	wg.Wait()

	now := time.Now()
	if want["usage"] && usageErr == nil {
		res.Data.Datastores = datastoresOf(usage, now)
	}
	if want["groups"] && usageErr == nil {
		if len(perName) > 0 && len(groups) == 0 {
			// Every datastore refused: say why once.
			for _, err := range perName {
				failed["groups"] = err
				break
			}
		}
		sort.SliceStable(groups, func(i, j int) bool {
			a, b := groups[i], groups[j]
			if a.Store != b.Store {
				return a.Store < b.Store
			}
			if a.Type != b.Type {
				return a.Type < b.Type
			}
			return a.ID < b.ID
		})
		res.Data.Groups = groups
	}
	if want["host"] && failed["host"] == nil {
		cpu := host.CPU * 100
		res.Data.CPU = &cpu
		if host.Memory.Total > 0 {
			m := percentOf(host.Memory.Used, host.Memory.Total)
			res.Data.Mem = &m
		}
		up := uptime(host.Uptime)
		res.Data.Uptime = &up
	}
	if want["tasks"] && failed["tasks"] == nil {
		res.Data.Backup, res.Data.Tasks = tasksOf(tasks, now)
	}
	names := make([]string, 0, len(failed))
	for k := range failed {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		res.Failed = append(res.Failed, k)
		if res.Notes == nil {
			res.Notes = map[string]string{}
		}
		res.Notes[k] = failed[k].Error()
	}
	return res
}

func percentOf(used, total float64) Usage {
	u := Usage{Used: used, Total: total}
	if total > 0 {
		u.Percent = used / total * 100
	}
	return u
}

func epoch(sec float64) time.Time {
	if sec <= 0 {
		return time.Time{}
	}
	return time.Unix(int64(sec), 0).UTC()
}

type usageEntry struct {
	Store string  `json:"store"`
	Total float64 `json:"total"`
	Used  float64 `json:"used"`
	Error string  `json:"error"`
	// The date PBS expects the datastore to be full, when it grows.
	Full float64 `json:"estimated-full-date"`
}

func datastoresOf(list []usageEntry, now time.Time) []Datastore {
	out := make([]Datastore, 0, len(list))
	for _, u := range list {
		d := Datastore{Name: u.Store, Error: u.Error, Usage: percentOf(u.Used, u.Total)}
		if f := epoch(u.Full); !f.IsZero() && f.After(now) {
			d.FullAt = &f
		}
		out = append(out, d)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

type hostEntry struct {
	CPU    float64 `json:"cpu"` // 0..1
	Uptime float64 `json:"uptime"`
	Memory struct {
		Total float64 `json:"total"`
		Used  float64 `json:"used"`
	} `json:"memory"`
}

type groupEntry struct {
	Type    string  `json:"backup-type"`
	ID      string  `json:"backup-id"`
	Last    float64 `json:"last-backup"`
	Count   int     `json:"backup-count"`
	Comment string  `json:"comment"`
}

type taskEntry struct {
	Type      string  `json:"worker_type"`
	ID        string  `json:"worker_id"`
	Status    string  `json:"status"`
	StartTime float64 `json:"starttime"`
	EndTime   float64 `json:"endtime"`
}

// tasksOf finds the latest backup and counts the tasks that failed in the last
// 24 hours. A task that is still running has no status yet; "WARNINGS: n" is a
// finished one with warnings; anything else but "OK" is an error message.
func tasksOf(list []taskEntry, now time.Time) (*Backup, *Tasks) {
	var latest *taskEntry
	out := &Tasks{}
	for i := range list {
		t := &list[i]
		if t.Type == "backup" {
			if latest == nil || t.StartTime > latest.StartTime {
				latest = t
			}
		}
		if t.Status == "" || t.Status == "OK" || strings.HasPrefix(t.Status, "WARNINGS") {
			continue
		}
		end := epoch(t.EndTime)
		if end.IsZero() || now.Sub(end) > 24*time.Hour {
			continue
		}
		out.Failed++
		if len(out.Items) < 10 {
			out.Items = append(out.Items, Task{Type: t.Type, Node: t.ID, Status: t.Status, End: end})
		}
	}
	sort.SliceStable(out.Items, func(i, j int) bool { return out.Items[i].End.After(out.Items[j].End) })
	var b *Backup
	if latest != nil {
		b = &Backup{Node: latest.ID}
		switch {
		case latest.Status == "":
			b.Status = "running"
			b.End = epoch(latest.StartTime)
		case latest.Status == "OK":
			b.Status = "ok"
		case strings.HasPrefix(latest.Status, "WARNINGS"):
			b.Status, b.Message = "warnings", latest.Status
		default:
			b.Status, b.Message = "failed", latest.Status
		}
		if latest.EndTime > 0 {
			b.End = epoch(latest.EndTime)
		}
	}
	return b, out
}

// uptime writes seconds like Glances does: "12 days, 3:45:10".
func uptime(sec float64) string {
	s := int(sec)
	d, s := s/86400, s%86400
	hms := fmt.Sprintf("%d:%02d:%02d", s/3600, s%3600/60, s%60)
	switch d {
	case 0:
		return hms
	case 1:
		return "1 day, " + hms
	}
	return fmt.Sprintf("%d days, %s", d, hms)
}
