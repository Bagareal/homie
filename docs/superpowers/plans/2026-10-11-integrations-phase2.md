# Proxmox VE + PBS Drivers Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add two zero-manual-config integration drivers (Proxmox VE, Proxmox Backup Server) on top of the Phase 1 platform, each auto-discovering its own targets (nodes / datastores), plus fix the widget-area CSS layout bug discovered during design so the new multi-value tiles render legibly.

**Architecture:** Two new self-registering drivers in `internal/integrations` (`proxmoxve.go`, `pbs.go`), both implementing a new optional `DiscoverableDriver` capability (`Targets()`) alongside the existing `Driver` interface. A new discovery HTTP endpoint pair mirrors the existing test-connection pair. The frontend gets a generic `MultiValueWidget.svelte` (a 3-column grid, replacing nothing — `CustomApiWidget.svelte` keeps its single-value look), `IntegrationDialog.svelte` becomes kind-aware instead of hardcoded to Custom API, and `ItemDialog.svelte` gets a live target picker instead of a hand-typed JSON path for the two new kinds. A CSS fix (`grid-column: 1 / -1`) corrects every widget kind's tile placement, including Custom API's.

**Tech Stack:** Go 1.22 (stdlib only), Svelte 5 + TypeScript — unchanged from Phase 1.

**Spec:** `docs/superpowers/specs/2026-10-11-integrations-phase2-proxmox-design.md`

## Global Constraints

- Go 1.22, stdlib only — no new entries in `go.mod`.
- Every new driver's HTTP client: 10s context timeout, response body capped via the shared `maxIntegrationBody` constant (2MB), `InsecureSkipVerify: true` — same pattern as Custom API's existing client.
- Auth header formats, confirmed against current Proxmox documentation and community references during this plan's research (see spec's "Endpoint verification" section): Proxmox VE — `Authorization: PVEAPIToken=<tokenId>=<secret>`; PBS — `Authorization: PBSAPIToken=<tokenId>:<secret>` (colon, not `=`, between token ID and secret — the one formatting difference between the two).
- No API response ever contains a secret value — only `hasSecret: bool`, unchanged from Phase 1. `tokenId` lives in the non-secret `config` object (like Custom API's header *names*); only `secret` lives in `SecretStore` (like Custom API's header *values*).
- Every widget tile's data area must render with `grid-column: 1 / -1` (full tile width) — applies to **every** widget kind, including the pre-existing Custom API one.
- `gofmt -l .` clean; `go vet ./...` clean; `npm run check` (svelte-check) 0 errors.
- All new user-facing strings in English.
- PBS being unreachable (host powered off) is a plain fetch error, handled entirely by the existing Phase 1 poller "last known state" mechanism — no new "expected offline" status anywhere in this plan.

## Review Focus

- A Proxmox VE or PBS token that authenticates but has zero visible nodes/datastores (e.g. wrong permissions) must fail `TestConnection`, not silently report success with an empty list. Owning tasks: Task 3 (Proxmox VE), Task 4 (PBS).
- The discovery endpoint called for a non-discoverable kind (`customapi`) must return a clean `{ok:false, error:...}`, never panic on a failed interface type assertion. Owning task: Task 5.
- `MultiValueWidget.svelte` must reproduce every state `CustomApiWidget.svelte` already has — stale/dimmed display on fetch failure, the small error-dot, "waiting for first check" before any successful fetch, "Integration not found" for a dangling reference — a new widget kind must not silently regress this. Owning task: Task 8.
- The widget-area layout fix must not visually change Custom API's existing single-value tile beyond fixing its position — it's a repositioning fix, not a redesign. Owning task: Task 6 (manual check against the already-running dev instance).
- A PBS datastore whose most recent backup task can't be correlated by this plan's `worker_id` prefix heuristic (see Task 4) must still show the raw last-backup timestamp rather than failing the whole fetch — a parsing gap in one of three PBS calls must degrade gracefully, not take down the other two values. Owning task: Task 4.

---

## Backend

### Task 1: Shared HTTP/formatting helpers

**Files:**
- Create: `internal/integrations/format.go`
- Create: `internal/integrations/format_test.go`
- Modify: `internal/integrations/customapi.go` (rename the body-size constant so it reads correctly once three drivers share it)

**Interfaces:**
- Produces: `const maxIntegrationBody = 2 << 20`, `func formatDuration(seconds float64) string`, `func relativeAgo(t time.Time) string`.

- [ ] **Step 1: Write the failing tests**

```go
// internal/integrations/format_test.go
package integrations

import (
	"testing"
	"time"
)

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		seconds float64
		want    string
	}{
		{90, "1m"},
		{3661, "1h 1m"},
		{90000, "1d 1h"},
		{259200, "3d 0h"},
	}
	for _, c := range cases {
		if got := formatDuration(c.seconds); got != c.want {
			t.Errorf("formatDuration(%v) = %q, want %q", c.seconds, got, c.want)
		}
	}
}

func TestRelativeAgo(t *testing.T) {
	cases := []struct {
		ago  time.Duration
		want string
	}{
		{30 * time.Second, "just now"},
		{5 * time.Minute, "5m ago"},
		{3 * time.Hour, "3h ago"},
		{50 * time.Hour, "2d ago"},
	}
	for _, c := range cases {
		if got := relativeAgo(time.Now().Add(-c.ago)); got != c.want {
			t.Errorf("relativeAgo(now-%v) = %q, want %q", c.ago, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/integrations/... -run "TestFormatDuration|TestRelativeAgo" -v`
Expected: FAIL to compile — `formatDuration`, `relativeAgo` undefined.

- [ ] **Step 3: Write the implementation**

```go
// internal/integrations/format.go
package integrations

import (
	"fmt"
	"time"
)

// maxIntegrationBody caps every integration driver's HTTP response body —
// shared across Custom API, Proxmox VE, and PBS — so a slow or oversized
// response from a misconfigured or malicious target can't exhaust memory
// or hang the poller.
const maxIntegrationBody = 2 << 20 // 2MB

// formatDuration renders a duration given in seconds as "Xd Yh", "Xh Ym",
// or "Xm" — used for Proxmox VE node uptime.
func formatDuration(seconds float64) string {
	d := time.Duration(seconds) * time.Second
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, hours)
	}
	minutes := int(d.Minutes()) % 60
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}

// relativeAgo renders how long ago t was: "just now", "Xm ago", "Xh ago",
// or "Xd ago" — used for PBS's last-backup timestamp.
func relativeAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
```

In `internal/integrations/customapi.go`, remove the now-duplicate constant and its uses:

```go
const maxCustomAPIBody = 2 << 20 // 2MB
```

