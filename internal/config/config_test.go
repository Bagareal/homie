package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func baseConfig() Config {
	c := Default()
	c.Groups = []Group{{
		ID:   "g1",
		Name: "Group",
		Items: []Item{{
			ID:   "i1",
			Name: "Site",
			URL:  "http://example.com",
		}},
	}}
	return c
}

func TestValidateRejectsDuplicateItemID(t *testing.T) {
	c := baseConfig()
	c.Groups = append(c.Groups, Group{ID: "g2", Name: "Group 2", Items: []Item{{ID: "i1", Name: "Dup", URL: "http://example.com"}}})
	if err := c.Validate(); err == nil {
		t.Fatal("expected an error for a duplicate item id across groups")
	}
}

func TestValidateRejectsInvalidURL(t *testing.T) {
	c := baseConfig()
	c.Groups[0].Items[0].URL = "not-a-url"
	if err := c.Validate(); err == nil {
		t.Fatal("expected an error for an invalid address")
	}
}

func TestValidateAcceptsCustomColors(t *testing.T) {
	c := baseConfig()
	c.Settings.BackgroundColor = "#101418"
	c.Settings.TileColor = "#1b222b"
	c.Settings.AccentColor = "#5aa9e6"
	if err := c.Validate(); err != nil {
		t.Fatalf("expected valid hex colors to be accepted, got %v", err)
	}
}

func TestValidateRejectsMalformedAccentColor(t *testing.T) {
	c := baseConfig()
	c.Settings.AccentColor = "blue"
	if err := c.Validate(); err == nil {
		t.Fatal("expected an error for a non-hex accent color")
	}
}

func TestNormalizeDefaultsEmptyGroupsSlice(t *testing.T) {
	c := Config{}
	c.normalize()
	if c.Groups == nil {
		t.Fatal("expected Groups to default to an empty slice, got nil")
	}
	if c.Settings.Theme != "auto" || c.Settings.Density != "comfortable" || c.Settings.CheckInterval != 60 {
		t.Fatalf("expected default settings to be filled in, got %+v", c.Settings)
	}
}

