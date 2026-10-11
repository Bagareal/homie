# Proxmox VE + Proxmox Backup Server drivers — design

Status: approved for implementation
Phase: 2 of 5 (see `2026-10-10-integrations-architecture-design.md` for the
Phase 1 platform this builds on, unchanged)

## Goal

Add two dedicated, **zero-manual-config** integration drivers on top of the
Phase 1 platform: **Proxmox VE** (node CPU/RAM/uptime, VM/LXC counts) and
**Proxmox Backup Server** (datastore usage, last backup status).

Contrast with Custom API (the only driver that exists today): Custom API
requires the admin to hand-write a JSON field path and, for most real
services, an auth header string. These two drivers must not require that —
host + a pasted API token is the whole setup. They also must handle both a
single Proxmox node and a multi-node cluster the same way, with no special
"cluster mode."

## Non-goals (this phase)

- A whole-cluster aggregate tile (e.g. "average CPU across all nodes"). One
  tile is always one node or one datastore. Explicitly deferred — not
  requested, adds a second tile shape for no asked-for benefit.
- Manual node-name text entry as a fallback. Rejected during design: it's
  exactly the kind of hand-typed config this phase exists to avoid, and it
  doesn't cleanly extend to "which of N cluster nodes."
- TrueNAS / AdGuard Home drivers (Phases 3-4) and Proxmox guest
  auto-discovery (Phase 5) — unchanged, still later phases.
- Redesigning Custom API's own single-value visual (the "hero number"
  look). It keeps its current appearance; it only gets the shared
  positioning/truncation fix below (Open residual issue).

## Carried over from Phase 1's final review

Phase 1 shipped (v0.4.0) with three parked minor issues and, found at the
very start of this phase's design session, a fourth, more fundamental one:

4. **The widget content area was rendering inside the tile's icon column
   (38px wide), not the full tile width.** Confirmed by screenshotting a
   live Custom API widget: a one-word value ("Stars") already sat in a
   visibly cramped column, and a multi-word string wrapped one word per
   line. This is the root cause underneath the already-known issue (long
   `.error` text not truncating — a long string in a 38px column wraps
   badly regardless of `text-overflow`). Fixing the grid placement (see
   Frontend → Widget area layout fix below) resolves both in one change.
   Short values like Custom API's typical "42%" never made this obvious
   before, which is presumably why it went unnoticed through Phase 1.

This phase fixes issue 4 (and, as a consequence, the original truncation
issue) as a prerequisite — the new multi-value grid needs the corrected
full-width area to be legible at all.

## Architecture

### Discovery: a new optional driver capability

Proxmox VE and PBS both need to show the admin a live list of selectable
targets (nodes, or datastores) once host+token are entered — a cluster is
just "more than one entry in that list," not a different code path. Custom
API has no equivalent concept and must not be forced to implement one, so
this is a second, optional interface in `internal/integrations`, not an
addition to `Driver` itself:

```go
type Target struct {
    ID    string // e.g. node name "morpheus", or datastore name "backup-store"
    Label string // what the picker shows; may equal ID
}

type DiscoverableDriver interface {
    Driver
    Targets(ctx context.Context, cfg map[string]any, secrets Secrets) ([]Target, error)
}
```

Callers (the new discovery endpoint, below) type-assert
`driver.(DiscoverableDriver)`; Custom API simply doesn't satisfy it, and
the UI only shows a target picker for kinds that do.

### Proxmox VE driver (`internal/integrations/proxmoxve.go`)

- `Kind()` → `"proxmoxve"`.
- `cfg`: `{"address": "https://morpheus:8006"}`.
- `secrets`: `{"tokenId": "root@pam!homie", "secret": "<uuid>"}` — sent as
  `Authorization: PVEAPIToken=<tokenId>=<secret>` on every request, same
  HTTP client pattern as Custom API (10s timeout, 2MB body cap,
  `InsecureSkipVerify: true`, same documented rationale).
- `Targets`: lists every node reachable from this host (works identically
  whether the host is standalone or part of a cluster — exact endpoint to
  be confirmed against current Proxmox API docs during implementation, see
  Endpoint verification below), returning `{ID: nodeName, Label: nodeName}`.
- `TestConnection`: hits the same endpoint `Targets` would use and
  succeeds if it returns without error and at least one node — a token
  that authenticates but has zero visible nodes (e.g. wrong permissions)
  should fail the test, not silently report success.
