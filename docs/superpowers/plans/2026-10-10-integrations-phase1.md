# Integrations Platform + Custom API Widget — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the pluggable integrations platform (`internal/integrations` driver registry, `secrets.json` storage, `/api/integrations` endpoints, widget data model) and validate it end-to-end with one real driver: Custom API.

**Architecture:** A `Driver` interface + self-registering registry in `internal/integrations` keeps adding a new integration type to "one new file." Non-secret integration config lives in `config.json` (existing `Store`); credentials live in a sibling `secrets.json` (new `SecretStore`), never serialized into any API response. A `Poller` (same periodic-loop shape as the existing `status.Checker`) fetches widget data on the config's check interval and keeps the last successful result when a fetch fails, so widgets always render "last known state" instead of breaking. The frontend adds an `Integrations` Settings section and renders widget-bearing tiles with a per-kind component instead of the generic status dot.

**Tech Stack:** Go 1.22 (stdlib only — no new dependencies), Svelte 5 + TypeScript.

**Spec:** `docs/superpowers/specs/2026-10-10-integrations-architecture-design.md`

## Global Constraints

- Go 1.22, stdlib only — no new entries in `go.mod`.
- `secrets.json`: mode `0600`, atomic write (temp file + rename), same pattern as `internal/config/config.go`'s `Store.write`.
- Every integration HTTP fetch: 10s context timeout, 2MB response body cap (`io.LimitReader`).
- TLS: `InsecureSkipVerify: true` for integration HTTP clients, matching `internal/status`'s existing precedent — self-signed certs are the homelab norm.
- No API response — ever — contains a secret value. Only `hasSecret: bool`.
- `gofmt -l .` must report nothing; `go vet ./...` must pass (existing CI gate).
- `npm run check` (svelte-check) must report 0 errors (existing CI gate).
- All new user-facing strings in English (established project-wide rule).

## Review Focus

- A widget whose `integrationId` points at a deleted/non-existent integration must still be a **valid** config — `Validate()` must accept it (the spec's "integration not found" is a normal render state, not a data error). Owning task: Task 5.
- No response body from any `/api/integrations*` endpoint may contain a configured secret value, even indirectly. Owning task: Task 7.
- `PUT /api/integrations/{id}` secret-patch semantics: a key present with `""` **deletes** that secret; a key **absent** from the patch leaves the existing value untouched. These are opposite behaviors and easy to invert. Owning task: Task 4 (store semantics) and Task 7 (handler wiring).
- A Custom API target that never responds must not hang the request — the 10s context timeout must be enforced end-to-end, not just configured and ignored. Owning task: Task 3.
- Widget `size` must be rejected by `Validate()` unless it's exactly `1x1`, `2x1`, or `2x2` — a bad value must never reach the frontend's CSS lookup. Owning task: Task 5.

---

## Backend

### Task 1: `internal/integrations` core types + registry

**Files:**
- Create: `internal/integrations/types.go`
- Create: `internal/integrations/registry.go`
- Test: `internal/integrations/registry_test.go`

**Interfaces:**
- Produces: `type Kind string`, `type Secrets map[string]string`, `type Value struct{Label, Formatted string; Raw any}`, `type Data struct{Values []Value; FetchedAt time.Time}`, `type Driver interface{Kind() Kind; TestConnection(ctx, cfg map[string]any, secrets Secrets) error; Fetch(ctx, cfg map[string]any, secrets Secrets, query map[string]any) (Data, error)}`, `func Register(d Driver)`, `func Get(kind Kind) (Driver, bool)`, `func Kinds() []Kind`.

- [ ] **Step 1: Write the failing test**

```go
// internal/integrations/registry_test.go
package integrations

import (
	"context"
	"testing"
)

type fakeDriver struct{ kind Kind }

func (f *fakeDriver) Kind() Kind { return f.kind }
func (f *fakeDriver) TestConnection(context.Context, map[string]any, Secrets) error { return nil }
func (f *fakeDriver) Fetch(context.Context, map[string]any, Secrets, map[string]any) (Data, error) {
	return Data{}, nil
}

func TestRegisterAndGet(t *testing.T) {
	Register(&fakeDriver{kind: "faketype"})

	d, ok := Get("faketype")
	if !ok {
		t.Fatal("expected Get to find the registered driver")
	}
	if d.Kind() != "faketype" {
		t.Fatalf("got kind %q, want %q", d.Kind(), "faketype")
	}

	if _, ok := Get("doesnotexist"); ok {
		t.Fatal("expected Get for an unregistered kind to return false")
	}

	found := false
	for _, k := range Kinds() {
		if k == "faketype" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected Kinds() to include the registered driver")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/integrations/... -v`
Expected: FAIL to compile — `Register`, `Get`, `Kind`, `Data` etc. are undefined.

- [ ] **Step 3: Write the implementation**

```go
// internal/integrations/types.go
// Package integrations defines the pluggable driver interface other
// services (Proxmox, TrueNAS, etc.) implement, and the registry that
// looks them up by kind. Adding a new integration type means adding one
// file that registers itself in init() — nothing else in this package,
// or in internal/server, needs to change.
package integrations

import (
	"context"
	"time"
)

// Kind identifies an integration driver, e.g. "customapi", "proxmoxve".
type Kind string

// Secrets holds an integration instance's credential fields. Keys are
// driver-specific (e.g. Custom API uses "headers").
type Secrets map[string]string

// Value is one labeled data point a widget displays.
type Value struct {
	Label     string `json:"label"`
	Formatted string `json:"formatted"`
	Raw       any    `json:"raw"`
}

// Data is the result of a successful Fetch.
type Data struct {
	Values    []Value   `json:"values"`
	FetchedAt time.Time `json:"fetchedAt"`
}

// Driver implements one integration type. cfg is an instance's non-secret
// configuration (e.g. a host address); query carries widget-specific
// display parameters (e.g. which field to show), kept separate from cfg
// so one instance can back several differently-configured widgets.
type Driver interface {
	Kind() Kind
	TestConnection(ctx context.Context, cfg map[string]any, secrets Secrets) error
	Fetch(ctx context.Context, cfg map[string]any, secrets Secrets, query map[string]any) (Data, error)
}
```

```go
// internal/integrations/registry.go
package integrations

import "sync"

var (
	mu       sync.RWMutex
	registry = map[Kind]Driver{}
)

// Register adds a driver to the registry. Call from a driver file's init().
func Register(d Driver) {
	mu.Lock()
	defer mu.Unlock()
	registry[d.Kind()] = d
}

// Get looks up a driver by kind.
func Get(kind Kind) (Driver, bool) {
	mu.RLock()
	defer mu.RUnlock()
	d, ok := registry[kind]
	return d, ok
}

// Kinds lists every registered driver kind.
func Kinds() []Kind {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Kind, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/integrations/... -v`
Expected: PASS

- [ ] **Step 5: Format and vet**

Run: `gofmt -l internal/integrations && go vet ./internal/integrations/...`
Expected: no output from gofmt, no errors from vet

- [ ] **Step 6: Commit**

```bash
git add internal/integrations/types.go internal/integrations/registry.go internal/integrations/registry_test.go
git commit -m "Add integrations driver interface and registry"
```

---

### Task 2: Custom API — JSON path resolver

**Files:**
- Create: `internal/integrations/customapi.go`
- Test: `internal/integrations/customapi_test.go`

**Interfaces:**
- Consumes: nothing from Task 1 yet (pure function).
- Produces: `func resolveJSONPath(root any, path string) (any, error)` — dot-separated segments, each optionally followed by `[N]` for one array index, e.g. `"data.nodes[0].cpu"`.

- [ ] **Step 1: Write the failing test**

```go
// internal/integrations/customapi_test.go
package integrations

import "testing"

func TestResolveJSONPath(t *testing.T) {
	doc := map[string]any{
		"data": map[string]any{
			"nodes": []any{
				map[string]any{"cpu": 0.42},
				map[string]any{"cpu": 0.11},
			},
			"name": "pve1",
		},
	}

	cases := []struct {
		name    string
		path    string
		want    any
		wantErr bool
	}{
		{"simple field", "data.name", "pve1", false},
		{"array index then field", "data.nodes[0].cpu", 0.42, false},
		{"second array element", "data.nodes[1].cpu", 0.11, false},
		{"missing field", "data.missing", nil, true},
		{"index out of range", "data.nodes[5].cpu", nil, true},
		{"index into non-array", "data.name[0]", nil, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := resolveJSONPath(doc, c.path)
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/integrations/... -run TestResolveJSONPath -v`
Expected: FAIL to compile — `resolveJSONPath` undefined.

- [ ] **Step 3: Write the implementation**

```go
// internal/integrations/customapi.go
package integrations

import (
	"fmt"
	"strconv"
	"strings"
)

// resolveJSONPath walks a decoded JSON value (the result of
// json.Unmarshal into `any`) following a dot-separated path. Each segment
// may end with one "[N]" array index, e.g. "data.nodes[0].cpu".
func resolveJSONPath(root any, path string) (any, error) {
	cur := root
	for _, seg := range strings.Split(path, ".") {
		if seg == "" {
			continue
		}
		name := seg
		idx := -1
		if open := strings.IndexByte(seg, '['); open >= 0 {
			if !strings.HasSuffix(seg, "]") {
				return nil, fmt.Errorf("invalid path segment %q: missing closing ]", seg)
			}
			name = seg[:open]
			n, err := strconv.Atoi(seg[open+1 : len(seg)-1])
			if err != nil {
				return nil, fmt.Errorf("invalid array index in %q: %w", seg, err)
			}
			idx = n
		}
		if name != "" {
			m, ok := cur.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("%q is not an object", name)
			}
			v, ok := m[name]
			if !ok {
				return nil, fmt.Errorf("field %q not found", name)
			}
			cur = v
		}
		if idx >= 0 {
			arr, ok := cur.([]any)
			if !ok {
				return nil, fmt.Errorf("%q is not an array", seg)
			}
			if idx >= len(arr) {
				return nil, fmt.Errorf("index %d out of range in %q (length %d)", idx, seg, len(arr))
			}
			cur = arr[idx]
		}
	}
	return cur, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/integrations/... -run TestResolveJSONPath -v`
Expected: PASS, all 6 subtests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/integrations/customapi.go internal/integrations/customapi_test.go
git commit -m "Add Custom API JSON path resolver"
```

---

### Task 3: Custom API driver — TestConnection + Fetch

**Files:**
- Modify: `internal/integrations/customapi.go`
- Modify: `internal/integrations/customapi_test.go`

**Interfaces:**
- Consumes: `Driver`, `Data`, `Value`, `Secrets`, `Register` (Task 1); `resolveJSONPath` (Task 2).
- Produces: a registered driver under `Kind("customapi")`. Instance `cfg`: `{"address": string, "headers": []string}` (header names). Instance `secrets`: `{"headers": "<json {name: value}>"}`. Widget `query`: `{"jsonPath": string, "label": string, "unit": string}`.

- [ ] **Step 1: Write the failing tests**

```go
// append to internal/integrations/customapi_test.go
package integrations

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCustomAPITestConnection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := &customAPIDriver{client: &http.Client{Timeout: 2 * time.Second}}
	cfg := map[string]any{"address": srv.URL, "headers": []any{"Authorization"}}
	secrets := Secrets{"headers": `{"Authorization":"Bearer secret-token"}`}

	if err := d.TestConnection(context.Background(), cfg, secrets); err != nil {
		t.Fatalf("expected success with correct token, got %v", err)
	}

	badSecrets := Secrets{"headers": `{"Authorization":"Bearer wrong"}`}
	if err := d.TestConnection(context.Background(), cfg, badSecrets); err == nil {
		t.Fatal("expected an error with a wrong token")
	}
}

func TestCustomAPIFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"nodes": []any{map[string]any{"cpu": 42}}},
		})
	}))
	defer srv.Close()

	d := &customAPIDriver{client: &http.Client{Timeout: 2 * time.Second}}
	cfg := map[string]any{"address": srv.URL}
	query := map[string]any{"jsonPath": "data.nodes[0].cpu", "label": "CPU", "unit": "%"}

	data, err := d.Fetch(context.Background(), cfg, Secrets{}, query)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(data.Values) != 1 {
		t.Fatalf("expected 1 value, got %d", len(data.Values))
	}
	v := data.Values[0]
	if v.Label != "CPU" || v.Formatted != "42%" {
		t.Fatalf("got %+v", v)
	}
}

func TestCustomAPIFetchTimesOut(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Use a client timeout much shorter than the handler's sleep so the
	// test runs fast while still proving the timeout is enforced.
	d := &customAPIDriver{client: &http.Client{Timeout: 20 * time.Millisecond}}
	cfg := map[string]any{"address": srv.URL}

	start := time.Now()
	_, err := d.Fetch(context.Background(), cfg, Secrets{}, map[string]any{"jsonPath": "x"})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if elapsed > 150*time.Millisecond {
		t.Fatalf("expected the call to fail fast via timeout, took %v", elapsed)
	}
}

func TestCustomAPIRegistered(t *testing.T) {
	if _, ok := Get("customapi"); !ok {
		t.Fatal("expected the customapi driver to self-register via init()")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/integrations/... -v`
Expected: FAIL to compile — `customAPIDriver` undefined.

- [ ] **Step 3: Write the implementation**

```go
// append to internal/integrations/customapi.go
package integrations

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

const maxCustomAPIBody = 2 << 20 // 2MB

type customAPIDriver struct {
	client *http.Client
}

func init() {
	Register(&customAPIDriver{client: &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			// Self-signed certificates are the norm in a homelab —
			// matches internal/status's existing checker precedent.
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}})
}

func (d *customAPIDriver) Kind() Kind { return "customapi" }

func customAPIHeaders(cfg map[string]any, secrets Secrets) (http.Header, error) {
	h := http.Header{}
	names, _ := cfg["headers"].([]any)
	var values map[string]string
	if raw, ok := secrets["headers"]; ok && raw != "" {
		if err := json.Unmarshal([]byte(raw), &values); err != nil {
			return nil, fmt.Errorf("invalid stored header secrets: %w", err)
		}
	}
	for _, n := range names {
		name, ok := n.(string)
		if !ok || name == "" {
			continue
		}
		h.Set(name, values[name])
	}
	return h, nil
}

func (d *customAPIDriver) request(ctx context.Context, cfg map[string]any, secrets Secrets) ([]byte, error) {
	address, _ := cfg["address"].(string)
	if address == "" {
		return nil, errors.New("missing address")
	}
	headers, err := customAPIHeaders(cfg, secrets)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, fmt.Errorf("invalid address: %w", err)
	}
	req.Header = headers
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxCustomAPIBody))
}

func (d *customAPIDriver) TestConnection(ctx context.Context, cfg map[string]any, secrets Secrets) error {
	_, err := d.request(ctx, cfg, secrets)
	return err
}