Delete that line entirely, and replace every `maxCustomAPIBody` reference in the file (there is exactly one, in the `request` method's `io.LimitReader(resp.Body, maxCustomAPIBody)` call) with `maxIntegrationBody`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/integrations/... -v`
Expected: PASS — every test in the package, including the pre-existing Custom API ones (confirms the rename didn't break anything).

- [ ] **Step 5: Format and vet**

Run: `gofmt -l internal/integrations && go vet ./internal/integrations/...`
Expected: clean

- [ ] **Step 6: Commit**

```bash
git add internal/integrations/format.go internal/integrations/format_test.go internal/integrations/customapi.go
git commit -m "Add shared formatting helpers; rename body-size const for reuse"
```

---

### Task 2: `DiscoverableDriver` capability + `Target` type

**Files:**
- Modify: `internal/integrations/types.go`
- Test: `internal/integrations/registry_test.go`

**Interfaces:**
- Consumes: `Driver` (existing, Phase 1).
- Produces: `type Target struct{ID, Label string}`, `type DiscoverableDriver interface{Driver; Targets(ctx, cfg map[string]any, secrets Secrets) ([]Target, error)}`.

- [ ] **Step 1: Write the failing test**

```go
// append to internal/integrations/registry_test.go
package integrations

import "context"

type fakeDiscoverableDriver struct {
	fakeDriver
	targets []Target
}

func (f *fakeDiscoverableDriver) Targets(context.Context, map[string]any, Secrets) ([]Target, error) {
	return f.targets, nil
}

func TestDiscoverableDriverTypeAssertion(t *testing.T) {
	plain := &fakeDriver{kind: "plain"}
	Register(plain)
	discoverable := &fakeDiscoverableDriver{fakeDriver: fakeDriver{kind: "discoverable"}, targets: []Target{{ID: "a", Label: "A"}}}
	Register(discoverable)

	d1, _ := Get("plain")
	if _, ok := d1.(DiscoverableDriver); ok {
		t.Fatal("expected the plain driver NOT to satisfy DiscoverableDriver")
	}

	d2, _ := Get("discoverable")
	dd, ok := d2.(DiscoverableDriver)
	if !ok {
		t.Fatal("expected the discoverable driver to satisfy DiscoverableDriver")
	}
	targets, err := dd.Targets(context.Background(), nil, nil)
	if err != nil || len(targets) != 1 || targets[0].ID != "a" {
		t.Fatalf("got %v, %v", targets, err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/integrations/... -run TestDiscoverableDriverTypeAssertion -v`
Expected: FAIL to compile — `Target`, `DiscoverableDriver` undefined.

- [ ] **Step 3: Write the implementation**

In `internal/integrations/types.go`, add after the existing `Driver` interface:

```go
// Target is one selectable item a DiscoverableDriver can fetch data for —
// e.g. a Proxmox VE node name, or a PBS datastore name.
type Target struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// DiscoverableDriver is an optional capability: a Driver that can list its
// own selectable targets live, so the UI offers a picker instead of
// requiring the admin to type a target name by hand. Not every driver
// needs this — Custom API has no equivalent concept and simply doesn't
// implement it; callers type-assert for it rather than it being part of
// the base Driver interface.
type DiscoverableDriver interface {
	Driver
	Targets(ctx context.Context, cfg map[string]any, secrets Secrets) ([]Target, error)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/integrations/... -v`
Expected: PASS — every test in the package.

- [ ] **Step 5: Format and vet**

Run: `gofmt -l internal/integrations && go vet ./internal/integrations/...`
Expected: clean

- [ ] **Step 6: Commit**

```bash
git add internal/integrations/types.go internal/integrations/registry_test.go
git commit -m "Add DiscoverableDriver capability and Target type"
```

---

### Task 3: Proxmox VE driver

**Files:**
- Create: `internal/integrations/proxmoxve.go`
- Test: `internal/integrations/proxmoxve_test.go`

**Interfaces:**
- Consumes: `Driver`, `DiscoverableDriver`, `Target`, `Data`, `Value`, `Secrets`, `Register` (Tasks 1-2); `maxIntegrationBody`, `formatDuration` (Task 1).
- Produces: a registered driver under `Kind("proxmoxve")`. Instance `config`: `{"address": "https://host:8006", "tokenId": "user@realm!tokenid"}`. Instance `secrets` (via `SecretStore`): `{"secret": "<uuid>"}`. Widget `query`: `{"node": "morpheus"}`.

- [ ] **Step 1: Write the failing tests**

```go
// internal/integrations/proxmoxve_test.go
package integrations

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

const pveResourcesFixture = `{"data":[
	{"type":"node","node":"morpheus","status":"online","cpu":0.42,"mem":6442450944,"maxmem":17179869184,"uptime":1220400},
	{"type":"node","node":"other","status":"offline","cpu":0,"mem":0,"maxmem":0,"uptime":0},
	{"type":"qemu","node":"morpheus","vmid":100,"status":"running","cpu":0.1,"mem":0,"maxmem":0,"uptime":600},
	{"type":"qemu","node":"morpheus","vmid":101,"status":"stopped","cpu":0,"mem":0,"maxmem":0,"uptime":0},
	{"type":"lxc","node":"morpheus","vmid":200,"status":"running","cpu":0.05,"mem":0,"maxmem":0,"uptime":600},
	{"type":"lxc","node":"other","vmid":300,"status":"running","cpu":0,"mem":0,"maxmem":0,"uptime":0}
]}`

func pveTestServer(t *testing.T, wantAuth string, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api2/json/cluster/resources" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if wantAuth != "" && r.Header.Get("Authorization") != wantAuth {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	}))
}

func pveTestCfgSecrets(addr string) (map[string]any, Secrets) {
	return map[string]any{"address": addr, "tokenId": "root@pam!homie"}, Secrets{"secret": "abc-123"}
}

func TestProxmoxVETargets(t *testing.T) {
	srv := pveTestServer(t, "PVEAPIToken=root@pam!homie=abc-123", pveResourcesFixture)
	defer srv.Close()

	d := &proxmoxVEDriver{client: srv.Client()}
	cfg, secrets := pveTestCfgSecrets(srv.URL)
	targets, err := d.Targets(context.Background(), cfg, secrets)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 2 || targets[0].ID != "morpheus" || targets[1].ID != "other" {
		t.Fatalf("got %+v", targets)
	}
}

func TestProxmoxVETargetsEmptyFails(t *testing.T) {
	srv := pveTestServer(t, "", `{"data":[]}`)
	defer srv.Close()

	d := &proxmoxVEDriver{client: srv.Client()}
	cfg, secrets := pveTestCfgSecrets(srv.URL)
	if _, err := d.Targets(context.Background(), cfg, secrets); err == nil {
		t.Fatal("expected an error when zero nodes are visible")
	}
}

func TestProxmoxVETestConnectionFailsOnWrongToken(t *testing.T) {
	srv := pveTestServer(t, "PVEAPIToken=root@pam!homie=abc-123", pveResourcesFixture)
	defer srv.Close()

	d := &proxmoxVEDriver{client: srv.Client()}
	cfg := map[string]any{"address": srv.URL, "tokenId": "root@pam!homie"}
	badSecrets := Secrets{"secret": "wrong"}
	if err := d.TestConnection(context.Background(), cfg, badSecrets); err == nil {
		t.Fatal("expected an error with a wrong secret")
	}
}

func TestProxmoxVEFetch(t *testing.T) {
	srv := pveTestServer(t, "", pveResourcesFixture)
	defer srv.Close()

	d := &proxmoxVEDriver{client: srv.Client()}
	cfg, secrets := pveTestCfgSecrets(srv.URL)
	data, err := d.Fetch(context.Background(), cfg, secrets, map[string]any{"node": "morpheus"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(data.Values) != 4 {
		t.Fatalf("expected 4 values, got %d: %+v", len(data.Values), data.Values)
	}
	want := map[string]string{"CPU": "42%", "RAM": "38%", "Uptime": "14d 2h", "VM/LXC": "2/3"}
	for _, v := range data.Values {
		if want[v.Label] != v.Formatted {
			t.Errorf("%s: got %q, want %q", v.Label, v.Formatted, want[v.Label])
		}
	}
}

func TestProxmoxVEFetchUnknownNode(t *testing.T) {
	srv := pveTestServer(t, "", pveResourcesFixture)
	defer srv.Close()

	d := &proxmoxVEDriver{client: srv.Client()}
	cfg, secrets := pveTestCfgSecrets(srv.URL)
	if _, err := d.Fetch(context.Background(), cfg, secrets, map[string]any{"node": "does-not-exist"}); err == nil {
		t.Fatal("expected an error for an unknown node")
	}
}

func TestProxmoxVERegistered(t *testing.T) {
	if _, ok := Get("proxmoxve"); !ok {
		t.Fatal("expected the proxmoxve driver to self-register via init()")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/integrations/... -run TestProxmoxVE -v`
Expected: FAIL to compile — `proxmoxVEDriver` undefined.

- [ ] **Step 3: Write the implementation**

```go
// internal/integrations/proxmoxve.go
package integrations

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"
)

type pveResource struct {
	Type   string  `json:"type"`
	Node   string  `json:"node"`
	Status string  `json:"status"`
	CPU    float64 `json:"cpu"`
	Mem    float64 `json:"mem"`
	MaxMem float64 `json:"maxmem"`
	Uptime float64 `json:"uptime"`
}

type pveResourcesResponse struct {
	Data []pveResource `json:"data"`
}

type proxmoxVEDriver struct {
	client *http.Client
}

func init() {
	Register(&proxmoxVEDriver{client: &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			// Self-signed certificates are the norm in a homelab —
			// matches internal/status and the Custom API driver.
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}})
}

func (d *proxmoxVEDriver) Kind() Kind { return "proxmoxve" }

// pveAuthHeader builds the Authorization header value for a Proxmox VE API
// token: "PVEAPIToken=<tokenId>=<secret>" — tokenId (e.g.
// "root@pam!homie") lives in the non-secret config, the UUID secret in
// SecretStore, mirroring Custom API's header-name/header-value split.
func pveAuthHeader(cfg map[string]any, secrets Secrets) (string, error) {
	tokenID, _ := cfg["tokenId"].(string)
	secret := secrets["secret"]
	if tokenID == "" || secret == "" {
		return "", errors.New("missing token ID or secret")
	}
	return "PVEAPIToken=" + tokenID + "=" + secret, nil
}

// resources calls /cluster/resources once and returns every node, VM, and
// LXC it reports. This single endpoint covers both a standalone node and a
// multi-node cluster identically — a cluster is just more entries in the
// same list, never a different code path.
func (d *proxmoxVEDriver) resources(ctx context.Context, cfg map[string]any, secrets Secrets) ([]pveResource, error) {
	address, _ := cfg["address"].(string)
	if address == "" {
		return nil, errors.New("missing address")
	}
	auth, err := pveAuthHeader(cfg, secrets)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address+"/api2/json/cluster/resources", nil)
	if err != nil {
		return nil, fmt.Errorf("invalid address: %w", err)
	}
	req.Header.Set("Authorization", auth)
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxIntegrationBody))
	if err != nil {
		return nil, err
	}
	var decoded pveResourcesResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, fmt.Errorf("response is not valid JSON: %w", err)
	}
	return decoded.Data, nil
}

func (d *proxmoxVEDriver) Targets(ctx context.Context, cfg map[string]any, secrets Secrets) ([]Target, error) {
	resources, err := d.resources(ctx, cfg, secrets)
	if err != nil {
		return nil, err
	}
	var targets []Target
	for _, r := range resources {
		if r.Type == "node" {
			targets = append(targets, Target{ID: r.Node, Label: r.Node})
		}
	}
	if len(targets) == 0 {
		return nil, errors.New("no nodes visible with this token")
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].ID < targets[j].ID })
	return targets, nil
}

// TestConnection reuses Targets: a token that authenticates but sees zero
// nodes (e.g. insufficient permissions) must fail the test, not report a
// false "OK".
func (d *proxmoxVEDriver) TestConnection(ctx context.Context, cfg map[string]any, secrets Secrets) error {
	_, err := d.Targets(ctx, cfg, secrets)
	return err
}

func (d *proxmoxVEDriver) Fetch(ctx context.Context, cfg map[string]any, secrets Secrets, query map[string]any) (Data, error) {
	node, _ := query["node"].(string)
	if node == "" {
		return Data{}, errors.New("missing node")
	}
	resources, err := d.resources(ctx, cfg, secrets)
	if err != nil {
		return Data{}, err
	}
	var nodeRes *pveResource
	totalGuests, runningGuests := 0, 0
	for i := range resources {
		r := &resources[i]
		if r.Type == "node" && r.Node == node {
			nodeRes = r
		}
		if (r.Type == "qemu" || r.Type == "lxc") && r.Node == node {
			totalGuests++
			if r.Status == "running" {
				runningGuests++
			}
		}
	}
	if nodeRes == nil {
		return Data{}, fmt.Errorf("node %q not found", node)
	}
	ramPct := 0.0
	if nodeRes.MaxMem > 0 {
		ramPct = nodeRes.Mem / nodeRes.MaxMem * 100
	}
	return Data{
		Values: []Value{
			{Label: "CPU", Formatted: fmt.Sprintf("%.0f%%", nodeRes.CPU*100), Raw: nodeRes.CPU},
			{Label: "RAM", Formatted: fmt.Sprintf("%.0f%%", ramPct), Raw: ramPct},
			{Label: "Uptime", Formatted: formatDuration(nodeRes.Uptime), Raw: nodeRes.Uptime},
			{Label: "VM/LXC", Formatted: fmt.Sprintf("%d/%d", runningGuests, totalGuests), Raw: totalGuests},
		},
		FetchedAt: time.Now(),
	}, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/integrations/... -run TestProxmoxVE -v`
Expected: PASS — all 6 tests. Double-check the uptime math by hand: 1220400s = 14d 2h24m, `formatDuration` truncates to whole hours, so `"14d 2h"` — matches the fixture's expected value in the test.

- [ ] **Step 5: Format and vet**

Run: `gofmt -l internal/integrations && go vet ./internal/integrations/...`
Expected: clean

- [ ] **Step 6: Commit**

```bash
git add internal/integrations/proxmoxve.go internal/integrations/proxmoxve_test.go
git commit -m "Add Proxmox VE driver: node/cluster discovery, CPU/RAM/uptime/guest counts"
```

---

### Task 4: PBS driver

**Files:**
- Create: `internal/integrations/pbs.go`
- Test: `internal/integrations/pbs_test.go`

**Interfaces:**
- Consumes: `Driver`, `DiscoverableDriver`, `Target`, `Data`, `Value`, `Secrets`, `Register` (Tasks 1-2); `maxIntegrationBody`, `relativeAgo` (Task 1).
- Produces: a registered driver under `Kind("pbs")`. Instance `config`: `{"address": "https://host:8007", "tokenId": "user@realm!tokenname"}`. Instance `secrets`: `{"secret": "<value>"}`. Widget `query`: `{"datastore": "backup-store"}`.

- [ ] **Step 1: Write the failing tests**

```go
// internal/integrations/pbs_test.go
package integrations

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func pbsTestServer(t *testing.T, routes map[string]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := routes[r.URL.Path]
		if !ok {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "PBSAPIToken=root@pam!homie:abc-123" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	}))
}

func pbsTestCfgSecrets(addr string) (map[string]any, Secrets) {
	return map[string]any{"address": addr, "tokenId": "root@pam!homie"}, Secrets{"secret": "abc-123"}
}

func TestPBSTargets(t *testing.T) {
	srv := pbsTestServer(t, map[string]string{
		"/api2/json/status/datastore-usage": `{"data":[{"store":"backup-store","used":100,"total":1000},{"store":"archive","used":5,"total":10}]}`,
	})
	defer srv.Close()

	d := &pbsDriver{client: srv.Client()}
	cfg, secrets := pbsTestCfgSecrets(srv.URL)
	targets, err := d.Targets(context.Background(), cfg, secrets)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 2 || targets[0].ID != "archive" || targets[1].ID != "backup-store" {
		t.Fatalf("got %+v", targets)
	}
}

func TestPBSTargetsEmptyFails(t *testing.T) {
	srv := pbsTestServer(t, map[string]string{"/api2/json/status/datastore-usage": `{"data":[]}`})
	defer srv.Close()

	d := &pbsDriver{client: srv.Client()}
	cfg, secrets := pbsTestCfgSecrets(srv.URL)
	if _, err := d.Targets(context.Background(), cfg, secrets); err == nil {
		t.Fatal("expected an error when zero datastores are visible")
	}
}

func TestPBSFetch(t *testing.T) {
	recentBackup := time.Now().Add(-2 * time.Hour).Unix()
	routes := map[string]string{
		"/api2/json/status/datastore-usage":              `{"data":[{"store":"backup-store","used":250,"total":1000}]}`,
		"/api2/json/admin/datastore/backup-store/groups": `{"data":[{"backup-type":"vm","backup-id":"100","last-backup":` + itoa(recentBackup) + `}]}`,
		"/api2/json/nodes/localhost/tasks":                `{"data":[{"upid":"UPID:pbs::...","worker_id":"backup-store:vm/100","worker_type":"backup","status":"OK","endtime":` + itoa(recentBackup) + `}]}`,
	}
	srv := pbsTestServer(t, routesWithQuery(routes))
	defer srv.Close()

	d := &pbsDriver{client: srv.Client()}
	cfg, secrets := pbsTestCfgSecrets(srv.URL)
	data, err := d.Fetch(context.Background(), cfg, secrets, map[string]any{"datastore": "backup-store"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(data.Values) != 2 {
		t.Fatalf("expected 2 values, got %d: %+v", len(data.Values), data.Values)
	}
	if data.Values[0].Label != "Used" || data.Values[0].Formatted != "25%" {
		t.Errorf("got %+v", data.Values[0])
	}
	if data.Values[1].Label != "Last backup" || data.Values[1].Formatted != "ok, 2h ago" {
		t.Errorf("got %+v", data.Values[1])
	}
}

func TestPBSFetchDegradesWithoutMatchingTask(t *testing.T) {
	// The groups call succeeds (so we know *when* the last backup was),
	// but the tasks call has no entry whose worker_id matches this
	// datastore's prefix — e.g. the 50-task window didn't reach back far
	// enough, or PBS's real worker_id format turns out to differ from
	// this plan's "<datastore>:" prefix assumption (see the doc comment
	// on lastBackupLabel). Fetch must still succeed with a plain "X ago"
	// label instead of failing the whole widget.
	recentBackup := time.Now().Add(-2 * time.Hour).Unix()
	srv := pbsTestServer(t, map[string]string{
		"/api2/json/status/datastore-usage":              `{"data":[{"store":"backup-store","used":250,"total":1000}]}`,
		"/api2/json/admin/datastore/backup-store/groups": `{"data":[{"backup-type":"vm","backup-id":"100","last-backup":` + itoa(recentBackup) + `}]}`,
		"/api2/json/nodes/localhost/tasks":                `{"data":[{"upid":"UPID:pbs::...","worker_id":"other-store:vm/100","worker_type":"backup","status":"OK","endtime":` + itoa(recentBackup) + `}]}`,
	})
	defer srv.Close()

	d := &pbsDriver{client: srv.Client()}
	cfg, secrets := pbsTestCfgSecrets(srv.URL)
	data, err := d.Fetch(context.Background(), cfg, secrets, map[string]any{"datastore": "backup-store"})
	if err != nil {
		t.Fatalf("expected Fetch to degrade gracefully, not fail: %v", err)
	}
	if data.Values[1].Formatted != "2h ago" {
		t.Errorf("got %q, want the plain timestamp with no ok/failed prefix since no task matched", data.Values[1].Formatted)
	}
}

func TestPBSFetchUnknownDatastore(t *testing.T) {
	srv := pbsTestServer(t, map[string]string{
		"/api2/json/status/datastore-usage": `{"data":[{"store":"backup-store","used":1,"total":10}]}`,
	})
	defer srv.Close()

	d := &pbsDriver{client: srv.Client()}
	cfg, secrets := pbsTestCfgSecrets(srv.URL)
	if _, err := d.Fetch(context.Background(), cfg, secrets, map[string]any{"datastore": "does-not-exist"}); err == nil {
		t.Fatal("expected an error for an unknown datastore")
	}
}

func TestPBSRegistered(t *testing.T) {
	if _, ok := Get("pbs"); !ok {
		t.Fatal("expected the pbs driver to self-register via init()")
	}
}

// itoa avoids importing strconv into the test file just for int64->string.
func itoa(n int64) string {
	return fmt.Sprintf("%d", n)
}

// routesWithQuery strips any query string before matching r.URL.Path
// against the fixed routes map above, since the tasks endpoint is called
// with "?typefilter=backup&limit=50".
func routesWithQuery(routes map[string]string) map[string]string {
	return routes
}
```

Note: `pbsTestServer`'s handler matches on `r.URL.Path` (which already excludes the query string in Go's `net/http`), so `routesWithQuery` is a no-op passthrough — it exists only to make the test's intent explicit; the real query-stripping happens for free via `r.URL.Path`. Add `"fmt"` to this file's imports for `itoa`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/integrations/... -run TestPBS -v`
Expected: FAIL to compile — `pbsDriver` undefined.

- [ ] **Step 3: Write the implementation**

```go
// internal/integrations/pbs.go
package integrations

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

type pbsDatastoreUsage struct {
	Store string `json:"store"`
	Used  int64  `json:"used"`
	Total int64  `json:"total"`
}

type pbsDatastoreUsageResponse struct {
	Data []pbsDatastoreUsage `json:"data"`
}

type pbsGroup struct {
	LastBackup int64 `json:"last-backup"`
}

type pbsGroupsResponse struct {
	Data []pbsGroup `json:"data"`
}

type pbsTask struct {
	WorkerID string `json:"worker_id"`
	Status   string `json:"status"`
	EndTime  int64  `json:"endtime"`
}

type pbsTasksResponse struct {
	Data []pbsTask `json:"data"`
}

type pbsDriver struct {
	client *http.Client
}

func init() {
	Register(&pbsDriver{client: &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}})
}

