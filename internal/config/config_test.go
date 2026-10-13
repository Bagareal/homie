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
	if w.Kind != "glances" || w.Refresh != 5 || w.Cols != 1 || w.HeightUnits != 1 || w.Precise == nil || !*w.Precise || w.Opacity != nil {
		t.Fatalf("defaults not applied: %+v", w)
	}
	if err := widgetConfig().Validate(); err != nil {
		t.Fatalf("a normalized widget should validate: %v", err)
	}
}

func TestWidgetSizeKeepsWhatWasSaved(t *testing.T) {
	// A widget from before sizes were counted in groups has a pixel height and no
	// "precise" value: it stays precise. One that says precise:false keeps that.
	c := widgetConfig()
	if p := c.Widgets[0].Precise; p == nil || !*p {
		t.Fatalf("a widget without the field should become precise, got %v", p)
	}
	no := false
	c.Widgets[0].Precise = &no
	c.normalize()
	if *c.Widgets[0].Precise {
		t.Error("an explicit precise:false was overwritten")
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
		"too wide":     func(w *Widget) { w.Cols = 4 },
		"too high":     func(w *Widget) { w.HeightUnits = 4 },
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

func TestUPSWidgets(t *testing.T) {
	c := widgetConfig()
	c.Widgets[0].Kind = "nut"
	c.Widgets[0].URL = "nut://192.168.1.10:3493/myups"
	c.Widgets[0].Rows = []Row{{ID: "a", Type: "status"}, {ID: "b", Type: "charge"}, {ID: "c", Type: "client", Key: "192.168.1.10", Name: "NAS", Role: "primary"}, {ID: "d", Type: "clients"}}
	if err := c.Validate(); err != nil {
		t.Fatalf("a UPS widget should validate: %v", err)
	}
	c.Widgets[0].URL = "http://192.168.1.10"
	if err := c.Validate(); err == nil {
		t.Error("a UPS address must start with nut://")
	}
	c.Widgets[0].URL = "nut://192.168.1.10"
	c.Widgets[0].Rows = []Row{{ID: "a", Type: "client"}}
	if err := c.Validate(); err == nil {
		t.Error("a client row needs an address")
	}
	c.Widgets[0].Rows = []Row{{ID: "a", Type: "client", Key: "10.0.0.2", Role: "boss"}}
	if err := c.Validate(); err == nil {
		t.Error("a client is either primary or secondary")
	}
	c.Widgets[0].Rows = []Row{{ID: "a", Type: "cpu", Load: true}}
	if err := c.Validate(); err == nil {
		t.Error("a Glances row on a UPS widget should be refused")
	}
}

func TestProxmoxWidgetsAndTheExtraGlancesSource(t *testing.T) {
	c := widgetConfig()
	c.Widgets[0].Kind = "proxmox"
	c.Widgets[0].URL = "https://192.168.1.10:8006"
	c.Widgets[0].Username = "root@pam!homie"
	c.Widgets[0].Rows = []Row{{ID: "a", Type: "lxc"}, {ID: "b", Type: "cpu", Load: true}, {ID: "c", Type: "storage", Key: "local"}, {ID: "d", Type: "quorum"}}
	if err := c.Validate(); err != nil {
		t.Fatalf("a Proxmox widget should validate: %v", err)
	}
	c.Widgets[0].Username = "homie"
	if err := c.Validate(); err == nil {
		t.Error("a Proxmox token id needs user@realm!name")
	}
	c.Widgets[0].Username = "root@pam!homie"

	c.Widgets[0].Glances = &Extra{URL: "http://192.168.1.10:61208", Nodes: map[string]string{"pve2": "http://192.168.1.11:61208"}}
	c.Widgets[0].Rows = append(c.Widgets[0].Rows, Row{ID: "e", Type: "cpu", Temp: true, Source: "glances"}, Row{ID: "f", Type: "disk", Source: "glances", Node: "pve2"})
	if err := c.Validate(); err != nil {
		t.Fatalf("the extra Glances source should validate: %v", err)
	}
	c.Widgets[0].Rows = []Row{{ID: "a", Type: "lxc", Source: "glances"}}
	if err := c.Validate(); err == nil {
		t.Error("a Proxmox row is not a Glances row")
	}
	c.Widgets[0].Rows = []Row{{ID: "a", Type: "cpu", Load: true}}
	c.Widgets[0].Glances.Nodes["pve2"] = "ftp://x"
	if err := c.Validate(); err == nil {
		t.Error("a node address must be http or https")
	}
	c.Widgets[0].Glances = nil
	c.Widgets[0].Rows = []Row{{ID: "a", Type: "disk", Source: "glances"}}
	if err := c.Validate(); err == nil {
		t.Error("a Glances row needs the Glances source")
	}
	g := widgetConfig()
	g.Widgets[0].Glances = &Extra{URL: "http://x:61208"}
	if err := g.Validate(); err == nil {
		t.Error("a Glances widget cannot have an extra Glances source")
	}

	// An extra source with no address is dropped; Redacted never leaks or alters the stored one.
	c = widgetConfig()
	c.Widgets[0].Kind = "truenas"
	c.Widgets[0].URL = "https://nas"
	c.Widgets[0].Glances = &Extra{URL: "http://nas:61208", Password: "pw"}
	red := c.Redacted()
	if red.Widgets[0].Glances.Password != "" || !red.Widgets[0].Glances.HasPassword || c.Widgets[0].Glances.Password != "pw" {
		t.Errorf("Redacted: %+v / stored %+v", red.Widgets[0].Glances, c.Widgets[0].Glances)
	}
	c.Widgets[0].Glances = &Extra{URL: "  "}
	c.normalize()
	if c.Widgets[0].Glances != nil {
		t.Error("an extra source with nothing to read should be dropped")
	}
}

func TestUPSAsAnExtraSource(t *testing.T) {
	for _, kind := range []string{"glances", "proxmox", "truenas"} {
		c := widgetConfig()
		c.Widgets[0].Kind = kind
		c.Widgets[0].URL = "https://192.168.1.10"
		c.Widgets[0].Username = "root@pam!homie"
		c.Widgets[0].Rows = []Row{{ID: "a", Type: "charge", Source: "nut"}, {ID: "b", Type: "client", Key: "10.0.0.2", Role: "secondary", Source: "nut"}}
		c.Widgets[0].NUT = &UPSExtra{URL: "nut://192.168.1.10"}
		if err := c.Validate(); err != nil {
			t.Fatalf("%s with a UPS source should validate: %v", kind, err)
		}
		c.Widgets[0].NUT.URL = "http://192.168.1.10"
		if err := c.Validate(); err == nil {
			t.Errorf("%s: the UPS address must start with nut://", kind)
		}
		c.Widgets[0].NUT = nil
		if err := c.Validate(); err == nil {
			t.Errorf("%s: a UPS row needs the UPS source", kind)
		}
	}
	// A UPS widget already is one, and it can still read Glances.
	c := widgetConfig()
	c.Widgets[0].Kind = "nut"
	c.Widgets[0].URL = "nut://192.168.1.10"
	c.Widgets[0].NUT = &UPSExtra{URL: "nut://192.168.1.11"}
	if err := c.Validate(); err == nil {
		t.Error("a UPS widget cannot have a UPS source")
	}
	c.Widgets[0].NUT = nil
	c.Widgets[0].Glances = &Extra{URL: "http://192.168.1.10:61208"}
	c.Widgets[0].Rows = []Row{{ID: "a", Type: "status"}, {ID: "b", Type: "cpu", Load: true, Source: "glances"}}
	if err := c.Validate(); err != nil {
		t.Fatalf("a UPS widget with Glances should validate: %v", err)
	}
	// An empty UPS address means the source is off.
	c.Widgets[0].Glances = nil
	c.Widgets[0].Rows = []Row{{ID: "a", Type: "status"}}
	c.Widgets[0].Kind = "truenas"
	c.Widgets[0].URL = "https://nas"
	c.Widgets[0].NUT = &UPSExtra{URL: "  "}
	c.normalize()
	if c.Widgets[0].NUT != nil {
		t.Error("an empty UPS source should be dropped")
	}
}

func TestCollapsedWidgets(t *testing.T) {
	c := widgetConfig()
	c.Widgets[0].Rows = []Row{
		{ID: "a", Type: "cpu", Load: true, Collapsed: true}, {ID: "b", Type: "mem", Collapsed: true}, {ID: "c", Type: "uptime", Collapsed: true},
		{ID: "d", Type: "swap", Collapsed: true}, {ID: "e", Type: "load"},
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("four collapsed values are fine: %v", err)
	}
	c.Widgets[0].Rows[4].Collapsed = true
	if err := c.Validate(); err == nil {
		t.Error("a collapsed widget shows at most four values")
	}
	c = widgetConfig()
	c.Settings.WidgetCollapseBelow = 1500
	if err := c.Validate(); err != nil {
		t.Errorf("a collapse width should validate: %v", err)
	}
	c.Settings.WidgetCollapseBelow = -1
	if err := c.Validate(); err == nil {
		t.Error("a negative collapse width must be refused")
	}
}

func TestSettingsStayFlatInTheJSON(t *testing.T) {
	// Appearance is embedded, so a saved config keeps its old shape and old
	// files keep loading.
	raw, err := json.Marshal(Default())
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Settings map[string]any `json:"settings"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"theme", "density", "layout", "checkInterval", "cardOpacity"} {
		if _, ok := m.Settings[key]; !ok {
			t.Errorf("settings.%s is missing: %v", key, m.Settings)
		}
	}
	if _, nested := m.Settings["Appearance"]; nested {
		t.Error("appearance must not be nested")
	}
}

func themedConfig() Config {
	c := widgetConfig()
	three := 3
	c.Themes = []Theme{{
		ID: "t1", Name: "Night",
		Appearance: Appearance{Theme: "dark", Density: "compact", Layout: "grid", AccentColor: "#39ff14", BackgroundImage: "bg-1.jpg", WidgetColor: "#20202080"},
		Widgets:    map[string]WidgetStyle{"w1": {Color: "#101010", BorderWidth: &three}},
	}}
	c.ActiveTheme = "t1"
	c.normalize()
	return c
}

func TestThemesValidate(t *testing.T) {
	if err := themedConfig().Validate(); err != nil {
		t.Fatalf("a good theme should validate: %v", err)
	}
	for name, mutate := range map[string]func(c *Config){
		"no name":        func(c *Config) { c.Themes[0].Name = "" },
		"bad id":         func(c *Config) { c.Themes[0].ID = "../x" },
		"duplicate id":   func(c *Config) { c.Themes = append(c.Themes, c.Themes[0]) },
		"bad color":      func(c *Config) { c.Themes[0].Appearance.AccentColor = "green" },
		"bad widget col": func(c *Config) { c.Themes[0].Appearance.WidgetColor = "nope" },
		"bad image":      func(c *Config) { c.Themes[0].Appearance.BackgroundImage = "../../etc/passwd" },
		"bad mode":       func(c *Config) { c.Themes[0].Appearance.Theme = "sepia" },
		"bad style":      func(c *Config) { c.Themes[0].Widgets["w1"] = WidgetStyle{TextColor: "x"} },
		"too many": func(c *Config) {
			for i := 0; i < maxThemes; i++ {
				c.Themes = append(c.Themes, Theme{ID: "x" + strings.Repeat("a", i+1), Name: "n", Appearance: c.Themes[0].Appearance})
			}
		},
	} {
		c := themedConfig()
		mutate(&c)
		if err := c.Validate(); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestThemeAppearanceGetsDefaults(t *testing.T) {
	c := Default()
	c.Themes = []Theme{{ID: "t", Name: "Bare", Appearance: Appearance{AccentColor: "#ff0000"}}}
	c.normalize()
	a := c.Themes[0].Appearance
	if a.Theme != "auto" || a.Layout != "list" || a.CardOpacity != 40 || a.WidgetOpacity != 100 {
		t.Errorf("defaults not filled in: %+v", a)
	}
	if err := c.Validate(); err != nil {
		t.Errorf("a bare theme should validate after normalizing: %v", err)
	}
}

func TestWidgetOpacityIsOptionalAndUpgraded(t *testing.T) {
	old := `{"title":"T","settings":{"theme":"auto","density":"comfortable","checkInterval":60},
	"groups":[],"widgets":[
	 {"id":"a","name":"A","kind":"glances","url":"http://h:61208","rows":[],"refresh":5,"width":300,"height":0,"opacity":100},
	 {"id":"b","name":"B","kind":"glances","url":"http://h:61208","rows":[],"refresh":5,"width":300,"height":0,"opacity":60}]}`
	c, err := UnmarshalConfig([]byte(old))
	if err != nil {
		t.Fatal(err)
	}
	if c.Widgets[0].Opacity != nil {
		t.Errorf("an opacity of 100 saved before themes was only the default, got %v", *c.Widgets[0].Opacity)
	}
	if c.Widgets[1].Opacity == nil || *c.Widgets[1].Opacity != 60 {
		t.Errorf("an explicit opacity must be kept: %v", c.Widgets[1].Opacity)
	}

	// Once the file has a "themes" key, an explicit 100 is a real choice.
	c.normalize()
	hundred := 100
	c.Widgets[1].Opacity = &hundred
	raw, _ := json.Marshal(c)
	again, err := UnmarshalConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	if again.Widgets[1].Opacity == nil || *again.Widgets[1].Opacity != 100 {
		t.Errorf("an explicit 100 was lost: %v", again.Widgets[1].Opacity)
	}
}
