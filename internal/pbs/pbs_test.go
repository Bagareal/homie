package pbs

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const token = "backup@pbs!homie"

// fake is a Proxmox Backup Server with one datastore that fills up, one that
// refuses, and a few tasks.
func fake(t *testing.T) *httptest.Server {
	t.Helper()
	now := time.Now()
	routes := map[string]string{
		"/api2/json/version": `{"data":{"version":"3.2","release":"1","repoid":"abc"}}`,
		"/api2/json/status/datastore-usage": fmt.Sprintf(`{"data":[
			{"store":"tank","total":1000,"used":250,"avail":750,"estimated-full-date":%d},
			{"store":"old","total":100,"used":99,"avail":1,"estimated-full-date":%d},
			{"store":"gone","error":"unable to open datastore"}]}`, now.Add(90*24*time.Hour).Unix(), now.Add(-time.Hour).Unix()),
		"/api2/json/nodes/localhost/status": `{"data":{"cpu":0.125,"uptime":90061,"memory":{"total":8000,"used":2000,"free":6000}}}`,
		"/api2/json/admin/datastore/tank/groups": fmt.Sprintf(`{"data":[
			{"backup-type":"vm","backup-id":"101","last-backup":%d,"backup-count":4,"comment":"web"},
			{"backup-type":"ct","backup-id":"100","last-backup":%d,"backup-count":9}]}`, now.Add(-3*time.Hour).Unix(), now.Add(-80*time.Hour).Unix()),
		"/api2/json/admin/datastore/old/groups": `{"data":[]}`,
		"/api2/json/nodes/localhost/tasks": fmt.Sprintf(`{"data":[
			{"worker_type":"backup","worker_id":"tank:vm/101","status":"OK","starttime":%d,"endtime":%d},
			{"worker_type":"garbage_collection","worker_id":"tank","status":"unable to open","starttime":%d,"endtime":%d},
			{"worker_type":"verify","worker_id":"tank","status":"unable to open","starttime":%d,"endtime":%d},
			{"worker_type":"prune","worker_id":"tank","status":"WARNINGS: 1","starttime":%d,"endtime":%d}]}`,
			now.Add(-4*time.Hour).Unix(), now.Add(-3*time.Hour).Unix(),
			now.Add(-5*time.Hour).Unix(), now.Add(-5*time.Hour).Unix(),
			now.Add(-80*time.Hour).Unix(), now.Add(-80*time.Hour).Unix(),
			now.Add(-2*time.Hour).Unix(), now.Add(-2*time.Hour).Unix()),
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := "PBSAPIToken=" + token + ":s3cret"
		switch got := r.Header.Get("Authorization"); {
		case got == "":
			http.Error(w, "no", http.StatusUnauthorized)
			return
		case got != want:
			http.Error(w, "bad", http.StatusUnauthorized)
			return
		}
		if body, ok := routes[r.URL.Path]; ok {
			_, _ = w.Write([]byte(body))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func target(srv *httptest.Server, secret string, metrics ...string) Target {
	return Target{URL: srv.URL, TokenID: token, Secret: secret, Metrics: metrics}
}

func TestPingSendsTheTokenWithAColon(t *testing.T) {
	srv := fake(t)
	v, err := New().Ping(context.Background(), target(srv, "s3cret"))
	if err != nil || v != "3.2-1" {
		t.Fatalf("version = %q, err = %v", v, err)
	}
	if _, err := New().Ping(context.Background(), target(srv, "wrong")); err == nil || !strings.Contains(err.Error(), "authentication failed") {
		t.Errorf("a wrong secret: %v", err)
	}
}

func TestFetchReadsEverything(t *testing.T) {
	srv := fake(t)
	r := New().Fetch(context.Background(), target(srv, "s3cret", Metrics...))
	if !r.Online || len(r.Failed) != 0 {
		t.Fatalf("online=%v failed=%v notes=%v error=%q", r.Online, r.Failed, r.Notes, r.Error)
	}
	d := r.Data
	if d.Version != "3.2-1" {
		t.Errorf("version = %q", d.Version)
	}
	if d.CPU == nil || *d.CPU != 12.5 || d.Mem == nil || d.Mem.Percent != 25 || d.Uptime == nil || *d.Uptime != "1 day, 1:01:01" {
		t.Errorf("host: cpu=%v mem=%+v uptime=%v", d.CPU, d.Mem, d.Uptime)
	}
	if len(d.Datastores) != 3 || d.Datastores[2].Name != "tank" || d.Datastores[2].Percent != 25 || d.Datastores[2].FullAt == nil {
		t.Fatalf("datastores = %+v", d.Datastores)
	}
	if d.Datastores[1].Name != "old" || d.Datastores[1].FullAt != nil {
		t.Errorf("a full date in the past is not shown: %+v", d.Datastores[1])
	}
	if d.Datastores[0].Name != "gone" || d.Datastores[0].Error == "" {
		t.Errorf("a datastore with an error: %+v", d.Datastores[0])
	}
	if len(d.Groups) != 2 || d.Groups[0].Type != "ct" || d.Groups[1].ID != "101" || d.Groups[1].Comment != "web" || d.Groups[1].Count != 4 {
		t.Errorf("groups = %+v", d.Groups)
	}
	if d.Backup == nil || d.Backup.Status != "ok" || d.Backup.Node != "tank:vm/101" {
		t.Errorf("backup = %+v", d.Backup)
	}
	// Two failures: one is older than a day, the WARNINGS one is not a failure.
	if d.Tasks == nil || d.Tasks.Failed != 1 || d.Tasks.Items[0].Type != "garbage_collection" {
		t.Errorf("tasks = %+v", d.Tasks)
	}
}

func TestFetchReadsOnlyWhatIsAskedFor(t *testing.T) {
	srv := fake(t)
	r := New().Fetch(context.Background(), target(srv, "s3cret", "host"))
	if !r.Online || r.Data.CPU == nil || r.Data.Datastores != nil || r.Data.Tasks != nil || r.Data.Groups != nil {
		t.Errorf("result = %+v", r)
	}
}

func TestFetchSaysWhyAGroupFails(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/version"):
			_, _ = w.Write([]byte(`{"data":{"version":"3.2","release":"1"}}`))
		case strings.HasSuffix(r.URL.Path, "/datastore-usage"):
			_, _ = w.Write([]byte(`{"data":[{"store":"tank","total":10,"used":1}]}`))
		default:
			http.Error(w, "no", http.StatusForbidden)
		}
	}))
	defer srv.Close()
	r := New().Fetch(context.Background(), Target{URL: srv.URL, TokenID: token, Secret: "x", Metrics: []string{"usage", "groups", "host"}})
	if !r.Online || len(r.Data.Datastores) != 1 {
		t.Fatalf("a refused group list does not make the server offline: %+v", r)
	}
	if len(r.Failed) != 2 || r.Failed[0] != "groups" || r.Failed[1] != "host" || !strings.Contains(r.Notes["groups"], "permission denied") {
		t.Errorf("failed=%v notes=%v", r.Failed, r.Notes)
	}
}

