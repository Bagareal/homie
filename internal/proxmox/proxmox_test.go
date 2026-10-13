package proxmox

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const token = "root@pam!homie"
const secret = "11111111-2222-3333-4444-555555555555"

// fakePVE answers like a two-node cluster, where pve2 is offline.
func fakePVE(t *testing.T, now time.Time) *httptest.Server {
	t.Helper()
	end := now.Add(-3 * time.Hour).Unix()
	routes := map[string]string{
		"/api2/json/version": `{"data":{"version":"8.2.4","release":"8.2"}}`,
		"/api2/json/cluster/resources": `{"data":[
			{"id":"node/pve1","type":"node","node":"pve1","status":"online","cpu":0.10,"maxcpu":8,"mem":8000,"maxmem":16000,"uptime":90061},
			{"id":"node/pve2","type":"node","node":"pve2","status":"offline"},
			{"id":"lxc/100","type":"lxc","vmid":100,"name":"web","node":"pve1","status":"running","cpu":0.02,"maxcpu":2,"mem":100,"maxmem":512,"uptime":50,"template":0,"tags":"prod;media","pool":"p1"},
			{"id":"qemu/200","type":"qemu","vmid":200,"name":"nas","node":"pve1","status":"stopped","cpu":0,"maxcpu":4,"mem":0,"maxmem":4096,"template":0},
			{"id":"qemu/900","type":"qemu","vmid":900,"name":"tpl","node":"pve1","status":"stopped","template":1},
			{"id":"storage/pve1/local","type":"storage","storage":"local","node":"pve1","status":"available","disk":50,"maxdisk":100,"shared":0,"plugintype":"dir"},
			{"id":"storage/pve1/nfs","type":"storage","storage":"nfs","node":"pve1","status":"available","disk":10,"maxdisk":100,"shared":1,"plugintype":"nfs"},
			{"id":"storage/pve2/nfs","type":"storage","storage":"nfs","node":"pve2","status":"unknown","shared":1,"plugintype":"nfs"},
			{"id":"pool/p1","type":"pool","poolid":"p1"}]}`,
		"/api2/json/cluster/status": `{"data":[{"type":"cluster","name":"lab","nodes":2,"quorate":1},{"type":"node","name":"pve1","online":1},{"type":"node","name":"pve2","online":0}]}`,
		"/api2/json/cluster/tasks": fmt.Sprintf(`{"data":[
			{"type":"vzdump","node":"pve1","status":"OK","starttime":%d,"endtime":%d},
			{"type":"vzdump","node":"pve1","status":"job errors","starttime":%d,"endtime":%d},
			{"type":"qmstart","node":"pve1","status":"OK","starttime":1,"endtime":2}]}`, end-100, end, end-86400*3-100, end-86400*3),
	}
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "PVEAPIToken="+token+"="+secret {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		body, ok := routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(body))
	}))
}