func (d *pbsDriver) Kind() Kind { return "pbs" }

// pbsAuthHeader builds "PBSAPIToken=<tokenId>:<secret>" — note the colon,
// not "=", between the token ID and secret. This is the one place PBS's
// token format differs from Proxmox VE's (see pveAuthHeader).
func pbsAuthHeader(cfg map[string]any, secrets Secrets) (string, error) {
	tokenID, _ := cfg["tokenId"].(string)
	secret := secrets["secret"]
	if tokenID == "" || secret == "" {
		return "", errors.New("missing token ID or secret")
	}
	return "PBSAPIToken=" + tokenID + ":" + secret, nil
}

func (d *pbsDriver) get(ctx context.Context, cfg map[string]any, secrets Secrets, path string, out any) error {
	address, _ := cfg["address"].(string)
	if address == "" {
		return errors.New("missing address")
	}
	auth, err := pbsAuthHeader(cfg, secrets)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address+path, nil)
	if err != nil {
		return fmt.Errorf("invalid address: %w", err)
	}
	req.Header.Set("Authorization", auth)
	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxIntegrationBody))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("response is not valid JSON: %w", err)
	}
	return nil
}

func (d *pbsDriver) datastoreUsage(ctx context.Context, cfg map[string]any, secrets Secrets) ([]pbsDatastoreUsage, error) {
	var res pbsDatastoreUsageResponse
	if err := d.get(ctx, cfg, secrets, "/api2/json/status/datastore-usage", &res); err != nil {
		return nil, err
	}
	return res.Data, nil
}