- `Fetch(ctx, cfg, secrets, query)`: `query = {"node": "morpheus"}`.
  Returns 4 `Value`s for that node: CPU % (`Label: "CPU"`), RAM %
  (`Label: "RAM"`), uptime formatted as e.g. `"14d 3h"` (`Label:
  "Uptime"`), and running/total VM+LXC count formatted as `"6/9"`
  (`Label: "VM/LXC"`). Exact source fields/endpoints confirmed during
  implementation (Endpoint verification).
- Recommended token role, documented in the UI's help text next to the
  Token ID/Secret fields: `PVEAuditor` (read-only), matching the existing
  roadmap note.

### PBS driver (`internal/integrations/pbs.go`)

- `Kind()` → `"pbs"`.
- `cfg`: `{"address": "https://pbs-host:8007"}`.
- `secrets`: `{"tokenId": "...", "secret": "..."}` — same token-auth
  shape as Proxmox VE (PBS uses the analogous `PBSAPIToken=` header
  format; confirmed during implementation).
- `Targets`: lists every datastore on this PBS instance, `{ID:
  datastoreName, Label: datastoreName}`.
- `TestConnection`: same pattern as Proxmox VE — must see at least one
  datastore to pass.
- `Fetch(ctx, cfg, secrets, query)`: `query = {"datastore": "backup-store"}`.
  Returns 2 `Value`s: usage % (`Label: "Used"`), and last backup status
  formatted as e.g. `"ok, 2d ago"` / `"failed, 2d ago"` (`Label: "Last
  backup"`).
- **No special-casing for an offline PBS host.** A connection
  refused/timeout from a powered-off PBS is a plain `error` return from
  `Fetch`, handled entirely by the existing Phase 1 poller mechanism:
  last known `Data` stays displayed, dimmed, with the existing small
  error indicator. This was an explicit design decision (confirmed with
  the user) — not a bug to work around, not a new "expected offline"
  status to build.

### Endpoint verification (first implementation step, not resolved here)

Before writing the parsing logic, confirm against the current official
Proxmox VE and PBS API documentation:
- Proxmox VE: the exact path(s) for listing nodes and reading per-node
  CPU/RAM/uptime and VM/LXC status — whether a single `cluster/resources`
  call covers all of it (preferred, since it uniformly handles standalone
  and clustered hosts) or multiple calls are needed per node.
- PBS: the exact path(s) for datastore usage and last-backup status per
  datastore (task history vs. snapshot listing — pick whichever gives a
  clean "succeeded/failed + when" without extra calls).
- The precise `Authorization` header syntax for both (`PVEAPIToken=`/
  `PBSAPIToken=`), and what Proxmox/PBS's own "Add API Token" screen
  labels the two pasted values, so the UI's field labels match exactly
  what the admin is looking at when copying them.

### Backend: discovery HTTP endpoint

New endpoint, mirroring the existing test-connection pair (works for both
an unsaved, in-progress integration and an already-saved one):

| Method & path | Body | Notes |
|---|---|---|
| `POST /api/integrations/targets` | `{kind, config, secrets}` | Discovers targets for an integration not yet saved (used while filling in the Add-integration form). |
| `POST /api/integrations/{id}/targets` | — | Discovers targets for an already-saved integration, using its stored secret. |

Both return `{ok: true, targets: [{id, label}, ...]}` or `{ok: false,
error: "..."}`. For a `Kind` whose driver doesn't implement
`DiscoverableDriver` (Custom API), this returns an error — the frontend
never calls it for that kind, but the backend shouldn't assume that.

## Data model changes

No changes to `Integration`, `Widget`, or `Item` shapes from Phase 1 —
`config`/`secrets`/`query` are already untyped per-kind maps. New kinds
simply use different keys:
- `Integration.config` for `proxmoxve`/`pbs`: `{"address": "..."}`.
- `Integration.secrets` (via `SecretStore`, same as Phase 1): `{"tokenId":
  "...", "secret": "..."}`.
- `Widget.query` for `proxmoxve`: `{"node": "morpheus"}`; for `pbs`:
  `{"datastore": "backup-store"}`.

`Config.Validate()` already checks `kind` against the registry generically
— the two new drivers register themselves and are validated for free, no
changes needed there either.

## Frontend

### Widget area layout fix (applies to all widget kinds, including Custom API)

`Tile.svelte`'s widget slot currently auto-places into the tile's CSS grid
as a plain flow item, landing in the icon's 38px-wide column (Issue 4,
above). Fix: give the widget container an explicit `grid-column: 1 / -1`
(span the full tile width, starting under the icon) so it always gets the
tile's full content width regardless of icon-column width. Once the area
is properly wide, add `overflow`/`text-overflow: ellipsis` truncation to
`CustomApiWidget.svelte`'s `.error` class, matching the existing
`.name`/`.desc` pattern in `app.css` — this was the originally-known
Phase 1 residual issue, now actually effective because the container is
wide enough for truncation to mean something.

