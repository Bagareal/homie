// Package config manages the dashboard configuration (groups, services, settings).
package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sync"
)

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}([0-9a-fA-F]{2})?$`)

// safeFilename matches the names Homie itself generates for uploaded
// background images (e.g. "bg-1739203812123456789.jpg"). Validated here too
// in case a config.json was imported or hand-edited.
var safeFilename = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,120}$`)

type Settings struct {
	Theme           string `json:"theme"`         // auto | light | dark
	Density         string `json:"density"`       // compact | comfortable
	CheckInterval   int    `json:"checkInterval"` // seconds
	OpenInNewTab    bool   `json:"openInNewTab"`
	BackgroundColor string `json:"backgroundColor,omitempty"` // #rrggbb or #rrggbbaa; empty = theme default
	TileColor       string `json:"tileColor,omitempty"`       // #rrggbb or #rrggbbaa; empty = theme default
	AccentColor     string `json:"accentColor,omitempty"`     // #rrggbb or #rrggbbaa; empty = theme default
	// BackgroundImage is a filename inside <data>/backgrounds, served at
	// GET /backgrounds/{name}; empty = no image (plain BackgroundColor).
	BackgroundImage string `json:"backgroundImage,omitempty"`
	// BackgroundOpacity is 1-100; 0 (and thus omitted) normalizes to 100
	// (fully opaque), matching every image uploaded before this setting
	// existed. Below the image, BackgroundColor always stays full opacity.
	BackgroundOpacity int `json:"backgroundOpacity,omitempty"`
	// Layout is "list" (groups stacked in one column) or "grid" (groups as
	// cards side by side).
	Layout string `json:"layout"`
	// BarOpacity is 1-100 for the top bar's background; 0 (omitted)
	// normalizes to 100 (solid).
	BarOpacity int `json:"barOpacity,omitempty"`
	// CardColor (#rrggbb[aa]; empty = theme default) and CardOpacity (1-100;
	// 0 normalizes to the default) style the group cards of the grid layout.
	CardColor string `json:"cardColor,omitempty"`
	// HoverColor is a tile's color under the pointer; PanelColor is the
	// background of panels (Settings sections, dialogs). Both #rrggbb[aa], empty =
	// theme default.
	HoverColor string `json:"hoverColor,omitempty"`
	PanelColor string `json:"panelColor,omitempty"`
	// Tile text (normal and under the pointer), border colors, and the glow
	// shown around a hovered tile. All #rrggbb[aa], empty = theme default.
	TileTextColor      string `json:"tileTextColor,omitempty"`
	TileTextHoverColor string `json:"tileTextHoverColor,omitempty"`
	TileBorderColor    string `json:"tileBorderColor,omitempty"`
	GroupBorderColor   string `json:"groupBorderColor,omitempty"`
	GlowColor          string `json:"glowColor,omitempty"`
	// Border widths in px (0-8); nil = the default of 1.
	TileBorderWidth  *int `json:"tileBorderWidth,omitempty"`
	GroupBorderWidth *int `json:"groupBorderWidth,omitempty"`
	CardOpacity      int  `json:"cardOpacity,omitempty"`
}

type Item struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	URL         string `json:"url"`
	Icon        string `json:"icon"`
	Description string `json:"description"`
	Check       bool   `json:"check"`
}

type Group struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Items []Item `json:"items"`
}

// Widget is a standalone block that shows live data from a monitored host.
// It sits beside the groups, not inside one.
type Widget struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Kind is the data source: "glances" or "truenas".
	Kind     string `json:"kind"`
	URL      string `json:"url"`
	Username string `json:"username,omitempty"`
	// Password is write-only: it is stored here but never sent to the browser
	// (see Redacted). An empty value on save keeps the stored one.
	Password string `json:"password,omitempty"`
	// HasPassword is output-only, set by Redacted so the editor can tell a
	// password exists. Whatever a client sends is discarded.
	HasPassword bool `json:"hasPassword,omitempty"`
	// ClearPassword is input-only: it asks Set to forget the stored password.
	ClearPassword bool `json:"clearPassword,omitempty"`
	// Rows are the values shown, top to bottom.
	Rows []Row `json:"rows"`
	// Refresh is how often the data is re-read, in seconds (2-300).
	Refresh int `json:"refresh"`
	// Width in px (120-1200); Height in px (0 = fit the content, up to 1000).
	Width  int `json:"width"`
	Height int `json:"height"`
	// Appearance. Colors are #rrggbb[aa]; empty = theme default.
	Color       string `json:"color,omitempty"`
	TextColor   string `json:"textColor,omitempty"`
	BorderColor string `json:"borderColor,omitempty"`
	BarColor    string `json:"barColor,omitempty"`
	// Opacity (1-100) of the background; BorderWidth in px (0-8, nil = 1).
	Opacity     int  `json:"opacity"`
	BorderWidth *int `json:"borderWidth,omitempty"`

	// Read only to upgrade widgets saved by v1.10.x (see migrateWidget); they
	// are emptied on load and never written again.
	LegacyMetrics    []string `json:"metrics,omitempty"`
	LegacyMounts     []string `json:"mounts,omitempty"`
	LegacyInterfaces []string `json:"interfaces,omitempty"`
	LegacySensor     string   `json:"sensor,omitempty"`
	LegacyPlacement  string   `json:"placement,omitempty"`
}

