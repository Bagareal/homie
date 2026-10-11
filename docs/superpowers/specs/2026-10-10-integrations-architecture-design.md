# Integrations architecture + Custom API widget — design

Status: approved for implementation
Phase: 1 of 5 (integrations platform + Custom API widget)

## Goal

Add a pluggable "integrations" subsystem to Homie so dashboard tiles can show
live data pulled from other services (Proxmox VE, Proxmox Backup Server,
TrueNAS, AdGuard Home, and anything else via a generic Custom API widget),
instead of just a link and an up/down dot.

This phase builds the platform and validates it with one real integration
type: **Custom API**. Later phases (separate specs) add Proxmox VE, Proxmox
Backup Server, TrueNAS, and AdGuard Home drivers, then Proxmox guest
auto-discovery, reusing this same platform unchanged.

## Non-goals (this phase)

- No Proxmox VE / PBS / TrueNAS / AdGuard drivers yet — only the generic
  Custom API one, enough to prove the architecture.
- No authentication layer for Homie itself. Anyone who can reach the
  dashboard today can already read and rewrite `config.json` via the API;
  this phase doesn't change that trust boundary, it only makes sure
  **credentials for other services** don't leak through it.
- No widget types beyond the single-value Custom API widget.

## Architecture

### `internal/integrations` package

```go
type Kind string

type Secrets map[string]string

type Value struct {
    Label     string
    Formatted string
    Raw       any
}

type Data struct {
    Values    []Value
    FetchedAt time.Time
}

type Driver interface {
    Kind() Kind
    // TestConnection validates that cfg+secrets can actually reach and
    // authenticate against the target. Used both by the "Test connection"
    // button and before persisting a new/edited instance.
    TestConnection(ctx context.Context, cfg map[string]any, secrets Secrets) error
    // Fetch retrieves current data. query carries widget-specific display
    // parameters (e.g. which JSON field to show), separate from the
    // instance's connection config, so one instance can back many widgets.
    Fetch(ctx context.Context, cfg map[string]any, secrets Secrets, query map[string]any) (Data, error)
}

func Register(d Driver)       // called from each driver's init()
func Get(kind Kind) (Driver, bool)
func Kinds() []Kind
```

Each integration type is one file under `internal/integrations/` (e.g.
`customapi.go`) that registers itself in `init()`. The registry
(`registry.go`) holds a `map[Kind]Driver` guarded by a mutex (registration
only happens at program init, but keep it safe regardless). Adding a new
integration type means adding one file and never touching the registry,
the API layer, or the poller.

### Why a registry+interface instead of a big switch statement

A `switch kind { case "proxmoxve": ... }` scattered across the API handlers
and the poller would mean every new integration type touches 3-4 existing
files. The registry keeps the blast radius of "add a type" to one new file,
which is the explicit requirement from the brief. The small cost is one
extra layer of indirection (`integrations.Get(kind)`), which is worth it
here given 5 types are already planned.

## Data model

### Go (`internal/config`)

```go
type Integration struct {
    ID     string         `json:"id"`
    Kind   string         `json:"kind"`
    Name   string         `json:"name"`
    Config map[string]any `json:"config"` // non-secret, kind-specific
}

type Widget struct {
    IntegrationID string         `json:"integrationId"`
    Kind          string         `json:"kind"`
    Query         map[string]any `json:"query"` // display params, e.g. jsonPath/label/unit
    Size          string         `json:"size"`  // "1x1" | "2x1" | "2x2"
}

type Item struct {
    ID          string  `json:"id"`
    Name        string  `json:"name"`
    URL         string  `json:"url"`
    Icon        string  `json:"icon"`
    Description string  `json:"description"`
    Check       bool    `json:"check"`
    Widget      *Widget `json:"widget,omitempty"`
}

type Config struct {
    Title        string        `json:"title"`
    Settings     Settings      `json:"settings"`
    Groups       []Group       `json:"groups"`
    Integrations []Integration `json:"integrations"`
}
```

`Validate()` gains:
- Integration `id` non-empty and unique (same `seen` set used for groups/items).
- Integration `kind` must be a registered driver (`integrations.Get` succeeds).
- `Name` non-empty, reasonable length (60 chars, matching group names).
- An `Item.Widget`, when set: `integrationId` must reference an existing
  integration, `kind` must match that integration's kind, `size` must be
  one of the three allowed values.
- When `Widget` is set, `Check` is forced to `false` on save (the
  integration poll is the liveness signal instead — see below). Documented
  behavior, not a validation error.

`normalize()` defaults `Integrations` to `[]Integration{}` when absent, so
existing `config.json` files from before this change load unchanged.

### Secrets store (`internal/config`, sibling to the existing `Store`)

New file `<data>/secrets.json`, mode `0600`, written atomically the same
way as `config.json` (temp file + rename):

```json
{
  "<integration-id>": { "headers": "{\"Authorization\":\"Bearer ...\"}" }
}
```

