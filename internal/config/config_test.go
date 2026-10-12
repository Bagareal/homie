package config

import (
	"encoding/json"
	"os"
	"path/filepath"
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