// Row is one line of a widget.
type Row struct {
	ID string `json:"id"`
	// Type is one of RowTypes[widget kind].
	Type string `json:"type"`
	// Key picks what the row is about: a mount point (disk), a sensor label
	// (temp) or an interface name (network). Empty = all disks, the CPU
	// package sensor, or the sum of the physical network cards.
	Key string `json:"key,omitempty"`
	// Name replaces the default label.
	Name string `json:"name,omitempty"`
	// Load and Temp, for a cpu row: show the usage and/or a temperature next
	// to each other. The bar follows the load, or the temperature when alone.
	Load bool `json:"load,omitempty"`
	Temp bool `json:"temp,omitempty"`
}

const (
	maxWidgets    = 20
	maxWidgetRows = 30
)

// RowTypes lists the kinds of row each kind of widget can have.
var RowTypes = map[string][]string{
	"glances": {"cpu", "mem", "swap", "load", "uptime", "temp", "disk", "network"},
	"truenas": {"cpu", "mem", "load", "uptime", "version", "pool", "drive", "alerts"},
}

type Config struct {
	Title    string   `json:"title"`
	Settings Settings `json:"settings"`
	Groups   []Group  `json:"groups"`
	Widgets  []Widget `json:"widgets"`
	// Order is the sequence of groups and widgets on the dashboard, by id.
	Order []string `json:"order"`
}

// Redacted returns a copy that is safe to send to the browser: widget
// passwords are replaced by the HasPassword flag.
func (c Config) Redacted() Config {
	c.Widgets = append(make([]Widget, 0, len(c.Widgets)), c.Widgets...)
	for i := range c.Widgets {
		w := &c.Widgets[i]
		w.HasPassword = w.Password != ""
		w.Password = ""
		w.ClearPassword = false
	}
	return c
}

