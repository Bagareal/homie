// Package config manages the dashboard configuration (groups, services, settings).
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sync"
)

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// safeFilename matches the names Homie itself generates for uploaded
// background images (e.g. "bg-1739203812123456789.jpg"). Validated here too
// in case a config.json was imported or hand-edited.
var safeFilename = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,120}$`)

type Settings struct {
	Theme           string `json:"theme"`         // auto | light | dark
	Density         string `json:"density"`       // compact | comfortable
	CheckInterval   int    `json:"checkInterval"` // seconds
	OpenInNewTab    bool   `json:"openInNewTab"`
	BackgroundColor string `json:"backgroundColor,omitempty"` // #rrggbb; empty = theme default
	TileColor       string `json:"tileColor,omitempty"`       // #rrggbb; empty = theme default
	AccentColor     string `json:"accentColor,omitempty"`     // #rrggbb; empty = theme default
	// BackgroundImage is a filename inside <data>/backgrounds, served at
	// GET /backgrounds/{name}; empty = no image (plain BackgroundColor).
	BackgroundImage string `json:"backgroundImage,omitempty"`
	// BackgroundOpacity is 1-100; 0 (and thus omitted) normalizes to 100
	// (fully opaque), matching every image uploaded before this setting
	// existed. Below the image, BackgroundColor always stays full opacity.
	BackgroundOpacity int `json:"backgroundOpacity,omitempty"`
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

type Config struct {
	Title    string   `json:"title"`
	Settings Settings `json:"settings"`
	Groups   []Group  `json:"groups"`
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
		},
		Groups: []Group{},
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
	if c.Groups == nil {
		c.Groups = []Group{}
	}
	for i := range c.Groups {
		if c.Groups[i].Items == nil {
			c.Groups[i].Items = []Item{}
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
	for name, v := range map[string]string{
		"background": c.Settings.BackgroundColor,
		"tile":       c.Settings.TileColor,
		"accent":     c.Settings.AccentColor,
	} {
		if v != "" && !hexColor.MatchString(v) {
			return fmt.Errorf("invalid %s color: must be in #rrggbb format", name)
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
	if err := s.write(c); err != nil {
		return err
	}
	s.cfg = c
	return nil
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