func (d *pbsDriver) Targets(ctx context.Context, cfg map[string]any, secrets Secrets) ([]Target, error) {
	usage, err := d.datastoreUsage(ctx, cfg, secrets)
	if err != nil {
		return nil, err
	}
	if len(usage) == 0 {
		return nil, errors.New("no datastores visible with this token")
	}
	targets := make([]Target, len(usage))
	for i, u := range usage {
		targets[i] = Target{ID: u.Store, Label: u.Store}
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].ID < targets[j].ID })
	return targets, nil
}

func (d *pbsDriver) TestConnection(ctx context.Context, cfg map[string]any, secrets Secrets) error {
	_, err := d.Targets(ctx, cfg, secrets)
	return err
}

// Fetch makes up to three calls: datastore usage (required — its failure
// fails the whole fetch), then backup-group last-backup timestamps and
// recent task history (both best-effort — a failure there degrades to a
// plainer "Last backup" label rather than failing everything, per the
// Review Focus item on this task).
func (d *pbsDriver) Fetch(ctx context.Context, cfg map[string]any, secrets Secrets, query map[string]any) (Data, error) {
	datastore, _ := query["datastore"].(string)
	if datastore == "" {
		return Data{}, errors.New("missing datastore")
	}
	usage, err := d.datastoreUsage(ctx, cfg, secrets)
	if err != nil {
		return Data{}, err
	}
	var used, total int64
	found := false
	for _, u := range usage {
		if u.Store == datastore {
			used, total, found = u.Used, u.Total, true
			break
		}
	}
	if !found {
		return Data{}, fmt.Errorf("datastore %q not found", datastore)
	}
	usagePct := 0.0
	if total > 0 {
		usagePct = float64(used) / float64(total) * 100
	}

	lastBackupLabel := d.lastBackupLabel(ctx, cfg, secrets, datastore)

	return Data{
		Values: []Value{
			{Label: "Used", Formatted: fmt.Sprintf("%.0f%%", usagePct), Raw: usagePct},
			{Label: "Last backup", Formatted: lastBackupLabel, Raw: lastBackupLabel},
		},
		FetchedAt: time.Now(),
	}, nil
}

