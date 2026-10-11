# Tile layout redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Split tiles into square link tiles (icon on top, name below, stay in Groups) and floating widget tiles (can sit anywhere on the page, individually, outside any group), and fix the pre-existing CSS bug where a tall widget tile stretches its grid row's other tiles.

**Architecture:** `Config.Groups []Group` becomes `Config.Blocks []Block`, a top-level ordered list mixing `Group` blocks and single floating-`Item` blocks (only items with a `Widget` may be a floating block). All group/item IDs already share one global uniqueness namespace, so a block is locatable by that single id with no new id type needed. Existing `config.json` files upgrade transparently on first read (no "blocks" key → wrap every legacy group as a group-block). Frontend drag-and-drop gains two new operations (`floatItem`, `sinkItem`) alongside a generalized `moveBlock` (today's `moveGroup`, widened to any block).

**Tech Stack:** Go (stdlib only) backend, Svelte 5 + TypeScript frontend, Vite. No frontend test runner exists in this repo (`ui/package.json` only has `check` = `svelte-check`) — frontend verification is type-checking plus one real manual pass in the browser, not new unit tests.

**Spec:** `docs/superpowers/specs/2026-10-11-tile-layout-redesign-design.md`

## Global Constraints

- The user's existing production `config.json` must upgrade transparently the first time this version loads it — no manual migration step, matching the "zero manual configuration" bar already set for the integrations work.
- A link tile (no `Widget`) can never become a floating block. Enforced in both `Config.Validate()` (backend) and every frontend drag-accept check (defense in depth, not redundancy — the frontend guard is what gives users correct drag feedback, the backend guard is what actually protects the data).
- All shipped strings (UI copy, error messages) stay in English, per this project's existing full-translation convention.
- No new frontend test tooling is introduced. Each frontend task's test step is `npm run check` (inside `ui/`); the plan's final task adds one consolidated manual browser verification pass, per this project's existing convention for UI changes.

## Review Focus

- Importing an old exported config file (pre-redesign `{"groups": [...]}` shape, no `"blocks"` key) through Settings' Import button must restore the dashboard, not silently wipe it to empty — covered in Task 1 (`UnmarshalConfig` migration tests) and Task 6 (Settings.svelte accepts both shapes).
- Dragging a plain link tile (no widget) toward a group's edge or onto an existing floating tile must never float it — no drop-accept indicator should appear, and even if it did, the backend must reject the resulting config — covered in Task 4/5 (frontend accept guards) and Task 1 (`Validate` rejects a widget-less floating block).
- A floating widget tile sized `2x2` must not stretch its floating-row neighbors to match its height — the exact bug this redesign is also fixing, now in a brand new CSS container where it could trivially resurface — covered in Task 7 (`align-items: flex-start` on `.float-row`, mirroring the `.grid` fix).
- Editing an already-floating tile through its dialog must not silently reassign it into a group just because a group has to be chosen somewhere — covered in Task 6 (the group `<select>` is hidden, not defaulted, when editing a floating item).
- Loading a `config.json` that is already in the new `"blocks"` shape must parse as-is and must not be mistaken for the legacy shape (e.g. by re-wrapping or duplicating blocks) on every normal restart — covered in Task 1 (`TestUnmarshalConfigLeavesCurrentShapeUntouched`).

---

## Task 1: Backend data model — `Block`, migration, validation

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces: `type Block struct { Type string; Group *Group; Item *Item }`, `Config.Blocks []Block` (replaces `Config.Groups`), `func (c Config) Items() []Item`, `func UnmarshalConfig(raw []byte) (Config, error)`.

- [ ] **Step 1: Rewrite the types, `Default`, `normalize`, and `Validate` in `config.go`**

Replace the `Config` struct and everything that reads `Groups` with the block-based shape. Full replacement for lines 24-194 of `internal/config/config.go`:

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

type Group struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Items     []Item `json:"items"`
	Color     string `json:"color,omitempty"`
	TextColor string `json:"textColor,omitempty"`
}

// Block is one entry in the dashboard's top-to-bottom layout: either a
// Group (rendered with its header, holding its own Items) or a single
// floating Item (rendered on its own, with no group around it). Only an
// Item with a Widget may ever be a floating block — see Validate.
type Block struct {
	Type  string `json:"type"`            // "group" | "item"
	Group *Group `json:"group,omitempty"` // set when Type == "group"
	Item  *Item  `json:"item,omitempty"`  // set when Type == "item"
}

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

type Config struct {
	Title        string        `json:"title"`
	Settings     Settings      `json:"settings"`
	Blocks       []Block       `json:"blocks"`
	Integrations []Integration `json:"integrations"`
}

// Items returns every item on the dashboard, whether it belongs to a
// group or floats on its own. Used by callers that just need to walk
// every item (the status checker, the widget poller) without caring
// about the layout structure.
func (c Config) Items() []Item {
	var items []Item
	for _, b := range c.Blocks {
		switch b.Type {
		case "group":
			if b.Group != nil {
				items = append(items, b.Group.Items...)
			}
		case "item":
			if b.Item != nil {
				items = append(items, *b.Item)
			}
		}
	}
	return items
}

func Default() Config {
	return Config{
		Title: "Homelab",
		Settings: Settings{
			Theme:         "auto",
			Density:       "comfortable",
			CheckInterval: 60,
			OpenInNewTab:  true,
		},
		Blocks:       []Block{},
		Integrations: []Integration{},
	}
}

// normalize fills in missing values (old files or partial imports).
func (c *Config) normalize() {
	d := Default()
	if c.Settings.Theme == "" {
		c.Settings.Theme = d.Settings.Theme
	}
	if c.Settings.Density == "" {
		c.Settings.Density = d.Settings.Density
	}
	if c.Settings.CheckInterval == 0 {
		c.Settings.CheckInterval = d.Settings.CheckInterval
	}
	if c.Blocks == nil {
		c.Blocks = []Block{}
	}
	if c.Integrations == nil {
		c.Integrations = []Integration{}
	}
	for i := range c.Blocks {
		if c.Blocks[i].Type == "" {
			c.Blocks[i].Type = "group"
		}
		switch {
		case c.Blocks[i].Type == "group" && c.Blocks[i].Group != nil:
			if c.Blocks[i].Group.Items == nil {
				c.Blocks[i].Group.Items = []Item{}
			}
			for j := range c.Blocks[i].Group.Items {
				if c.Blocks[i].Group.Items[j].Widget != nil {
					// A widget's own poll result is the liveness signal;
					// the lightweight HTTP check is for plain link tiles.
					c.Blocks[i].Group.Items[j].Check = false
				}
			}
		case c.Blocks[i].Type == "item" && c.Blocks[i].Item != nil && c.Blocks[i].Item.Widget != nil:
			c.Blocks[i].Item.Check = false
		}
	}
}

