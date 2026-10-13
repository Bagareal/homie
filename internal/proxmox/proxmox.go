// Package proxmox reads a Proxmox VE node or cluster through its REST API with
// an API token and turns it into the small shape the dashboard widgets use.
//
// One request to /cluster/resources lists every node, guest (VM and LXC) and
// storage of the whole cluster, so it is enough to ask any single node; a
// standalone node answers the same way.
package proxmox

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
var Metrics = []string{"resources", "version", "cluster", "tasks"}

// Target is one Proxmox VE system and which of the Metrics to read.
type Target struct {
	URL     string // https://host:8006
	TokenID string // user@realm!name
	Secret  string
	Metrics []string
}

type Usage struct {
	Percent float64 `json:"percent"`
	Used    float64 `json:"used"`
	Total   float64 `json:"total"`
}

// Node is one machine of the cluster.
type Node struct {
	Name   string   `json:"name"`
	Online bool     `json:"online"`
	CPU    *float64 `json:"cpu,omitempty"` // percent
	Cores  int      `json:"cores,omitempty"`
	Mem    *Usage   `json:"mem,omitempty"`
	Uptime *string  `json:"uptime,omitempty"`
}

// Guest is a virtual machine ("qemu") or a container ("lxc").
type Guest struct {
	ID       int      `json:"id"`
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Node     string   `json:"node"`
	Status   string   `json:"status"`
	CPU      float64  `json:"cpu"` // percent of the guest's own cores
	Cores    int      `json:"cores,omitempty"`
	Mem      Usage    `json:"mem"`
	Uptime   *string  `json:"uptime,omitempty"`
	Template bool     `json:"template,omitempty"`
	Tags     []string `json:"tags,omitempty"`
	Pool     string   `json:"pool,omitempty"`
}

// Store is a storage. A shared one is listed once, a local one per node.
type Store struct {
	ID     string `json:"id"`
	Node   string `json:"node"`
	Type   string `json:"type,omitempty"`
	Shared bool   `json:"shared,omitempty"`
	Status string `json:"status,omitempty"`
	Usage
}

// Cluster is present only when the node belongs to a cluster.
type Cluster struct {
	Name    string `json:"name"`
	Quorate bool   `json:"quorate"`
	Nodes   int    `json:"nodes"`
	Online  int    `json:"online"`
}

// Backup is the most recent backup job (vzdump task).
type Backup struct {
	Status  string    `json:"status"` // ok, warnings, failed, running
	End     time.Time `json:"end,omitempty"`
	Node    string    `json:"node,omitempty"`
	Message string    `json:"message,omitempty"`
}