// lastBackupLabel finds the newest snapshot timestamp across every backup
// group in datastore, then tries to find a matching recent "backup"-type
// task to prefix the label with "ok"/"failed". The worker_id prefix match
// ("<datastore>:") is this plan's best-effort reading of PBS's UPID
// encoding, confirmed against community references rather than a live
// instance — verify it against the real PBS host during manual testing
// (Task 11) and adjust the prefix check here if it doesn't match.
func (d *pbsDriver) lastBackupLabel(ctx context.Context, cfg map[string]any, secrets Secrets, datastore string) string {
	var groups pbsGroupsResponse
	if err := d.get(ctx, cfg, secrets, "/api2/json/admin/datastore/"+datastore+"/groups", &groups); err != nil {
		return "unknown"
	}
	var newest int64
	for _, g := range groups.Data {
		if g.LastBackup > newest {
			newest = g.LastBackup
		}
	}
	if newest == 0 {
		return "never"
	}
	agoLabel := relativeAgo(time.Unix(newest, 0))

	var tasks pbsTasksResponse
	if err := d.get(ctx, cfg, secrets, "/api2/json/nodes/localhost/tasks?typefilter=backup&limit=50", &tasks); err != nil {
		return agoLabel
	}
	var latest *pbsTask
	for i := range tasks.Data {
		t := &tasks.Data[i]
		if !strings.HasPrefix(t.WorkerID, datastore+":") {
			continue
		}
		if latest == nil || t.EndTime > latest.EndTime {
			latest = t
		}
	}
	if latest == nil {
		return agoLabel
	}
	outcome := "failed"
	if latest.Status == "OK" {
		outcome = "ok"
	}
	return outcome + ", " + agoLabel
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/integrations/... -run TestPBS -v`
Expected: PASS — all 6 tests.

- [ ] **Step 5: Format and vet**

Run: `gofmt -l internal/integrations && go vet ./internal/integrations/...`
Expected: clean

- [ ] **Step 6: Commit**

```bash
git add internal/integrations/pbs.go internal/integrations/pbs_test.go
git commit -m "Add PBS driver: datastore discovery, usage, and last-backup status"
```

---

### Task 5: Discovery HTTP endpoints

**Files:**
- Modify: `internal/server/server.go` (route wiring)
- Modify: `internal/server/integrations.go`
- Modify: `internal/server/integrations_test.go`

**Interfaces:**
- Consumes: `integrations.Get`, `integrations.Kind`, `integrations.DiscoverableDriver`, `integrations.Target`, `integrations.Secrets` (Tasks 2-4).
- Produces: `POST /api/integrations/targets`, `POST /api/integrations/{id}/targets`, both returning `{ok, targets?, error?}`.

- [ ] **Step 1: Write the failing tests**

```go
// append to internal/server/integrations_test.go
package server

func TestDiscoverTargetsUnsaved(t *testing.T) {
	// A fake discoverable kind is registered by proxmoxve_test.go/pbs_test.go
	// at the package level in internal/integrations, but this test only
	// needs any registered kind without DiscoverableDriver to prove the
	// "not discoverable" branch, plus a reachable fixture for a kind that
	// does implement it — customapi is always registered and does NOT
	// implement DiscoverableDriver, which is exactly the negative case.
	s := newTestServer(t)
	h := s.Handler()

	w := doJSON(t, h, "POST", "/api/integrations/targets", map[string]any{
		"kind":   "customapi",
		"config": map[string]any{"address": "http://example.com"},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("got %d, body %s", w.Code, w.Body.String())
	}
	var res struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res.OK || res.Error == "" {
		t.Fatalf("expected ok=false with an error for a non-discoverable kind, got %+v", res)
	}
}

func TestDiscoverTargetsUnknownKind(t *testing.T) {
	s := newTestServer(t)
	h := s.Handler()

	w := doJSON(t, h, "POST", "/api/integrations/targets", map[string]any{
		"kind":   "doesnotexist",
		"config": map[string]any{},
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("got %d, body %s", w.Code, w.Body.String())
	}
}

func TestDiscoverSavedTargetsNotFound(t *testing.T) {
	s := newTestServer(t)
	h := s.Handler()

	w := doJSON(t, h, "POST", "/api/integrations/does-not-exist/targets", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("got %d, body %s", w.Code, w.Body.String())
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/server/... -v`
Expected: FAIL — 404s for the new routes (not yet wired).

- [ ] **Step 3: Write the implementation**

In `internal/server/server.go`, add the two routes next to the existing integration ones:

```go
	mux.HandleFunc("POST /api/integrations/{id}/test", s.testSavedIntegration)
	mux.HandleFunc("POST /api/integrations/targets", s.discoverTargets)
	mux.HandleFunc("POST /api/integrations/{id}/targets", s.discoverSavedTargets)
```

Append to `internal/server/integrations.go`:

```go
type targetsRequest struct {
	Kind    string            `json:"kind"`
	Config  map[string]any    `json:"config"`
	Secrets map[string]string `json:"secrets"`
}

type targetsResponse struct {
	OK      bool                  `json:"ok"`
	Targets []integrations.Target `json:"targets,omitempty"`
	Error   string                `json:"error,omitempty"`
}

func runDiscoverTargets(w http.ResponseWriter, r *http.Request, kind string, cfg map[string]any, secrets integrations.Secrets) {
	driver, ok := integrations.Get(integrations.Kind(kind))
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown integration kind: "+kind)
		return
	}
	discoverable, ok := driver.(integrations.DiscoverableDriver)
	if !ok {
		writeJSON(w, http.StatusOK, targetsResponse{OK: false, Error: "this integration kind has no discoverable targets"})
		return
	}
	targets, err := discoverable.Targets(r.Context(), cfg, secrets)
	if err != nil {
		writeJSON(w, http.StatusOK, targetsResponse{OK: false, Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, targetsResponse{OK: true, Targets: targets})
}

func (s *Server) discoverTargets(w http.ResponseWriter, r *http.Request) {
	var req targetsRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "unreadable request: invalid JSON or too large")
		return
	}
	runDiscoverTargets(w, r, req.Kind, req.Config, req.Secrets)
}

func (s *Server) discoverSavedTargets(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cfg := s.store.Get()
	var in *config.Integration
	for i := range cfg.Integrations {
		if cfg.Integrations[i].ID == id {
			in = &cfg.Integrations[i]
		}
	}
	if in == nil {
		writeError(w, http.StatusNotFound, "integration not found")
		return
	}
	secrets, _ := s.secrets.Get(id)
	runDiscoverTargets(w, r, in.Kind, in.Config, secrets)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/server/... -v`
Expected: PASS — every test in the package.

- [ ] **Step 5: Format and vet, then full backend suite**

Run: `gofmt -l internal/server && go vet ./... && go test ./...`
Expected: clean, all packages PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/server/server.go internal/server/integrations.go internal/server/integrations_test.go
git commit -m "Add target-discovery endpoints for Proxmox VE/PBS pickers"
```

---

## Frontend

### Task 6: Widget area layout fix

**Files:**
- Modify: `ui/src/app.css`
- Modify: `ui/src/components/CustomApiWidget.svelte`

**Interfaces:**
- Consumes: nothing new — pure CSS fix on existing elements.
- Produces: every widget's root element now spans the tile's full content width instead of landing in the icon's 38px column; `.error` truncates with an ellipsis instead of wrapping word-by-word.

- [ ] **Step 1: Reproduce the bug, for your own confirmation**

With the dev servers already running (backend on :8080, frontend on :5173 — `HOMIE_DATA=./data go run ./cmd/homie` and `npm run dev` from `ui/`), open `http://localhost:5173` and look at the "GitHub stars" tile if one is already configured from the design session, or add one: Settings → Integrations → Add integration → Custom API, address `https://api.github.com/repos/Bagareal/homie`, no headers, save; then add a tile with that widget, JSON path `stargazers_count`, label "Stars". Confirm the value renders cramped into the icon's column, wrapping word-by-word — this is the bug this task fixes.

- [ ] **Step 2: Apply the CSS fix**

In `ui/src/app.css`, find the `.led` rule block (search for `/* Spia di stato, come il LED di una porta di rete */`) and add immediately before it:

```css
/* Il contenuto del widget occupa tutta la larghezza della tile, non solo
   la colonna stretta dell'icona — altrimenti anche un valore corto va a
   capo parola per parola. */
.tile > .widget,
.tile > .multi-widget {
  grid-column: 1 / -1;
}
```

In `ui/src/components/CustomApiWidget.svelte`, find the `.error` rule in the `<style>` block:

```css
  .error {
    color: var(--down);
    font-size: 0.85rem;
  }
```

Replace it with:

```css
  .error {
    color: var(--down);
    font-size: 0.85rem;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
```

- [ ] **Step 3: Verify in the browser**

Reload `http://localhost:5173`. The "Stars" value should now sit under/beside the full tile width, on one line. To also confirm the truncation half: temporarily point the Custom API integration's address at a non-JSON page (e.g. `https://example.com`) via Settings, save, wait for the next poll (or click the tile's integration and hit "Test connection" to confirm the error, then check the tile) — the long "invalid character '<' looking for beginning of value" error should now show as one truncated line with an ellipsis, not a multi-line wrap. Revert the address back to the GitHub API URL afterward so the tile shows live data again for later manual checks.

- [ ] **Step 4: Type-check**

Run: `npm run check` (from `ui/`)
Expected: 0 errors.

- [ ] **Step 5: Commit**

```bash
git add ui/src/app.css ui/src/components/CustomApiWidget.svelte
git commit -m "Fix widget area landing in the icon column instead of full tile width"
```

---

### Task 7: Frontend types + API client for discovery

**Files:**
- Modify: `ui/src/lib/types.ts`
- Modify: `ui/src/lib/api.ts`
- Modify: `ui/src/lib/state.svelte.ts`

**Interfaces:**
- Produces: `export interface Target { id: string; label: string }`, `api.discoverTargets(body)`, `api.discoverSavedTargets(id)`, `app.discoverTargets(body)`, `app.discoverSavedTargets(id)`.

- [ ] **Step 1: Add the `Target` type**

In `ui/src/lib/types.ts`, add after `export interface Integration { ... }`:

```ts
export interface Target {
  id: string
  label: string
}
```

- [ ] **Step 2: Add the API client functions**

In `ui/src/lib/api.ts`, add after `testSavedIntegration`:

```ts
export const discoverTargets = (body: { kind: string; config: Record<string, unknown>; secrets: Record<string, string> }) =>
  request<{ ok: boolean; targets?: Target[]; error?: string }>('api/integrations/targets', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })

export const discoverSavedTargets = (id: string) =>
  request<{ ok: boolean; targets?: Target[]; error?: string }>(`api/integrations/${id}/targets`, { method: 'POST' })
```

Add `Target` to the existing `import type { Config, Integration, Status, WidgetResult } from './types'` line at the top of the file.

- [ ] **Step 3: Add thin wrappers on `app`**

In `ui/src/lib/state.svelte.ts`, add after `testSavedIntegration`:

```ts
  discoverTargets(body: { kind: string; config: Record<string, unknown>; secrets: Record<string, string> }) {
    return api.discoverTargets(body)
  }

  discoverSavedTargets(id: string) {
    return api.discoverSavedTargets(id)
  }
```

- [ ] **Step 4: Type-check**

Run: `npm run check` (from `ui/`)
Expected: 0 errors.

- [ ] **Step 5: Commit**

```bash
git add ui/src/lib/types.ts ui/src/lib/api.ts ui/src/lib/state.svelte.ts
git commit -m "Add frontend client for integration target discovery"
```

---

### Task 8: `MultiValueWidget.svelte` + `Tile.svelte` dispatch

**Files:**
- Create: `ui/src/components/MultiValueWidget.svelte`
- Modify: `ui/src/components/Tile.svelte`

**Interfaces:**
- Consumes: `WidgetResult` (existing type), `item.widget.kind` (existing field).
- Produces: a `MultiValueWidget` component with the same `{ result, missing }` prop shape as `CustomApiWidget`, rendering `result.data.values` as a 3-column grid.

- [ ] **Step 1: Write the component**

```svelte
<!-- ui/src/components/MultiValueWidget.svelte -->
<script lang="ts">
  import type { WidgetResult } from '../lib/types'
  import { formatRelativeTime } from '../lib/time'

  let { result, missing }: { result: WidgetResult | undefined; missing?: boolean } = $props()

  const values = $derived(result?.data?.values ?? [])
  const hasData = $derived(values.length > 0)
  const updated = $derived(result?.fetchedAt ? formatRelativeTime(result.fetchedAt) : '')
</script>

<div class="multi-widget" class:stale={!!result?.error}>
  {#if missing}
    <span class="error">Integration not found</span>
  {:else if hasData}
    <div class="grid">
      {#each values as v (v.label)}
        <div class="cell">
          <span class="v">{v.formatted}</span>
          <span class="l">{v.label}</span>
        </div>
      {/each}
    </div>
    {#if updated}<span class="updated">updated {updated}</span>{/if}
  {:else if result?.error}
    <span class="error">{result.error}</span>
  {:else}
    <span class="updated">waiting for first check</span>
  {/if}
  {#if !missing && hasData && result?.error}
    <span class="error-dot" title={result.error}></span>
  {/if}
</div>

<style>
  .multi-widget {
    display: flex;
    flex-direction: column;
    gap: 4px;
    min-width: 0;
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 8px;
  }
  .cell {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .v {
    font-size: 1.05rem;
    font-weight: 700;
    line-height: 1.1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .l {
    color: var(--muted);
    font-size: 0.72rem;
  }
  .updated {
    color: var(--muted);
    font-size: 0.75rem;
  }
  .multi-widget.stale .v,
  .multi-widget.stale .l {
    opacity: 0.55;
  }
  .error {
    color: var(--down);
    font-size: 0.85rem;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .error-dot {
    position: absolute;
    top: 9px;
    right: 9px;
    width: 10px;
    height: 10px;
    border-radius: 50%;
    background: var(--down);
  }
</style>
```

- [ ] **Step 2: Wire it into `Tile.svelte`**

In `ui/src/components/Tile.svelte`, add the import alongside the existing one:

```ts
  import CustomApiWidget from './CustomApiWidget.svelte'
  import MultiValueWidget from './MultiValueWidget.svelte'
```

Replace the widget line inside the `body` snippet:

```svelte
  {#if item.widget}
    <CustomApiWidget result={app.widgets[item.id]} missing={widgetIntegrationMissing} />
  {:else if led !== 'off'}
```

with:

```svelte
  {#if item.widget}
    {#if item.widget.kind === 'customapi'}
      <CustomApiWidget result={app.widgets[item.id]} missing={widgetIntegrationMissing} />
    {:else}
      <MultiValueWidget result={app.widgets[item.id]} missing={widgetIntegrationMissing} />
    {/if}
  {:else if led !== 'off'}
```

- [ ] **Step 3: Type-check**

Run: `npm run check` (from `ui/`)
Expected: 0 errors.

- [ ] **Step 4: Manual visual check**

In the running dev instance, temporarily hand-edit a widget tile's `widget.kind` to something other than `customapi` via the browser devtools console is awkward — instead, skip visual confirmation of `MultiValueWidget` until Task 10 wires up real Proxmox VE/PBS tiles that actually use it; this task's own correctness is covered by the type-check plus Task 11's end-to-end pass.

- [ ] **Step 5: Commit**

```bash
git add ui/src/components/MultiValueWidget.svelte ui/src/components/Tile.svelte
git commit -m "Add MultiValueWidget (3-column grid) and dispatch on widget.kind"
```

---

### Task 9: `IntegrationDialog.svelte` becomes kind-aware

**Files:**
- Modify: `ui/src/components/IntegrationDialog.svelte`
- Modify: `ui/src/components/Settings.svelte`

**Interfaces:**
- Consumes: `app.integrations`, `app.createIntegration`, `app.updateIntegration`, `app.testIntegration`, `app.testSavedIntegration` (existing); `Target` (Task 7, unused directly here but the dialog now creates integrations the target picker in Task 10 will query).
- Produces: `IntegrationDialog` gains a new `initialKind` prop (used only when creating); renders Custom API's existing address+headers fields for `kind === 'customapi'`, and address+Token ID+Secret fields for `proxmoxve`/`pbs`.

- [ ] **Step 1: Rewrite `IntegrationDialog.svelte`**

Replace the entire file:

```svelte
<script lang="ts">
  import { onMount } from 'svelte'
  import { app, uid } from '../lib/state.svelte'

  let { integrationId, initialKind, onclose }: { integrationId: string | null; initialKind: string; onclose: () => void } = $props()

  let dialog: HTMLDialogElement
  // svelte-ignore state_referenced_locally
  const existing = integrationId ? app.integrations.find((i) => i.id === integrationId) : undefined
  // svelte-ignore state_referenced_locally
  const kind = existing?.kind ?? initialKind

  const kindLabels: Record<string, string> = {
    customapi: 'Custom API',
    proxmoxve: 'Proxmox VE',
    pbs: 'Proxmox Backup Server',
  }

  let name = $state(existing?.name ?? '')
  let address = $state((existing?.config.address as string) ?? '')
  // svelte-ignore state_referenced_locally
  let headerRows = $state<{ id: string; name: string; value: string }[]>(
    ((existing?.config.headers as string[]) ?? []).map((n) => ({ id: uid(), name: n, value: '' })),
  )
  // svelte-ignore state_referenced_locally
  let tokenId = $state((existing?.config.tokenId as string) ?? '')
  let secret = $state('')
  let error = $state('')
  let testResult = $state<{ ok: boolean; error?: string } | null>(null)
  let testing = $state(false)
  let saving = $state(false)

  onMount(() => dialog.showModal())

  function addHeaderRow() {
    headerRows = [...headerRows, { id: uid(), name: '', value: '' }]
  }
  function removeHeaderRow(id: string) {
    headerRows = headerRows.filter((r) => r.id !== id)
  }

  function buildConfig() {
    if (kind === 'customapi') {
      return { address: address.trim(), headers: headerRows.filter((r) => r.name.trim()).map((r) => r.name.trim()) }
    }
    return { address: address.trim(), tokenId: tokenId.trim() }
  }

  // The value inputs always start blank, so "nothing typed" must mean
  // "leave the stored secret alone" when editing — an empty string is the
  // SecretStore's delete sentinel, and sending it would wipe a credential
  // the user was never shown and therefore could not retype.
  function buildSecrets(): Record<string, string> | undefined {
    if (kind === 'customapi') {
      const values: Record<string, string> = {}
      for (const r of headerRows) {
        if (r.name.trim() && r.value) values[r.name.trim()] = r.value
      }
      if (existing) {
        if (headerRows.length === 0) return { headers: '' } // all rows removed: explicit clear
        if (Object.keys(values).length === 0) return undefined // nothing retyped: leave the stored secret untouched
        return { headers: JSON.stringify(values) }
      }
      return { headers: Object.keys(values).length ? JSON.stringify(values) : '' }
    }
    if (secret) return { secret }
    return existing ? undefined : { secret: '' }
  }

  async function test() {
    testing = true
    testResult = null
    try {
      testResult = existing
        ? await app.testSavedIntegration(existing.id)
        : // Unsaved integration: buildSecrets() never returns undefined here.
          await app.testIntegration({ kind, config: buildConfig(), secrets: buildSecrets()! })
    } catch (e) {
      testResult = { ok: false, error: (e as Error).message }
    } finally {
      testing = false
    }
  }

  async function submit(e: SubmitEvent) {
    e.preventDefault()
    if (!name.trim()) return void (error = 'Enter a name for the integration.')
    if (!address.trim()) return void (error = 'Enter an address to call.')
    saving = true
    try {
      if (existing) {
        const secrets = buildSecrets()
        await app.updateIntegration(existing.id, { name: name.trim(), config: buildConfig(), ...(secrets ? { secrets } : {}) })
      } else {
        // On the create path buildSecrets() never returns undefined.
        await app.createIntegration({ name: name.trim(), kind, config: buildConfig(), secrets: buildSecrets()! })
      }
      dialog.close()
    } catch (e) {
      error = (e as Error).message
    } finally {
      saving = false
    }
  }

  async function remove() {
    if (existing && confirm(`Delete the integration "${existing.name}"? Widgets using it will show "integration not found".`)) {
      await app.deleteIntegration(existing.id)
      dialog.close()
    }
  }
</script>

<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_noninteractive_element_interactions -->
<dialog bind:this={dialog} class="dialog" {onclose} onclick={(e) => e.target === dialog && dialog.close()}>
  <form onsubmit={submit} novalidate>
    <h2>{existing ? 'Edit integration' : 'New integration'} ({kindLabels[kind] ?? kind})</h2>

    <label>
      Name
      <!-- svelte-ignore a11y_autofocus -->
      <input bind:value={name} maxlength="60" autofocus />
    </label>

    <label>
      Address
      <input bind:value={address} placeholder="https://..." inputmode="url" />
    </label>

    {#if kind === 'customapi'}
      <div class="header-rows">
        <p class="hint">Headers (e.g. Authorization). Values are write-only and never shown again.</p>
        {#each headerRows as row (row.id)}
          <div class="header-row">
            <input bind:value={row.name} placeholder="Header name" />
            <input type="password" bind:value={row.value} placeholder="Value" />
            <button type="button" onclick={() => removeHeaderRow(row.id)} aria-label="Remove header">&times;</button>
          </div>
        {/each}
        <button type="button" onclick={addHeaderRow}>Add header</button>
      </div>
    {:else}
      <label>
        Token ID
        <input bind:value={tokenId} placeholder="root@pam!homie" />
        <small>From {kindLabels[kind]}'s "Add API Token" screen — the Token ID field, not the Secret.</small>
      </label>
      <label>
        Secret
        <input type="password" bind:value={secret} placeholder={existing ? 'Unchanged — leave blank to keep it' : ''} />
        <small>The Secret shown once when the token was created. Write-only — never shown again after saving.</small>
      </label>
    {/if}

    <div class="row">
      <button type="button" onclick={test} disabled={testing}>{testing ? 'Testing...' : 'Test connection'}</button>
      {#if testResult}
        <span class:ok={testResult.ok} class:fail={!testResult.ok}>
          {testResult.ok ? 'Connection OK' : `Failed: ${testResult.error}`}
        </span>
      {/if}
    </div>

    {#if error}<p class="form-error" role="alert">{error}</p>{/if}

    <div class="dialog-actions">
      {#if existing}<button type="button" class="danger" onclick={remove}>Delete integration</button>{/if}
      <button type="button" onclick={() => dialog.close()}>Cancel</button>
      <button type="submit" class="primary" disabled={saving}>{existing ? 'Save changes' : 'Add integration'}</button>
    </div>
  </form>
</dialog>

<style>
  .header-rows {
    display: grid;
    gap: 6px;
  }
  .header-row {
    display: grid;
    grid-template-columns: 1fr 1fr auto;
    gap: 6px;
  }
  .row {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .ok {
    color: var(--up);
  }
  .fail {
    color: var(--down);
  }
</style>
```

- [ ] **Step 2: Add the kind picker to `Settings.svelte`**

In `ui/src/components/Settings.svelte`, change the integration-edit state to also track which kind a new integration should start as:

```ts
  let integrationEdit = $state<{ id: string | null } | null>(null)
```

becomes:

```ts
  let integrationEdit = $state<{ id: string | null } | null>(null)
  let newIntegrationKind = $state('customapi')
```

Replace the "Add integration" button:

```svelte
    <button type="button" onclick={() => (integrationEdit = { id: null })}>Add integration</button>
```

with:

```svelte
    <div class="row">
      <select bind:value={newIntegrationKind}>
        <option value="customapi">Custom API</option>
        <option value="proxmoxve">Proxmox VE</option>
        <option value="pbs">Proxmox Backup Server</option>
      </select>
      <button type="button" onclick={() => (integrationEdit = { id: null })}>Add integration</button>
    </div>
```

Replace the `<IntegrationDialog>` usage to pass the chosen kind:

```svelte
    <IntegrationDialog integrationId={integrationEdit.id} onclose={() => (integrationEdit = null)} />
```

with:

```svelte
    <IntegrationDialog integrationId={integrationEdit.id} initialKind={newIntegrationKind} onclose={() => (integrationEdit = null)} />
```

If `Settings.svelte` doesn't already have a `.row` style (it reuses several across the file — check before adding), add one near its other small layout rules:

```css
  .row {
    display: flex;
    align-items: center;
    gap: 10px;
  }
```

- [ ] **Step 3: Type-check**

Run: `npm run check` (from `ui/`)
Expected: 0 errors.

- [ ] **Step 4: Manual verification**

In the running dev instance, open Settings → Integrations, pick "Proxmox VE" from the new dropdown, click "Add integration" — confirm the dialog shows Address/Token ID/Secret fields (not headers), and the title reads "New integration (Proxmox VE)". Switch the dropdown to "Proxmox Backup Server" and repeat. Confirm "Custom API" still behaves exactly as before. Don't save a real Proxmox/PBS integration yet — there's no live host reachable from this dev session to test against until Task 11's manual pass.

- [ ] **Step 5: Commit**

```bash
git add ui/src/components/IntegrationDialog.svelte ui/src/components/Settings.svelte
git commit -m "Make IntegrationDialog kind-aware: Proxmox VE/PBS token fields"
```

---

### Task 10: `ItemDialog.svelte` target picker

**Files:**
- Modify: `ui/src/components/ItemDialog.svelte`

**Interfaces:**
- Consumes: `app.integrations`, `app.discoverSavedTargets` (Task 7), `Target` (Task 7).
- Produces: for a selected `proxmoxve`/`pbs` integration, a live node/datastore dropdown replaces the JSON-path/label/unit fields; the saved `widget.query` becomes `{node: "..."}` or `{datastore: "..."}`.

- [ ] **Step 1: Rewrite `ItemDialog.svelte`**

Replace the entire file:

```svelte
<script lang="ts">
  import { onMount } from 'svelte'
  import { app, uid } from '../lib/state.svelte'
  import type { Item, Target } from '../lib/types'
  import Icon from './Icon.svelte'

  let { groupId, item, onclose }: { groupId: string; item: Item | null; onclose: () => void } = $props()

  let dialog: HTMLDialogElement
  // Fields start from the service's values and are edited locally until saved.
  // svelte-ignore state_referenced_locally
  let name = $state(item?.name ?? '')
  // svelte-ignore state_referenced_locally
  let url = $state(item?.url ?? '')
  // svelte-ignore state_referenced_locally
  let icon = $state(item?.icon ?? '')
  // svelte-ignore state_referenced_locally
  let description = $state(item?.description ?? '')
  // svelte-ignore state_referenced_locally
  let check = $state(item?.check ?? true)
  // svelte-ignore state_referenced_locally
  let target = $state(groupId)
  let error = $state('')

  // svelte-ignore state_referenced_locally
  let widgetIntegrationId = $state(item?.widget?.integrationId ?? '')
  // svelte-ignore state_referenced_locally
  let widgetJsonPath = $state((item?.widget?.query.jsonPath as string) ?? '')
  // svelte-ignore state_referenced_locally
  let widgetLabel = $state((item?.widget?.query.label as string) ?? '')
  // svelte-ignore state_referenced_locally
  let widgetUnit = $state((item?.widget?.query.unit as string) ?? '')
  // svelte-ignore state_referenced_locally
  let widgetTarget = $state(((item?.widget?.query.node ?? item?.widget?.query.datastore) as string) ?? '')
  // svelte-ignore state_referenced_locally
  let widgetSize = $state(item?.widget?.size ?? '1x1')

  let widgetTargets = $state<Target[]>([])
  let widgetTargetsLoading = $state(false)
  let widgetTargetsError = $state('')

  const selectedIntegration = $derived(app.integrations.find((i) => i.id === widgetIntegrationId))
  const widgetKind = $derived(selectedIntegration?.kind ?? '')
  const targetParam = $derived(widgetKind === 'pbs' ? 'datastore' : 'node')
  const targetLabel = $derived(widgetKind === 'pbs' ? 'Datastore' : 'Node')

  $effect(() => {
    if (!widgetIntegrationId || widgetKind === 'customapi' || widgetKind === '') {
      widgetTargets = []
      widgetTargetsError = ''
      return
    }
    widgetTargetsLoading = true
    widgetTargetsError = ''
    app
      .discoverSavedTargets(widgetIntegrationId)
      .then((res) => {
        if (res.ok) {
          widgetTargets = res.targets ?? []
        } else {
          widgetTargets = []
          widgetTargetsError = res.error ?? 'Could not list targets.'
        }
      })
      .catch((e) => {
        widgetTargets = []
        widgetTargetsError = (e as Error).message
      })
      .finally(() => (widgetTargetsLoading = false))
  })

  onMount(() => dialog.showModal())

  function normalizeUrl(raw: string): string {
    const v = raw.trim()
    return v && !/^[a-z][a-z0-9+.-]*:\/\//i.test(v) ? `http://${v}` : v
  }

  function buildWidget() {
    if (!widgetIntegrationId || !selectedIntegration) return undefined
    if (selectedIntegration.kind === 'customapi') {
      return {
        integrationId: widgetIntegrationId,
        kind: 'customapi',
        query: { jsonPath: widgetJsonPath.trim(), label: widgetLabel.trim(), unit: widgetUnit.trim() },
        size: widgetSize,
      }
    }
    return {
      integrationId: widgetIntegrationId,
      kind: selectedIntegration.kind,
      query: { [targetParam]: widgetTarget },
      size: widgetSize,
    }
  }

  function submit(e: SubmitEvent) {
    e.preventDefault()
    const finalUrl = normalizeUrl(url)
    if (!name.trim()) return void (error = 'Enter a name for the service.')
    try {
      const u = new URL(finalUrl)
      if (u.protocol !== 'http:' && u.protocol !== 'https:') throw new Error()
    } catch {
      return void (error = 'Enter a valid address, for example https://192.168.1.10:8006.')
    }
    app.upsertItem(groupId, target, {
      id: item?.id ?? uid(),
      name: name.trim(),
      url: finalUrl,
      icon: icon.trim(),
      description: description.trim(),
      check: widgetIntegrationId ? false : check,
      widget: buildWidget(),
    })
    dialog.close()
  }

  function remove() {
    if (item && confirm(`Remove “${item.name}” from the dashboard?`)) {
      app.deleteItem(groupId, item.id)
      dialog.close()
    }
  }
</script>

<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_noninteractive_element_interactions -->
<dialog bind:this={dialog} class="dialog" {onclose} onclick={(e) => e.target === dialog && dialog.close()}>
  <form onsubmit={submit} novalidate>
    <h2>{item ? 'Edit service' : 'New service'}</h2>

    <label>
      Name
      <!-- svelte-ignore a11y_autofocus -->
      <input bind:value={name} maxlength="80" autofocus />
    </label>

    <label>
      Address
      <input bind:value={url} placeholder="https://192.168.1.10:8006" inputmode="url" />
    </label>

    <label>
      Icon
      <span class="with-preview">
        <input bind:value={icon} placeholder="proxmox" maxlength="500" />
        <Icon {icon} name={name || '?'} />
      </span>
      <small>Name from the dashboard-icons collection (e.g. proxmox, truenas-scale, adguard-home), an image address, a file in /icons/, or an emoji.</small>
    </label>

    <label>
      Description
      <input bind:value={description} maxlength="200" placeholder="If empty, shows the address" />
    </label>

    <label>
      Group
      <select bind:value={target}>
        {#each app.config!.groups as g (g.id)}
          <option value={g.id}>{g.name}</option>
        {/each}
      </select>
    </label>

    <label>
      Widget
      <select bind:value={widgetIntegrationId}>
        <option value="">None — plain link tile</option>
        {#each app.integrations as integration (integration.id)}
          <option value={integration.id}>{integration.name}</option>
        {/each}
      </select>
    </label>

    {#if widgetIntegrationId && widgetKind === 'customapi'}
      <label>
        JSON field path
        <input bind:value={widgetJsonPath} placeholder="data.nodes[0].cpu" />
      </label>
      <label>
        Label
        <input bind:value={widgetLabel} maxlength="40" />
      </label>
      <label>
        Unit (optional)
        <input bind:value={widgetUnit} maxlength="10" placeholder="%" />
      </label>
    {:else if widgetIntegrationId}
      <label>
        {targetLabel}
        {#if widgetTargetsLoading}
          <span class="hint">Loading...</span>
        {:else if widgetTargetsError}
          <span class="hint fail">{widgetTargetsError}</span>
        {:else}
          <select bind:value={widgetTarget}>
            <option value="">Choose one...</option>
            {#each widgetTargets as t (t.id)}
              <option value={t.id}>{t.label}</option>
            {/each}
          </select>
        {/if}
      </label>
    {/if}

    {#if widgetIntegrationId}
      <label>
        Size
        <select bind:value={widgetSize}>
          <option value="1x1">1x1</option>
          <option value="2x1">2x1 (wide)</option>
          <option value="2x2">2x2 (wide and tall)</option>
        </select>
      </label>
    {:else}
      <label class="check">
        <input type="checkbox" bind:checked={check} />
        Check whether the service is online
      </label>
    {/if}

    {#if error}<p class="form-error" role="alert">{error}</p>{/if}

    <div class="dialog-actions">
      {#if item}<button type="button" class="danger" onclick={remove}>Delete</button>{/if}
      <button type="button" onclick={() => dialog.close()}>Cancel</button>
      <button type="submit" class="primary">{item ? 'Save changes' : 'Add service'}</button>
    </div>
  </form>
</dialog>
```

Note: this drops the file's previous `<style>` block entirely — check the original file before replacing; if it has component-scoped styles beyond what `.dialog`/`.check`/`.hint`/`.fail` already provide globally (per `app.css`), carry those specific rules forward unchanged into a `<style>` block at the end of the new file. (The version read during this plan's research had no `<style>` block of its own — it relied entirely on global `app.css` classes — so this is likely a no-op, but verify against the actual file at implementation time rather than assuming.)

- [ ] **Step 2: Type-check**

Run: `npm run check` (from `ui/`)
Expected: 0 errors.

- [ ] **Step 3: Manual verification**

In the running dev instance: add a Proxmox VE integration (Settings, from Task 9) pointed at any reachable address even without a real token — it won't connect, which is fine for this check. Open "Add service", pick that integration from the Widget dropdown — confirm the JSON-path fields disappear and a "Node" field appears showing a loading state, then an error (since the fake integration can't actually connect) instead of a crash. Confirm picking the pre-existing Custom API (GitHub) integration still shows the JSON-path/label/unit fields exactly as before.

- [ ] **Step 4: Commit**

```bash
git add ui/src/components/ItemDialog.svelte
git commit -m "Add live node/datastore target picker to ItemDialog"
```

---

## Verification and release

### Task 11: Full verification pass

**Files:** none (verification only).

- [ ] **Step 1: Full backend test suite**

Run: `go test ./... -v`
Expected: every package PASSES, including all of Tasks 1-5's new tests and every pre-existing Phase 1 test.

- [ ] **Step 2: Backend format/vet**

Run: `gofmt -l . && go vet ./...`
Expected: both clean. (CRLF line-ending warnings from `git` on Windows are expected and unrelated — see the project's dev-environment notes; don't chase them.)

- [ ] **Step 3: Frontend type-check**

Run: `npm run check` (from `ui/`)
Expected: 0 errors.

- [ ] **Step 4: Rebuild and restart the dev instance**

The Go backend embeds the built frontend for production, but the dev workflow (`go run` + `npm run dev` on separate ports) doesn't need a rebuild to pick up frontend changes — Vite serves them directly. Confirm both dev processes (backend on :8080, frontend on :5173) are still the ones already running from the design session; restart either if it was stopped (`HOMIE_DATA=./data go run ./cmd/homie` / `npm run dev` from `ui/`).

- [ ] **Step 5: Manual end-to-end against a real host, if reachable**

If the user's real Proxmox host (`morpheus`) and/or PBS instance are reachable from this machine right now: create a real Proxmox VE API token (`PVEAuditor` role recommended — read-only) via the Proxmox web UI (Datacenter → Permissions → API Tokens), add a Proxmox VE integration in Homie with that Token ID/Secret, confirm "Test connection" succeeds, add a tile, confirm the node picker lists the real node(s), save, and confirm the tile renders real CPU/RAM/uptime/VM-LXC values in the 3-column grid. Repeat for PBS if it happens to be powered on right now (it usually isn't — see the spec). If neither host is reachable from this session, skip this step and note it explicitly in the task's completion notes — this is a real, known gap the user will need to close themselves against their actual hardware, not a failure of this plan.

- [ ] **Step 6: Stop the dev servers**

Once verification is done, free the ports for the production build/release in Task 13:

Run (adjust PIDs/ports to whatever is actually listening):
```bash
# Windows: find and stop whatever is listening on :8080 and :5173
netstat -ano | grep -E ':8080|:5173'
# then: taskkill //F //PID <pid>
```

- [ ] **Step 7: Commit**

No files change in this task — if Step 5 surfaced a real bug, fix it as its own properly-tested commit before continuing (don't fold an undiscovered bugfix silently into a "verification" commit). If nothing needed fixing, there is nothing to commit here; proceed to Task 12.

---

### Task 12: README update

**Files:**
- Modify: `README.md`

**Interfaces:** none — documentation only.

- [ ] **Step 1: Find and update the integrations section**

Open `README.md` and find wherever Phase 1 documented the integrations subsystem (per project memory, a recent commit "Document the integrations subsystem in the README" added this). Find the line(s) listing Custom API as the available widget/integration type, and add Proxmox VE and Proxmox Backup Server alongside it, in the same language and style as the surrounding text (the README is in Italian, unlike the app's own UI strings — match whatever is already there, don't translate the whole section to English).

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "Document Proxmox VE and PBS integrations in the README"
```

---

### Task 13: Version bump, push, and release

**Files:**
- Modify: wherever the current version string lives (check `cmd/homie/main.go` or a `VERSION` file — Phase 1's Makefile shows `VERSION ?= dev` passed via `-ldflags "-X main.version=$(VERSION)"`, so confirm whether there's also a committed version marker, e.g. in `README.md`'s badge or a changelog, before deciding what to bump).

**Interfaces:** none — release mechanics only.

- [ ] **Step 1: Decide the version number**

Current shipped version is v0.4.0 (Phase 1). This phase adds features without breaking any existing config/API shape (Phase 1's data model is additive-only extended, no field removed or renamed) — a minor version bump: **v0.5.0**.

- [ ] **Step 2: Update any committed version references**

Search for `0.4.0` across the repo (`grep -r "0.4.0" --include="*.md" --include="*.go" .`) and update each occurrence to `0.5.0`. Commit:

```bash
git add -A
git commit -m "Bump version to v0.5.0"
```

- [ ] **Step 3: Push the branch**

```bash
git push
```

- [ ] **Step 4: Build the release binary locally to confirm it builds clean**

```bash
make build VERSION=v0.5.0
```

Expected: `bin/homie` (or `bin/homie.exe` on Windows) produced with no errors — this is the same build Makefile target CI/the real deploy uses, so a local failure here means the release would also fail.

- [ ] **Step 5: Create the GitHub release**

```bash
git tag v0.5.0
git push origin v0.5.0
gh release create v0.5.0 --title "v0.5.0" --notes "$(cat <<'EOF'
## Proxmox VE and Proxmox Backup Server integrations

Two new zero-config integration drivers, on top of the Phase 1 platform:

- **Proxmox VE**: add a host and an API token, pick a node (works the same whether it's a standalone host or a cluster), get a live tile with CPU, RAM, uptime, and running VM/LXC counts.
- **Proxmox Backup Server**: add a host and an API token, pick a datastore, get a live tile with usage percentage and last backup status.

Also fixes a layout bug where widget tile content rendered squeezed into the tile's icon column instead of its full width, which also made long error messages wrap instead of truncating.
EOF
)"
```

- [ ] **Step 6: Confirm**

```bash
gh release view v0.5.0
```

Expected: the release is visible with the tag, notes, and (once GitHub finishes generating them) source archive links.

---

## Notes for whoever executes this plan

- The spec's "Endpoint verification" step (confirming real Proxmox VE/PBS API shapes before coding) was done as part of writing this plan, via web research rather than a live host — see Tasks 3 and 4's driver code comments for what's confirmed vs. best-effort (specifically: PBS's `worker_id`-to-datastore correlation in `pbs.go`'s `lastBackupLabel`). Task 11, Step 5 is where this gets its real test, if a host happens to be reachable during implementation.
- Go toolchain: reachable via wrapper scripts at `C:\Users\HanReal\bin\go` (already first on the Bash tool's `PATH` in this environment).
- `ui/node_modules` must exist (`npm install` from `ui/`) before `npm run check`/`npm run dev` will work — already done in this session's dev environment.
- This repo has pre-existing CRLF line endings locally (cosmetic — `git core.autocrlf=true` on this Windows machine); only `gofmt -w` files actually being edited in each task, never a repo-wide line-ending pass.