Each integration's secret fields are a flat `map[string]string`; a driver
decides what keys it needs (Custom API uses one key, `headers`, holding a
JSON-encoded `map[string]string` of header name → value — see below).
`SecretStore.Get(id) (Secrets, bool)`, `Set(id string, patch Secrets)`
(merges; a key present with an empty string deletes that key, a key absent
from the patch is left untouched), `Delete(id)`.

The secrets file is never serialized into any API response and is
excluded from "Export configuration" (that endpoint already only ever
reads from the `Store`, not `SecretStore`, so this is true by construction
— no extra filtering code needed, which is itself worth stating plainly in
the handler as a comment so it isn't "accidentally" wired up later).

## API endpoints

All under `/api/integrations`, separate from the existing `PUT /api/config`
— deliberately, so the generic config endpoint is structurally incapable of
accepting a secret value.

| Method & path | Body | Notes |
|---|---|---|
| `GET /api/integrations` | — | Returns instances with `hasSecret: bool` computed from `SecretStore`, never the secret value itself. |
| `POST /api/integrations` | `{name, kind, config, secrets}` | Creates. Validates via `Validate()` + `TestConnection` is **not** required to save, so a user can save now and fix credentials later — but the UI always offers Test first. |
| `PUT /api/integrations/{id}` | `{name, config, secrets}` | Updates. In `secrets`, an **absent** key leaves that secret field untouched; a key present with value `""` clears it. This lets you rename an instance without retyping its token. |
| `DELETE /api/integrations/{id}` | — | Deletes the instance and its secrets. Widgets referencing it are left in place (not auto-deleted) and render an "integration not found" state rather than failing to load the page. |
| `POST /api/integrations/test` | `{kind, config, secrets}` | Tests **before saving** — lets the UI validate credentials while you're still typing them. |
| `POST /api/integrations/{id}/test` | — | Re-tests an already-saved instance, reusing its stored secret. |

`writeError` continues to be the shared error envelope. `TestConnection`
failures return `200 {ok:false, error:"..."}` rather than a 4xx/5xx — a
failed test is an expected, common outcome, not a client error.

Note on overlap with `PUT /api/config`: `Config.Integrations` is part of
the regular config blob, so a normal group/item save (which round-trips
the full config, `integrations` included) naturally leaves it untouched.
The dedicated endpoints above are what the Settings → Integrations UI
actually calls for create/edit/delete, since only they know how to route
secret fields into `secrets.json`; a hand-crafted `PUT /api/config` could
in principle edit the non-secret half of an integration directly, but
that's no different from the pre-existing fact that the whole API is
unauthenticated (see Non-goals) — not a new gap introduced here.

## Security

- The Custom API fetch (user-chosen address + headers) happens **only** on
  the backend, never the browser — otherwise the secret headers would have
  to reach client JS. This is a deliberate, scoped SSRF: the backend will
  fetch whatever URL an admin configures. In a single-admin homelab
  dashboard this is the intended use (reaching Proxmox/TrueNAS on the LAN),
  and whoever can configure a widget already has full read/write access to
  `config.json` via the existing API — so this doesn't cross a trust
  boundary that doesn't already exist. Explicitly **not** blocking private
  IP ranges, because that would break the feature's actual purpose.
- Every integration fetch gets a context timeout (10s) and a response body
  size cap (2MB) — a slow or oversized response from a misconfigured or
  malicious target can't hang the poller or exhaust memory.
- TLS verification: `InsecureSkipVerify: true`, matching the existing
  `internal/status` checker's precedent (self-signed certs are the norm in
  a homelab) — but this is a one-line, clearly-commented decision, not a
  silent default.
- `secrets.json`: `0600`, atomic write, separate file from `config.json`,
  never serialized into any HTTP response, excluded from config export by
  construction (the export handler only reads `Store`, not `SecretStore`).
- No endpoint ever returns a secret value, masked or otherwise — only
  `hasSecret: bool`.
- The Custom API JSON-path resolver (dot notation + `[n]` array indices)
  is a small hand-written parser/walker over the decoded JSON value — no
  `eval`, no reflection beyond standard `encoding/json` `any` trees.

## Polling

New `integrations.Poller` (own file, modeled on the existing
`internal/status.Checker`): same periodic-loop-with-`Wake()` shape, reusing
`Settings.CheckInterval`. On each tick, for every `Item` with `Widget` set,
resolve the integration, call `Fetch` with a bounded concurrency semaphore
(same pattern as the status checker), and store the result keyed by item
ID:

```go
type WidgetResult struct {
    Data      integrations.Data
    FetchedAt time.Time
    Error     string // non-empty if the most recent fetch failed
}
```

A failed fetch **keeps the previous successful `Data`** in the result
(only `Error` and a "last attempt" timestamp update) — this is what lets
the frontend show "last known state" instead of blanking out. `Wake()` is
called both from `PUT /api/config` (existing pattern) and from the
integrations create/update/delete endpoints, so a newly fixed credential
is reflected within seconds, not a full interval later.