type Task struct {
	Type   string    `json:"type"`
	Node   string    `json:"node"`
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
	Version string   `json:"version,omitempty"`
	Cluster *Cluster `json:"cluster,omitempty"`
	Nodes   []Node   `json:"nodes,omitempty"`
	// CPU and Mem are the whole cluster: the cores' average and the memory sum
	// of the nodes that are online.
	CPU     *float64 `json:"cpu,omitempty"`
	Mem     *Usage   `json:"mem,omitempty"`
	Guests  []Guest  `json:"guests,omitempty"`
	Storage []Store  `json:"storage,omitempty"`
	Backup  *Backup  `json:"backup,omitempty"`
	Tasks   *Tasks   `json:"tasks,omitempty"`
	// Glances and GlancesNodes are filled in by the caller from the widget's
	// extra Glances source.
	Glances      any            `json:"glances,omitempty"`
	GlancesNodes map[string]any `json:"glancesNodes,omitempty"`
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

// Client reads Proxmox VE systems.
type Client struct{ http *http.Client }

func New() *Client {
	return &Client{http: &http.Client{
		Timeout: 8 * time.Second,
		Transport: &http.Transport{
			// Proxmox ships with a self-signed certificate.
			TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
			MaxIdleConnsPerHost: 2,
			IdleConnTimeout:     60 * time.Second,
		},
		// Never follow a redirect: it could carry the token elsewhere.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

var errAuth = errors.New("authentication failed: check the token id (user@realm!name) and its secret")
var errPerm = errors.New("permission denied: give the user, and the token itself if it has privilege separation, the PVEAuditor role on /")

func (c *Client) get(ctx context.Context, t Target, path string, v any) error {
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(t.URL), "/") + "/api2/json" + path)
	if err != nil || u.Host == "" {
		return errors.New("invalid address")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "PVEAPIToken="+strings.TrimSpace(t.TokenID)+"="+strings.TrimSpace(t.Secret))
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
			return fmt.Errorf("the address redirects to %s: Proxmox VE speaks https, so use https:// and its port (8006)", loc)
		}
		return fmt.Errorf("the server answered %s: Proxmox VE speaks https on port 8006", resp.Status)
	case resp.StatusCode >= 300:
		return fmt.Errorf("the server answered %s (is this the Proxmox VE address, port 8006?)", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return err
	}
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return errors.New("this does not look like the Proxmox VE API (is the port 8006?)")
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

// Ping checks the address and the token, and returns the Proxmox VE version.
func (c *Client) Ping(ctx context.Context, t Target) (string, error) {
	var v struct {
		Version string `json:"version"`
	}
	if err := c.get(ctx, t, "/version", &v); err != nil {
		return "", err
	}
	return v.Version, nil
}

// Fetch reads what the metrics ask for. The resources decide whether the
// system counts as online; the other groups fail on their own.
func (c *Client) Fetch(ctx context.Context, t Target) Result {
	res := Result{UpdatedAt: time.Now().UTC()}
	want := map[string]bool{}
	for _, m := range t.Metrics {
		want[m] = true
	}
	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		rs      []resource
		rsErr   error
		cluster []clusterEntry
		tasks   []taskEntry
		failed  = map[string]error{}
		version string
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
	run("resources", func() error { rsErr = c.get(ctx, t, "/cluster/resources", &rs); return rsErr })
	run("version", func() error {
		var v struct {
			Version string `json:"version"`
		}
		err := c.get(ctx, t, "/version", &v)
		version = v.Version
		return err
	})
	run("cluster", func() error { return c.get(ctx, t, "/cluster/status", &cluster) })
	run("tasks", func() error { return c.get(ctx, t, "/cluster/tasks", &tasks) })
	wg.Wait()

	if want["resources"] && rsErr != nil {
		res.Error = rsErr.Error()
		return res
	}
	if !want["resources"] {
		// Nothing was read from the cluster, so there is nothing to call unreachable.
		res.Online = true
	} else {
		res.Online = true
		res.Data.Nodes, res.Data.Guests, res.Data.Storage = split(rs)
		res.Data.CPU, res.Data.Mem = total(res.Data.Nodes)
	}
	res.Data.Version = version
	if want["cluster"] && failed["cluster"] == nil {
		res.Data.Cluster = clusterOf(cluster)
	}
	if want["tasks"] && failed["tasks"] == nil {
		res.Data.Backup, res.Data.Tasks = tasksOf(tasks, time.Now())
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

type resource struct {
	Type    string  `json:"type"`
	Node    string  `json:"node"`
	Name    string  `json:"name"`
	VMID    int     `json:"vmid"`
	Status  string  `json:"status"`
	CPU     float64 `json:"cpu"`
	MaxCPU  float64 `json:"maxcpu"`
	Mem     float64 `json:"mem"`
	MaxMem  float64 `json:"maxmem"`
	Disk    float64 `json:"disk"`
	MaxDisk float64 `json:"maxdisk"`
	Uptime  float64 `json:"uptime"`
	// Proxmox sends these as 0 or 1.
	Template float64 `json:"template"`
	Shared   float64 `json:"shared"`
	Tags     string  `json:"tags"`
	Pool     string  `json:"pool"`
	Storage  string  `json:"storage"`
	Plugin   string  `json:"plugintype"`
}

func usage(used, total float64) Usage {
	u := Usage{Used: used, Total: total}
	if total > 0 {
		u.Percent = used / total * 100
	}
	return u
}

// split sorts the resources of a cluster into nodes, guests and storages.
func split(rs []resource) (nodes []Node, guests []Guest, stores []Store) {
	seenShared := map[string]bool{}
	for _, r := range rs {
		switch r.Type {
		case "node":
			n := Node{Name: r.Node, Online: r.Status == "online", Cores: int(r.MaxCPU)}
			if n.Online {
				cpu := r.CPU * 100
				mem := usage(r.Mem, r.MaxMem)
				up := uptime(r.Uptime)
				n.CPU, n.Mem, n.Uptime = &cpu, &mem, &up
			}
			nodes = append(nodes, n)
		case "qemu", "lxc":
			g := Guest{
				ID: r.VMID, Name: r.Name, Type: r.Type, Node: r.Node, Status: r.Status,
				CPU: r.CPU * 100, Cores: int(r.MaxCPU), Mem: usage(r.Mem, r.MaxMem),
				Template: r.Template != 0, Pool: r.Pool, Tags: tagsOf(r.Tags),
			}
			if r.Uptime > 0 {
				up := uptime(r.Uptime)
				g.Uptime = &up
			}
			guests = append(guests, g)
		case "storage":
			if r.Shared != 0 {
				if seenShared[r.Storage] {
					continue // the same storage, reported by every node
				}
				seenShared[r.Storage] = true
			}
			stores = append(stores, Store{ID: r.Storage, Node: r.Node, Type: r.Plugin, Shared: r.Shared != 0, Status: r.Status, Usage: usage(r.Disk, r.MaxDisk)})
		}
	}
	sort.SliceStable(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })
	sort.SliceStable(guests, func(i, j int) bool { return guests[i].ID < guests[j].ID })
	sort.SliceStable(stores, func(i, j int) bool {
		if stores[i].ID != stores[j].ID {
			return stores[i].ID < stores[j].ID
		}
		return stores[i].Node < stores[j].Node
	})
	return
}

func tagsOf(s string) []string {
	var out []string
	for _, t := range strings.FieldsFunc(s, func(r rune) bool { return r == ';' || r == ',' || r == ' ' }) {
		out = append(out, t)
	}
	return out
}

// total adds up the nodes that are online: the cores' average load, weighted by
// the number of cores, and the memory.
func total(nodes []Node) (*float64, *Usage) {
	var cpu, cores, used, size float64
	found := false
	for _, n := range nodes {
		if !n.Online || n.CPU == nil || n.Mem == nil {
			continue
		}
		found = true
		w := float64(n.Cores)
		if w == 0 {
			w = 1
		}
		cpu += *n.CPU * w
		cores += w
		used += n.Mem.Used
		size += n.Mem.Total
	}
	if !found {
		return nil, nil
	}
	avg := cpu / cores
	u := usage(used, size)
	return &avg, &u
}

type clusterEntry struct {
	Type    string  `json:"type"`
	Name    string  `json:"name"`
	Nodes   float64 `json:"nodes"`
	Quorate float64 `json:"quorate"`
	Online  float64 `json:"online"`
}

// clusterOf is nil for a standalone node, which has no cluster entry.
func clusterOf(entries []clusterEntry) *Cluster {
	var c *Cluster
	online, nodes := 0, 0
	for _, e := range entries {
		switch e.Type {
		case "cluster":
			c = &Cluster{Name: e.Name, Quorate: e.Quorate != 0}
		case "node":
			nodes++
			if e.Online != 0 {
				online++
			}
		}
	}
	if c != nil {
		c.Nodes, c.Online = nodes, online
	}
	return c
}

type taskEntry struct {
	Type      string  `json:"type"`
	Node      string  `json:"node"`
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
		if t.Type == "vzdump" {
			if latest == nil || t.StartTime > latest.StartTime {
				latest = t
			}
		}
		if t.Status == "" || t.Status == "OK" || strings.HasPrefix(t.Status, "WARNINGS") {
			continue
		}
		end := time.Unix(int64(t.EndTime), 0).UTC()
		if t.EndTime == 0 || now.Sub(end) > 24*time.Hour {
			continue
		}
		out.Failed++
		if len(out.Items) < 10 {
			out.Items = append(out.Items, Task{Type: t.Type, Node: t.Node, Status: t.Status, End: end})
		}
	}
	sort.SliceStable(out.Items, func(i, j int) bool { return out.Items[i].End.After(out.Items[j].End) })
	var b *Backup
	if latest != nil {
		b = &Backup{Node: latest.Node}
		switch {
		case latest.Status == "":
			b.Status = "running"
			b.End = time.Unix(int64(latest.StartTime), 0).UTC()
		case latest.Status == "OK":
			b.Status = "ok"
		case strings.HasPrefix(latest.Status, "WARNINGS"):
			b.Status, b.Message = "warnings", latest.Status
		default:
			b.Status, b.Message = "failed", latest.Status
		}
		if latest.EndTime > 0 {
			b.End = time.Unix(int64(latest.EndTime), 0).UTC()
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