func TestFetchCluster(t *testing.T) {
	now := time.Now()
	srv := fakePVE(t, now)
	defer srv.Close()
	r := New().Fetch(context.Background(), Target{URL: srv.URL, TokenID: token, Secret: secret, Metrics: Metrics})
	if !r.Online || r.Error != "" || len(r.Failed) > 0 {
		t.Fatalf("not healthy: %+v", r)
	}
	d := r.Data
	if d.Version != "8.2.4" {
		t.Errorf("version %q", d.Version)
	}
	if d.Cluster == nil || d.Cluster.Name != "lab" || !d.Cluster.Quorate || d.Cluster.Nodes != 2 || d.Cluster.Online != 1 {
		t.Errorf("cluster: %+v", d.Cluster)
	}
	if len(d.Nodes) != 2 || !d.Nodes[0].Online || d.Nodes[1].Online || d.Nodes[1].CPU != nil {
		t.Fatalf("nodes: %+v", d.Nodes)
	}
	if got := *d.Nodes[0].CPU; got < 9.99 || got > 10.01 {
		t.Errorf("node cpu %v", got)
	}
	if *d.Nodes[0].Uptime != "1 day, 1:01:01" {
		t.Errorf("uptime %q", *d.Nodes[0].Uptime)
	}
	if d.CPU == nil || d.Mem == nil || d.Mem.Total != 16000 { // only the online node counts
		t.Errorf("cluster totals: %v %+v", d.CPU, d.Mem)
	}
	if len(d.Guests) != 3 || d.Guests[0].ID != 100 || d.Guests[0].Tags[1] != "media" || !d.Guests[2].Template || d.Guests[1].Status != "stopped" {
		t.Errorf("guests: %+v", d.Guests)
	}
	if len(d.Storage) != 2 { // local, and the shared nfs once
		t.Fatalf("storage: %+v", d.Storage)
	}
	if d.Storage[0].ID != "local" || d.Storage[0].Percent != 50 || d.Storage[1].ID != "nfs" || d.Storage[1].Node != "pve1" {
		t.Errorf("storage: %+v", d.Storage)
	}
	if d.Backup == nil || d.Backup.Status != "ok" || d.Backup.Node != "pve1" {
		t.Errorf("backup: %+v", d.Backup)
	}
	if d.Tasks == nil || d.Tasks.Failed != 0 { // the failed one is three days old
		t.Errorf("tasks: %+v", d.Tasks)
	}
}

func TestFailedBackup(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	b, ts := tasksOf([]taskEntry{
		{Type: "vzdump", Node: "a", Status: "OK", StartTime: 100, EndTime: 200},
		{Type: "vzdump", Node: "a", Status: "ERROR: boom", StartTime: 999_000, EndTime: 999_500},
		{Type: "qmigrate", Node: "b", Status: "migration aborted", StartTime: 999_100, EndTime: 999_200},
		{Type: "vzdump", Node: "c", Status: "WARNINGS: 1", StartTime: 10, EndTime: 20},
	}, now)
	if b.Status != "failed" || b.Message != "ERROR: boom" {
		t.Errorf("backup: %+v", b)
	}
	if ts.Failed != 2 || ts.Items[0].Type != "vzdump" {
		t.Errorf("tasks: %+v", ts)
	}
	b, _ = tasksOf([]taskEntry{{Type: "vzdump", Status: "", StartTime: 50}}, now)
	if b.Status != "running" {
		t.Errorf("running backup: %+v", b)
	}
	if b, _ := tasksOf(nil, now); b != nil {
		t.Errorf("no backup expected: %+v", b)
	}
}

func TestPlainHTTPRedirectIsExplained(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://pve.example/api2/json/version", http.StatusMovedPermanently)
	}))
	defer srv.Close()
	_, err := New().Ping(context.Background(), Target{URL: srv.URL, TokenID: token, Secret: secret})
	if err == nil || !strings.Contains(err.Error(), "use https://") {
		t.Fatalf("expected a hint to use https, got %v", err)
	}
}

func TestStandaloneHasNoCluster(t *testing.T) {
	if clusterOf([]clusterEntry{{Type: "node", Name: "pve", Online: 1}}) != nil {
		t.Error("a standalone node is not a cluster")
	}
}

func TestAuthAndErrors(t *testing.T) {
	srv := fakePVE(t, time.Now())
	defer srv.Close()
	c := New()
	r := c.Fetch(context.Background(), Target{URL: srv.URL, TokenID: token, Secret: "wrong", Metrics: Metrics})
	if r.Online || !strings.Contains(r.Error, "authentication failed") {
		t.Errorf("expected an auth error: %+v", r)
	}
	if _, err := c.Ping(context.Background(), Target{URL: srv.URL, TokenID: token, Secret: secret}); err != nil {
		t.Errorf("ping: %v", err)
	}
	if r := c.Fetch(context.Background(), Target{URL: "https://127.0.0.1:1", TokenID: token, Secret: secret, Metrics: Metrics}); r.Online || r.Error == "" {
		t.Errorf("unreachable should be offline: %+v", r)
	}
}