New `GET /api/widgets` returns `map[itemID]WidgetResult` — kept separate
from `GET /api/status` because the shapes genuinely differ (up/down+latency
vs. structured `Data`), and conflating them would make both harder to read.

## Frontend

### Types (`ui/src/lib/types.ts`)

```ts
export interface Integration {
  id: string
  kind: string
  name: string
  config: Record<string, unknown>
  hasSecret: boolean
}

export interface Widget {
  integrationId: string
  kind: string
  query: Record<string, unknown>
  size: '1x1' | '2x1' | '2x2'
}

export interface Item {
  // ...existing fields...
  widget?: Widget
}
```

### Rendering

A tile with `item.widget` set stays a normal `Item` — same name, icon,
address, description, same click-through to `url` (confirmed: clicking a
widget opens the service's own UI, same as any tile). Only the body that
today shows the online/offline dot is replaced by a small per-kind
renderer, dispatched on `widget.kind` (for this phase, just
`CustomApiWidget.svelte`). The edit dialog hides the "check if online"
toggle when a widget is attached — the integration's own poll result is
the liveness signal.

Grid sizing: `grid-column: span N` driven by `widget.size`
(`1x1`→1, `2x1`/`2x2`→2 columns; `2x2` additionally spans 2 grid *rows* —
CSS grid's implicit row sizing handles this if the tile's `min-height` is
set consistently, confirmed feasible with the existing `auto-fill` grid).

### Unreachable / stale state

Every widget always renders its last known `Data` if one exists (dimmed,
with a relative-time caption: "updated 3m ago" / "updated 2d ago"), plus a
small, non-blocking error indicator when the most recent fetch failed.
Before any successful fetch has ever happened, it shows "waiting for first
check" — the same language and visual treatment the existing LED dot uses
today for `unknown`, for consistency.

### Settings → Integrations

New section: list of instances (name, kind, a "configured"/"not
configured" badge driven by `hasSecret`), Add/Edit dialog with fields
rendered per `kind` (only Custom API exists this phase: address field,
plus a repeatable header-name/header-value row list — names are plain
text, values are password-style inputs), a **Test connection** button
(calls `POST /api/integrations/test` while editing, `POST
/api/integrations/{id}/test` once saved) showing a clear inline
success/failure message, and Delete with a confirmation that mentions any
widgets referencing it will start showing "integration not found".

## Custom API driver specifics

- Instance `config`: `{"address": "https://...", "headers": ["Authorization", "X-Api-Key"]}` —
  address and header **names** are not secret, so they live in the plain
  `config` object, readable and editable like any other field.
- Instance `secrets`: `{"headers": "<json-encoded {headerName: value}>"}` —
  one secret blob, keyed by the same names listed in `config.headers`,
  holding each header's value. The UI renders one row per
  `config.headers` entry (name as plain text, value as a write-only
  password-style input that always starts blank); saving reassembles both
  the `config.headers` name list and the `secrets.headers` value map from
  those rows.
- Widget `query`: `{"jsonPath": "data.nodes[0].cpu", "label": "CPU",
  "unit": "%"}`.
- `TestConnection`: GETs the address with the configured headers, 10s
  timeout, succeeds on any `2xx`.
- `Fetch`: same GET, decodes JSON, walks `jsonPath` (dot segments, each
  optionally followed by `[n]` for array indexing), formats the resolved
  scalar as `fmt.Sprint(value) + query["unit"]`, returns one `Value`.
  Path-resolution failures (missing field, index out of range, non-JSON
  response) are returned as a regular `error` — surfaced by the poller as
  `WidgetResult.Error`, rendered by the existing unreachable-state UI. No
  special-casing needed.

## Testing

- Go: table-driven tests for the JSON-path resolver (nested objects,
  arrays, missing paths, non-numeric values); `SecretStore` round-trip
  (set/get/merge-patch/delete, file permissions); `Config.Validate()` new
  integration/widget cases (unknown kind, mismatched widget kind,
  dangling `integrationId`, invalid `size`). Handler tests for the
  integrations endpoints covering the "secrets never appear in a GET
  response" invariant explicitly, since that's the one property that must
  never regress.
- Frontend: `svelte-check` + manual verification in a dev build (add a
  Custom API instance pointed at a small local JSON endpoint, add a
  widget, confirm live value + stale/error states by stopping that
  endpoint).
- Manual end-to-end on the actual Proxmox host once merged: add a Custom
  API instance against some local JSON (e.g. Homie's own `/api/version`)
  as a smoke test before Phase 2 builds the real Proxmox driver on top of
  this same platform.

## Open items carried to later phases (explicitly out of scope here)

- Proxmox VE / PBS / TrueNAS / AdGuard drivers (Phases 2-4).
- Proxmox guest auto-discovery (Phase 5) — depends on Phase 2's Proxmox VE
  driver existing.
