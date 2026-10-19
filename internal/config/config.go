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
	"strings"
	"sync"

	"github.com/Bagareal/homie/internal/homeassistant"
)

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}([0-9a-fA-F]{2})?$`)

// safeFilename matches the names Homie itself generates for uploaded
// background images (e.g. "bg-1739203812123456789.jpg"). Validated here too
// in case a config.json was imported or hand-edited.
var safeFilename = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,120}$`)

// Settings are the options of the whole dashboard. The visual ones live in
// Appearance (embedded, so they stay flat in the JSON) because a theme saves
// exactly those.
type Settings struct {
	CheckInterval int  `json:"checkInterval"` // seconds
	OpenInNewTab  bool `json:"openInNewTab"`
	// WidgetCollapseBelow collapses the widgets in a window narrower than this
	// many pixels (0 = never). What each device does by hand is kept in its browser.
	WidgetCollapseBelow int `json:"widgetCollapseBelow,omitempty"`
	// NoUpdateCheck turns off asking GitHub whether a new version exists.
	NoUpdateCheck bool `json:"noUpdateCheck,omitempty"`
	Appearance
}

// Appearance is everything that decides how the dashboard looks: what a theme
// stores and applies.
type Appearance struct {
	Theme           string `json:"theme"`                     // auto | light | dark
	Density         string `json:"density"`                   // compact | comfortable
	BackgroundColor string `json:"backgroundColor,omitempty"` // #rrggbb or #rrggbbaa; empty = theme default
	TileColor       string `json:"tileColor,omitempty"`       // #rrggbb or #rrggbbaa; empty = theme default
	AccentColor     string `json:"accentColor,omitempty"`     // #rrggbb or #rrggbbaa; empty = theme default
	// BackgroundImage is a filename inside <data>/backgrounds, served at
	// GET /backgrounds/{name}; empty = no image (plain BackgroundColor). A file
	// stays on disk while the settings or any saved theme uses it.
	BackgroundImage string `json:"backgroundImage,omitempty"`
	// BackgroundOpacity is 1-100; 0 (and thus omitted) normalizes to 100
	// (fully opaque), matching every image uploaded before this setting
	// existed. Below the image, BackgroundColor always stays full opacity.
	BackgroundOpacity int `json:"backgroundOpacity,omitempty"`
	// Layout is always "grid" (groups as cards side by side). The one-column "list"
	// layout was removed: an old file that says "list" is read as "grid".
	Layout string `json:"layout"`
	// WidgetLayout says where the widgets go next to the groups: "masonry" (mixed
	// with the groups, each block in the shortest column), "column" (one column of
	// their own, on the WidgetSide) or "strip" (compact, in a row above the groups).
	WidgetLayout string `json:"widgetLayout,omitempty"`
	// WidgetSide is "left" (the default) or "right": the side of the widget column.
	WidgetSide string `json:"widgetSide,omitempty"`
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
	// The look every widget has unless it sets its own (see WidgetStyle). Empty
	// colors follow the tiles; WidgetOpacity 0 normalizes to 100.
	WidgetColor       string `json:"widgetColor,omitempty"`
	WidgetTextColor   string `json:"widgetTextColor,omitempty"`
	WidgetBorderColor string `json:"widgetBorderColor,omitempty"`
	WidgetBarColor    string `json:"widgetBarColor,omitempty"`
	WidgetOpacity     int    `json:"widgetOpacity,omitempty"`
	WidgetBorderWidth *int   `json:"widgetBorderWidth,omitempty"`
}

// WidgetStyle is how one widget looks when it differs from the global widget
// look. Every field is optional: unset = follow Settings.
type WidgetStyle struct {
	Color       string `json:"color,omitempty"`
	TextColor   string `json:"textColor,omitempty"`
	BorderColor string `json:"borderColor,omitempty"`
	BarColor    string `json:"barColor,omitempty"`
	// Opacity (1-100) of the background; BorderWidth in px (0-8).
	Opacity     *int `json:"opacity,omitempty"`
	BorderWidth *int `json:"borderWidth,omitempty"`
}

// Theme is a saved look: the appearance settings plus the style of each widget
// (by widget id).
type Theme struct {
	ID         string                 `json:"id"`
	Name       string                 `json:"name"`
	Appearance Appearance             `json:"appearance"`
	Widgets    map[string]WidgetStyle `json:"widgets,omitempty"`
}