func TestFetchOfflineAndRedirect(t *testing.T) {
	r := New().Fetch(context.Background(), Target{URL: "https://127.0.0.1:1", TokenID: token, Secret: "x"})
	if r.Online || r.Error == "" {
		t.Errorf("unreachable: %+v", r)
	}
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://elsewhere:8007/", http.StatusMovedPermanently)
	}))
	defer plain.Close()
	r = New().Fetch(context.Background(), Target{URL: plain.URL, TokenID: token, Secret: "x"})
	if r.Online || !strings.Contains(r.Error, "https://elsewhere:8007/") {
		t.Errorf("redirect: %+v", r)
	}
}

func TestLatestBackupStates(t *testing.T) {
	now := time.Now()
	run := func(status string, end float64) *Backup {
		b, _ := tasksOf([]taskEntry{{Type: "backup", ID: "s:vm/1", Status: status, StartTime: float64(now.Unix() - 60), EndTime: end}}, now)
		return b
	}
	if b := run("", 0); b.Status != "running" {
		t.Errorf("running: %+v", b)
	}
	if b := run("WARNINGS: 2", float64(now.Unix())); b.Status != "warnings" {
		t.Errorf("warnings: %+v", b)
	}
	if b := run("connection reset", float64(now.Unix())); b.Status != "failed" || b.Message == "" {
		t.Errorf("failed: %+v", b)
	}
	if b, _ := tasksOf(nil, now); b != nil {
		t.Errorf("no backup yet: %+v", b)
	}
}