func Default() Config {
	return Config{
		Title: "Homelab",
		Settings: Settings{
			Theme:             "auto",
			Density:           "comfortable",
			CheckInterval:     60,
			OpenInNewTab:      true,
			BackgroundOpacity: 100,
			Layout:            "list",
			BarOpacity:        100,
			CardOpacity:       40,
		},
		Groups:  []Group{},
		Widgets: []Widget{},
		Order:   []string{},
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
	if c.Settings.BackgroundOpacity == 0 {
		c.Settings.BackgroundOpacity = d.Settings.BackgroundOpacity
	}
	if c.Settings.CardOpacity == 0 {
		c.Settings.CardOpacity = d.Settings.CardOpacity
	}
	if c.Settings.Layout == "" {
		c.Settings.Layout = d.Settings.Layout
	}
	if c.Settings.BarOpacity == 0 {
		c.Settings.BarOpacity = d.Settings.BarOpacity
	}
	if c.Groups == nil {
		c.Groups = []Group{}
	}
	for i := range c.Groups {
		if c.Groups[i].Items == nil {
			c.Groups[i].Items = []Item{}
		}
	}
	// A copy, so editing a widget never touches a slice shared with Get().
	c.Widgets = append(make([]Widget, 0, len(c.Widgets)), c.Widgets...)
	for i := range c.Widgets {
		w := &c.Widgets[i]
		if w.Kind == "" {
			w.Kind = "glances"
		}
		if w.Refresh == 0 {
			w.Refresh = 5
		}
		if w.Width == 0 {
			w.Width = 300
		}
		if w.Opacity == 0 {
			w.Opacity = 100
		}
		w.HasPassword = false
		migrateWidget(w)
		if w.Rows == nil {
			w.Rows = []Row{}
		}
		for j := range w.Rows {
			if w.Rows[j].ID == "" {
				w.Rows[j].ID = newID()
			}
		}
	}
	c.normalizeOrder()
}

// normalizeOrder makes Order list every group and widget exactly once. A
// config without one (saved before widgets could sit anywhere) gets the old
// arrangement: top widgets, the groups, bottom widgets.
func (c *Config) normalizeOrder() {
	var groups, widgets, top, bottom []string
	for _, g := range c.Groups {
		groups = append(groups, g.ID)
	}
	for _, w := range c.Widgets {
		widgets = append(widgets, w.ID)
		if w.LegacyPlacement == "bottom" {
			bottom = append(bottom, w.ID)
		} else {
			top = append(top, w.ID)
		}
	}
	var base []string
	if len(c.Order) == 0 {
		base = append(append(top, groups...), bottom...)
	} else {
		base = append(append(append([]string(nil), c.Order...), groups...), widgets...)
	}
	known := map[string]bool{}
	for _, id := range groups {
		known[id] = true
	}
	for _, id := range widgets {
		known[id] = true
	}
	c.Order = []string{}
	placed := map[string]bool{}
	for _, id := range base {
		if known[id] && !placed[id] {
			placed[id] = true
			c.Order = append(c.Order, id)
		}
	}
	for i := range c.Widgets {
		c.Widgets[i].LegacyPlacement = ""
	}
}

// migrateWidget turns the metric list of a v1.10.x widget into rows, in the
// same order, and forgets the old fields.
func migrateWidget(w *Widget) {
	if len(w.Rows) == 0 {
		for _, m := range w.LegacyMetrics {
			switch m {
			case "cpu":
				w.Rows = append(w.Rows, Row{Type: "cpu", Load: true})
			case "temp":
				w.Rows = append(w.Rows, Row{Type: "temp", Key: w.LegacySensor})
			case "disks":
				for _, mount := range w.LegacyMounts {
					w.Rows = append(w.Rows, Row{Type: "disk", Key: mount})
				}
				if len(w.LegacyMounts) == 0 {
					w.Rows = append(w.Rows, Row{Type: "disk"})
				}
			case "network":
				for _, name := range w.LegacyInterfaces {
					w.Rows = append(w.Rows, Row{Type: "network", Key: name})
				}
				if len(w.LegacyInterfaces) == 0 {
					w.Rows = append(w.Rows, Row{Type: "network"})
				}
			default:
				w.Rows = append(w.Rows, Row{Type: m})
			}
		}
	}
	w.LegacyMetrics, w.LegacyMounts, w.LegacyInterfaces, w.LegacySensor = nil, nil, nil, ""
}

func newID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
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
	switch c.Settings.Layout {
	case "", "list", "grid": // "" = unset, filled in by normalize
	default:
		return fmt.Errorf("invalid layout: %q", c.Settings.Layout)
	}
	for name, w := range map[string]*int{"tile": c.Settings.TileBorderWidth, "group": c.Settings.GroupBorderWidth} {
		if w != nil && (*w < 0 || *w > 8) {
			return fmt.Errorf("%s border width must be between 0 and 8", name)
		}
	}
	if o := c.Settings.CardOpacity; o < 0 || o > 100 {
		return errors.New("card opacity must be between 0 and 100")
	}
	if o := c.Settings.BarOpacity; o < 0 || o > 100 {
		return errors.New("top bar opacity must be between 0 and 100")
	}
	if c.Settings.CheckInterval < 10 || c.Settings.CheckInterval > 3600 {
		return errors.New("the check interval must be between 10 and 3600 seconds")
	}
	for name, v := range map[string]string{
		"background":      c.Settings.BackgroundColor,
		"tile":            c.Settings.TileColor,
		"accent":          c.Settings.AccentColor,
		"card":            c.Settings.CardColor,
		"hover":           c.Settings.HoverColor,
		"panel":           c.Settings.PanelColor,
		"tile text":       c.Settings.TileTextColor,
		"tile text hover": c.Settings.TileTextHoverColor,
		"tile border":     c.Settings.TileBorderColor,
		"group border":    c.Settings.GroupBorderColor,
		"glow":            c.Settings.GlowColor,
	} {
		if v != "" && !hexColor.MatchString(v) {
			return fmt.Errorf("invalid %s color: must be in #rrggbb or #rrggbbaa format", name)
		}
	}
	if img := c.Settings.BackgroundImage; img != "" && !safeFilename.MatchString(img) {
		return errors.New("invalid background image filename")
	}
	if o := c.Settings.BackgroundOpacity; o < 0 || o > 100 {
		return errors.New("background opacity must be between 0 and 100")
	}

	seen := map[string]bool{}
	for _, g := range c.Groups {
		if g.ID == "" || seen[g.ID] {
			return fmt.Errorf("group %q has a missing or duplicate id", g.Name)
		}
		seen[g.ID] = true
		if g.Name == "" || len(g.Name) > 60 {
			return errors.New("every group needs a name (60 characters max)")
		}
		for _, it := range g.Items {
			if it.ID == "" || seen[it.ID] {
				return fmt.Errorf("service %q has a missing or duplicate id", it.Name)
			}
			seen[it.ID] = true
			if it.Name == "" || len(it.Name) > 80 {
				return fmt.Errorf("the service in group %q needs a name (80 characters max)", g.Name)
			}
			if len(it.Description) > 200 || len(it.Icon) > 500 {
				return fmt.Errorf("description or icon of %q is too long", it.Name)
			}
			u, err := url.Parse(it.URL)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				return fmt.Errorf("invalid address for %q: must start with http:// or https://", it.Name)
			}
		}
	}
	return c.validateWidgets(seen)
}