func (c Config) Validate() error {
	if len(c.Title) > 100 {
		return errors.New("title too long (100 characters max)")
	}
	switch c.Settings.Theme {
	case "auto", "light", "dark":
	default:
		return fmt.Errorf("invalid theme: %q", c.Settings.Theme)
	}
	switch c.Settings.Density {
	case "compact", "comfortable":
	default:
		return fmt.Errorf("invalid density: %q", c.Settings.Density)
	}
	if c.Settings.CheckInterval < 10 || c.Settings.CheckInterval > 3600 {
		return errors.New("the check interval must be between 10 and 3600 seconds")
	}

	seen := map[string]bool{}
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

	validateItem := func(it Item, context string) error {
		if it.ID == "" || seen[it.ID] {
			return fmt.Errorf("service %q has a missing or duplicate id", it.Name)
		}
		seen[it.ID] = true
		if it.Name == "" || len(it.Name) > 80 {
			return fmt.Errorf("the service %s needs a name (80 characters max)", context)
		}
		if len(it.Description) > 200 || len(it.Icon) > 500 {
			return fmt.Errorf("description or icon of %q is too long", it.Name)
		}
		u, err := url.Parse(it.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("invalid address for %q: must start with http:// or https://", it.Name)
		}
		if it.Widget == nil {
			return nil
		}
		w := it.Widget
		if w.IntegrationID == "" {
			return fmt.Errorf("the widget on %q is missing an integration", it.Name)
		}
		if !validSizes[w.Size] {
			return fmt.Errorf("invalid widget size on %q: %q", it.Name, w.Size)
		}
		// A dangling integrationId (its integration was since deleted) is
		// allowed: the UI shows "integration not found" rather than
		// rejecting the whole save.
		if in, ok := integrationsByID[w.IntegrationID]; ok && in.Kind != w.Kind {
			return fmt.Errorf("the widget on %q doesn't match its integration's kind", it.Name)
		}
		return nil
	}

	for _, b := range c.Blocks {
		switch b.Type {
		case "group":
			if b.Group == nil || b.Item != nil {
				return errors.New("a group block must carry a group and nothing else")
			}
			g := b.Group
			if g.ID == "" || seen[g.ID] {
				return fmt.Errorf("group %q has a missing or duplicate id", g.Name)
			}
			seen[g.ID] = true
			if g.Name == "" || len(g.Name) > 60 {
				return errors.New("every group needs a name (60 characters max)")
			}
			if g.Color != "" && !hexColor.MatchString(g.Color) {
				return fmt.Errorf("invalid background color for group %q: must be in #rrggbb format", g.Name)
			}
			if g.TextColor != "" && !hexColor.MatchString(g.TextColor) {
				return fmt.Errorf("invalid text color for group %q: must be in #rrggbb format", g.Name)
			}
			for _, it := range g.Items {
				if err := validateItem(it, fmt.Sprintf("in group %q", g.Name)); err != nil {
					return err
				}
			}
		case "item":
			if b.Item == nil || b.Group != nil {
				return errors.New("a floating block must carry an item and nothing else")
			}
			if b.Item.Widget == nil {
				return fmt.Errorf("%q can't float on its own: only a service with a widget can sit outside a group", b.Item.Name)
			}
			if err := validateItem(*b.Item, "floating on the dashboard"); err != nil {
				return err
			}
		default:
			return fmt.Errorf("invalid block type: %q", b.Type)
		}
	}
	return nil
}
```

- [ ] **Step 2: Add `UnmarshalConfig` and wire it into `Store.Open`**

Add this function above `Store` in `config.go` (after `Validate`, before the `Store` type):

```go
// UnmarshalConfig parses raw config JSON, transparently upgrading the
// legacy {"groups": [...]} shape (no "blocks" key, used before the
// floating-tile redesign) into the current {"blocks": [...]} shape. Used
// both for config.json on disk and for a config imported through the
// API, so an old exported file never gets silently emptied on import.
func UnmarshalConfig(raw []byte) (Config, error) {
	var c Config
	if err := json.Unmarshal(raw, &c); err != nil {
		return Config{}, err
	}
	if len(c.Blocks) == 0 {
		var legacy struct {
			Groups []Group `json:"groups"`
		}
		if err := json.Unmarshal(raw, &legacy); err == nil {
			for i := range legacy.Groups {
				g := legacy.Groups[i]
				c.Blocks = append(c.Blocks, Block{Type: "group", Group: &g})
			}
		}
	}
	return c, nil
}
```

Then update `Store.Open` (currently decodes directly into `Config`) to use it. Replace:

```go
	var c Config
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON: %w", s.path, err)
	}
	c.normalize()
```

with:

```go
	c, err := UnmarshalConfig(raw)
	if err != nil {
		return nil, fmt.Errorf("%s is not valid JSON: %w", s.path, err)
	}
	c.normalize()
```

- [ ] **Step 3: Update existing test fixtures in `config_test.go` to the new shape**

Replace the `baseConfigWithWidget` helper and the one other fixture that builds a `Config` directly. Add a `groupBlock` helper and use it; everything else in the file (the test function bodies themselves) stays exactly as it is — only these two fixtures change:

```go
func groupBlock(g Group) Block { return Block{Type: "group", Group: &g} }

func baseConfigWithWidget(widget *Widget, integrations []Integration) Config {
	c := Default()
	c.Integrations = integrations
	c.Blocks = []Block{groupBlock(Group{
		ID:   "g1",
		Name: "Group",
		Items: []Item{{
			ID:     "i1",
			Name:   "Widget tile",
			URL:    "http://example.com",
			Widget: widget,
		}},
	})}
	return c
}
```

And `TestNormalizeDefaultsIntegrationsAndForcesCheckOff`:

```go
func TestNormalizeDefaultsIntegrationsAndForcesCheckOff(t *testing.T) {
	c := Config{Blocks: []Block{groupBlock(Group{
		ID:   "g1",
		Name: "Group",
		Items: []Item{{
			ID:     "i1",
			Name:   "Widget tile",
			Check:  true,
			Widget: &Widget{IntegrationID: "x", Kind: "customapi", Size: "1x1"},
		}},
	})}}
	c.normalize()
	if c.Integrations == nil {
		t.Fatal("expected Integrations to default to an empty slice, got nil")
	}
	if c.Blocks[0].Group.Items[0].Check {
		t.Fatal("expected Check to be forced false on an item with a widget")
	}
}
```

- [ ] **Step 4: Run the existing suite to confirm the rename preserved behavior**

Run: `go test ./internal/config/...`
Expected: PASS (all pre-existing tests, now against the new shape).

- [ ] **Step 5: Add new tests for the genuinely new behavior**

Append to `config_test.go`:

```go
func TestValidateFloatingItemRequiresWidget(t *testing.T) {
	c := Default()
	c.Blocks = []Block{{Type: "item", Item: &Item{
		ID: "i1", Name: "Floating", URL: "http://example.com",
	}}}
	if err := c.Validate(); err == nil {
		t.Fatal("expected an error: a floating item without a widget must be rejected")
	}
}

func TestValidateFloatingItemWithWidgetIsValid(t *testing.T) {
	c := Default()
	c.Integrations = []Integration{{ID: "int1", Kind: "customapi", Name: "My API"}}
	c.Blocks = []Block{{Type: "item", Item: &Item{
		ID: "i1", Name: "Floating", URL: "http://example.com",
		Widget: &Widget{IntegrationID: "int1", Kind: "customapi", Size: "1x1"},
	}}}
	if err := c.Validate(); err != nil {
		t.Fatalf("expected a floating item with a widget to be valid, got %v", err)
	}
}

