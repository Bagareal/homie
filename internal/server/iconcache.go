package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// The icon collections (dashboard-icons and selfh.st) are fetched by Homie and
// kept in <data>/icon-cache, so the browser never has to reach a CDN: an icon
// is downloaded once, the first time somebody asks for it, and served from the
// data folder from then on. The cache is a few kilobytes per icon.

// Each collection can be read from several addresses, tried in turn: GitHub
// itself first (the host Homie already needs for its updates), then the CDNs
// in front of it, for networks that block one of them.
var iconCollections = map[string][]string{
	"dashboard-icons": {
		"https://raw.githubusercontent.com/homarr-labs/dashboard-icons/main",
		"https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons",
		"https://gcore.jsdelivr.net/gh/homarr-labs/dashboard-icons",
	},
	"selfhst": {
		"https://raw.githubusercontent.com/selfhst/icons/main",
		"https://cdn.jsdelivr.net/gh/selfhst/icons",
		"https://gcore.jsdelivr.net/gh/selfhst/icons",
	},
}

const (
	maxIconBytes  = 4 << 20
	maxIndexBytes = 16 << 20
	indexMaxAge   = 7 * 24 * time.Hour
	missingFor    = 24 * time.Hour // an icon the collection does not have
	failedFor     = 30 * time.Second
)

var (
	iconFile  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*\.(png|svg|webp)$`)
	indexFile = map[string]bool{"tree.json": true, "metadata.json": true, "index.json": true}
	iconTypes = map[string]string{".png": "image/png", ".svg": "image/svg+xml", ".webp": "image/webp", ".json": "application/json"}
)

type iconCache struct {
	dir     string
	origins map[string][]string // collection -> base addresses
	client  *http.Client
	slots   chan struct{} // bounds the downloads running at once

	mu    sync.Mutex
	skip  map[string]time.Time // key -> do not ask upstream again until then
	locks map[string]*sync.Mutex
}

func newIconCache(dataDir string) *iconCache {
	return &iconCache{
		dir:     filepath.Join(dataDir, "icon-cache"),
		origins: iconCollections,
		client:  &http.Client{Timeout: 20 * time.Second},
		slots:   make(chan struct{}, 8),
		skip:    map[string]time.Time{},
		locks:   map[string]*sync.Mutex{},
	}
}

var errMissing = errors.New("not in the collection")

// get returns the path of the cached file, downloading it if needed. An
// out-of-date index is served anyway when the CDN cannot be reached.
func (c *iconCache) get(ctx context.Context, collection, dir, file string) (string, error) {
	bases, ok := c.origins[collection]
	if !ok {
		return "", errMissing
	}
	index := dir == "index"
	if index && !indexFile[file] || !index && (!iconFile.MatchString(file) || !strings.HasSuffix(file, "."+dir)) {
		return "", errMissing
	}
	key := collection + "/" + dir + "/" + file
	local := filepath.Join(c.dir, collection, dir, file)

	fresh := func() bool {
		st, err := os.Stat(local)
		return err == nil && (!index || time.Since(st.ModTime()) < indexMaxAge)
	}
	if fresh() {
		return local, nil
	}

	lock := c.lockFor(key)
	lock.Lock()
	defer lock.Unlock()
	if fresh() {
		return local, nil
	}
	_, statErr := os.Stat(local)
	hasOld := statErr == nil
	if until, ok := c.skipped(key); ok && time.Now().Before(until) {
		if hasOld {
			return local, nil
		}
		return "", errMissing
	}

	path := dir + "/" + file
	if index {
		path = file
	}
	limit := int64(maxIconBytes)
	if index {
		limit = maxIndexBytes
	}
	err := c.download(ctx, bases, path, local, limit)
	switch {
	case err == nil:
		return local, nil
	case errors.Is(err, errMissing):
		c.hold(key, missingFor)
		return "", errMissing
	}
	c.hold(key, failedFor)
	if hasOld {
		return local, nil // stale, but better than nothing
	}
	return "", err
}

func (c *iconCache) lockFor(key string) *sync.Mutex {
	c.mu.Lock()
	defer c.mu.Unlock()
	m := c.locks[key]
	if m == nil {
		m = &sync.Mutex{}
		c.locks[key] = m
	}
	return m
}

func (c *iconCache) skipped(key string) (time.Time, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	t, ok := c.skip[key]
	return t, ok
}

func (c *iconCache) hold(key string, d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.skip[key] = time.Now().Add(d)
}

// download tries each address until one answers. A 404 is final (the icon is
// not in the collection); anything else moves on to the next address.
func (c *iconCache) download(ctx context.Context, bases []string, path, local string, limit int64) error {
	err := errors.New("no address to download from")
	for _, base := range bases {
		if err = c.fetch(ctx, base+"/"+path, local, limit); err == nil || errors.Is(err, errMissing) || ctx.Err() != nil {
			return err
		}
	}
	return err
}

func (c *iconCache) fetch(ctx context.Context, url, local string, limit int64) error {
	select {
	case c.slots <- struct{}{}:
		defer func() { <-c.slots }()
	case <-ctx.Done():
		return ctx.Err()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return errMissing
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("the icon host answered %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return err
	}
	if len(body) == 0 || int64(len(body)) > limit {
		return errors.New("unexpected icon size")
	}
	if err := os.MkdirAll(filepath.Dir(local), 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(local), ".dl-*")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(body)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		os.Remove(tmp.Name())
		return errors.New("could not save the icon")
	}
	if err := os.Chmod(tmp.Name(), 0o640); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), local); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

// cdnIcon serves /cdn/{collection}/{dir}/{file}: dir is the image format
// (png, svg, webp) or "index" for the lists the icon picker searches.
func (s *Server) cdnIcon(w http.ResponseWriter, r *http.Request) {
	dir, file := r.PathValue("dir"), r.PathValue("file")
	p, err := s.icons.get(r.Context(), r.PathValue("collection"), dir, file)
	switch {
	case errors.Is(err, errMissing):
		http.NotFound(w, r)
		return
	case err != nil:
		http.Error(w, "the icon could not be downloaded", http.StatusBadGateway)
		return
	}
	h := w.Header()
	h.Set("Content-Type", iconTypes[strings.ToLower(filepath.Ext(file))])
	// An SVG opened as a page must not be able to run anything.
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	h.Set("X-Content-Type-Options", "nosniff")
	if dir == "index" {
		h.Set("Cache-Control", "public, max-age=3600")
	} else {
		h.Set("Cache-Control", "public, max-age=604800")
	}
	http.ServeFile(w, r, p)
}