func (c Config) validateWidgets(seen map[string]bool) error {
	if len(c.Widgets) > maxWidgets {
		return fmt.Errorf("too many widgets (%d max)", maxWidgets)
	}
	for _, w := range c.Widgets {
		if w.ID == "" || seen[w.ID] {
			return fmt.Errorf("widget %q has a missing or duplicate id", w.Name)
		}
		seen[w.ID] = true
		if w.Name == "" || len(w.Name) > 60 {
			return errors.New("every widget needs a name (60 characters max)")
		}
		if _, ok := RowTypes[w.Kind]; !ok {
			return fmt.Errorf("invalid kind for widget %q: %q", w.Name, w.Kind)
		}
		u, err := url.Parse(w.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || len(w.URL) > 300 {
			return fmt.Errorf("invalid address for widget %q: must start with http:// or https://", w.Name)
		}
		if w.Kind == "truenas" && u.Scheme != "https" {
			// TrueNAS revokes an API key that is sent without TLS.
			return fmt.Errorf("widget %q: a TrueNAS address must start with https://", w.Name)
		}
		if len(w.Username) > 100 || len(w.Password) > 200 {
			return fmt.Errorf("username or password of widget %q is too long", w.Name)
		}
		if err := validateRows(w); err != nil {
			return err
		}
		if w.Refresh < 2 || w.Refresh > 300 {
			return fmt.Errorf("widget %q: the refresh interval must be between 2 and 300 seconds", w.Name)
		}
		if w.Width < 120 || w.Width > 1200 {
			return fmt.Errorf("widget %q: the width must be between 120 and 1200 px", w.Name)
		}
		if w.Height < 0 || w.Height > 1000 {
			return fmt.Errorf("widget %q: the height must be between 0 and 1000 px", w.Name)
		}
		if w.Opacity < 1 || w.Opacity > 100 {
			return fmt.Errorf("widget %q: the opacity must be between 1 and 100", w.Name)
		}
		if w.BorderWidth != nil && (*w.BorderWidth < 0 || *w.BorderWidth > 8) {
			return fmt.Errorf("widget %q: the border width must be between 0 and 8", w.Name)
		}
		for name, v := range map[string]string{"background": w.Color, "text": w.TextColor, "border": w.BorderColor, "bar": w.BarColor} {
			if v != "" && !hexColor.MatchString(v) {
				return fmt.Errorf("widget %q: invalid %s color, must be in #rrggbb or #rrggbbaa format", w.Name, name)
			}
		}
	}
	return nil
}

func validateRows(w Widget) error {
	if len(w.Rows) > maxWidgetRows {
		return fmt.Errorf("widget %q has too many rows (%d max)", w.Name, maxWidgetRows)
	}
	ids := map[string]bool{}
	for _, r := range w.Rows {
		if r.ID == "" || len(r.ID) > 40 || ids[r.ID] {
			return fmt.Errorf("widget %q has a row with a missing or duplicate id", w.Name)
		}
		ids[r.ID] = true
		if !slices.Contains(RowTypes[w.Kind], r.Type) {
			return fmt.Errorf("widget %q has a row of unsupported type %q", w.Name, r.Type)
		}
		if len(r.Key) > 200 || len(r.Name) > 60 {
			return fmt.Errorf("widget %q has a row whose name or key is too long", w.Name)
		}
		if r.Type == "cpu" && !r.Load && !r.Temp {
			return fmt.Errorf("widget %q: a CPU row must show the load, the temperature, or both", w.Name)
		}
	}
	return nil
}

// legacyBlock mirrors the brief v0.6.x shape ({"blocks": [...]}, a group or
// a single floating widget item per entry) so an export taken during that
// window still imports instead of being rejected. Any widget data on an
// item is intentionally dropped: Homie no longer renders live-data tiles.
type legacyBlock struct {
	Type  string `json:"type"`
	Group *struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Items []Item `json:"items"`
	} `json:"group"`
	Item *Item `json:"item"`
}