### New component: `MultiValueWidget.svelte`

Renders `Data.Values[]` (already an array since Phase 1; Custom API has
just never put more than one entry in it) as a grid: **3 values per row,
wrapping to additional rows** for a 4th+ value — confirmed via mockup
during design (Proxmox VE's 4 values render as a 3-then-1 layout). Each
cell shows the formatted value (bold) above its label (muted, smaller),
matching the visual weight of the existing `.value`/`.label` classes in
`CustomApiWidget.svelte`. Same stale/error treatment as Phase 1: last
known values dimmed on fetch failure, small error-dot indicator, "waiting
for first check" before any successful fetch.

`CustomApiWidget.svelte` is unchanged visually (still its single
large-number look) beyond the shared layout fix above — it is not
rewritten to use `MultiValueWidget`, since a 1-value grid cell would look
visually smaller/weaker than Custom API's current treatment and nobody
asked for that to change.

`Tile.svelte` dispatches on `widget.kind`: `customapi` →
`CustomApiWidget`, `proxmoxve`/`pbs` → `MultiValueWidget`.

### `IntegrationDialog.svelte` becomes kind-aware

Today this component is hardcoded to Custom API (title literally says "
(Custom API)", builds `kind: 'customapi'` on submit, only has
address+headers fields). Changes:
- Settings' "Add integration" button now first asks which kind (Custom
  API / Proxmox VE / Proxmox Backup Server) — a simple choice, not a
  separate dialog step; could be the dialog's own first screen or a
  dropdown at the top that swaps the fields below it.
- Custom API kind: unchanged fields (address, header rows).
- Proxmox VE / PBS kind: Address field, plus **Token ID** and **Secret**
  fields (the latter password-style, write-only like Custom API's header
  values — same "blank on edit means unchanged" semantics already
  implemented for headers, reused for these two secret keys). Help text
  under the fields names exactly what Proxmox/PBS's own token-creation
  screen calls these two values (confirmed during endpoint verification).
- Test connection button behavior unchanged in spirit: for
  `proxmoxve`/`pbs` it calls the same test endpoint, which (per driver
  design above) also exercises discovery, so "Connection OK" for these
  kinds effectively also confirms at least one node/datastore was found.

### `ItemDialog.svelte`: target picker instead of JSON path

Today the Widget section only lists `customapi` integrations and always
shows JSON-path/label/unit/size fields. Changes:
- The integration dropdown lists integrations of **any** kind.
- When the selected integration's kind is `customapi`: unchanged (JSON
  path, label, unit, size fields).
- When it's `proxmoxve`/`pbs`: no JSON path/label/unit fields (the driver
  decides labels and formatting). Instead, a **node** (or **datastore**)
  dropdown, populated by calling the new discovery endpoint for that
  integration when it's selected. Size field stays (still 1x1/2x1/2x2;
  `MultiValueWidget`'s 3-per-row grid is legible from 2x1 up, matches the
  mockup reviewed during design — 1x1 remains available but will look
  cramped with 4 values, same tradeoff Custom API already has with long
  text at 1x1).

## Testing

- Go: table-driven tests for both drivers' response parsing (CPU/RAM/
  uptime/VM-LXC-count extraction; datastore usage/last-backup-status
  extraction), using `httptest` fixtures shaped like the verified real API
  responses — mirrors `customapi_test.go`'s existing pattern. `Targets()`
  tests covering zero/one/multiple nodes or datastores. Handler tests for
  the two new discovery endpoints, including the "unknown/non-discoverable
  kind returns an error" case.
- Frontend: `svelte-check`; manual verification in a dev build — the
  layout fix and `MultiValueWidget` grid get confirmed visually (already
  prototyped during design) before wiring real data.
- Manual end-to-end against the user's real `morpheus` Proxmox host and
  real PBS instance once merged — the actual risk in this phase is
  endpoint/field-name accuracy against the live API, which only a real
  host can confirm. PBS being powered off most of the time means that
  specific verification may need to wait for a day it's on; the
  stale/offline rendering path can be verified separately by simply
  stopping the dev fixture server mid-session.

## Open items carried to later phases

- TrueNAS SCALE widget (Phase 3) — separate research task already noted
  in the roadmap (REST vs JSON-RPC-over-WebSocket).
- AdGuard Home widget (Phase 4).
- Proxmox guest auto-discovery (Phase 5) — can reuse this phase's
  `proxmoxve.go` `Targets()`-style discovery as a building block.
- Optional future whole-cluster aggregate tile — explicitly deferred, not
  designed here.
