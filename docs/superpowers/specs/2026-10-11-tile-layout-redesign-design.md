# Tile layout redesign: square link tiles + floating widget tiles

## Context

Today every tile (`Item`) lives inside exactly one `Group`, and `Group.Items[]`
is the only place an `Item` can exist (`internal/config/config.go`). Link
tiles (no widget) and widget tiles (Proxmox VE/PBS/Custom API) render through
the same horizontal icon-left/name-right layout in `Tile.svelte`, and both are
confined to their group's CSS grid.

This redesign splits tile behavior in two ways:

1. **Link tiles** (no widget) get a new square layout: logo on top, service
   name below. They keep living inside Groups exactly as today — no data
   model change for them.
2. **Widget tiles** (have a `Widget`) can additionally become **floating**:
   detached from any group, individually positioned anywhere in the page's
   top-to-bottom order — before the first group, after the last one, or
   between any two groups. Each floating tile is independent: two floating
   tiles next to each other in the page order render side by side (so a "row
   of status tiles" is possible), but there is no container object tying
   them together — either can be dragged away on its own.

Link tiles can never float. This is enforced in both the backend validator
and the frontend drag logic.

Alongside this, the redesign fixes the one open layout bug that touches the
same CSS: a `size-2x2` widget tile (2 grid rows tall) stretches every other
tile in its grid row to match its height, because `.grid` never overrides
CSS Grid's default `align-items: stretch`.

## Data model

### Go (`internal/config/config.go`)

`Config.Groups []Group` is replaced by `Config.Blocks []Block`, a top-level
ordered list that can hold a `Group` or a single floating `Item`:

```go
type Block struct {
	Type  string `json:"type"`            // "group" | "item"
	Group *Group `json:"group,omitempty"` // set when Type == "group"
	Item  *Item  `json:"item,omitempty"`  // set when Type == "item" (floating)
}

type Config struct {
	Title        string        `json:"title"`
	Settings     Settings      `json:"settings"`
	Blocks       []Block       `json:"blocks"`
	Integrations []Integration `json:"integrations"`
}
```

`Group` and `Item` themselves are unchanged.

**Validation additions** (`Config.Validate`):
- Every `Block.Type` must be `"group"` or `"item"`.
- A `"group"` block must have `Group != nil` (and `Item == nil`); a `"item"`
  block must have `Item != nil` (and `Group == nil`).
- A floating item (`Block.Type == "item"`) **must have a non-nil `Widget`**.
  A plain link item can never be a top-level block — this is the backend
  enforcement backing the frontend drag restriction below.
- All existing per-group and per-item validation (unique IDs, name length,
  URL scheme, widget size, color format, etc.) applies unchanged, just
  walking `Blocks` instead of `Groups` directly.

**Migration** (`Store.Open`, loading `config.json`): existing files on disk
still have the old shape, `{"groups": [...]}`, with no `"blocks"` key and no
`"type"` discriminator anywhere. On load:
1. Try to unmarshal into the new `Config` shape (looks for `"blocks"`).
2. If `Blocks` comes back empty AND the raw JSON has a top-level `"groups"`
   key, unmarshal that key into `[]Group` the old way and wrap each group as
   `Block{Type: "group", Group: &g}`.
3. Proceed with `normalize()` as usual.

This is a one-time, read-only upgrade path: the first `Store.Set` (or the
next `write()` from normal use) persists the new `"blocks"` shape, and the
old `"groups"` key is gone from disk from then on. No user action required,
matches the "zero manual config" bar already set for the integrations work.

`normalize()` gains: default `Block.Type` to `"group"` for any block that's
missing it entirely (defensive, matches "most things here used to be a
group").

### TypeScript (`ui/src/lib/types.ts`)

```ts
export type Block =
  | { type: 'group'; group: Group }
  | { type: 'item'; item: Item }

export interface Config {
  title: string
  settings: Settings
  blocks: Block[]
  // Integrations intentionally omitted — see existing comment in types.ts.
}
```

`Group` and `Item` are unchanged.

## Rendering

### `Dashboard.svelte`

Iterates `cfg.blocks` instead of `cfg.groups`. For each block:
- `type === 'group'` → render `<Shelf>` exactly as today (search filtering
  logic moves from operating on `cfg.groups` to filtering `block.group.items`
  per group-block, unchanged otherwise).
- `type === 'item'` → render a bare `<Tile>` directly in the page flow, with
  no `Shelf` wrapper (no header/tape, no grid box around it).

The search/summary logic (`visible`, `summary`, `anyDown`) walks blocks
instead of groups; floating item blocks participate in search/status the
same way a group's items do today (match on name/description/url; count
toward the up/down summary if `check` or widget is present).

### Floating tiles visually forming a row

Consecutive `type === 'item'` blocks in `Blocks` are wrapped, at render time
only, into one flex-wrap container per consecutive run (not persisted as a
data structure — purely a `Dashboard.svelte` presentation grouping, computed
from the current `blocks` order each render). This gives the "row of status
tiles" look when several floating tiles are adjacent, while each stays
individually draggable and the underlying array still stores them as
separate, independent blocks. A single floating tile between two groups is
just a run of length 1 and renders as one tile-sized element in the flow.

### `Tile.svelte` — square link-tile layout

For items **without** a widget, the tile body switches from the current
horizontal `icon | text` grid to a vertical layout: icon/logo on top,
service name below it, tile itself roughly square (new CSS class, e.g.
`.tile.link`). The description/host subtitle is dropped from this layout
(per explicit requirement — URL text visibility is not a concern either
way, and the square format has no room for it); the full address remains
available as the tile's `title`/tooltip and as the link's `href` for
accessibility and middle-click/copy behavior.

Widget tiles are **not** touched by this change — `item.widget` presence
still selects the existing horizontal icon-left/content-right layout
(`CustomApiWidget`/`MultiValueWidget` rendering is unaffected), so size
classes (`size-1x1`/`2x1`/`2x2`) keep meaning only for widget tiles exactly
as today.

## Drag and drop

`state.svelte.ts`'s `DragData` and move operations are reshaped around
`Blocks`:

- **`moveGroup`** becomes **`moveBlock`**: generalized splice-out/splice-in
  on `config.blocks` (instead of `config.groups`), used for reordering any
  two blocks relative to each other — a group relative to a group, a
  floating item relative to a floating item, or a group relative to a
  floating item. This is the same algorithm as today's `moveGroup`, just
  operating on the heterogeneous array.
- **`moveItem`** (item moving between two groups' `Items[]`) is unchanged in
  behavior, just looks up the source/target group through
  `blocks.find(b => b.type === 'group' && b.group.id === id)`.
- **New: `floatItem(fromGroupId, itemId, targetBlockIndex, after)`** — removes
  the item from its group's `Items[]` and inserts a new
  `{type:'item', item}` block into `Blocks` at the given position. Only
  callable when the item has a widget (the drag handler below is the single
  gate for this; `Validate()` is the backing safety net).
- **New: `sinkItem(itemBlockIndex, toGroupId, targetItemId, after)`** — the
  reverse: removes a floating item block from `Blocks` and appends/inserts
  the item into a group's `Items[]`, re-enabling the regular `moveItem` path
  for it from then on.

UI affordances (editing mode only, same drag-events-with-ghost-state pattern
already used in `Tile.svelte`/`Shelf.svelte`):
- A thin drop strip between every two blocks (including before the first
  and after the last) accepts: another block being reordered (→
  `moveBlock`), or — **only if the dragged item carries a widget** — an item
  being dragged out of a group (→ `floatItem`). Dragging a plain link item
  over these strips shows no drop affordance; the backend would reject it
  even if the frontend had a bug here.
- Each `Shelf`'s existing grid-drop-zone (`gridDrop`, today "drop appends to
  this group") additionally accepts a floating item block being dragged in
  (→ `sinkItem`), appending it to that group's items — this is how a tile
  goes from floating back into a group.
- A floating tile itself (`type: 'item'` block, rendered bare) is draggable
  like a group's tape: dragging it over another block's boundary triggers
  `moveBlock`; dragging it over a group's grid triggers `sinkItem`.

`order(cfg)` (the no-op-save guard) is recomputed from `Blocks` so it still
detects genuine order changes vs. a drop that lands back where it started.

## CSS stretch-bug fix

Root cause (confirmed): `.grid` in `app.css` never sets `align-items`, so it
inherits CSS Grid's default `stretch` — every tile in a row stretches to
match the tallest (e.g. a `size-2x2` Proxmox tile's 2-row span). Fix: add
`align-items: start` to `.grid`. Apply the same rule to the new floating-row
flex container so the bug can't resurface there once floating tiles exist.

## Out of scope / explicit non-goals

- No change to `Widget`, `CustomApiWidget.svelte`, or `MultiValueWidget.svelte`
  rendering internals — this redesign only changes *where* a widget tile can
  sit and how a non-widget tile's body is laid out.
- No new size options for floating tiles — they keep using the existing
  `Widget.Size` values and grid-span CSS.
- No bulk "convert existing config" tooling beyond the automatic
  `groups` → `blocks` migration on load; there's no new UI needed since the
  migration is transparent.