// UnmarshalConfig parses raw config JSON into the current shape,
// transparently upgrading two earlier shapes:
//   - the original {"groups": [...]} shape, including when its items carry
//     a now-removed "widget"/"color"/"textColor" field (json.Unmarshal
//     ignores unknown fields, so this needs no special handling below);
//   - the brief v0.6.x {"blocks": [...]} shape (groups + floating widget
//     items). Floating items are preserved as plain links (their widget
//     data dropped) inside a synthetic "Other" group, rather than being
//     silently discarded.
func UnmarshalConfig(raw []byte) (Config, error) {
	var c Config
	if err := json.Unmarshal(raw, &c); err != nil {
		return Config{}, err
	}
	if len(c.Groups) > 0 {
		return c, nil
	}
	var legacy struct {
		Blocks []legacyBlock `json:"blocks"`
	}
	if err := json.Unmarshal(raw, &legacy); err != nil {
		return Config{}, err
	}
	var other []Item
	for _, b := range legacy.Blocks {
		switch b.Type {
		case "group":
			if b.Group == nil {
				continue
			}
			c.Groups = append(c.Groups, Group{ID: b.Group.ID, Name: b.Group.Name, Items: b.Group.Items})
		case "item":
			if b.Item == nil {
				continue
			}
			other = append(other, *b.Item)
		}
	}
	if len(other) > 0 {
		c.Groups = append(c.Groups, Group{ID: "other", Name: "Other", Items: other})
	}
	return c, nil
}

// Store keeps the configuration in memory and on disk (config.json).
type Store struct {
	path string
	mu   sync.RWMutex
	cfg  Config
}

func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	s := &Store{path: filepath.Join(dir, "config.json")}
	raw, err := os.ReadFile(s.path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		s.cfg = Default()
		return s, s.write(s.cfg)
	case err != nil:
		return nil, err
	}
	c, err := UnmarshalConfig(raw)
	if err != nil {
		return nil, fmt.Errorf("%s is not valid JSON: %w", s.path, err)
	}
	c.normalize()
	s.cfg = c
	return s, nil
}

// Get returns the current configuration. Treat the value as read-only.
func (s *Store) Get() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

func (s *Store) Set(c Config) error {
	c.normalize()
	if err := c.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c.keepPasswords(s.cfg)
	if err := s.write(c); err != nil {
		return err
	}
	s.cfg = c
	return nil
}

// keepPasswords carries stored widget passwords over to a configuration that
// came from the browser, which never sees them: a widget saved with an empty
// password keeps the one it had, unless it asked to clear it. The stored
// password is only carried over while the address and username are unchanged:
// otherwise anyone who can open Homie could aim a widget at a server of their
// own and collect the credentials it sends.
func (c *Config) keepPasswords(old Config) {
	for i := range c.Widgets {
		w := &c.Widgets[i]
		if w.Password == "" && !w.ClearPassword {
			for _, o := range old.Widgets {
				if o.ID == w.ID && o.URL == w.URL && o.Username == w.Username {
					w.Password = o.Password
					break
				}
			}
		}
		if w.ClearPassword {
			w.Password = ""
		}
		w.ClearPassword = false
	}
}

// write saves atomically: temp file, then rename.
func (s *Store) write(c Config) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".config-*.json")
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