func TestUnmarshalConfigAcceptsCurrentShape(t *testing.T) {
	raw := []byte(`{"title":"New","settings":{"theme":"auto","density":"comfortable","checkInterval":60},"groups":[{"id":"g1","name":"Group","items":[{"id":"i1","name":"Site","url":"http://example.com"}]}]}`)
	c, err := UnmarshalConfig(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(c.Groups) != 1 || c.Groups[0].ID != "g1" || len(c.Groups[0].Items) != 1 {
		t.Fatalf("expected the config to parse as-is, got %+v", c.Groups)
	}
}

func TestUnmarshalConfigDropsWidgetAndTapeColorFieldsFromAnOldExport(t *testing.T) {
	// A v0.5.0 export: items can carry a "widget" object, groups a "color"
	// and "textColor". Neither field exists on the current Item/Group
	// shape, so Go's standard unknown-field handling already drops them —
	// this test pins that this stays true after the widget removal.
	raw := []byte(`{"title":"Old","settings":{"theme":"auto","density":"comfortable","checkInterval":60},"groups":[{"id":"g1","name":"Group","color":"#e9b949","textColor":"#2b2306","items":[{"id":"i1","name":"Site","url":"http://example.com","widget":{"integrationId":"x","kind":"customapi","size":"1x1"}}]}]}`)
	c, err := UnmarshalConfig(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(c.Groups) != 1 || len(c.Groups[0].Items) != 1 || c.Groups[0].Items[0].ID != "i1" {
		t.Fatalf("expected the group and its item to survive without the dropped fields, got %+v", c.Groups)
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("expected the migrated config to validate cleanly, got %v", err)
	}
}

func TestUnmarshalConfigMigratesV06BlocksShape(t *testing.T) {
	// The brief v0.6.x shape: a "blocks" array mixing group blocks and
	// single floating widget-item blocks. The group survives as a group;
	// the floating widget item survives as a plain link inside a
	// synthetic "Other" group, its widget data dropped.
	raw := []byte(`{"title":"Old","settings":{"theme":"auto","density":"comfortable","checkInterval":60},"blocks":[{"type":"item","item":{"id":"px","name":"Proxmox","url":"https://pve.local:8006","widget":{"integrationId":"x","kind":"proxmoxve","size":"2x2"}}},{"type":"group","group":{"id":"g1","name":"Services","items":[{"id":"i1","name":"Site","url":"http://example.com"}]}}]}`)
	c, err := UnmarshalConfig(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(c.Groups) != 2 {
		t.Fatalf("expected 2 groups (Services + the synthetic Other), got %d: %+v", len(c.Groups), c.Groups)
	}
	if c.Groups[0].ID != "g1" || len(c.Groups[0].Items) != 1 {
		t.Fatalf("expected the Services group to survive first, got %+v", c.Groups[0])
	}
	other := c.Groups[1]
	if other.ID != "other" || len(other.Items) != 1 || other.Items[0].ID != "px" || other.Items[0].Name != "Proxmox" {
		t.Fatalf("expected the floating Proxmox item to land in a synthetic Other group, got %+v", other)
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("expected the migrated config to validate cleanly, got %v", err)
	}
}

func TestUnmarshalConfigErrorsOnMalformedLegacyBlocks(t *testing.T) {
	malformed := []byte(`{"title":"Old","settings":{"theme":"auto","density":"comfortable","checkInterval":60},"blocks":[{"type":"group","group":{"id":"g1","name":"Group","items":"not-an-array"}}]}`)
	_, err := UnmarshalConfig(malformed)
	if err == nil {
		t.Fatal("expected an error for a malformed legacy blocks shape, not a silently emptied config")
	}
}

func TestStoreOpenMigratesLegacyGroupsFileAndPersistsOnSave(t *testing.T) {
	dir := t.TempDir()
	legacy := []byte(`{"title":"Old","settings":{"theme":"auto","density":"comfortable","checkInterval":60},"groups":[{"id":"g1","name":"Group","items":[{"id":"i1","name":"Site","url":"http://example.com","widget":{"integrationId":"x","kind":"customapi","size":"1x1"}}]}]}`)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), legacy, 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	got := s.Get()
	if len(got.Groups) != 1 || got.Groups[0].ID != "g1" || len(got.Groups[0].Items) != 1 {
		t.Fatalf("expected the legacy group to survive Open, got %+v", got.Groups)
	}

	if err := s.Set(got); err != nil {
		t.Fatalf("Set: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	var onDisk map[string]any
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatalf("unmarshal on-disk: %v", err)
	}
	groups, _ := onDisk["groups"].([]any)
	if len(groups) != 1 {
		t.Fatalf("expected one group on disk, got %+v", onDisk["groups"])
	}
	items, _ := groups[0].(map[string]any)["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected one item on disk, got %+v", groups[0])
	}
	if _, hasWidget := items[0].(map[string]any)["widget"]; hasWidget {
		t.Fatal("expected the widget field to be gone from the saved item")
	}
}

func TestStoreOpenMigratesV06BlocksFileAndPersistsOnSave(t *testing.T) {
	dir := t.TempDir()
	legacy := []byte(`{"title":"Old","settings":{"theme":"auto","density":"comfortable","checkInterval":60},"blocks":[{"type":"item","item":{"id":"px","name":"Proxmox","url":"https://pve.local:8006","widget":{"integrationId":"x","kind":"proxmoxve","size":"2x2"}}}]}`)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), legacy, 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	got := s.Get()
	if len(got.Groups) != 1 || got.Groups[0].ID != "other" || len(got.Groups[0].Items) != 1 || got.Groups[0].Items[0].Name != "Proxmox" {
		t.Fatalf("expected the floating item to survive as a plain link in an Other group, got %+v", got.Groups)
	}

	if err := s.Set(got); err != nil {
		t.Fatalf("Set: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	var onDisk map[string]any
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatalf("unmarshal on-disk: %v", err)
	}
	if _, hasBlocks := onDisk["blocks"]; hasBlocks {
		t.Fatal("expected the legacy \"blocks\" key to be gone from disk after a save")
	}
	if _, hasGroups := onDisk["groups"]; !hasGroups {
		t.Fatal("expected the on-disk config to use the \"groups\" key after a save")
	}
}

func TestValidateColorAcceptsOptionalAlpha(t *testing.T) {
	for color, ok := range map[string]bool{
		"#e9b949":   true,
		"#e9b94980": true,
		"#E9B949FF": true,
		"#e9b94":    false,
		"#e9b9498":  false,
		"e9b949":    false,
		"#gggggg":   false,
	} {
		c := Default()
		c.Settings.AccentColor = color
		if err := c.Validate(); (err == nil) != ok {
			t.Errorf("accent %q: valid=%v, got err=%v", color, ok, err)
		}
	}
}

func widgetConfig() Config {
	c := baseConfig()
	c.Widgets = []Widget{{
		ID: "w1", Name: "Proxmox", URL: "http://pve.local:61208",
		Rows: []Row{{ID: "r1", Type: "cpu", Load: true}, {ID: "r2", Type: "mem"}, {ID: "r3", Type: "disk", Key: "/"}},
	}}
	c.normalize()
	return c
}

func TestWidgetDefaultsAreFilledIn(t *testing.T) {
	w := widgetConfig().Widgets[0]
	if w.Kind != "glances" || w.Refresh != 5 || w.Width != 300 || w.Opacity != 100 {
		t.Fatalf("defaults not applied: %+v", w)
	}
	if err := widgetConfig().Validate(); err != nil {
		t.Fatalf("a normalized widget should validate: %v", err)
	}
}

func TestValidateRejectsBadWidgets(t *testing.T) {
	for name, mutate := range map[string]func(w *Widget){
		"no name":         func(w *Widget) { w.Name = "" },
		"bad scheme":      func(w *Widget) { w.URL = "ftp://pve" },
		"unknown row":     func(w *Widget) { w.Rows[0].Type = "gpu" },
		"empty cpu row":   func(w *Widget) { w.Rows[0].Load = false },
		"repeated row id": func(w *Widget) { w.Rows[1].ID = "r1" },
		"long key":        func(w *Widget) { w.Rows[2].Key = strings.Repeat("x", 300) },
		"too many rows": func(w *Widget) {
			for i := 0; i < 40; i++ {
				w.Rows = append(w.Rows, Row{ID: "x" + strings.Repeat("a", i), Type: "mem"})
			}
		},
		"fast refresh": func(w *Widget) { w.Refresh = 1 },
		"narrow":       func(w *Widget) { w.Width = 50 },
		"tall":         func(w *Widget) { w.Height = 5000 },
		"bad color":    func(w *Widget) { w.Color = "red" },
		"thick border": func(w *Widget) { b := 9; w.BorderWidth = &b },
		"unknown kind": func(w *Widget) { w.Kind = "zabbix" },
	} {
		c := widgetConfig()
		mutate(&c.Widgets[0])
		if err := c.Validate(); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	c := widgetConfig()
	c.Widgets = append(c.Widgets, c.Widgets[0])
	if err := c.Validate(); err == nil {
		t.Error("a duplicate widget id should be rejected")
	}
}

func TestRedactedHidesPasswords(t *testing.T) {
	c := widgetConfig()
	c.Widgets[0].Password = "s3cret"
	r := c.Redacted()
	if r.Widgets[0].Password != "" || !r.Widgets[0].HasPassword {
		t.Fatalf("redacted widget = %+v", r.Widgets[0])
	}
	if c.Widgets[0].Password != "s3cret" {
		t.Fatal("Redacted must not modify the original")
	}
	raw, _ := json.Marshal(r)
	if strings.Contains(string(raw), "s3cret") {
		t.Fatal("password present in the JSON sent to the browser")
	}
}

func TestStoreKeepsThePasswordWhenTheBrowserSendsNone(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c := widgetConfig()
	c.Widgets[0].Password = "s3cret"
	if err := st.Set(c); err != nil {
		t.Fatal(err)
	}

	// The browser saves again (e.g. a reorder) without ever having seen it.
	c = st.Get().Redacted()
	c.Title = "Renamed"
	if err := st.Set(c); err != nil {
		t.Fatal(err)
	}
	if got := st.Get().Widgets[0].Password; got != "s3cret" {
		t.Fatalf("password = %q, want it kept", got)
	}

	// Pointing the widget somewhere else must not carry the password along.
	c = st.Get().Redacted()
	c.Widgets[0].URL = "http://elsewhere.local:61208"
	if err := st.Set(c); err != nil {
		t.Fatal(err)
	}
	if got := st.Get().Widgets[0].Password; got != "" {
		t.Fatalf("password = %q after the address changed, want it dropped", got)
	}
	c = st.Get().Redacted()
	c.Widgets[0].URL = "http://pve.local:61208"
	c.Widgets[0].Password = "s3cret"
	if err := st.Set(c); err != nil {
		t.Fatal(err)
	}

	// An explicit new value replaces it.
	c = st.Get().Redacted()
	c.Widgets[0].Password = "other"
	if err := st.Set(c); err != nil {
		t.Fatal(err)
	}
	if got := st.Get().Widgets[0].Password; got != "other" {
		t.Fatalf("password = %q, want the new one", got)
	}

	// ...and an explicit request removes it.
	c = st.Get().Redacted()
	c.Widgets[0].ClearPassword = true
	if err := st.Set(c); err != nil {
		t.Fatal(err)
	}
	if got := st.Get().Widgets[0].Password; got != "" {
		t.Fatalf("password = %q, want it cleared", got)
	}
	w := st.Get().Widgets[0]
	if w.ClearPassword || w.HasPassword {
		t.Fatalf("transient flags must not be stored: %+v", w)
	}
}

func TestWidgetsFromV110AreUpgradedToRowsAndOrder(t *testing.T) {
	dir := t.TempDir()
	old := `{"title":"T","settings":{"theme":"auto","density":"comfortable","checkInterval":60},
	"groups":[{"id":"g1","name":"G","items":[]},{"id":"g2","name":"H","items":[]}],
	"widgets":[
	 {"id":"wtop","name":"Top","kind":"glances","url":"http://a:61208","metrics":["cpu","temp","disks","network"],
	  "mounts":["/","/mnt/x"],"sensor":"Composite","refresh":5,"placement":"top","width":300,"height":0,"opacity":100},
	 {"id":"wbot","name":"Bottom","kind":"glances","url":"http://b:61208","metrics":["mem","disks"],
	  "refresh":5,"placement":"bottom","width":300,"height":0,"opacity":100}]}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	c := st.Get()
	if err := c.Validate(); err != nil {
		t.Fatalf("upgraded config should validate: %v", err)
	}
	if got := strings.Join(c.Order, ","); got != "wtop,g1,g2,wbot" {
		t.Errorf("order = %s, want the old top/groups/bottom arrangement", got)
	}
	var kinds []string
	for _, r := range c.Widgets[0].Rows {
		kinds = append(kinds, r.Type+":"+r.Key)
	}
	if got := strings.Join(kinds, " "); got != "cpu: temp:Composite disk:/ disk:/mnt/x network:" {
		t.Errorf("rows = %s", got)
	}
	if !c.Widgets[0].Rows[0].Load {
		t.Error("the CPU row should keep showing the load")
	}
	if got := c.Widgets[1].Rows; len(got) != 2 || got[1].Type != "disk" || got[1].Key != "" {
		t.Errorf("a widget without disk filters should show all disks: %+v", got)
	}
	raw, _ := json.Marshal(c)
	for _, gone := range []string{`"metrics"`, `"mounts"`, `"placement"`, `"sensor"`} {
		if strings.Contains(string(raw), gone) {
			t.Errorf("old field %s still written", gone)
		}
	}
}

func TestOrderListsEveryBlockOnceAndFollowsTheSaved(t *testing.T) {
	c := widgetConfig() // group g1, widget w1
	c.Groups = append(c.Groups, Group{ID: "g2", Name: "Two", Items: []Item{}})
	c.Order = []string{"g2", "ghost", "w1", "g2"}
	c.normalize()
	if got := strings.Join(c.Order, ","); got != "g2,w1,g1" {
		t.Errorf("order = %s: unknown and repeated ids should go, a missing group should be appended", got)
	}
}

func TestOldConfigWithoutWidgetsStillLoads(t *testing.T) {
	dir := t.TempDir()
	old := `{"title":"T","settings":{"theme":"auto","density":"comfortable","checkInterval":60},"groups":[]}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if st.Get().Widgets == nil {
		t.Fatal("widgets should default to an empty list, not null")
	}
}

func TestTrueNASWidgets(t *testing.T) {
	c := widgetConfig()
	c.Widgets[0].Kind = "truenas"
	c.Widgets[0].URL = "https://truenas.local"
	c.Widgets[0].Rows = []Row{{ID: "a", Type: "pool", Key: "tank"}, {ID: "b", Type: "alerts"}, {ID: "c", Type: "drive"}, {ID: "d", Type: "cpu", Load: true}}
	if err := c.Validate(); err != nil {
		t.Fatalf("a TrueNAS widget should validate: %v", err)
	}

	c.Widgets[0].URL = "http://truenas.local"
	if err := c.Validate(); err == nil {
		t.Error("TrueNAS over plain http must be refused: it revokes the API key")
	}
	c.Widgets[0].URL = "https://truenas.local"

	c.Widgets[0].Rows = []Row{{ID: "a", Type: "swap"}}
	if err := c.Validate(); err == nil {
		t.Error("a Glances-only row on a TrueNAS widget should be refused")
	}
	c = widgetConfig()
	c.Widgets[0].Rows = []Row{{ID: "a", Type: "pool"}}
	if err := c.Validate(); err == nil {
		t.Error("a TrueNAS-only row on a Glances widget should be refused")
	}
}