const maxThemes = 30

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
	// Kind is the data source: "glances", "truenas", "proxmox", "pbs" (Proxmox
	// Backup Server), "nut" (a UPS; its URL is nut://host[:port][/ups]) or
	// "homeassistant". For Proxmox VE and Backup Server, Username is the API token id
	// (user@realm!name) and Password its secret; for Home Assistant, Password is a
	// long-lived access token.
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
	// Glances is an optional second source for the same machine: its rows (those
	// with Source "glances") read from a Glances server. Not for a Glances widget.
	Glances *Extra `json:"glances,omitempty"`
	// NUT is another optional source: the UPS of the machine, read from its NUT
	// server (rows with Source "nut"). Not for a UPS widget, which is one already.
	NUT *UPSExtra `json:"nut,omitempty"`
	// Rows are the values shown, top to bottom.
	Rows []Row `json:"rows"`
	// Refresh is how often the data is re-read, in seconds (2-300).
	Refresh int `json:"refresh"`
	// Size, counted in groups: Cols is the width (1-3 group columns) and
	// HeightUnits the height (1-3 rows of tiles). With Precise the height is Height
	// in px instead (0 = fit the content, up to 1000). Precise is a pointer so a
	// widget saved before these existed (no value) keeps its pixel height.
	Cols        int   `json:"cols"`
	HeightUnits int   `json:"heightUnits"`
	Precise     *bool `json:"precise,omitempty"`
	Height      int   `json:"height"`
	// Appearance overrides (flat in the JSON); unset = the global widget look.
	WidgetStyle

	// Read only to upgrade widgets saved by v1.10.x (see migrateWidget); they
	// are emptied on load and never written again.
	LegacyMetrics    []string `json:"metrics,omitempty"`
	LegacyMounts     []string `json:"mounts,omitempty"`
	LegacyInterfaces []string `json:"interfaces,omitempty"`
	LegacySensor     string   `json:"sensor,omitempty"`
	LegacyPlacement  string   `json:"placement,omitempty"`
}