func (d *customAPIDriver) Fetch(ctx context.Context, cfg map[string]any, secrets Secrets, query map[string]any) (Data, error) {
	body, err := d.request(ctx, cfg, secrets)
	if err != nil {
		return Data{}, err
	}
	var decoded any
	if err := json.Unmarshal(body, &decoded); err != nil {
		return Data{}, fmt.Errorf("response is not valid JSON: %w", err)
	}
	path, _ := query["jsonPath"].(string)
	if path == "" {
		return Data{}, errors.New("missing jsonPath")
	}
	val, err := resolveJSONPath(decoded, path)
	if err != nil {
		return Data{}, err
	}
	label, _ := query["label"].(string)
	unit, _ := query["unit"].(string)
	return Data{
		Values:    []Value{{Label: label, Formatted: fmt.Sprint(val) + unit, Raw: val}},
		FetchedAt: time.Now(),
	}, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/integrations/... -v`
Expected: PASS — all tests including `TestCustomAPIFetchTimesOut` complete in well under a second.

- [ ] **Step 5: Format and vet**

Run: `gofmt -l internal/integrations && go vet ./internal/integrations/...`
Expected: clean

- [ ] **Step 6: Commit**

```bash
git add internal/integrations/customapi.go internal/integrations/customapi_test.go
git commit -m "Add Custom API driver: TestConnection and Fetch"
```

---

### Task 4: `SecretStore`

**Files:**
- Create: `internal/config/secrets.go`
- Test: `internal/config/secrets_test.go`

**Interfaces:**
- Produces: `func OpenSecrets(dir string) (*SecretStore, error)`, `(s *SecretStore) Has(id string) bool`, `(s *SecretStore) Get(id string) (map[string]string, bool)`, `(s *SecretStore) Set(id string, patch map[string]string) error`, `(s *SecretStore) Delete(id string) error`.

- [ ] **Step 1: Write the failing test**

```go
// internal/config/secrets_test.go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSecretStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenSecrets(dir)
	if err != nil {
		t.Fatalf("OpenSecrets: %v", err)
	}

	if s.Has("abc") {
		t.Fatal("expected no secret for an unknown id")
	}

	if err := s.Set("abc", map[string]string{"token": "xyz", "note": "keep"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if !s.Has("abc") {
		t.Fatal("expected Has to be true after Set")
	}
	got, ok := s.Get("abc")
	if !ok || got["token"] != "xyz" || got["note"] != "keep" {
		t.Fatalf("got %v, ok=%v", got, ok)
	}

	// Absent key in a patch leaves other keys untouched.
	if err := s.Set("abc", map[string]string{"token": "updated"}); err != nil {
		t.Fatalf("Set (patch): %v", err)
	}
	got, _ = s.Get("abc")
	if got["token"] != "updated" || got["note"] != "keep" {
		t.Fatalf("patch should only touch the given key, got %v", got)
	}

	// An explicit empty string deletes that key.
	if err := s.Set("abc", map[string]string{"note": ""}); err != nil {
		t.Fatalf("Set (delete key): %v", err)
	}
	got, _ = s.Get("abc")
	if _, exists := got["note"]; exists {
		t.Fatalf("expected \"note\" to be deleted, got %v", got)
	}
	if got["token"] != "updated" {
		t.Fatalf("expected \"token\" to survive, got %v", got)
	}

	if err := s.Delete("abc"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if s.Has("abc") {
		t.Fatal("expected Has to be false after Delete")
	}
}

func TestSecretStoreFilePermissions(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenSecrets(dir)
	if err != nil {
		t.Fatalf("OpenSecrets: %v", err)
	}
	if err := s.Set("abc", map[string]string{"token": "xyz"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, "secrets.json"))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("got mode %o, want 0600", mode)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -run TestSecretStore -v`
Expected: FAIL to compile — `OpenSecrets` undefined.

- [ ] **Step 3: Write the implementation**

```go
// internal/config/secrets.go
package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// SecretStore keeps integration credentials on disk, separate from
// config.json, at <dir>/secrets.json with 0600 permissions. Values are
// never exposed through the HTTP API — only whether a given id has any
// secret set at all (see Has).
type SecretStore struct {
	path string
	mu   sync.RWMutex
}

func OpenSecrets(dir string) (*SecretStore, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	s := &SecretStore{path: filepath.Join(dir, "secrets.json")}
	if _, err := os.Stat(s.path); errors.Is(err, os.ErrNotExist) {
		if err := s.writeAll(map[string]map[string]string{}); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	return s, nil
}

func (s *SecretStore) readAll() (map[string]map[string]string, error) {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return nil, err
	}
	out := map[string]map[string]string{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// writeAll saves atomically: temp file (created 0600 by os.CreateTemp),
// then rename — same pattern as Store.write.
func (s *SecretStore) writeAll(m map[string]map[string]string) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".secrets-*.json")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), s.path)
}

func (s *SecretStore) Has(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m, err := s.readAll()
	if err != nil {
		return false
	}
	return len(m[id]) > 0
}

func (s *SecretStore) Get(id string) (map[string]string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m, err := s.readAll()
	if err != nil {
		return nil, false
	}
	v, ok := m[id]
	return v, ok
}

// Set merges patch into the stored secrets for id. A key present with an
// empty value deletes that key; a key absent from patch is left as-is.
func (s *SecretStore) Set(id string, patch map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.readAll()
	if err != nil {
		return err
	}
	cur := m[id]
	if cur == nil {
		cur = map[string]string{}
	}
	for k, v := range patch {
		if v == "" {
			delete(cur, k)
		} else {
			cur[k] = v
		}
	}
	m[id] = cur
	return s.writeAll(m)
}

func (s *SecretStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.readAll()
	if err != nil {
		return err
	}
	delete(m, id)
	return s.writeAll(m)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/... -run TestSecretStore -v`
Expected: PASS

- [ ] **Step 5: Format and vet**

Run: `gofmt -l internal/config && go vet ./internal/config/...`
Expected: clean

- [ ] **Step 6: Commit**

```bash
git add internal/config/secrets.go internal/config/secrets_test.go
git commit -m "Add SecretStore for integration credentials"
```

---

### Task 5: `config.go` — Integration/Widget data model

**Files:**
- Modify: `internal/config/config.go`
- Create: `internal/config/config_test.go`

**Interfaces:**
- Produces: `type Integration struct{ID, Kind, Name string; Config map[string]any}`, `type Widget struct{IntegrationID, Kind string; Query map[string]any; Size string}`, `Item.Widget *Widget`, `Config.Integrations []Integration`.

- [ ] **Step 1: Write the failing tests**

```go
// internal/config/config_test.go
package config

import "testing"

func baseConfigWithWidget(widget *Widget, integrations []Integration) Config {
	c := Default()
	c.Integrations = integrations
	c.Groups = []Group{{
		ID:   "g1",
		Name: "Group",
		Items: []Item{{
			ID:     "i1",
			Name:   "Widget tile",
			URL:    "http://example.com",
			Widget: widget,
		}},
	}}
	return c
}

func TestValidateWidgetRequiresKnownSize(t *testing.T) {
	c := baseConfigWithWidget(&Widget{IntegrationID: "int1", Kind: "customapi", Size: "huge"}, []Integration{
		{ID: "int1", Kind: "customapi", Name: "My API"},
	})
	if err := c.Validate(); err == nil {
		t.Fatal("expected an error for an invalid widget size")
	}
}

func TestValidateWidgetAllowsDanglingIntegrationID(t *testing.T) {
	// The integration this widget points at doesn't exist (e.g. it was
	// deleted after the widget was created). The spec requires this to
	// stay a VALID config — the UI renders "integration not found"
	// rather than the save being rejected.
	c := baseConfigWithWidget(&Widget{IntegrationID: "does-not-exist", Kind: "customapi", Size: "1x1"}, nil)
	if err := c.Validate(); err != nil {
		t.Fatalf("expected a dangling integrationId to be valid, got %v", err)
	}
}

func TestValidateWidgetKindMustMatchIntegrationWhenItExists(t *testing.T) {
	c := baseConfigWithWidget(&Widget{IntegrationID: "int1", Kind: "wrongkind", Size: "1x1"}, []Integration{
		{ID: "int1", Kind: "customapi", Name: "My API"},
	})
	if err := c.Validate(); err == nil {
		t.Fatal("expected an error when the widget kind doesn't match its integration's kind")
	}
}

func TestValidateWidgetRequiresNonEmptyIntegrationID(t *testing.T) {
	c := baseConfigWithWidget(&Widget{IntegrationID: "", Kind: "customapi", Size: "1x1"}, nil)
	if err := c.Validate(); err == nil {
		t.Fatal("expected an error for an empty integrationId")
	}
}

func TestValidateIntegrationRequiresUniqueID(t *testing.T) {
	c := Default()
	c.Integrations = []Integration{
		{ID: "dup", Kind: "customapi", Name: "A"},
		{ID: "dup", Kind: "customapi", Name: "B"},
	}
	if err := c.Validate(); err == nil {
		t.Fatal("expected an error for duplicate integration ids")
	}
}

func TestNormalizeDefaultsIntegrationsAndForcesCheckOff(t *testing.T) {
	c := Config{Groups: []Group{{
		ID:   "g1",
		Name: "Group",
		Items: []Item{{
			ID:     "i1",
			Name:   "Widget tile",
			Check:  true,
			Widget: &Widget{IntegrationID: "x", Kind: "customapi", Size: "1x1"},
		}},
	}}}
	c.normalize()
	if c.Integrations == nil {
		t.Fatal("expected Integrations to default to an empty slice, got nil")
	}
	if c.Groups[0].Items[0].Check {
		t.Fatal("expected Check to be forced false on an item with a widget")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/... -run TestValidateWidget -v`
Expected: FAIL to compile — `Widget`, `Integration`, `Item.Widget` undefined.

- [ ] **Step 3: Modify the implementation**

In `internal/config/config.go`, add new types after the existing `Group` struct and before `Config`:

```go
type Integration struct {
	ID     string         `json:"id"`
	Kind   string         `json:"kind"`
	Name   string         `json:"name"`
	Config map[string]any `json:"config"`
}

type Widget struct {
	IntegrationID string         `json:"integrationId"`
	Kind          string         `json:"kind"`
	Query         map[string]any `json:"query"`
	Size          string         `json:"size"` // "1x1" | "2x1" | "2x2"
}
```

Change `Item` to add the `Widget` field:

```go
type Item struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	URL         string  `json:"url"`
	Icon        string  `json:"icon"`
	Description string  `json:"description"`
	Check       bool    `json:"check"`
	Widget      *Widget `json:"widget,omitempty"`
}
```

Change `Config` to add `Integrations`:

```go
type Config struct {
	Title        string        `json:"title"`
	Settings     Settings      `json:"settings"`
	Groups       []Group       `json:"groups"`
	Integrations []Integration `json:"integrations"`
}
```

In `normalize()`, replace:

```go
	for i := range c.Groups {
		if c.Groups[i].Items == nil {
			c.Groups[i].Items = []Item{}
		}
	}
}
```

with:

```go
	if c.Integrations == nil {
		c.Integrations = []Integration{}
	}
	for i := range c.Groups {
		if c.Groups[i].Items == nil {
			c.Groups[i].Items = []Item{}
		}
		for j := range c.Groups[i].Items {
			if c.Groups[i].Items[j].Widget != nil {
				// A widget's own poll result is the liveness signal;
				// the lightweight HTTP check is for plain link tiles.
				c.Groups[i].Items[j].Check = false
			}
		}
	}
}
```

In `Validate()`, after the existing group/item loop's closing `}` (right before `return nil`), add:

```go
	validSizes := map[string]bool{"1x1": true, "2x1": true, "2x2": true}
	integrationsByID := map[string]Integration{}
	for _, in := range c.Integrations {
		if in.ID == "" || seen[in.ID] {
			return fmt.Errorf("integration %q has a missing or duplicate id", in.Name)
		}
		seen[in.ID] = true
		if in.Kind == "" {
			return fmt.Errorf("integration %q is missing a kind", in.Name)
		}
		if in.Name == "" || len(in.Name) > 60 {
			return errors.New("every integration needs a name (60 characters max)")
		}
		integrationsByID[in.ID] = in
	}
	for _, g := range c.Groups {
		for _, it := range g.Items {
			if it.Widget == nil {
				continue
			}
			w := it.Widget
			if w.IntegrationID == "" {
				return fmt.Errorf("the widget on %q is missing an integration", it.Name)
			}
			if !validSizes[w.Size] {
				return fmt.Errorf("invalid widget size on %q: %q", it.Name, w.Size)
			}
			// A dangling integrationId (its integration was since
			// deleted) is allowed: the UI shows "integration not
			// found" rather than rejecting the whole save.
			if in, ok := integrationsByID[w.IntegrationID]; ok && in.Kind != w.Kind {
				return fmt.Errorf("the widget on %q doesn't match its integration's kind", it.Name)
			}
		}
	}
```

Note: this second loop re-walks `c.Groups`/`g.Items` separately from the
existing validation loop above it (which already validates id uniqueness,
name length, description/icon length, and URL format) rather than being
folded into it, because it needs `integrationsByID` built from the *full*
`c.Integrations` list first — and `c.Integrations` is validated in its own
loop immediately before this one, so a single combined pass would need to
interleave two independent concerns. Two short, readable loops over
`Validate`'s existing data beat one tangled one.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/... -v`
Expected: PASS — every test in the package, including the pre-existing `SecretStore` tests from Task 4.

- [ ] **Step 5: Format and vet**

Run: `gofmt -l internal/config && go vet ./internal/config/...`
Expected: clean

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "Add Integration/Widget data model to config"
```

---

### Task 6: `Poller`

**Files:**
- Create: `internal/integrations/poller.go`
- Test: `internal/integrations/poller_test.go`

**Interfaces:**
- Consumes: `config.Store`, `config.SecretStore` (constructors `config.Open`/`config.OpenSecrets` from the existing package), `integrations.Get`, `Driver.Fetch` (Tasks 1/3).
- Produces: `type WidgetResult struct{Data Data; FetchedAt time.Time; Error string}`, `func NewPoller(store *config.Store, secrets *config.SecretStore) *Poller`, `(p *Poller) Wake()`, `(p *Poller) Results() map[string]WidgetResult`, `(p *Poller) PollOnce(ctx context.Context)`, `(p *Poller) Run(ctx context.Context)`.

- [ ] **Step 1: Write the failing test**

```go
// internal/integrations/poller_test.go
package integrations

import (
	"context"
	"errors"
	"testing"

	"github.com/Bagareal/homie/internal/config"
)

type scriptedDriver struct {
	kind      Kind
	responses []func() (Data, error)
	call      int
}

func (d *scriptedDriver) Kind() Kind { return d.kind }
func (d *scriptedDriver) TestConnection(context.Context, map[string]any, Secrets) error { return nil }
func (d *scriptedDriver) Fetch(context.Context, map[string]any, Secrets, map[string]any) (Data, error) {
	r := d.responses[d.call]
	if d.call < len(d.responses)-1 {
		d.call++
	}
	return r()
}

func TestPollerKeepsLastDataOnFailure(t *testing.T) {
	driver := &scriptedDriver{
		kind: "scripted",
		responses: []func() (Data, error){
			func() (Data, error) { return Data{Values: []Value{{Label: "ok", Formatted: "1"}}}, nil },
			func() (Data, error) { return Data{}, errors.New("target unreachable") },
		},
	}
	Register(driver)

	dir := t.TempDir()
	store, err := config.Open(dir)
	if err != nil {
		t.Fatalf("config.Open: %v", err)
	}
	secrets, err := config.OpenSecrets(dir)
	if err != nil {
		t.Fatalf("config.OpenSecrets: %v", err)
	}

	cfg := store.Get()
	cfg.Integrations = []config.Integration{{ID: "int1", Kind: "scripted", Name: "Scripted"}}
	cfg.Groups = []config.Group{{
		ID:   "g1",
		Name: "Group",
		Items: []config.Item{{
			ID:     "i1",
			Name:   "Widget tile",
			URL:    "http://example.com",
			Widget: &config.Widget{IntegrationID: "int1", Kind: "scripted", Size: "1x1"},
		}},
	}}
	if err := store.Set(cfg); err != nil {
		t.Fatalf("store.Set: %v", err)
	}

	p := NewPoller(store, secrets)

	p.PollOnce(context.Background())
	r, ok := p.Results()["i1"]
	if !ok || r.Error != "" || len(r.Data.Values) != 1 || r.Data.Values[0].Formatted != "1" {
		t.Fatalf("expected a successful first result, got %+v (ok=%v)", r, ok)
	}
	firstFetchedAt := r.FetchedAt

	p.PollOnce(context.Background())
	r2 := p.Results()["i1"]
	if r2.Error == "" {
		t.Fatal("expected the second poll's error to be recorded")
	}
	if len(r2.Data.Values) != 1 || r2.Data.Values[0].Formatted != "1" {
		t.Fatalf("expected the previous successful data to be kept, got %+v", r2.Data)
	}
	if !r2.FetchedAt.Equal(firstFetchedAt) {
		t.Fatal("expected FetchedAt to stay at the last successful fetch, not update on failure")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/integrations/... -run TestPoller -v`
Expected: FAIL to compile — `NewPoller`, `PollOnce`, `Results` undefined.

- [ ] **Step 3: Write the implementation**

```go
// internal/integrations/poller.go
package integrations

import (
	"context"
	"sync"
	"time"

	"github.com/Bagareal/homie/internal/config"
)

// WidgetResult is the latest known state of one widget-bearing item. On a
// failed fetch, Data and FetchedAt are left at their last successful
// values and only Error is updated — this is what lets the UI show
// "last known state" instead of blanking out.
type WidgetResult struct {
	Data      Data      `json:"data"`
	FetchedAt time.Time `json:"fetchedAt"`
	Error     string    `json:"error,omitempty"`
}

// Poller periodically fetches data for every widget-bearing item, on the
// same interval/Wake() shape as internal/status.Checker.
type Poller struct {
	store   *config.Store
	secrets *config.SecretStore
	wake    chan struct{}
	mu      sync.RWMutex
	results map[string]WidgetResult
}

func NewPoller(store *config.Store, secrets *config.SecretStore) *Poller {
	return &Poller{
		store:   store,
		secrets: secrets,
		wake:    make(chan struct{}, 1),
		results: map[string]WidgetResult{},
	}
}

func (p *Poller) Wake() {
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

func (p *Poller) Results() map[string]WidgetResult {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make(map[string]WidgetResult, len(p.results))
	for k, v := range p.results {
		out[k] = v
	}
	return out
}

func (p *Poller) Run(ctx context.Context) {
	for {
		p.PollOnce(ctx)
		interval := time.Duration(p.store.Get().Settings.CheckInterval) * time.Second
		t := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-p.wake:
			t.Stop()
		case <-t.C:
		}
	}
}

// PollOnce fetches every widget-bearing item once, each isolated so one
// slow or failing integration never affects another.
func (p *Poller) PollOnce(ctx context.Context) {
	cfg := p.store.Get()
	integrationsByID := make(map[string]config.Integration, len(cfg.Integrations))
	for _, in := range cfg.Integrations {
		integrationsByID[in.ID] = in
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for _, g := range cfg.Groups {
		for _, it := range g.Items {
			if it.Widget == nil {
				continue
			}
			itemID, w := it.ID, it.Widget
			in, ok := integrationsByID[w.IntegrationID]
			if !ok {
				// Dangling reference: no data to fetch, leave whatever
				// result (if any) is already stored — the frontend
				// treats a missing integration as its own render state.
				continue
			}
			driver, ok := Get(Kind(in.Kind))
			if !ok {
				continue
			}
			secrets, _ := p.secrets.Get(in.ID)

			wg.Add(1)
			sem <- struct{}{}
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				data, err := driver.Fetch(ctx, in.Config, secrets, w.Query)
				p.mu.Lock()
				prev := p.results[itemID]
				if err != nil {
					prev.Error = err.Error()
				} else {
					prev = WidgetResult{Data: data, FetchedAt: data.FetchedAt}
				}
				p.results[itemID] = prev
				p.mu.Unlock()
			}()
		}
	}
	wg.Wait()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/integrations/... -v`
Expected: PASS — every test in the package.

- [ ] **Step 5: Format and vet**

Run: `gofmt -l internal/integrations internal/config && go vet ./...`
Expected: clean

- [ ] **Step 6: Commit**

```bash
git add internal/integrations/poller.go internal/integrations/poller_test.go
git commit -m "Add widget Poller"
```

---

### Task 7: `/api/integrations` + `/api/widgets` endpoints

**Files:**
- Modify: `internal/server/server.go`
- Create: `internal/server/integrations.go`
- Create: `internal/server/integrations_test.go`

**Interfaces:**
- Consumes: `config.Store`, `config.SecretStore`, `config.Integration`, `integrations.Poller`, `integrations.Get`, `integrations.Kind` (Tasks 4-6).
- Produces: `Server.secrets *config.SecretStore`, `Server.widgets *integrations.Poller` fields; new `server.New` signature `New(store *config.Store, checker *status.Checker, widgets *integrations.Poller, secrets *config.SecretStore, ui fs.FS, dataDir, version string) *Server`; routes below.

- [ ] **Step 1: Write the failing tests**

```go
// internal/server/integrations_test.go
package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Bagareal/homie/internal/config"
	"github.com/Bagareal/homie/internal/integrations"
	"github.com/Bagareal/homie/internal/status"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	store, err := config.Open(dir)
	if err != nil {
		t.Fatalf("config.Open: %v", err)
	}
	secrets, err := config.OpenSecrets(dir)
	if err != nil {
		t.Fatalf("config.OpenSecrets: %v", err)
	}
	checker := status.New(store)
	widgets := integrations.NewPoller(store, secrets)
	return New(store, checker, widgets, secrets, nil, dir, "test")
}

func doJSON(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		r = httptest.NewRequest(method, path, bytes.NewReader(b))
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestIntegrationLifecycle(t *testing.T) {
	s := newTestServer(t)
	h := s.Handler()

	// Create with a secret.
	createBody := map[string]any{
		"name": "My API",
		"kind": "customapi",
		"config": map[string]any{"address": "http://example.com", "headers": []string{"Authorization"}},
		"secrets": map[string]string{"headers": `{"Authorization":"Bearer super-secret-value"}`},
	}
	w := doJSON(t, h, "POST", "/api/integrations", createBody)
	if w.Code != http.StatusOK {
		t.Fatalf("create: got %d, body %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "super-secret-value") {
		t.Fatal("create response must never contain the secret value")
	}
	var created struct {
		ID        string `json:"id"`
		HasSecret bool   `json:"hasSecret"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.ID == "" || !created.HasSecret {
		t.Fatalf("got %+v", created)
	}

	// List must never leak the secret either.
	w = doJSON(t, h, "GET", "/api/integrations", nil)
	if strings.Contains(w.Body.String(), "super-secret-value") {
		t.Fatal("list response must never contain the secret value")
	}

	// Update: rename only, secrets omitted entirely -> must be preserved.
	w = doJSON(t, h, "PUT", "/api/integrations/"+created.ID, map[string]any{
		"name":   "Renamed API",
		"config": map[string]any{"address": "http://example.com", "headers": []string{"Authorization"}},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("update: got %d, body %s", w.Code, w.Body.String())
	}
	got, _ := s.secrets.Get(created.ID)
	if got["headers"] != `{"Authorization":"Bearer super-secret-value"}` {
		t.Fatalf("expected the stored secret to survive an update that omits it, got %v", got)
	}

	// Update: explicitly clear the secret with an empty string.
	w = doJSON(t, h, "PUT", "/api/integrations/"+created.ID, map[string]any{
		"name":    "Renamed API",
		"config":  map[string]any{"address": "http://example.com", "headers": []string{"Authorization"}},
		"secrets": map[string]string{"headers": ""},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("update (clear secret): got %d, body %s", w.Code, w.Body.String())
	}
	if s.secrets.Has(created.ID) {
		t.Fatal("expected the secret to be cleared")
	}

	// Delete: an item whose widget references this integration must
	// remain a VALID config afterwards (dangling reference is allowed).
	cfg := s.store.Get()
	cfg.Groups = []config.Group{{
		ID:   "g1",
		Name: "Group",
		Items: []config.Item{{
			ID:     "i1",
			Name:   "Widget tile",
			URL:    "http://example.com",
			Widget: &config.Widget{IntegrationID: created.ID, Kind: "customapi", Size: "1x1"},
		}},
	}}
	if err := s.store.Set(cfg); err != nil {
		t.Fatalf("seed widget: %v", err)
	}

	w = doJSON(t, h, "DELETE", "/api/integrations/"+created.ID, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("delete: got %d, body %s", w.Code, w.Body.String())
	}
	if s.secrets.Has(created.ID) {
		t.Fatal("expected the secret to be deleted")
	}
	w = doJSON(t, h, "GET", "/api/config", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected the config (with its now-dangling widget) to still load fine, got %d: %s", w.Code, w.Body.String())
	}
}

func TestIntegrationTestEndpointBeforeSave(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	s := newTestServer(t)
	h := s.Handler()

	w := doJSON(t, h, "POST", "/api/integrations/test", map[string]any{
		"kind":   "customapi",
		"config": map[string]any{"address": target.URL},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("got %d, body %s", w.Code, w.Body.String())
	}
	var res struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil || !res.OK {
		t.Fatalf("expected ok=true, got %s (err=%v)", w.Body.String(), err)
	}

	w = doJSON(t, h, "POST", "/api/integrations/test", map[string]any{
		"kind":   "customapi",
		"config": map[string]any{"address": "http://127.0.0.1:1"}, // nothing listening
	})
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res.OK {
		t.Fatal("expected ok=false against an unreachable address")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/server/... -v`
Expected: FAIL to compile — `New` has the wrong signature, `s.secrets`/`s.store` undefined, routes missing.

- [ ] **Step 3: Write the implementation**

Modify `internal/server/server.go`:

```go
import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"net/http"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/Bagareal/homie/internal/config"
	"github.com/Bagareal/homie/internal/integrations"
	"github.com/Bagareal/homie/internal/status"
)
```

```go
type Server struct {
	store   *config.Store
	checker *status.Checker
	widgets *integrations.Poller
	secrets *config.SecretStore
	ui      fs.FS
	dataDir string
	version string
}

func New(store *config.Store, checker *status.Checker, widgets *integrations.Poller, secrets *config.SecretStore, ui fs.FS, dataDir, version string) *Server {
	return &Server{store: store, checker: checker, widgets: widgets, secrets: secrets, ui: ui, dataDir: dataDir, version: version}
}
```

In `Handler()`, add the new routes alongside the existing ones:

```go
	mux.HandleFunc("GET /api/status", s.getStatus)
	mux.HandleFunc("GET /api/version", s.getVersion)
	mux.HandleFunc("GET /api/widgets", s.getWidgets)
	mux.HandleFunc("GET /api/integrations", s.listIntegrations)
	mux.HandleFunc("POST /api/integrations", s.createIntegration)
	mux.HandleFunc("POST /api/integrations/test", s.testIntegration)
	mux.HandleFunc("PUT /api/integrations/{id}", s.updateIntegration)
	mux.HandleFunc("DELETE /api/integrations/{id}", s.deleteIntegration)
	mux.HandleFunc("POST /api/integrations/{id}/test", s.testSavedIntegration)
	mux.HandleFunc("GET /icons/{name}", s.icon)
```

Also wake the widget poller after a config save, alongside the existing
checker wake, in `putConfig`:

```go
	if err := s.store.Set(c); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.checker.Wake()
	s.widgets.Wake()
	writeJSON(w, http.StatusOK, s.store.Get())
```

Add `getWidgets`:

```go
func (s *Server) getWidgets(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.widgets.Results())
}
```

Create `internal/server/integrations.go`:

```go
package server

import (
	"encoding/json"
	"net/http"

	"github.com/Bagareal/homie/internal/config"
	"github.com/Bagareal/homie/internal/integrations"
)

// integrationView is what GET /api/integrations returns: the stored
// non-secret fields plus whether a secret is set, never the secret
// itself.
type integrationView struct {
	config.Integration
	HasSecret bool `json:"hasSecret"`
}

func (s *Server) integrationViews() []integrationView {
	cfg := s.store.Get()
	out := make([]integrationView, 0, len(cfg.Integrations))
	for _, in := range cfg.Integrations {
		out = append(out, integrationView{Integration: in, HasSecret: s.secrets.Has(in.ID)})
	}
	return out
}

func (s *Server) listIntegrations(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.integrationViews())
}

type integrationRequest struct {
	Name    string            `json:"name"`
	Kind    string            `json:"kind"`
	Config  map[string]any    `json:"config"`
	Secrets map[string]string `json:"secrets"`
}

func decodeIntegrationRequest(w http.ResponseWriter, r *http.Request) (integrationRequest, bool) {
	var req integrationRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "unreadable request: invalid JSON or too large")
		return req, false
	}
	return req, true
}

func (s *Server) createIntegration(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeIntegrationRequest(w, r)
	if !ok {
		return
	}
	cfg := s.store.Get()
	in := config.Integration{ID: uid(), Kind: req.Kind, Name: req.Name, Config: req.Config}
	cfg.Integrations = append(cfg.Integrations, in)
	if err := s.store.Set(cfg); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(req.Secrets) > 0 {
		if err := s.secrets.Set(in.ID, req.Secrets); err != nil {
			writeError(w, http.StatusInternalServerError, "saved the integration but failed to store its credentials: "+err.Error())
			return
		}
	}
	s.widgets.Wake()
	writeJSON(w, http.StatusOK, integrationView{Integration: in, HasSecret: s.secrets.Has(in.ID)})
}

func (s *Server) updateIntegration(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	req, ok := decodeIntegrationRequest(w, r)
	if !ok {
		return
	}
	cfg := s.store.Get()
	idx := -1
	for i, in := range cfg.Integrations {
		if in.ID == id {
			idx = i
		}
	}
	if idx < 0 {
		writeError(w, http.StatusNotFound, "integration not found")
		return
	}
	cfg.Integrations[idx].Name = req.Name
	cfg.Integrations[idx].Config = req.Config
	if err := s.store.Set(cfg); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(req.Secrets) > 0 {
		if err := s.secrets.Set(id, req.Secrets); err != nil {
			writeError(w, http.StatusInternalServerError, "saved the integration but failed to update its credentials: "+err.Error())
			return
		}
	}
	s.widgets.Wake()
	writeJSON(w, http.StatusOK, integrationView{Integration: cfg.Integrations[idx], HasSecret: s.secrets.Has(id)})
}

func (s *Server) deleteIntegration(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cfg := s.store.Get()
	kept := cfg.Integrations[:0]
	found := false
	for _, in := range cfg.Integrations {
		if in.ID == id {
			found = true
			continue
		}
		kept = append(kept, in)
	}
	if !found {
		writeError(w, http.StatusNotFound, "integration not found")
		return
	}
	cfg.Integrations = kept
	if err := s.store.Set(cfg); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.secrets.Delete(id); err != nil {
		writeError(w, http.StatusInternalServerError, "deleted the integration but failed to remove its credentials: "+err.Error())
		return
	}
	s.widgets.Wake()
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

type testRequest struct {
	Kind    string            `json:"kind"`
	Config  map[string]any    `json:"config"`
	Secrets map[string]string `json:"secrets"`
}

type testResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

func runTestConnection(w http.ResponseWriter, r *http.Request, kind string, cfg map[string]any, secrets integrations.Secrets) {
	driver, ok := integrations.Get(integrations.Kind(kind))
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown integration kind: "+kind)
		return
	}
	err := driver.TestConnection(r.Context(), cfg, secrets)
	res := testResponse{OK: err == nil}
	if err != nil {
		res.Error = err.Error()
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) testIntegration(w http.ResponseWriter, r *http.Request) {
	var req testRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "unreadable request: invalid JSON or too large")
		return
	}
	runTestConnection(w, r, req.Kind, req.Config, req.Secrets)
}

func (s *Server) testSavedIntegration(w http.ResponseWriter, r *http.Request) {
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
	runTestConnection(w, r, in.Kind, in.Config, secrets)
}
```

Add a tiny id generator so the server package doesn't need to import
anything frontend-specific (the frontend already has its own `uid()` for
groups/items; the backend needs one too for integrations created without
a client-supplied id). `crypto/rand` and `encoding/hex` are already in the
import block edited above. Add this function to `internal/server/server.go`,
near the other helpers (`writeJSON`/`writeError`):

```go
func uid() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/server/... -v`
Expected: PASS

- [ ] **Step 5: Format and vet**

Run: `gofmt -l internal/server && go vet ./...`
Expected: clean

- [ ] **Step 6: Commit**

```bash
git add internal/server/server.go internal/server/integrations.go internal/server/integrations_test.go
git commit -m "Add /api/integrations and /api/widgets endpoints"
```

---

### Task 8: Wire `SecretStore` and `Poller` into `main.go`

**Files:**
- Modify: `cmd/homie/main.go`

**Interfaces:**
- Consumes: `config.OpenSecrets` (Task 4), `integrations.NewPoller` (Task 6), new `server.New` signature (Task 7).

- [ ] **Step 1: Modify the implementation**

```go
import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Bagareal/homie/internal/config"
	"github.com/Bagareal/homie/internal/integrations"
	"github.com/Bagareal/homie/internal/server"
	"github.com/Bagareal/homie/internal/status"
	"github.com/Bagareal/homie/internal/webui"
)
```

Replace the body from `store, err := config.Open(dataDir)` through the
`srv := &http.Server{...}` block:

```go
	store, err := config.Open(dataDir)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	secrets, err := config.OpenSecrets(dataDir)
	if err != nil {
		log.Fatalf("secrets: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	checker := status.New(store)
	go checker.Run(ctx)

	widgets := integrations.NewPoller(store, secrets)
	go widgets.Run(ctx)

	srv := &http.Server{
		Addr:              addr,
		Handler:           server.New(store, checker, widgets, secrets, webui.FS(), dataDir, version).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
```

- [ ] **Step 2: Verify it builds**

Run: `go build ./...`
Expected: no output, exit 0.

- [ ] **Step 3: Run the full test suite**

Run: `go test ./... && gofmt -l . && go vet ./...`
Expected: all packages pass, gofmt reports nothing, vet reports nothing.

- [ ] **Step 4: Commit**

```bash
git add cmd/homie/main.go
git commit -m "Wire SecretStore and widget Poller into main"
```

---

### Task 9: CI — run Go tests, add a `make test` target

**Files:**
- Modify: `.github/workflows/ci.yml`
- Modify: `Makefile`

**Interfaces:**
- Consumes: nothing new — wires up what Tasks 1-8 already built.

- [ ] **Step 1: Modify `.github/workflows/ci.yml`**

Replace:

```yaml
      - name: Backend
        run: |
          test -z "$(gofmt -l .)"
          go vet ./...
          go build ./cmd/homie
```

with:

```yaml
      - name: Backend
        run: |
          test -z "$(gofmt -l .)"
          go vet ./...
          go test ./...
          go build ./cmd/homie
```

Also translate the file's one remaining Italian comment while touching
this file — replace:

```yaml
# Controlla che tutto compili a ogni push e pull request.
```

with:

```yaml
# Checks that everything builds on every push and pull request.
```

- [ ] **Step 2: Add a `test` target to `Makefile`**

Add, right after the `build` target:

```makefile
test:
	go test ./...
```

And update the `.PHONY` line at the top from:

```makefile
.PHONY: ui build run dev clean
```

to:

```makefile
.PHONY: ui build run dev test clean
```

- [ ] **Step 3: Verify locally**

Run: `make test`
Expected: PASS — this runs every Go test added in Tasks 1-7 in one shot.

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/ci.yml Makefile
git commit -m "Run go test in CI, add a make test target"
```

---

## Frontend

### Task 10: Types + API client

**Files:**
- Modify: `ui/src/lib/types.ts`
- Modify: `ui/src/lib/api.ts`

**Interfaces:**
- Produces: `Integration`, `Widget`, `WidgetResult`, `WidgetSize` types; `Item.widget?`; `Config.integrations`; `getIntegrations`, `createIntegration`, `updateIntegration`, `deleteIntegration`, `testIntegration`, `testSavedIntegration`, `getWidgets` functions.

- [ ] **Step 1: Modify `ui/src/lib/types.ts`**

```ts
export type WidgetSize = '1x1' | '2x1' | '2x2'

export interface Widget {
  integrationId: string
  kind: string
  query: Record<string, unknown>
  size: WidgetSize
}

export interface Item {
  id: string
  name: string
  url: string
  icon: string
  description: string
  check: boolean
  widget?: Widget
}

export interface Integration {
  id: string
  kind: string
  name: string
  config: Record<string, unknown>
  hasSecret: boolean
}

export interface WidgetValue {
  label: string
  formatted: string
  raw: unknown
}

export interface WidgetData {
  values: WidgetValue[]
  fetchedAt: string
}

export interface WidgetResult {
  data: WidgetData
  fetchedAt: string
  error?: string
}
```

Change `Config` to add `integrations`:

```ts
export interface Config {
  title: string
  settings: Settings
  groups: Group[]
  integrations: Integration[]
}
```

- [ ] **Step 2: Modify `ui/src/lib/api.ts`**

Add at the end of the file:

```ts
export const getIntegrations = () => request<Integration[]>('api/integrations')

export const createIntegration = (body: {
  name: string
  kind: string
  config: Record<string, unknown>
  secrets: Record<string, string>
}) =>
  request<Integration>('api/integrations', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })

export const updateIntegration = (
  id: string,
  body: { name: string; config: Record<string, unknown>; secrets?: Record<string, string> },
) =>
  request<Integration>(`api/integrations/${id}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })

export const deleteIntegration = (id: string) => request<{ deleted: boolean }>(`api/integrations/${id}`, { method: 'DELETE' })

export const testIntegration = (body: { kind: string; config: Record<string, unknown>; secrets: Record<string, string> }) =>
  request<{ ok: boolean; error?: string }>('api/integrations/test', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })

export const testSavedIntegration = (id: string) =>
  request<{ ok: boolean; error?: string }>(`api/integrations/${id}/test`, { method: 'POST' })

export const getWidgets = () => request<Record<string, WidgetResult>>('api/widgets')
```

Add the new types to the top-of-file import:

```ts
import type { Config, Integration, Status, WidgetResult } from './types'
```

- [ ] **Step 3: Verify**

Run: `cd ui && npm run check`
Expected: 0 errors.

- [ ] **Step 4: Commit**

```bash
git add ui/src/lib/types.ts ui/src/lib/api.ts
git commit -m "Add Integration/Widget types and API client functions"
```

---

### Task 11: Client state for integrations and widgets

**Files:**
- Modify: `ui/src/lib/state.svelte.ts`
- Modify: `ui/src/App.svelte`

**Interfaces:**
- Consumes: `api.getIntegrations`, `api.createIntegration`, `api.updateIntegration`, `api.deleteIntegration`, `api.testIntegration`, `api.testSavedIntegration`, `api.getWidgets` (Task 10).
- Produces: `app.integrations: Integration[]`, `app.widgets: Record<string, WidgetResult>`, `app.loadIntegrations()`, `app.createIntegration(...)`, `app.updateIntegration(...)`, `app.deleteIntegration(id)`, `app.testIntegration(...)`, `app.testSavedIntegration(id)`, `app.refreshWidgets()`.

- [ ] **Step 1: Modify `ui/src/lib/state.svelte.ts`**

Change the top import:

```ts
import type { Config, Group, Integration, Item, Status, WidgetResult } from './types'
```

Add two new `$state` fields alongside the existing ones (`status`, `editing`, etc.):

```ts
  integrations = $state<Integration[]>([])
  widgets = $state<Record<string, WidgetResult>>({})
```

Add new methods at the end of the class, right before the closing `}` of `AppState`:

```ts
  async loadIntegrations() {
    try {
      this.integrations = await api.getIntegrations()
    } catch {
      // The Settings page shows its own error state on next render.
    }
  }

  async refreshWidgets() {
    try {
      this.widgets = await api.getWidgets()
    } catch {
      // Next tick retries; widgets keep showing their last known data.
    }
  }

  async createIntegration(body: { name: string; kind: string; config: Record<string, unknown>; secrets: Record<string, string> }) {
    const created = await api.createIntegration(body)
    this.integrations = [...this.integrations, created]
    this.notify('Integration added')
    return created
  }

  async updateIntegration(id: string, body: { name: string; config: Record<string, unknown>; secrets?: Record<string, string> }) {
    const updated = await api.updateIntegration(id, body)
    this.integrations = this.integrations.map((i) => (i.id === id ? updated : i))
    this.notify('Integration updated')
    return updated
  }

  async deleteIntegration(id: string) {
    await api.deleteIntegration(id)
    this.integrations = this.integrations.filter((i) => i.id !== id)
    this.notify('Integration deleted')
  }

  testIntegration(body: { kind: string; config: Record<string, unknown>; secrets: Record<string, string> }) {
    return api.testIntegration(body)
  }

  testSavedIntegration(id: string) {
    return api.testSavedIntegration(id)
  }
```

- [ ] **Step 2: Modify `ui/src/App.svelte`**

In `onMount`, alongside the existing `app.load()` call, add:

```ts
    app.load().catch((e: Error) => (loadError = e.message))
    app.loadIntegrations()
```

Add a second `$effect` for widgets, mirroring the existing status-refresh
effect:

```ts
  $effect(() => {
    const interval = app.config?.settings.checkInterval
    if (!interval) return
    app.refreshWidgets()
    const t = setInterval(() => app.refreshWidgets(), Math.min(interval, 30) * 1000)
    return () => clearInterval(t)
  })
```

- [ ] **Step 3: Verify**

Run: `cd ui && npm run check`
Expected: 0 errors.

- [ ] **Step 4: Commit**

```bash
git add ui/src/lib/state.svelte.ts ui/src/App.svelte
git commit -m "Add integrations and widgets client state"
```

---

### Task 12: `IntegrationDialog.svelte`

**Files:**
- Create: `ui/src/components/IntegrationDialog.svelte`

**Interfaces:**
- Consumes: `app.createIntegration`, `app.updateIntegration`, `app.deleteIntegration`, `app.testIntegration`, `app.testSavedIntegration` (Task 11); `app.integrations` to find the one being edited.

- [ ] **Step 1: Write the component**

```svelte
<script lang="ts">
  import { onMount } from 'svelte'
  import { app } from '../lib/state.svelte'

  let { integrationId, onclose }: { integrationId: string | null; onclose: () => void } = $props()

  let dialog: HTMLDialogElement
  // svelte-ignore state_referenced_locally
  const existing = integrationId ? app.integrations.find((i) => i.id === integrationId) : undefined

  let name = $state(existing?.name ?? '')
  let address = $state((existing?.config.address as string) ?? '')
  // svelte-ignore state_referenced_locally
  let headerNames = $state<string[]>((existing?.config.headers as string[]) ?? [])
  let headerValues = $state<Record<string, string>>({})
  let error = $state('')
  let testResult = $state<{ ok: boolean; error?: string } | null>(null)
  let testing = $state(false)
  let saving = $state(false)

  onMount(() => dialog.showModal())

  function addHeaderRow() {
    headerNames = [...headerNames, '']
  }
  function removeHeaderRow(i: number) {
    const name = headerNames[i]
    headerNames = headerNames.filter((_, idx) => idx !== i)
    const { [name]: _removed, ...rest } = headerValues
    headerValues = rest
  }

  function buildConfig() {
    return { address: address.trim(), headers: headerNames.filter((n) => n.trim()) }
  }
  function buildSecrets(): Record<string, string> {
    const values: Record<string, string> = {}
    for (const n of headerNames) {
      if (n.trim() && headerValues[n]) values[n] = headerValues[n]
    }
    return { headers: Object.keys(values).length ? JSON.stringify(values) : '' }
  }

  async function test() {
    testing = true
    testResult = null
    try {
      testResult = existing
        ? await app.testSavedIntegration(existing.id)
        : await app.testIntegration({ kind: 'customapi', config: buildConfig(), secrets: buildSecrets() })
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
        await app.updateIntegration(existing.id, { name: name.trim(), config: buildConfig(), secrets: buildSecrets() })
      } else {
        await app.createIntegration({ name: name.trim(), kind: 'customapi', config: buildConfig(), secrets: buildSecrets() })
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
    <h2>{existing ? 'Edit integration' : 'New integration'} (Custom API)</h2>

    <label>
      Name
      <!-- svelte-ignore a11y_autofocus -->
      <input bind:value={name} maxlength="60" autofocus />
    </label>

    <label>
      Address
      <input bind:value={address} placeholder="https://..." inputmode="url" />
    </label>

    <div class="header-rows">
      <p class="hint">Headers (e.g. Authorization). Values are write-only and never shown again.</p>
      {#each headerNames as hname, i (i)}
        <div class="header-row">
          <input bind:value={headerNames[i]} placeholder="Header name" />
          <input type="password" bind:value={headerValues[hname]} placeholder="Value" />
          <button type="button" onclick={() => removeHeaderRow(i)} aria-label="Remove header">&times;</button>
        </div>
      {/each}
      <button type="button" onclick={addHeaderRow}>Add header</button>
    </div>

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

- [ ] **Step 2: Verify**

Run: `cd ui && npm run check`
Expected: 0 errors.

- [ ] **Step 3: Commit**

```bash
git add ui/src/components/IntegrationDialog.svelte
git commit -m "Add IntegrationDialog component"
```

---

### Task 13: Settings → Integrations section

**Files:**
- Modify: `ui/src/components/Settings.svelte`

**Interfaces:**
- Consumes: `app.integrations` (Task 11), `IntegrationDialog` (Task 12).

- [ ] **Step 1: Modify `ui/src/components/Settings.svelte`**

Add to the script block's imports and state:

```ts
  import IntegrationDialog from './IntegrationDialog.svelte'
```

```ts
  let integrationEdit = $state<{ id: string | null } | null>(null)
```

Add a new `<section>` right before the closing `</main>`:

```svelte
  <section>
    <h2>Integrations</h2>
    <p class="hint">Connect other services so dashboard tiles can show live data.</p>
    {#if app.integrations.length === 0}
      <p class="hint">No integrations yet.</p>
    {:else}
      <ul class="integration-list">
        {#each app.integrations as integration (integration.id)}
          <li>
            <button type="button" class="integration-row" onclick={() => (integrationEdit = { id: integration.id })}>
              <span class="name">{integration.name}</span>
              <span class="kind">{integration.kind}</span>
              <span class="badge" class:on={integration.hasSecret}>{integration.hasSecret ? 'configured' : 'not configured'}</span>
            </button>
          </li>
        {/each}
      </ul>
    {/if}
    <button type="button" onclick={() => (integrationEdit = { id: null })}>Add integration</button>
  </section>
```

Add right before the closing `</main>` (after the new section) — the
dialog mount point, matching the pattern used for `groupEdit`/`itemEdit`
in `Dashboard.svelte`:

```svelte
{#if integrationEdit}
  <IntegrationDialog integrationId={integrationEdit.id} onclose={() => (integrationEdit = null)} />
{/if}
```

Add minimal styling in a `<style>` block at the end of the file:

```svelte
<style>
  .integration-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 6px;
  }
  .integration-row {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    text-align: left;
    padding: 8px 10px;
    border: 1px solid var(--line);
    border-radius: 5px;
    background: var(--tile);
  }
  .integration-row .kind {
    color: var(--muted);
    font-size: 0.85rem;
  }
  .badge {
    margin-left: auto;
    font-size: 0.8rem;
    color: var(--muted);
  }
  .badge.on {
    color: var(--up);
  }
</style>
```

- [ ] **Step 2: Verify**

Run: `cd ui && npm run check && npm run build`
Expected: 0 errors, build succeeds.

- [ ] **Step 3: Commit**

```bash
git add ui/src/components/Settings.svelte
git commit -m "Add Integrations section to Settings"
```

---

### Task 14: `CustomApiWidget.svelte` + relative time helper

**Files:**
- Create: `ui/src/lib/time.ts`
- Create: `ui/src/components/CustomApiWidget.svelte`

**Interfaces:**
- Produces: `formatRelativeTime(iso: string): string`; `<CustomApiWidget result={...} />` taking `result: WidgetResult | undefined`.

- [ ] **Step 1: Write `ui/src/lib/time.ts`**

```ts
export function formatRelativeTime(iso: string): string {
  const then = new Date(iso).getTime()
  if (Number.isNaN(then)) return ''
  const seconds = Math.max(0, Math.floor((Date.now() - then) / 1000))
  if (seconds < 60) return 'just now'
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m ago`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}h ago`
  const days = Math.floor(hours / 24)
  return `${days}d ago`
}
```

- [ ] **Step 2: Write `ui/src/components/CustomApiWidget.svelte`**

```svelte
<script lang="ts">
  import type { WidgetResult } from '../lib/types'
  import { formatRelativeTime } from '../lib/time'

  let { result }: { result: WidgetResult | undefined } = $props()

  const value = $derived(result?.data?.values?.[0])
  const hasData = $derived(!!value)
  const updated = $derived(result?.fetchedAt ? formatRelativeTime(result.fetchedAt) : '')
</script>

<div class="widget" class:stale={!!result?.error}>
  {#if hasData}
    <span class="value">{value!.formatted}</span>
    {#if value!.label}<span class="label">{value!.label}</span>{/if}
    {#if updated}<span class="updated">updated {updated}</span>{/if}
  {:else if result?.error}
    <span class="error">{result.error}</span>
  {:else}
    <span class="updated">waiting for first check</span>
  {/if}
  {#if hasData && result?.error}
    <span class="error-dot" title={result.error}></span>
  {/if}
</div>

<style>
  .widget {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }
  .value {
    font-size: 1.3rem;
    font-weight: 700;
    line-height: 1.1;
  }
  .label {
    color: var(--muted);
    font-size: 0.85rem;
  }
  .updated {
    color: var(--muted);
    font-size: 0.75rem;
  }
  .widget.stale .value,
  .widget.stale .label {
    opacity: 0.55;
  }
  .error {
    color: var(--down);
    font-size: 0.85rem;
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

- [ ] **Step 3: Verify**

Run: `cd ui && npm run check`
Expected: 0 errors.

- [ ] **Step 4: Commit**

```bash
git add ui/src/lib/time.ts ui/src/components/CustomApiWidget.svelte
git commit -m "Add CustomApiWidget render component"
```

---

### Task 15: `ItemDialog.svelte` — attach a widget to a tile

**Files:**
- Modify: `ui/src/components/ItemDialog.svelte`

**Interfaces:**
- Consumes: `app.integrations` (Task 11), `Widget`/`WidgetSize` types (Task 10).

- [ ] **Step 1: Modify the script block**

Add to state (alongside the existing `check`/`target` state):

```ts
  // svelte-ignore state_referenced_locally
  const customApiIntegrations = $derived(app.integrations.filter((i) => i.kind === 'customapi'))
  // svelte-ignore state_referenced_locally
  let widgetIntegrationId = $state(item?.widget?.integrationId ?? '')
  // svelte-ignore state_referenced_locally
  let widgetJsonPath = $state((item?.widget?.query.jsonPath as string) ?? '')
  // svelte-ignore state_referenced_locally
  let widgetLabel = $state((item?.widget?.query.label as string) ?? '')
  // svelte-ignore state_referenced_locally
  let widgetUnit = $state((item?.widget?.query.unit as string) ?? '')
  // svelte-ignore state_referenced_locally
  let widgetSize = $state(item?.widget?.size ?? '1x1')
```

In `submit`, change the `app.upsertItem` call to build the widget
conditionally:

```ts
    app.upsertItem(groupId, target, {
      id: item?.id ?? uid(),
      name: name.trim(),
      url: finalUrl,
      icon: icon.trim(),
      description: description.trim(),
      check: widgetIntegrationId ? false : check,
      widget: widgetIntegrationId
        ? {
            integrationId: widgetIntegrationId,
            kind: 'customapi',
            query: { jsonPath: widgetJsonPath.trim(), label: widgetLabel.trim(), unit: widgetUnit.trim() },
            size: widgetSize,
          }
        : undefined,
    })
```

- [ ] **Step 2: Modify the markup**

Replace the existing "check" `<label class="check">` block with this
(widget section first, then the check toggle only shown when no widget is
attached):

```svelte
    <label>
      Widget
      <select bind:value={widgetIntegrationId}>
        <option value="">None — plain link tile</option>
        {#each customApiIntegrations as integration (integration.id)}
          <option value={integration.id}>{integration.name}</option>
        {/each}
      </select>
    </label>

    {#if widgetIntegrationId}
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
```

- [ ] **Step 3: Verify**

Run: `cd ui && npm run check`
Expected: 0 errors.

- [ ] **Step 4: Commit**

```bash
git add ui/src/components/ItemDialog.svelte
git commit -m "Add widget attachment fields to ItemDialog"
```

---

### Task 16: `Tile.svelte` + grid CSS — render widgets, size spanning

**Files:**
- Modify: `ui/src/components/Tile.svelte`
- Modify: `ui/src/app.css`

**Interfaces:**
- Consumes: `CustomApiWidget` (Task 14), `app.widgets` (Task 11).

- [ ] **Step 1: Modify `ui/src/components/Tile.svelte`**

Add the import:

```ts
  import CustomApiWidget from './CustomApiWidget.svelte'
```

Add a derived value for the span class, alongside the existing derived
values:

```ts
  const sizeClass = $derived(item.widget ? `size-${item.widget.size}` : '')
```

Replace the `{#snippet body()}` block's status-dot section so a widget
replaces it when present:

```svelte
{#snippet body()}
  <Icon icon={item.icon} name={item.name} />
  <span class="text">
    <span class="name">{item.name}</span>
    {#if !item.widget}<span class="desc">{item.description || host}</span>{/if}
  </span>
  {#if item.widget}
    <CustomApiWidget result={app.widgets[item.id]} />
  {:else if led !== 'off'}
    <span class="led {led}" title={ledLabel}></span>
    <span class="sr-only">{ledLabel}</span>
  {/if}
{/snippet}
```

Add `class:tile-widget={!!item.widget}` and the size class to both the
editing `<button>` and the plain `<a>` — modify their class attributes:

```svelte
    class="tile editing {sizeClass}"
    class:dragging={app.drag?.kind === 'item' && app.drag.id === item.id}
    class:drop-before={drop === 'before'}
    class:drop-after={drop === 'after'}
```

and:

```svelte
  <a class="tile {sizeClass}" href={item.url} target={newTab ? '_blank' : undefined} rel="noopener noreferrer">
```

- [ ] **Step 2: Modify `ui/src/app.css`**

Add after the existing `.tile` rule:

```css
.tile.size-2x1,
.tile.size-2x2 {
  grid-column: span 2;
}

.tile.size-2x2 {
  grid-row: span 2;
}
```

- [ ] **Step 3: Verify**

Run: `cd ui && npm run check && npm run build`
Expected: 0 errors, build succeeds.

- [ ] **Step 4: Manual end-to-end smoke test**

Run: `make dev` (starts the Go backend on :8080 and the Vite dev server on :5173)

1. Open the dashboard, enter edit mode, open Settings → Integrations → Add integration.
2. Name it "Self", address `http://localhost:8080/api/version`, no headers, click "Test connection" — expect "Connection OK".
3. Save it. Add or edit a service tile, under Widget pick "Self", JSON field path `version`, label `Homie`, size `1x1`. Save.
4. Confirm the tile shows the running version string instead of the online/offline dot, with an "updated just now" caption.
5. Stop the backend (`Ctrl+C` the `go run` process). Wait for the next poll interval. Confirm the tile keeps showing the last known version, now with a dimmed appearance, an error dot, and an "updated Xm ago" caption that keeps advancing — not a blank or broken tile.

- [ ] **Step 5: Commit**

```bash
git add ui/src/components/Tile.svelte ui/src/app.css
git commit -m "Render Custom API widgets on tiles, add size spanning"
```