func TestItemsFlattensGroupsAndFloatingItems(t *testing.T) {
	c := Default()
	c.Blocks = []Block{
		groupBlock(Group{ID: "g1", Name: "G", Items: []Item{{ID: "i1", Name: "A"}, {ID: "i2", Name: "B"}}}),
		{Type: "item", Item: &Item{ID: "i3", Name: "Floating", Widget: &Widget{IntegrationID: "x", Kind: "customapi", Size: "1x1"}}},
	}
	items := c.Items()
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
}

func TestUnmarshalConfigMigratesLegacyGroupsShape(t *testing.T) {
	legacy := []byte(`{"title":"Old","settings":{"theme":"auto","density":"comfortable","checkInterval":60},"groups":[{"id":"g1","name":"Group","items":[{"id":"i1","name":"Site","url":"http://example.com"}]}]}`)
	c, err := UnmarshalConfig(legacy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(c.Blocks) != 1 || c.Blocks[0].Type != "group" || c.Blocks[0].Group == nil || c.Blocks[0].Group.ID != "g1" {
		t.Fatalf("expected the legacy group to migrate into a group block, got %+v", c.Blocks)
	}
	if len(c.Blocks[0].Group.Items) != 1 || c.Blocks[0].Group.Items[0].ID != "i1" {
		t.Fatalf("expected the legacy group's item to carry over, got %+v", c.Blocks[0].Group.Items)
	}
}

func TestUnmarshalConfigLeavesCurrentShapeUntouched(t *testing.T) {
	current := []byte(`{"title":"New","settings":{"theme":"auto","density":"comfortable","checkInterval":60},"blocks":[{"type":"group","group":{"id":"g1","name":"Group","items":[]}}]}`)
	c, err := UnmarshalConfig(current)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(c.Blocks) != 1 || c.Blocks[0].Group.ID != "g1" {
		t.Fatalf("expected the current-shape config to parse as-is, got %+v", c.Blocks)
	}
}
```

- [ ] **Step 6: Run the full config test suite again**

Run: `go test ./internal/config/... -v`
Expected: PASS, all tests including the 5 new ones.

- [ ] **Step 7: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "Replace Config.Groups with Config.Blocks to allow floating widget tiles

Blocks is a top-level ordered list mixing Group blocks and single
floating-Item blocks. Only items with a Widget may float. Existing
config.json files upgrade transparently via UnmarshalConfig, which
wraps a legacy top-level groups array into group blocks on first read."
```

---

## Task 2: Backend consumers — status checker, widget poller, config import

**Files:**
- Modify: `internal/status/status.go:83-91`
- Modify: `internal/integrations/poller.go:76-135`
- Modify: `internal/server/server.go` (`putConfig` handler)
- Modify: `internal/integrations/poller_test.go`
- Modify: `internal/server/integrations_test.go`

**Interfaces:**
- Consumes: `config.Config.Items() []Item`, `config.UnmarshalConfig([]byte) (Config, error)` (from Task 1).

- [ ] **Step 1: Update the two existing test fixtures that build `cfg.Groups` directly**

In `internal/integrations/poller_test.go`, both `cfg.Groups = []config.Group{...}` assignments (lines 59-68 and 126, 141) become `cfg.Blocks = []config.Block{{Type: "group", Group: &config.Group{...}}}` with the same inner contents. For example, the first one becomes:

```go
	cfg.Blocks = []config.Block{{Type: "group", Group: &config.Group{
		ID:   "g1",
		Name: "Group",
		Items: []config.Item{{
			ID:     "i1",
			Name:   "Widget tile",
			URL:    "http://example.com",
			Widget: &config.Widget{IntegrationID: "int1", Kind: "scripted", Size: "1x1"},
		}},
	}}}
```

Apply the same `Blocks: []config.Block{{Type: "group", Group: &config.Group{...}}}` wrapping to the other two assignments in that file (`TestPollerDropsResultsForRemovedWidgets`, both the initial `widgetItem` seed and the later `plain` re-seed), keeping their existing inner `Group{...}` contents unchanged.

In `internal/server/integrations_test.go`, line 113's `cfg.Groups = []config.Group{...}` becomes the same `cfg.Blocks = []config.Block{{Type: "group", Group: &config.Group{...}}}` wrapping, inner contents unchanged.

- [ ] **Step 2: Run the suites to confirm they fail to compile against the old field name**

Run: `go build ./...`
Expected: FAIL — `status.go` and `poller.go` still reference `.Groups`, which no longer exists on `Config`.

- [ ] **Step 3: Update `status.go` to use `Items()`**

In `internal/status/status.go`, replace the `checkAll` item-collection loop (currently lines 84-90):

```go
	var items []config.Item
	for _, g := range c.store.Get().Groups {
		for _, it := range g.Items {
			if it.Check {
				items = append(items, it)
			}
		}
	}
```

with:

```go
	var items []config.Item
	for _, it := range c.store.Get().Items() {
		if it.Check {
			items = append(items, it)
		}
	}
```

- [ ] **Step 4: Update `poller.go` to use `Items()`**

In `internal/integrations/poller.go`, replace the fetch loop (currently lines 85-121's outer iteration):

```go
	for _, g := range cfg.Groups {
		for _, it := range g.Items {
			if it.Widget == nil {
				continue
			}
			itemID, w := it.ID, it.Widget
```

with:

```go
	for _, it := range cfg.Items() {
		if it.Widget == nil {
			continue
		}
		itemID, w := it.ID, it.Widget
```

...and remove the now-extra closing brace this leaves behind (the inner loop body is unchanged; only the double `for` becomes a single `for`). Then replace the "active" bookkeeping loop (currently lines 128-135):

```go
	active := make(map[string]bool, len(cfg.Groups))
	for _, g := range cfg.Groups {
		for _, it := range g.Items {
			if it.Widget != nil {
				active[it.ID] = true
			}
		}
	}
```

with:

```go
	items := cfg.Items()
	active := make(map[string]bool, len(items))
	for _, it := range items {
		if it.Widget != nil {
			active[it.ID] = true
		}
	}
```

- [ ] **Step 5: Run the test suites**

Run: `go test ./internal/status/... ./internal/integrations/... ./internal/server/...`
Expected: PASS.

- [ ] **Step 6: Update `putConfig` to accept legacy-shaped imports**

In `internal/server/server.go`, replace the `putConfig` handler body:

```go
func (s *Server) putConfig(w http.ResponseWriter, r *http.Request) {
	var c config.Config
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	if err := dec.Decode(&c); err != nil {
		writeError(w, http.StatusBadRequest, "unreadable configuration: invalid JSON or too large")
		return
	}
	// Integrations are owned by the dedicated /api/integrations endpoints;
	// never let a stale or absent value in a generic config save touch them.
	c.Integrations = s.store.Get().Integrations
	if err := s.store.Set(c); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.checker.Wake()
	s.widgets.Wake()
	writeJSON(w, http.StatusOK, s.store.Get())
}
```

with:

```go
func (s *Server) putConfig(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		writeError(w, http.StatusBadRequest, "unreadable configuration: invalid JSON or too large")
		return
	}
	// UnmarshalConfig also accepts a legacy pre-redesign export (a
	// top-level "groups" array, no "blocks" key) so an old exported file
	// can still be imported without silently emptying the dashboard.
	c, err := config.UnmarshalConfig(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "unreadable configuration: invalid JSON or too large")
		return
	}
	// Integrations are owned by the dedicated /api/integrations endpoints;
	// never let a stale or absent value in a generic config save touch them.
	c.Integrations = s.store.Get().Integrations
	if err := s.store.Set(c); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.checker.Wake()
	s.widgets.Wake()
	writeJSON(w, http.StatusOK, s.store.Get())
}
```

Add `"io"` to the import block at the top of `server.go` (check whether `encoding/json` is still used elsewhere in the file — it is, by `writeJSON`/`writeError` — so leave that import in place).

- [ ] **Step 7: Add a regression test proving a legacy-shaped import survives `PUT /api/config`**

Append to `internal/server/integrations_test.go`:

```go
func TestPutConfigMigratesLegacyGroupsShape(t *testing.T) {
	s := newTestServer(t)
	h := s.Handler()

	legacy := map[string]any{
		"title":    "Old export",
		"settings": map[string]any{"theme": "auto", "density": "comfortable", "checkInterval": 60, "openInNewTab": true},
		"groups": []map[string]any{{
			"id": "g1", "name": "Group",
			"items": []map[string]any{{"id": "i1", "name": "Site", "url": "http://example.com"}},
		}},
	}
	w := doJSON(t, h, "PUT", "/api/config", legacy)
	if w.Code != http.StatusOK {
		t.Fatalf("put legacy config: got %d, body %s", w.Code, w.Body.String())
	}

	got := s.store.Get()
	if len(got.Blocks) != 1 || got.Blocks[0].Group == nil || got.Blocks[0].Group.ID != "g1" {
		t.Fatalf("expected the legacy group to survive as a block, got %+v", got.Blocks)
	}
	if len(got.Blocks[0].Group.Items) != 1 || got.Blocks[0].Group.Items[0].ID != "i1" {
		t.Fatalf("expected the legacy item to survive, got %+v", got.Blocks[0].Group.Items)
	}
}
```

- [ ] **Step 8: Run the full backend suite**

Run: `go test ./...`
Expected: PASS across every package.

- [ ] **Step 9: Commit**

```bash
git add internal/status/status.go internal/integrations/poller.go internal/integrations/poller_test.go internal/server/server.go internal/server/integrations_test.go
git commit -m "Update status checker, widget poller, and config import for Blocks

Both background loops now walk Config.Items() instead of re-deriving
the group/item nesting themselves. PUT /api/config goes through
UnmarshalConfig so importing a pre-redesign exported file upgrades it
instead of silently discarding every group."
```

---

## Task 3: Frontend types, row-building helper, and app state rewrite

**Files:**
- Modify: `ui/src/lib/types.ts`
- Create: `ui/src/lib/rows.ts`
- Modify: `ui/src/lib/state.svelte.ts`

**Interfaces:**
- Produces: `type Block`, `Config.blocks`; `buildRows(blocks, query): Row[]`, `allItems(cfg): Item[]`; `AppState.groups`, `AppState.moveBlock`, `AppState.floatItem`, `AppState.sinkItem`, generalized `AppState.upsertItem`/`deleteItem`, new `DragData` union.
- Consumes: nothing new from earlier tasks (this is the frontend's own foundation layer, consumed by Tasks 4-6).

- [ ] **Step 1: Update `Config`/`Block` types**

In `ui/src/lib/types.ts`, replace:

```ts
// Integrations are deliberately absent here: they are owned by the
// dedicated /api/integrations endpoints (see app.integrations), and the
// server ignores the field on PUT /api/config, so a generic config save
// can never overwrite them with a stale copy.
export interface Config {
  title: string
  settings: Settings
  groups: Group[]
}
```

with:

```ts
// A Block is either a Group (its own header + items) or a single
// floating Item — only an item with a widget may ever be floating. See
// internal/config/config.go's Block type for the backend mirror.
export type Block = { type: 'group'; group: Group } | { type: 'item'; item: Item }

// Integrations are deliberately absent here: they are owned by the
// dedicated /api/integrations endpoints (see app.integrations), and the
// server ignores the field on PUT /api/config, so a generic config save
// can never overwrite them with a stale copy.
export interface Config {
  title: string
  settings: Settings
  blocks: Block[]
}
```

- [ ] **Step 2: Create `ui/src/lib/rows.ts`**

```ts
import type { Block, Config, Item } from './types'

export type Row = { kind: 'group'; group: import('./types').Group; items: Item[] } | { kind: 'float'; items: Item[] }

// Groups keep their own shelf; consecutive floating items are rendered
// together in one flex row. This grouping is purely a rendering
// convenience recomputed from the current block order on every call —
// nothing about it is persisted.
export function buildRows(blocks: Block[], query: string): Row[] {
  const q = query.trim().toLowerCase()
  const matches = (it: Item) => !q || `${it.name} ${it.description} ${it.url}`.toLowerCase().includes(q)
  const rows: Row[] = []
  for (const b of blocks) {
    if (b.type === 'group') {
      const items = q ? b.group.items.filter(matches) : b.group.items
      if (!q || items.length > 0) rows.push({ kind: 'group', group: b.group, items })
    } else if (matches(b.item)) {
      const last = rows[rows.length - 1]
      if (last?.kind === 'float') last.items.push(b.item)
      else rows.push({ kind: 'float', items: [b.item] })
    }
  }
  return rows
}

// Every item on the dashboard, whether grouped or floating.
export function allItems(cfg: Config): Item[] {
  const items: Item[] = []
  for (const b of cfg.blocks) {
    if (b.type === 'group') items.push(...b.group.items)
    else items.push(b.item)
  }
  return items
}
```

- [ ] **Step 3: Rewrite `state.svelte.ts`**

Replace the entire file with:

```ts
import * as api from './api'
import type { Config, Group, Integration, Item, Status, WidgetResult } from './types'

export type DragData =
  | { kind: 'item'; id: string; groupId: string; widget: boolean }
  | { kind: 'floating'; id: string }
  | { kind: 'group'; id: string }

// crypto.randomUUID requires HTTPS: not available on a plain-http LAN.
export function uid(): string {
  return Date.now().toString(36) + Math.random().toString(36).slice(2, 8)
}

class AppState {
  config = $state<Config | null>(null)
  status = $state<Record<string, Status>>({})
  editing = $state(false)
  query = $state('')
  drag = $state<DragData | null>(null)
  toast = $state<{ text: string; error: boolean; key: number } | null>(null)
  integrations = $state<Integration[]>([])
  // Only true once the list has actually been fetched: an empty list must
  // not be read as "the integration was deleted" while the first request
  // is still in flight or has failed.
  integrationsLoaded = $state(false)
  widgets = $state<Record<string, WidgetResult>>({})
  #toastTimer: ReturnType<typeof setTimeout> | undefined

  async load() {
    this.config = await api.getConfig()
  }

  async save(message = 'Changes saved') {
    if (!this.config) return
    try {
      await api.putConfig($state.snapshot(this.config) as Config)
      this.notify(message)
    } catch (e) {
      this.notify(`Save failed: ${(e as Error).message}`, true)
      // Re-sync the UI with what the server actually saved.
      await this.load().catch(() => {})
    }
  }

  async refreshStatus() {
    try {
      this.status = await api.getStatus()
    } catch {
      // Next tick retries.
    }
  }

  notify(text: string, error = false) {
    clearTimeout(this.#toastTimer)
    this.toast = { text, error, key: Date.now() }
    this.#toastTimer = setTimeout(() => (this.toast = null), error ? 6000 : 2200)
  }

  get groups(): Group[] {
    const out: Group[] = []
    for (const b of this.config?.blocks ?? []) if (b.type === 'group') out.push(b.group)
    return out
  }

  group(id: string): Group | undefined {
    return this.groups.find((g) => g.id === id)
  }

  // Locates a block by the id of whatever it carries (a group's id, or a
  // floating item's id). Every id in a Config is unique across groups,
  // items, and integrations (enforced by the backend validator), so this
  // alone is enough to find the right block.
  #blockIndex(id: string): number {
    return (
      this.config?.blocks.findIndex((b) => (b.type === 'group' ? b.group.id : b.item.id) === id) ?? -1
    )
  }

  moveItem(fromId: string, itemId: string, toId: string, targetId: string | null, after: boolean) {
    const from = this.group(fromId)
    const to = this.group(toId)
    if (!from || !to || itemId === targetId) return
    const before = order(this.config!)
    const i = from.items.findIndex((x) => x.id === itemId)
    if (i < 0) return
    const [item] = from.items.splice(i, 1)
    let j = targetId ? to.items.findIndex((x) => x.id === targetId) : -1
    j = j < 0 ? to.items.length : j + (after ? 1 : 0)
    to.items.splice(j, 0, item)
    if (order(this.config!) !== before) this.save('Order updated')
  }

  // Reorders a whole block (a group, or a floating item) relative to
  // another block — group-vs-group, floating-vs-floating, or
  // group-vs-floating alike.
  moveBlock(id: string, targetId: string, after: boolean) {
    const blocks = this.config?.blocks
    if (!blocks || id === targetId) return
    const before = order(this.config!)
    const i = this.#blockIndex(id)
    if (i < 0) return
    const [b] = blocks.splice(i, 1)
    let j = this.#blockIndex(targetId)
    j = j < 0 ? blocks.length : j + (after ? 1 : 0)
    blocks.splice(j, 0, b)
    if (order(this.config!) !== before) this.save('Order updated')
  }

  // Pulls a widget item out of its group and drops it on the page as a
  // standalone floating block, positioned relative to another block.
  floatItem(fromGroupId: string, itemId: string, targetId: string, after: boolean) {
    const blocks = this.config?.blocks
    const from = this.group(fromGroupId)
    if (!blocks || !from) return
    const i = from.items.findIndex((x) => x.id === itemId)
    if (i < 0) return
    const [item] = from.items.splice(i, 1)
    let j = this.#blockIndex(targetId)
    j = j < 0 ? blocks.length : j + (after ? 1 : 0)
    blocks.splice(j, 0, { type: 'item', item })
    this.save('Order updated')
  }

  // Drops a floating item into a group, appending it to that group's items.
  sinkItem(itemBlockId: string, toGroupId: string) {
    const blocks = this.config?.blocks
    const to = this.group(toGroupId)
    if (!blocks || !to) return
    const i = this.#blockIndex(itemBlockId)
    const b = i >= 0 ? blocks[i] : undefined
    if (b?.type !== 'item') return
    blocks.splice(i, 1)
    to.items.push(b.item)
    this.save('Order updated')
  }

  // fromId/toId are group ids, or null when the item is (or should stay) floating.
  upsertItem(fromId: string | null, toId: string | null, data: Item) {
    if (toId === null) {
      // Editing an existing floating item in place: it keeps its block,
      // only its fields change.
      const i = this.#blockIndex(data.id)
      const b = i >= 0 ? this.config!.blocks[i] : undefined
      if (b?.type === 'item') b.item = data
      this.save()
      return
    }
    const from = fromId ? this.group(fromId) : undefined
    const to = this.group(toId)
    if (!to) return
    const i = from ? from.items.findIndex((x) => x.id === data.id) : -1
    if (from && i >= 0) {
      if (fromId === toId) from.items[i] = data
      else {
        from.items.splice(i, 1)
        to.items.push(data)
      }
    } else {
      to.items.push(data)
    }
    this.save()
  }

  deleteItem(groupId: string | null, itemId: string) {
    if (!this.config) return
    if (groupId === null) {
      this.config.blocks = this.config.blocks.filter((b) => b.type === 'group' || b.item.id !== itemId)
      this.save('Service deleted')
      return
    }
    const g = this.group(groupId)
    if (!g) return
    g.items = g.items.filter((x) => x.id !== itemId)
    this.save('Service deleted')
  }

  upsertGroup(id: string | null, name: string, color?: string, textColor?: string) {
    if (!this.config) return
    const g = id ? this.group(id) : undefined
    if (g) {
      g.name = name
      g.color = color
      g.textColor = textColor
    } else {
      this.config.blocks.push({ type: 'group', group: { id: uid(), name, items: [], color, textColor } })
    }
    this.save()
  }

  deleteGroup(id: string) {
    if (!this.config) return
    this.config.blocks = this.config.blocks.filter((b) => !(b.type === 'group' && b.group.id === id))
    this.save('Group deleted')
  }

  async loadIntegrations() {
    try {
      this.integrations = await api.getIntegrations()
      this.integrationsLoaded = true
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

  discoverTargets(body: { kind: string; config: Record<string, unknown>; secrets: Record<string, string> }) {
    return api.discoverTargets(body)
  }

  discoverSavedTargets(id: string) {
    return api.discoverSavedTargets(id)
  }
}

function order(cfg: Config): string {
  return cfg.blocks
    .map((b) => (b.type === 'group' ? 'g:' + b.group.id + ':' + b.group.items.map((i) => i.id).join(',') : 'i:' + b.item.id))
    .join('|')
}

export const app = new AppState()
```

- [ ] **Step 4: Type-check**

Run (inside `ui/`): `npm run check`
Expected: fails on every file that still references `app.config.groups` or `Config.groups` directly — `Dashboard.svelte`, `Shelf.svelte`, `Tile.svelte`, `ItemDialog.svelte`, `Settings.svelte`. That's expected; those are fixed in Tasks 4-6. Confirm the errors are confined to those files and not to `rows.ts`/`state.svelte.ts`/`types.ts` themselves.

- [ ] **Step 5: Commit**

```bash
git add ui/src/lib/types.ts ui/src/lib/rows.ts ui/src/lib/state.svelte.ts
git commit -m "Rework frontend state around Blocks: groups + floating widget tiles

Adds moveBlock/floatItem/sinkItem alongside the existing moveItem, and
a buildRows helper that groups consecutive floating items into one
visual row for rendering without persisting that grouping anywhere.
Components are updated in the following commits — this one is
expected to fail svelte-check on its own until then."
```

---

## Task 4: `Tile.svelte` — dual-mode drag (grouped vs. floating) + square link layout

**Files:**
- Modify: `ui/src/components/Tile.svelte`

**Interfaces:**
- Consumes: `app.moveItem`, `app.moveBlock`, `app.floatItem` (Task 3).
- Produces: `Tile`'s `groupId` prop becomes `string | null` (`null` = this tile is a floating block) — consumed by `Dashboard.svelte` in Task 5.

- [ ] **Step 1: Replace `Tile.svelte` in full**

```svelte
<script lang="ts">
  import { app } from '../lib/state.svelte'
  import type { Item } from '../lib/types'
  import Icon from './Icon.svelte'
  import EditIcon from './EditIcon.svelte'
  import CustomApiWidget from './CustomApiWidget.svelte'
  import MultiValueWidget from './MultiValueWidget.svelte'

  // groupId is null when this tile is a floating block (not inside any group).
  let { item, groupId, onedit }: { item: Item; groupId: string | null; onedit: () => void } = $props()

  let drop = $state<'before' | 'after' | null>(null)
  const st = $derived(item.check ? app.status[item.id] : undefined)
  const led = $derived(!item.check ? 'off' : !st ? 'unknown' : st.up ? 'up' : 'down')
  const ledLabel = $derived.by(() => {
    if (led === 'off') return 'Status check disabled'
    if (led === 'unknown') return 'Waiting for the first check'
    if (led === 'up') return `Online, responded in ${st!.latencyMs} ms`
    return `Unreachable: ${st!.error ?? `HTTP ${st!.code}`}`
  })
  const host = $derived.by(() => {
    try {
      return new URL(item.url).host
    } catch {
      return item.url
    }
  })
  const newTab = $derived(app.config?.settings.openInNewTab ?? true)
  const sizeClass = $derived(item.widget ? `size-${item.widget.size}` : '')
  const linkClass = $derived(item.widget ? '' : 'link')
  const linkTitle = $derived(item.widget ? undefined : item.description || host)
  const widgetIntegrationMissing = $derived(
    app.integrationsLoaded && !!item.widget && !app.integrations.some((i) => i.id === item.widget!.integrationId),
  )

  function dragStart(e: DragEvent) {
    app.drag =
      groupId === null
        ? { kind: 'floating', id: item.id }
        : { kind: 'item', id: item.id, groupId, widget: !!item.widget }
    e.dataTransfer?.setData('text/plain', item.name)
    if (e.dataTransfer) e.dataTransfer.effectAllowed = 'move'
  }
  function dragEnd() {
    app.drag = null
    drop = null
  }
  // Grouped tiles only accept another item (same-group or cross-group
  // reorder, handled by moveItem). Floating tiles accept any block-level
  // drag (reorder) or a widget item being pulled out of a group to float
  // next to this one — never a plain link tile.
  function accepts(d: typeof app.drag): boolean {
    if (!d || d.id === item.id) return false
    if (groupId !== null) return d.kind === 'item'
    return d.kind === 'group' || d.kind === 'floating' || (d.kind === 'item' && d.widget)
  }
  function dragOver(e: DragEvent) {
    if (!accepts(app.drag)) return
    e.preventDefault()
    e.stopPropagation()
    const r = (e.currentTarget as HTMLElement).getBoundingClientRect()
    drop = e.clientX < r.left + r.width / 2 ? 'before' : 'after'
  }
  function dragLeave(e: DragEvent) {
    const el = e.currentTarget as HTMLElement
    if (!el.contains(e.relatedTarget as Node)) drop = null
  }
  function onDrop(e: DragEvent) {
    const d = app.drag
    if (!accepts(d)) return
    e.preventDefault()
    e.stopPropagation()
    const after = drop === 'after'
    drop = null
    app.drag = null
    if (groupId !== null) {
      if (d!.kind === 'item') app.moveItem(d.groupId, d.id, groupId, item.id, after)
    } else if (d!.kind === 'group' || d!.kind === 'floating') {
      app.moveBlock(d.id, item.id, after)
    } else if (d!.kind === 'item' && d.widget) {
      app.floatItem(d.groupId, d.id, item.id, after)
    }
  }
</script>

{#snippet body()}
  <Icon icon={item.icon} name={item.name} />
  <span class="text">
    <span class="name">{item.name}</span>
  </span>
  {#if item.widget}
    {#if item.widget.kind === 'customapi'}
      <CustomApiWidget result={app.widgets[item.id]} missing={widgetIntegrationMissing} />
    {:else}
      <MultiValueWidget result={app.widgets[item.id]} missing={widgetIntegrationMissing} />
    {/if}
  {:else if led !== 'off'}
    <span class="led {led}" title={ledLabel}></span>
    <span class="sr-only">{ledLabel}</span>
  {/if}
{/snippet}

{#if app.editing}
  <button
    type="button"
    class="tile {sizeClass} {linkClass}"
    class:dragging={(app.drag?.kind === 'item' || app.drag?.kind === 'floating') && app.drag.id === item.id}
    class:drop-before={drop === 'before'}
    class:drop-after={drop === 'after'}
    draggable="true"
    ondragstart={dragStart}
    ondragend={dragEnd}
    ondragover={dragOver}
    ondragleave={dragLeave}
    ondrop={onDrop}
    onclick={onedit}
    title="Drag to move, click to edit"
  >
    {@render body()}
    <EditIcon />
  </button>
{:else}
  <a class="tile {sizeClass} {linkClass}" href={item.url} target={newTab ? '_blank' : undefined} rel="noopener noreferrer" title={linkTitle}>
    {@render body()}
  </a>
{/if}
```

- [ ] **Step 2: Type-check**

Run (inside `ui/`): `npm run check`
Expected: `Tile.svelte` itself is now clean. Remaining errors should only be in `Shelf.svelte`, `Dashboard.svelte`, `ItemDialog.svelte`, `Settings.svelte` (Tasks 5-6).

- [ ] **Step 3: Commit**

```bash
git add ui/src/components/Tile.svelte
git commit -m "Tile.svelte: square link-tile layout + dual-mode floating drag

A tile without a widget drops its description/host subtitle (kept as
a hover title instead) and gets a new .link CSS hook for the square
icon-on-top layout added in app.css. A tile whose groupId is null is
a floating block: it drags/accepts like a group's tape (reorder
anywhere, or absorb a widget tile pulled out of a group) instead of
the plain within-group item reorder."
```

---

## Task 5: `Shelf.svelte` + `Dashboard.svelte` — floating rows and extended drop targets

**Files:**
- Modify: `ui/src/components/Shelf.svelte`
- Modify: `ui/src/components/Dashboard.svelte`

**Interfaces:**
- Consumes: `app.moveBlock`, `app.floatItem`, `app.sinkItem` (Task 3); `Tile`'s nullable `groupId` (Task 4); `buildRows`, `allItems` (Task 3).

- [ ] **Step 1: Replace `Shelf.svelte`'s script block**

Keep the `<script>` imports and the `$props()` destructure as they are; replace everything from `let groupDrop` through the end of the `<script>` block with:

```ts
  let groupDrop = $state<'before' | 'after' | null>(null)
  let gridOver = $state(false)

  function tapeDragStart(e: DragEvent) {
    app.drag = { kind: 'group', id: group.id }
    e.dataTransfer?.setData('text/plain', group.name)
    if (e.dataTransfer) e.dataTransfer.effectAllowed = 'move'
  }
  function dragEnd() {
    app.drag = null
    groupDrop = null
  }
  function leaving(e: DragEvent) {
    return !(e.currentTarget as HTMLElement).contains(e.relatedTarget as Node)
  }

  // The tape accepts reordering this group against any other block
  // (another group, or a floating tile), and accepts a widget tile being
  // pulled out of its own group to float right before/after this one.
  function tapeAccepts(d: typeof app.drag): boolean {
    if (!d) return false
    if (d.kind === 'group') return d.id !== group.id
    if (d.kind === 'floating') return true
    return d.kind === 'item' && d.widget
  }
  function shelfOver(e: DragEvent) {
    if (!tapeAccepts(app.drag)) return
    e.preventDefault()
    const r = (e.currentTarget as HTMLElement).getBoundingClientRect()
    groupDrop = e.clientY < r.top + r.height / 2 ? 'before' : 'after'
  }
  function shelfDrop(e: DragEvent) {
    const d = app.drag
    if (!tapeAccepts(d)) return
    e.preventDefault()
    const after = groupDrop === 'after'
    const valid = groupDrop !== null
    groupDrop = null
    app.drag = null
    if (!valid || !d) return
    if (d.kind === 'group' || d.kind === 'floating') app.moveBlock(d.id, group.id, after)
    else if (d.kind === 'item' && d.widget) app.floatItem(d.groupId, d.id, group.id, after)
  }

  const tapeStyle = $derived(
    [group.color && `--tape: ${group.color}`, group.textColor && `--tape-ink: ${group.textColor}`].filter(Boolean).join('; ') || undefined,
  )

  // Dropping in the group's empty area appends: works for reordering
  // within/between groups (an 'item' drag) and for sinking a floating
  // tile into this group (a 'floating' drag).
  function gridDragOver(e: DragEvent) {
    if (app.drag?.kind !== 'item' && app.drag?.kind !== 'floating') return
    e.preventDefault()
    gridOver = true
  }
  function gridDrop(e: DragEvent) {
    const d = app.drag
    if (d?.kind !== 'item' && d?.kind !== 'floating') return
    e.preventDefault()
    gridOver = false
    app.drag = null
    if (d.kind === 'item') app.moveItem(d.groupId, d.id, group.id, null, false)
    else app.sinkItem(d.id, group.id)
  }
```

The `<section>`/`<div class="grid">` markup below it is unchanged (still calls `shelfOver`/`shelfDrop`/`gridDragOver`/`gridDrop`, now pointing at the widened functions above). The `<Tile {item} groupId={group.id} .../>` line inside the grid is also unchanged — a grouped tile always gets a real `groupId`.

- [ ] **Step 2: Replace `Dashboard.svelte` in full**

```svelte
<script lang="ts">
  import { app } from '../lib/state.svelte'
  import type { Item } from '../lib/types'
  import { buildRows, allItems } from '../lib/rows'
  import Shelf from './Shelf.svelte'
  import Tile from './Tile.svelte'
  import ItemDialog from './ItemDialog.svelte'
  import GroupDialog from './GroupDialog.svelte'

  let itemEdit = $state<{ groupId: string | null; item: Item | null } | null>(null)
  let groupEdit = $state<{ id: string | null } | null>(null)
  let search: HTMLInputElement | undefined = $state()

  const cfg = $derived(app.config!)
  const rows = $derived(buildRows(cfg.blocks, app.query))

  const summary = $derived.by(() => {
    let up = 0
    let down = 0
    for (const it of allItems(cfg)) {
      const s = it.check ? app.status[it.id] : undefined
      if (s) s.up ? up++ : down++
    }
    if (up + down === 0) return ''
    const parts = [`${up} online`]
    if (down) parts.push(down === 1 ? '1 unreachable' : `${down} unreachable`)
    return parts.join(', ')
  })
  const anyDown = $derived(summary.includes('unreachable'))

  function onWindowKey(e: KeyboardEvent) {
    if (app.editing || itemEdit || groupEdit) return
    const t = e.target as HTMLElement
    if (e.key === '/' && !['INPUT', 'TEXTAREA', 'SELECT'].includes(t.tagName)) {
      e.preventDefault()
      search?.focus()
    }
  }
  function onSearchKey(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      app.query = ''
      search?.blur()
    } else if (e.key === 'Enter') {
      const first = rows[0]?.items[0]
      if (first) window.open(first.url, cfg.settings.openInNewTab ? '_blank' : '_self', 'noopener')
    }
  }
  function toggleEdit() {
    app.editing = !app.editing
    app.query = ''
  }
</script>

<svelte:window onkeydown={onWindowKey} />

<header class="bar">
  <h1>{cfg.title || 'Homie'}</h1>
  {#if !app.editing}
    <input
      bind:this={search}
      bind:value={app.query}
      onkeydown={onSearchKey}
      class="search"
      type="search"
      placeholder="Search (press /)"
      aria-label="Search services"
    />
  {/if}
  {#if summary}
    <p class="summary" class:alert={anyDown}>{summary}</p>
  {/if}
  <nav class="actions">
    <button type="button" class:primary={app.editing} onclick={toggleEdit}>
      {app.editing ? 'Done editing' : 'Edit'}
    </button>
    <a class="btn" href="#/settings">Settings</a>
  </nav>
</header>

{#if app.editing && cfg.blocks.length > 0}
  <p class="edit-hint">Drag tiles and group labels to reorder them. Click an item to edit it.</p>
{/if}

<main class="shelves" class:editing={app.editing}>
  {#if cfg.blocks.length === 0}
    <div class="empty">
      <p>The dashboard is empty. Create a group, then add your services to it.</p>
      <button
        type="button"
        class="primary"
        onclick={() => {
          app.editing = true
          groupEdit = { id: null }
        }}>Add group</button
      >
    </div>
  {:else if rows.length === 0}
    <p class="empty">No service matches “{app.query}”.</p>
  {:else}
    {#each rows as row, i (row.kind === 'group' ? row.group.id : 'float-' + i)}
      {#if row.kind === 'group'}
        <Shelf
          group={row.group}
          items={row.items}
          onEditItem={(item) => (itemEdit = { groupId: row.group.id, item })}
          onAddItem={() => (itemEdit = { groupId: row.group.id, item: null })}
          onEditGroup={() => (groupEdit = { id: row.group.id })}
        />
      {:else}
        <div class="float-row">
          {#each row.items as item (item.id)}
            <Tile {item} groupId={null} onedit={() => (itemEdit = { groupId: null, item })} />
          {/each}
        </div>
      {/if}
    {/each}
  {/if}
  {#if app.editing && cfg.blocks.length > 0}
    <button type="button" class="add-group" onclick={() => (groupEdit = { id: null })}>Add group</button>
  {/if}
</main>

{#if itemEdit}
  <ItemDialog groupId={itemEdit.groupId} item={itemEdit.item} onclose={() => (itemEdit = null)} />
{/if}
{#if groupEdit}
  <GroupDialog groupId={groupEdit.id} onclose={() => (groupEdit = null)} />
{/if}
```

- [ ] **Step 3: Type-check**

Run (inside `ui/`): `npm run check`
Expected: `Shelf.svelte` and `Dashboard.svelte` are now clean. Remaining errors confined to `ItemDialog.svelte`/`Settings.svelte` (Task 6).

- [ ] **Step 4: Commit**

```bash
git add ui/src/components/Shelf.svelte ui/src/components/Dashboard.svelte
git commit -m "Dashboard: render Blocks as groups + floating widget rows

Dashboard iterates buildRows() instead of cfg.groups, rendering bare
Tiles (no Shelf wrapper) for consecutive floating blocks inside a new
.float-row container. Shelf's tape and grid-drop zones now also
accept a floating tile being reordered, or a widget tile being pulled
out of the group to float next to/into this one."
```

---

## Task 6: `ItemDialog.svelte` + `Settings.svelte` — nullable group, import compatibility

**Files:**
- Modify: `ui/src/components/ItemDialog.svelte`
- Modify: `ui/src/components/Settings.svelte`

**Interfaces:**
- Consumes: `app.groups` getter, generalized `app.upsertItem`/`app.deleteItem` (Task 3); `itemEdit.groupId: string | null` (Task 5).

- [ ] **Step 1: Update `ItemDialog.svelte`'s prop type and derived state**

Change the props line:

```ts
let { groupId, item, onclose }: { groupId: string; item: Item | null; onclose: () => void } = $props()
```

to:

```ts
let { groupId, item, onclose }: { groupId: string | null; item: Item | null; onclose: () => void } = $props()
```

And the `target` state initializer:

```ts
  // svelte-ignore state_referenced_locally
  let target = $state(groupId)
```

to:

```ts
  // svelte-ignore state_referenced_locally
  let target = $state(groupId ?? '')
```

- [ ] **Step 2: Update `submit`/`remove` to pass `groupId` through as-is**

Change the `upsertItem` call inside `submit`:

```ts
    app.upsertItem(groupId, target, {
```

to:

```ts
    app.upsertItem(groupId, groupId === null ? null : target, {
```

`remove()`'s `app.deleteItem(groupId, item.id)` call needs no change — `deleteItem` already accepts `string | null` from Task 3.

- [ ] **Step 3: Replace the Group `<select>` block**

Replace:

```svelte
    <label>
      Group
      <select bind:value={target}>
        {#each app.config!.groups as g (g.id)}
          <option value={g.id}>{g.name}</option>
        {/each}
      </select>
    </label>
```

with:

```svelte
    {#if groupId !== null}
      <label>
        Group
        <select bind:value={target}>
          {#each app.groups as g (g.id)}
            <option value={g.id}>{g.name}</option>
          {/each}
        </select>
      </label>
    {:else}
      <p class="hint">This tile is floating on the dashboard. Drag it onto a group to move it there.</p>
    {/if}
```

- [ ] **Step 4: Update `Settings.svelte`'s import validation**

Change:

```ts
      if (!Array.isArray(parsed?.groups)) throw new Error('the file does not contain a Homie configuration')
```

to:

```ts
      if (!Array.isArray(parsed?.blocks) && !Array.isArray(parsed?.groups)) {
        throw new Error('the file does not contain a Homie configuration')
      }
```

- [ ] **Step 5: Type-check**

Run (inside `ui/`): `npm run check`
Expected: PASS, zero errors across the whole `ui/` project.

- [ ] **Step 6: Commit**

```bash
git add ui/src/components/ItemDialog.svelte ui/src/components/Settings.svelte
git commit -m "ItemDialog/Settings: support editing floating items and importing legacy exports

Editing an already-floating item hides the Group picker instead of
forcing a choice that would silently un-float it. Settings' import
validation accepts either the current blocks shape or a pre-redesign
groups export — the backend's own migration (UnmarshalConfig) handles
upgrading the latter."
```

---

## Task 7: CSS — square link tiles, floating row, and the row-stretch fix

**Files:**
- Modify: `ui/src/app.css`

**Interfaces:**
- Consumes: `.tile.link` class (Task 4), `.float-row` container (Task 5 markup).

- [ ] **Step 1: Fix the row-stretch bug on `.grid`**

In `ui/src/app.css`, change:

```css
.grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(var(--tile-min), 1fr));
  gap: var(--gap);
  padding: var(--gap);
  align-content: start;
  border-radius: 0 6px 6px 0;
}
```

to:

```css
.grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(var(--tile-min), 1fr));
  gap: var(--gap);
  padding: var(--gap);
  align-content: start;
  align-items: start;
  border-radius: 0 6px 6px 0;
}
```

- [ ] **Step 2: Add the square link-tile layout**

After the existing `.tile.size-2x2 { grid-row: span 2; }` rule, add:

```css
.tile.link {
  grid-template-columns: 1fr;
  grid-template-rows: auto auto;
  justify-items: center;
  align-items: center;
  text-align: center;
  aspect-ratio: 1;
  padding: var(--tile-pad);
}
```

- [ ] **Step 3: Add the floating-row container, with the same stretch fix applied**

After the `.shelf.dragging { opacity: 0.5; }` rule, add:

```css
/* A row of floating widget tiles, with no group header around them. */
.float-row {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-start;
  gap: var(--gap);
}

.float-row .tile {
  flex: 0 0 var(--tile-min);
}

.float-row .tile.size-2x1,
.float-row .tile.size-2x2 {
  flex-basis: calc(var(--tile-min) * 2 + var(--gap));
}
```

- [ ] **Step 4: Cancel the widened floating-tile size on the narrow-viewport single-column breakpoint**

Inside the existing `@media (max-width: 640px) { ... }` block, right after the rule that cancels `.tile.size-2x1`/`.size-2x2`'s `grid-column: span 2` back to `span 1`, add the flex equivalent:

```css
  :root:not([data-density='compact']) .float-row .tile.size-2x1,
  :root:not([data-density='compact']) .float-row .tile.size-2x2 {
    flex-basis: var(--tile-min);
  }
```

- [ ] **Step 5: Build the frontend and type-check**

Run (inside `ui/`): `npm run check && npm run build`
Expected: both PASS with zero errors.

- [ ] **Step 6: Manual verification in the browser**

Run (inside `ui/`): `npm run dev`, open the printed local URL.

Walk through, in edit mode:
1. Confirm every plain link tile now renders as a square: icon on top, name below, no address line.
2. Add or use an existing Proxmox/PBS/Custom API widget tile sized `2x2`. Drag another widget tile (or a link tile) from inside the same group out onto the gap above the first group's tape (hover the top half) — confirm it detaches into its own floating row at the top of the page, and confirm neighboring tiles in its OLD group's row did not stretch to its height before the move, and the new floating row doesn't stretch either after.
3. Drag a second widget tile so it lands next to the first floating tile — confirm they render side by side in one flex row.
4. Drag one of the floating tiles back down onto a group's grid area — confirm it re-joins that group as a normal grouped tile (appended at the end).
5. Try dragging a plain link tile (no widget) toward the gap above/below a group's tape, or onto a floating tile — confirm there is no drop indicator and nothing happens on release.
6. Click a floating tile to open its edit dialog — confirm there is no Group dropdown, just the "floating on the dashboard" hint — change its name, save, confirm it's still floating afterward.
7. Resize the browser window below ~400px width and confirm nothing overflows horizontally, in both comfortable and compact density (Settings page).
8. In Settings, export the current config, then re-import that same exported file — confirm the dashboard looks identical afterward (proves the current-shape export/import round-trips).

- [ ] **Step 7: Commit**

```bash
git add ui/src/app.css
git commit -m "CSS: square link tiles, floating-row layout, and the grid row-stretch fix

.grid and the new .float-row both set align-items so a tall 2x2
widget tile no longer stretches its neighbors to match its height.
Link tiles (no widget) get a square icon-on-top layout via .tile.link."
```

---

## Final check

- [ ] Run `go build ./... && go test ./...` from the repo root — expect a clean build and a full PASS.
- [ ] Run `npm run check && npm run build` inside `ui/` — expect zero errors and a successful build.
- [ ] Confirm `git log --oneline -7` shows the seven commits from this plan, each scoped to the files it claims.