// Extra is an additional source of a widget. Like the main one, its password
// is write-only.
type Extra struct {
	URL      string `json:"url"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	// HasPassword is output-only; ClearPassword is input-only (see Widget).
	HasPassword   bool `json:"hasPassword,omitempty"`
	ClearPassword bool `json:"clearPassword,omitempty"`
	// Nodes gives a separate address per node of a Proxmox cluster, which has one
	// Glances per machine. The same login is used for all of them.
	Nodes map[string]string `json:"nodes,omitempty"`
}

// UPSExtra is the NUT server of a widget that is about something else. Reading
// a NUT server needs no login, so there is nothing secret in it.
type UPSExtra struct {
	URL string `json:"url"` // nut://host[:port][/ups]
}

// urls lists every address this source sends its credentials to.
func (e *Extra) urls() []string {
	if e == nil {
		return nil
	}
	var out []string
	if e.URL != "" {
		out = append(out, e.URL)
	}
	for _, u := range e.Nodes {
		out = append(out, u)
	}
	return out
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
	// Drive, for a TrueNAS pool row with Temp: the drive whose temperature is
	// shown next to the capacity (empty = the hottest drive of the pool).
	Drive string `json:"drive,omitempty"`
	// Collapsed marks a value shown when the widget is collapsed (up to maxCollapsedRows).
	Collapsed bool `json:"collapsed,omitempty"`
	// Source says where the value comes from: empty is the widget's own source,
	// "glances" its extra Glances source or "nut" its extra UPS source (then Type
	// is a row type of that kind).
	Source string `json:"source,omitempty"`
	// Node, for a row of the extra Glances source of a Proxmox cluster: which
	// node's Glances to read (empty = the widget's general Glances address).
	Node string `json:"node,omitempty"`
	// Role, for a client row of a UPS: "primary" or "secondary" (upsd does not
	// report it, so it is what the user says the machine is).
	Role string `json:"role,omitempty"`
	// Control, for a Home Assistant row: the row has a switch (or a button) and
	// Homie may send commands for this entity. Only the entities that were given
	// one can ever be switched, and only the kinds in homeassistant.Actions.
	Control bool `json:"control,omitempty"`
}

const (
	maxWidgets    = 20
	maxWidgetRows = 30
	// A collapsed widget shows at most this many values.
	maxCollapsedRows = 4
)

// RowTypes lists the kinds of row each kind of widget can have.
var RowTypes = map[string][]string{
	"glances": {"cpu", "mem", "swap", "load", "uptime", "temp", "disk", "network"},
	"truenas": {"cpu", "mem", "load", "uptime", "version", "pool", "drive", "alerts", "apps"},
	"proxmox": {"cpu", "mem", "uptime", "version", "nodes", "lxc", "vm", "guest", "storage", "backup", "quorum", "tasks"},
	"pbs":     {"cpu", "mem", "uptime", "version", "datastore", "group", "backup", "tasks"},
	"nut":     {"status", "charge", "runtime", "upsload", "power", "voltage", "model", "var", "clients", "client"},
	// A Home Assistant row is one entity (its id is the row's Key).
	"homeassistant": {"entity"},
}

type Config struct {
	Title    string   `json:"title"`
	Settings Settings `json:"settings"`
	Groups   []Group  `json:"groups"`
	Widgets  []Widget `json:"widgets"`
	// Order is the sequence of groups and widgets on the dashboard, by id.
	Order []string `json:"order"`
	// Themes are the saved looks. ActiveTheme is the id of the one last
	// applied (a built-in one starts with "builtin:"); it only drives the
	// "modified" hint in the interface.
	Themes      []Theme `json:"themes"`
	ActiveTheme string  `json:"activeTheme,omitempty"`
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
		if w.Glances != nil {
			g := *w.Glances // a copy: the stored one keeps its password
			g.HasPassword = g.Password != ""
			g.Password = ""
			g.ClearPassword = false
			w.Glances = &g
		}
	}
	return c
}

func Default() Config {
	return Config{
		Title: "Homelab",
		Settings: Settings{
			CheckInterval: 60,
			OpenInNewTab:  true,
			Appearance: Appearance{
				Theme:             "auto",
				Density:           "comfortable",
				BackgroundOpacity: 100,
				Layout:            "grid",
				WidgetLayout:      "masonry",
				BarOpacity:        100,
				CardOpacity:       40,
				WidgetOpacity:     100,
			},
		},
		Groups:  []Group{},
		Widgets: []Widget{},
		Order:   []string{},
		Themes:  []Theme{},
	}
}

// normalize fills in missing values (old files or partial imports).
func (c *Config) normalize() {
	if c.Settings.CheckInterval == 0 {
		c.Settings.CheckInterval = Default().Settings.CheckInterval
	}
	c.Settings.Appearance.normalize()
	if c.Themes == nil {
		c.Themes = []Theme{}
	}
	c.Themes = append(make([]Theme, 0, len(c.Themes)), c.Themes...)
	for i := range c.Themes {
		c.Themes[i].Appearance.normalize()
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
		if w.Cols == 0 {
			w.Cols = 1
		}
		if w.HeightUnits == 0 {
			w.HeightUnits = 1
		}
		if w.Precise == nil {
			legacy := true
			w.Precise = &legacy
		}
		w.HasPassword = false
		if w.NUT != nil {
			w.NUT.URL = strings.TrimSpace(w.NUT.URL)
			if w.NUT.URL == "" {
				w.NUT = nil
			}
		}
		if g := w.Glances; g != nil {
			g.HasPassword = false
			g.URL = strings.TrimSpace(g.URL)
			if g.URL == "" && len(g.Nodes) == 0 {
				w.Glances = nil // nothing to read: the extra source is off
			}
		}
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

// normalize fills in the defaults of an appearance (old files, partial imports).
func (a *Appearance) normalize() {
	d := Default().Settings.Appearance
	if a.Theme == "" {
		a.Theme = d.Theme
	}
	if a.Density == "" {
		a.Density = d.Density
	}
	if a.BackgroundOpacity == 0 {
		a.BackgroundOpacity = d.BackgroundOpacity
	}
	if a.CardOpacity == 0 {
		a.CardOpacity = d.CardOpacity
	}
	if a.Layout == "" || a.Layout == "list" {
		a.Layout = d.Layout
	}
	if a.WidgetLayout == "" {
		a.WidgetLayout = d.WidgetLayout
	}
	if a.BarOpacity == 0 {
		a.BarOpacity = d.BarOpacity
	}
	if a.WidgetOpacity == 0 {
		a.WidgetOpacity = d.WidgetOpacity
	}
}

// validate checks an appearance. what names it in the messages ("" for the
// dashboard's own settings, a theme's name otherwise).
func (a Appearance) validate(what string) error {
	pre := ""
	if what != "" {
		pre = fmt.Sprintf("theme %q: ", what)
	}
	switch a.Theme {
	case "auto", "light", "dark":
	default:
		return fmt.Errorf("%sinvalid theme: %q", pre, a.Theme)
	}
	switch a.Density {
	case "compact", "comfortable":
	default:
		return fmt.Errorf("%sinvalid density: %q", pre, a.Density)
	}
	switch a.Layout {
	case "", "list", "grid": // "" = unset and "list" = removed: normalize makes both "grid"
	default:
		return fmt.Errorf("%sinvalid layout: %q", pre, a.Layout)
	}
	switch a.WidgetLayout {
	case "", "masonry", "column", "strip": // "" = unset, filled in by normalize
	default:
		return fmt.Errorf("%sinvalid widget layout: %q", pre, a.WidgetLayout)
	}
	switch a.WidgetSide {
	case "", "left", "right":
	default:
		return fmt.Errorf("%sinvalid widget side: %q", pre, a.WidgetSide)
	}
	for name, w := range map[string]*int{"tile": a.TileBorderWidth, "group": a.GroupBorderWidth, "widget": a.WidgetBorderWidth} {
		if w != nil && (*w < 0 || *w > 8) {
			return fmt.Errorf("%s%s border width must be between 0 and 8", pre, name)
		}
	}
	for name, o := range map[string]int{"card": a.CardOpacity, "top bar": a.BarOpacity, "background": a.BackgroundOpacity, "widget": a.WidgetOpacity} {
		if o < 0 || o > 100 {
			return fmt.Errorf("%s%s opacity must be between 0 and 100", pre, name)
		}
	}
	for name, v := range map[string]string{
		"background":      a.BackgroundColor,
		"tile":            a.TileColor,
		"accent":          a.AccentColor,
		"card":            a.CardColor,
		"hover":           a.HoverColor,
		"panel":           a.PanelColor,
		"tile text":       a.TileTextColor,
		"tile text hover": a.TileTextHoverColor,
		"tile border":     a.TileBorderColor,
		"group border":    a.GroupBorderColor,
		"glow":            a.GlowColor,
		"widget":          a.WidgetColor,
		"widget text":     a.WidgetTextColor,
		"widget border":   a.WidgetBorderColor,
		"widget bar":      a.WidgetBarColor,
	} {
		if v != "" && !hexColor.MatchString(v) {
			return fmt.Errorf("%sinvalid %s color: must be in #rrggbb or #rrggbbaa format", pre, name)
		}
	}
	if img := a.BackgroundImage; img != "" && !safeFilename.MatchString(img) {
		return fmt.Errorf("%sinvalid background image filename", pre)
	}
	return nil
}

// validate checks one widget style.
func (st WidgetStyle) validate(who string) error {
	for name, v := range map[string]string{"background": st.Color, "text": st.TextColor, "border": st.BorderColor, "bar": st.BarColor} {
		if v != "" && !hexColor.MatchString(v) {
			return fmt.Errorf("%s: invalid %s color, must be in #rrggbb or #rrggbbaa format", who, name)
		}
	}
	if st.Opacity != nil && (*st.Opacity < 1 || *st.Opacity > 100) {
		return fmt.Errorf("%s: the opacity must be between 1 and 100", who)
	}
	if st.BorderWidth != nil && (*st.BorderWidth < 0 || *st.BorderWidth > 8) {
		return fmt.Errorf("%s: the border width must be between 0 and 8", who)
	}
	return nil
}

var themeID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,60}$`)

