// Package homeassistant reads, and switches, the entities of a Home Assistant
// instance through its REST API with a long-lived access token.
//
// Reading asks for each entity the dashboard shows, one by one, so a large
// installation is never downloaded whole on every refresh. Switching goes
// through a short list of services (see Services): what a widget may do is
// decided by the server from the widget's own configuration, never by the
// request of a browser.
package homeassistant

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// DefaultPort is where Home Assistant listens.
const DefaultPort = "8123"

const (
	timeout     = 8 * time.Second
	maxBody     = 16 << 20
	maxListed   = 5000
	parallelism = 6
)

// EntityID is what an entity id looks like: domain.object_id.
var EntityID = regexp.MustCompile(`^[a-z0-9_]{1,64}\.[a-z0-9_]{1,128}$`)

// Domain is the part of an entity id before the dot.
func Domain(id string) string {
	d, _, _ := strings.Cut(id, ".")
	return d
}

// Service is a call a widget row may make.
type Service struct{ Domain, Name string }

// Actions says, for each domain a row can control, what each action calls. A
// domain that is not here (locks, alarms, covers, garage doors, climate, ...)
// cannot be controlled from Homie, whatever a request says.
var Actions = map[string]map[string]string{
	// Things that are on or off.
	"light":         {"toggle": "toggle", "on": "turn_on", "off": "turn_off"},
	"switch":        {"toggle": "toggle", "on": "turn_on", "off": "turn_off"},
	"input_boolean": {"toggle": "toggle", "on": "turn_on", "off": "turn_off"},
	"fan":           {"toggle": "toggle", "on": "turn_on", "off": "turn_off"},
	"automation":    {"toggle": "toggle", "on": "turn_on", "off": "turn_off"},
	// Things that are run.
	"scene":        {"trigger": "turn_on"},
	"script":       {"trigger": "turn_on"},
	"button":       {"trigger": "press"},
	"input_button": {"trigger": "press"},
}

// Controllable tells whether rows of this entity can be given a switch.
func Controllable(id string) bool {
	_, ok := Actions[Domain(id)]
	return ok
}

// ServiceFor maps an action ("toggle", "on", "off", "trigger") of an entity to
// the Home Assistant service that does it.
func ServiceFor(id, action string) (Service, error) {
	if !EntityID.MatchString(id) {
		return Service{}, errors.New("invalid entity id")
	}
	d := Domain(id)
	acts, ok := Actions[d]
	if !ok {
		return Service{}, fmt.Errorf("%s cannot be controlled from Homie", d)
	}
	name, ok := acts[action]
	if !ok {
		return Service{}, fmt.Errorf("%s cannot be told to %s", d, action)
	}
	return Service{Domain: d, Name: name}, nil
}

// Target is one Home Assistant and the entities to read.
type Target struct {
	URL      string // http(s)://host:8123
	Token    string
	Entities []string
}

// Entity is one reading.
type Entity struct {
	ID    string `json:"id"`
	State string `json:"state"`
	// Name is the friendly name Home Assistant gives it.
	Name  string `json:"name,omitempty"`
	Unit  string `json:"unit,omitempty"`
	Class string `json:"class,omitempty"` // device_class: temperature, door, motion...
	// Brightness of a light, in percent.
	Brightness *int `json:"brightness,omitempty"`
	// Missing: Home Assistant does not know this id (renamed or deleted).
	Missing bool `json:"missing,omitempty"`
}

// Data is what the widget shows.
type Data struct {
	Version  string            `json:"version,omitempty"`
	Location string            `json:"location,omitempty"`
	Entities map[string]Entity `json:"entities"`
}

// Result is what the dashboard receives for one widget.
type Result struct {
	Online    bool              `json:"online"`
	Error     string            `json:"error,omitempty"`
	UpdatedAt time.Time         `json:"updatedAt"`
	Data      Data              `json:"data"`
	Failed    []string          `json:"failed,omitempty"`
	Notes     map[string]string `json:"notes,omitempty"`
}

// Client talks to Home Assistant.
type Client struct{ http *http.Client }

func New() *Client {
	return &Client{http: &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			MaxIdleConnsPerHost: 4,
			IdleConnTimeout:     60 * time.Second,
		},
		// Never follow a redirect: it could carry the token somewhere else.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

var errAuth = errors.New("the token was refused: create a long-lived access token in your Home Assistant profile")

// notFound is a 404 for one entity.
var notFound = errors.New("not found")

func base(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(raw), "/"))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, errors.New("invalid address: use http://host:8123 or https://host")
	}
	return u, nil
}

