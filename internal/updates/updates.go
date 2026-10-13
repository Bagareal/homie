// Package updates finds out which versions of Homie have been released, so the
// dashboard can tell the user about a new one and what changed. The server asks
// GitHub (the same place `update` downloads from), not the browser, and keeps
// the answer for a few hours.
package updates

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultURL lists the latest releases of the project.
const DefaultURL = "https://api.github.com/repos/Bagareal/homie/releases?per_page=30"

const (
	okFor      = 6 * time.Hour    // how long a good answer is kept
	failedFor  = 10 * time.Minute // and a failed one, before trying again
	forceEvery = time.Minute      // "check now" at most this often
	maxNotes   = 30000            // bytes of notes kept per release
)

// Release is one published version.
type Release struct {
	Version string `json:"version"` // v1.23.4
	URL     string `json:"url"`
	Date    string `json:"date"` // RFC 3339
	// Notes is the changelog text, shown as plain text.
	Notes string `json:"notes"`
}

// Checker reads the releases. The zero value is not usable; use New.
type Checker struct {
	url  string
	http *http.Client

	mu       sync.Mutex
	releases []Release
	err      string
	checked  time.Time // the last answer, good or not
	tried    time.Time // the last attempt
}

func New(url string) *Checker {
	if url == "" {
		url = DefaultURL
	}
	return &Checker{url: url, http: &http.Client{Timeout: 8 * time.Second}}
}

// Get returns the releases, newest first, when they were last read, and why they
// could not be read (empty if they could). force asks for a fresh reading, within
// reason.
func (c *Checker) Get(ctx context.Context, force bool) ([]Release, time.Time, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	age := time.Since(c.tried)
	switch {
	case c.tried.IsZero():
	case force && age > forceEvery:
	case c.err == "" && age > okFor:
	case c.err != "" && age > failedFor:
	default:
		return c.releases, c.checked, c.err
	}
	c.tried = time.Now()
	rels, err := c.fetch(ctx)
	if err != nil {
		c.err = err.Error()
		// An older good answer stays useful while GitHub is unreachable.
	} else {
		c.releases, c.err = rels, ""
	}
	c.checked = time.Now()
	return c.releases, c.checked, c.err
}

var versionRE = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)

func (c *Checker) fetch(ctx context.Context) ([]Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Homie")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot reach GitHub: %s", short(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub answered %s", resp.Status)
	}
	var raw []struct {
		Tag        string `json:"tag_name"`
		URL        string `json:"html_url"`
		Body       string `json:"body"`
		Published  string `json:"published_at"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&raw); err != nil {
		return nil, fmt.Errorf("unreadable answer from GitHub")
	}
	var out []Release
	for _, r := range raw {
		// Only real releases, with a link that goes to GitHub: the page will open it.
		if r.Draft || r.Prerelease || !versionRE.MatchString(r.Tag) || !strings.HasPrefix(r.URL, "https://github.com/") {
			continue
		}
		notes := r.Body
		if len(notes) > maxNotes {
			notes = notes[:maxNotes]
		}
		out = append(out, Release{Version: r.Tag, URL: r.URL, Date: r.Published, Notes: notes})
	}
	sort.SliceStable(out, func(i, j int) bool { return Compare(out[i].Version, out[j].Version) > 0 })
	return out, nil
}

func short(err error) string {
	msg := err.Error()
	if i := strings.LastIndex(msg, ": "); i >= 0 {
		msg = msg[i+2:]
	}
	return msg
}

// Compare orders two versions like v1.23.4: -1, 0 or 1. Anything that is not a
// version (such as "dev") is older than every version.
func Compare(a, b string) int {
	pa, okA := parts(a)
	pb, okB := parts(b)
	switch {
	case !okA && !okB:
		return 0
	case !okA:
		return -1
	case !okB:
		return 1
	}
	for i := range pa {
		if pa[i] != pb[i] {
			if pa[i] < pb[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

func parts(v string) ([3]int, bool) {
	var p [3]int
	if !versionRE.MatchString(v) {
		return p, false
	}
	for i, s := range strings.Split(v[1:], ".") {
		n, _ := strconv.Atoi(s)
		p[i] = n
	}
	return p, true
}