func (c Config) validateThemes() error {
	if len(c.Themes) > maxThemes {
		return fmt.Errorf("too many themes (%d max)", maxThemes)
	}
	if len(c.ActiveTheme) > 80 {
		return errors.New("invalid active theme")
	}
	ids := map[string]bool{}
	for _, t := range c.Themes {
		if !themeID.MatchString(t.ID) || ids[t.ID] {
			return fmt.Errorf("theme %q has a missing or duplicate id", t.Name)
		}
		ids[t.ID] = true
		if t.Name == "" || len(t.Name) > 60 {
			return errors.New("every theme needs a name (60 characters max)")
		}
		if err := t.Appearance.validate(t.Name); err != nil {
			return err
		}
		if len(t.Widgets) > 2*maxWidgets {
			return fmt.Errorf("theme %q has too many widget styles", t.Name)
		}
		for id, st := range t.Widgets {
			if len(id) > 60 {
				return fmt.Errorf("theme %q has an invalid widget id", t.Name)
			}
			if err := st.validate(fmt.Sprintf("theme %q", t.Name)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c Config) Validate() error {
	if len(c.Title) > 100 {
		return errors.New("title too long (100 characters max)")
	}
	if c.Settings.WidgetCollapseBelow < 0 || c.Settings.WidgetCollapseBelow > 5000 {
		return errors.New("the collapse width must be between 0 and 5000 px")
	}
	if c.Settings.CheckInterval < 10 || c.Settings.CheckInterval > 3600 {
		return errors.New("the check interval must be between 10 and 3600 seconds")
	}
	if err := c.Settings.Appearance.validate(""); err != nil {
		return err
	}
	if err := c.validateThemes(); err != nil {
		return err
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
		if err != nil {
			return fmt.Errorf("invalid address for widget %q", w.Name)
		}
		scheme, want := "http:// or https://", u.Scheme == "http" || u.Scheme == "https"
		if w.Kind == "nut" {
			scheme, want = "nut://", u.Scheme == "nut"
		}
		if !want || u.Host == "" || len(w.URL) > 300 {
			return fmt.Errorf("invalid address for widget %q: must start with %s", w.Name, scheme)
		}
		if w.Kind == "truenas" && u.Scheme != "https" {
			// TrueNAS revokes an API key that is sent without TLS.
			return fmt.Errorf("widget %q: a TrueNAS address must start with https://", w.Name)
		}
		// A Home Assistant access token is a long JWT.
		maxSecret := 200
		if w.Kind == "homeassistant" {
			maxSecret = 2000
		}
		if len(w.Username) > 100 || len(w.Password) > maxSecret {
			return fmt.Errorf("username or password of widget %q is too long", w.Name)
		}
		if (w.Kind == "proxmox" || w.Kind == "pbs") && !(strings.Contains(w.Username, "@") && strings.Contains(w.Username, "!")) {
			return fmt.Errorf("widget %q: the Proxmox API token id looks like user@realm!name", w.Name)
		}
		if err := validateExtra(w); err != nil {
			return err
		}
		if err := validateRows(w); err != nil {
			return err
		}
		if w.Refresh < 2 || w.Refresh > 300 {
			return fmt.Errorf("widget %q: the refresh interval must be between 2 and 300 seconds", w.Name)
		}
		if w.Cols < 1 || w.Cols > 3 {
			return fmt.Errorf("widget %q: the width must be 1, 2 or 3 groups", w.Name)
		}
		if w.HeightUnits < 1 || w.HeightUnits > 3 {
			return fmt.Errorf("widget %q: the height must be 1, 2 or 3 groups", w.Name)
		}
		if w.Height < 0 || w.Height > 1000 {
			return fmt.Errorf("widget %q: the height must be between 0 and 1000 px", w.Name)
		}
		if err := w.WidgetStyle.validate(fmt.Sprintf("widget %q", w.Name)); err != nil {
			return err
		}
	}
	return nil
}

func validateExtra(w Widget) error {
	if n := w.NUT; n != nil {
		if w.Kind == "nut" {
			return fmt.Errorf("widget %q: a UPS widget already reads a NUT server", w.Name)
		}
		u, err := url.Parse(n.URL)
		if err != nil || u.Scheme != "nut" || u.Host == "" || len(n.URL) > 300 {
			return fmt.Errorf("widget %q: the UPS address must start with nut://", w.Name)
		}
	}
	g := w.Glances
	if g == nil {
		return nil
	}
	if w.Kind == "glances" {
		return fmt.Errorf("widget %q: a Glances widget already reads Glances", w.Name)
	}
	if len(g.Username) > 100 || len(g.Password) > 200 || len(g.Nodes) > 50 {
		return fmt.Errorf("widget %q: the Glances login or node list is too long", w.Name)
	}
	check := func(raw string) error {
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || len(raw) > 300 {
			return fmt.Errorf("widget %q: the Glances address %q must start with http:// or https://", w.Name, raw)
		}
		return nil
	}
	if g.URL != "" {
		if err := check(g.URL); err != nil {
			return err
		}
	}
	for node, raw := range g.Nodes {
		if node == "" || len(node) > 100 {
			return fmt.Errorf("widget %q: a Glances node name is missing or too long", w.Name)
		}
		if err := check(raw); err != nil {
			return err
		}
	}
	return nil
}

func validateRows(w Widget) error {
	if len(w.Rows) > maxWidgetRows {
		return fmt.Errorf("widget %q has too many rows (%d max)", w.Name, maxWidgetRows)
	}
	ids := map[string]bool{}
	collapsed := 0
	for _, r := range w.Rows {
		if r.Collapsed {
			collapsed++
		}
		if r.ID == "" || len(r.ID) > 40 || ids[r.ID] {
			return fmt.Errorf("widget %q has a row with a missing or duplicate id", w.Name)
		}
		ids[r.ID] = true
		if collapsed > maxCollapsedRows {
			return fmt.Errorf("widget %q: a collapsed widget shows at most %d values", w.Name, maxCollapsedRows)
		}
		kind := w.Kind
		switch r.Source {
		case "":
		case "glances":
			if w.Glances == nil || w.Kind == "glances" {
				return fmt.Errorf("widget %q has a Glances row but no Glances address", w.Name)
			}
			kind = "glances"
		case "nut":
			if w.NUT == nil || w.Kind == "nut" {
				return fmt.Errorf("widget %q has a UPS row but no UPS address", w.Name)
			}
			kind = "nut"
		default:
			return fmt.Errorf("widget %q has a row with an unknown source %q", w.Name, r.Source)
		}
		if !slices.Contains(RowTypes[kind], r.Type) {
			return fmt.Errorf("widget %q has a row of unsupported type %q", w.Name, r.Type)
		}
		if len(r.Node) > 100 {
			return fmt.Errorf("widget %q has a row whose node name is too long", w.Name)
		}
		if len(r.Drive) > 100 {
			return fmt.Errorf("widget %q has a row whose drive name is too long", w.Name)
		}
		if len(r.Key) > 200 || len(r.Name) > 60 {
			return fmt.Errorf("widget %q has a row whose name or key is too long", w.Name)
		}
		if r.Role != "" && r.Role != "primary" && r.Role != "secondary" {
			return fmt.Errorf("widget %q has a client that is neither primary nor secondary", w.Name)
		}
		if r.Type == "client" && r.Key == "" {
			return fmt.Errorf("widget %q: a client needs its address", w.Name)
		}
		if r.Type == "entity" && !homeassistant.EntityID.MatchString(r.Key) {
			return fmt.Errorf("widget %q: %q is not a Home Assistant entity id (like sensor.power or light.desk)", w.Name, r.Key)
		}
		if r.Control && !(r.Type == "entity" && r.Source == "" && homeassistant.Controllable(r.Key)) {
			return fmt.Errorf("widget %q: %q cannot be controlled from Homie", w.Name, r.Key)
		}
		// A Proxmox node's (or a Backup Server's) CPU row has no temperature: that comes from Glances.
		if r.Type == "cpu" && !r.Load && !r.Temp && !((w.Kind == "proxmox" || w.Kind == "pbs") && r.Source == "") {
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
	c, err := unmarshalConfig(raw)
	if err != nil {
		return Config{}, err
	}
	// Before themes existed every widget was saved with an opacity of 100, which
	// was only the default: let those follow the global widget look instead.
	var keys map[string]json.RawMessage
	if json.Unmarshal(raw, &keys) == nil {
		if _, ok := keys["themes"]; !ok {
			for i := range c.Widgets {
				if o := c.Widgets[i].Opacity; o != nil && (*o == 100 || *o == 0) {
					c.Widgets[i].Opacity = nil
				}
			}
		}
	}
	return c, nil
}

func unmarshalConfig(raw []byte) (Config, error) {
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
		if g := w.Glances; g != nil {
			if g.Password == "" && !g.ClearPassword {
				for _, o := range old.Widgets {
					// Only while every address it would be sent to was already one of
					// the stored ones (the same reasoning as above).
					if o.ID == w.ID && o.Glances != nil && o.Glances.Username == g.Username && subset(g.urls(), o.Glances.urls()) {
						g.Password = o.Glances.Password
						break
					}
				}
			}
			if g.ClearPassword {
				g.Password = ""
			}
			g.ClearPassword = false
		}
	}
}

// subset reports whether every address of a is also in b.
func subset(a, b []string) bool {
	for _, x := range a {
		if !slices.Contains(b, x) {
			return false
		}
	}
	return true
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