// do sends one request and decodes a JSON answer into v (when not nil).
func (c *Client) do(ctx context.Context, t Target, method, path string, body any, v any) error {
	u, err := base(t.URL)
	if err != nil {
		return err
	}
	u.Path = strings.TrimRight(u.Path, "/") + path
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(t.Token))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return describe(err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return errAuth
	case resp.StatusCode == http.StatusNotFound:
		return notFound
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		if loc := resp.Header.Get("Location"); loc != "" {
			return fmt.Errorf("the address redirects to %s: use that address instead", loc)
		}
		return fmt.Errorf("the server answered %s", resp.Status)
	case resp.StatusCode >= 400:
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		var e struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(msg, &e) == nil && e.Message != "" {
			return fmt.Errorf("Home Assistant: %s", e.Message)
		}
		return fmt.Errorf("the server answered %s (is this the Home Assistant address, port 8123?)", resp.Status)
	}
	if v == nil {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return errors.New("this does not look like the Home Assistant API (is the port 8123?)")
	}
	return nil
}

func describe(err error) error {
	var ne interface{ Timeout() bool }
	if errors.As(err, &ne) && ne.Timeout() {
		return errors.New("timed out: the server did not answer")
	}
	msg := err.Error()
	if strings.Contains(msg, "x509") || strings.Contains(msg, "certificate") {
		return errors.New("the certificate is not trusted: use a valid one, or the plain http address of Home Assistant on your network")
	}
	if i := strings.LastIndex(msg, ": "); i >= 0 {
		msg = msg[i+2:]
	}
	return fmt.Errorf("cannot reach the server: %s", msg)
}

// Ping checks the address and the token, and returns the version.
func (c *Client) Ping(ctx context.Context, t Target) (version, location string, err error) {
	var cfg struct {
		Version  string `json:"version"`
		Location string `json:"location_name"`
	}
	if err := c.do(ctx, t, http.MethodGet, "/api/config", nil, &cfg); err != nil {
		if errors.Is(err, notFound) {
			return "", "", errors.New("this does not look like Home Assistant (is the port 8123?)")
		}
		return "", "", err
	}
	return cfg.Version, cfg.Location, nil
}

type rawState struct {
	EntityID   string         `json:"entity_id"`
	State      string         `json:"state"`
	Attributes map[string]any `json:"attributes"`
}

func entityOf(s rawState) Entity {
	e := Entity{ID: s.EntityID, State: s.State}
	if v, ok := s.Attributes["friendly_name"].(string); ok {
		e.Name = v
	}
	if v, ok := s.Attributes["unit_of_measurement"].(string); ok {
		e.Unit = v
	}
	if v, ok := s.Attributes["device_class"].(string); ok {
		e.Class = v
	}
	if b, ok := s.Attributes["brightness"].(float64); ok && Domain(s.EntityID) == "light" {
		pct := int(b/255*100 + 0.5)
		e.Brightness = &pct
	}
	return e
}

// Entity reads one entity.
func (c *Client) Entity(ctx context.Context, t Target, id string) (Entity, error) {
	if !EntityID.MatchString(id) {
		return Entity{}, errors.New("invalid entity id")
	}
	var s rawState
	if err := c.do(ctx, t, http.MethodGet, "/api/states/"+id, nil, &s); err != nil {
		if errors.Is(err, notFound) {
			return Entity{ID: id, Missing: true}, nil
		}
		return Entity{}, err
	}
	return entityOf(s), nil
}

// Fetch reads the entities. The system is online when Home Assistant answered at
// least once; an entity it does not know is marked Missing.
func (c *Client) Fetch(ctx context.Context, t Target) Result {
	res := Result{UpdatedAt: time.Now().UTC(), Data: Data{Entities: map[string]Entity{}}}
	ids := dedupe(t.Entities)
	if len(ids) == 0 {
		if _, _, err := c.Ping(ctx, t); err != nil {
			res.Error = err.Error()
			return res
		}
		res.Online = true
		return res
	}
	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		firstErr error
		answered bool
	)
	sem := make(chan struct{}, parallelism)
	for _, id := range ids {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			e, err := c.Entity(ctx, t, id)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				res.Failed = append(res.Failed, id)
				return
			}
			answered = true
			res.Data.Entities[id] = e
		}()
	}
	wg.Wait()
	if !answered {
		res.Error = firstErr.Error()
		return res
	}
	res.Online = true
	sort.Strings(res.Failed)
	if firstErr != nil {
		res.Notes = map[string]string{"entities": firstErr.Error()}
	}
	return res
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, id := range in {
		if EntityID.MatchString(id) && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// List reads every entity there is, for the editor to pick from.
func (c *Client) List(ctx context.Context, t Target) ([]Entity, error) {
	var all []rawState
	if err := c.do(ctx, t, http.MethodGet, "/api/states", nil, &all); err != nil {
		return nil, err
	}
	out := make([]Entity, 0, len(all))
	for _, s := range all {
		if EntityID.MatchString(s.EntityID) {
			out = append(out, entityOf(s))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	if len(out) > maxListed {
		out = out[:maxListed]
	}
	return out, nil
}

// Call runs a service on one entity. The service must come from ServiceFor.
func (c *Client) Call(ctx context.Context, t Target, s Service, id string) error {
	if !EntityID.MatchString(id) || Domain(id) != s.Domain {
		return errors.New("invalid entity id")
	}
	return c.do(ctx, t, http.MethodPost, "/api/services/"+s.Domain+"/"+s.Name, map[string]string{"entity_id": id}, nil)
}
