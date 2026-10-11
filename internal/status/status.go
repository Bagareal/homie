// Package status periodically checks whether services respond.
package status

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/Bagareal/homie/internal/config"
)

type Result struct {
	Up        bool      `json:"up"`
	Code      int       `json:"code,omitempty"`
	LatencyMs int64     `json:"latencyMs"`
	CheckedAt time.Time `json:"checkedAt"`
	Error     string    `json:"error,omitempty"`
}

type Checker struct {
	store   *config.Store
	client  *http.Client
	wake    chan struct{}
	mu      sync.RWMutex
	results map[string]Result
}

func New(store *config.Store) *Checker {
	return &Checker{
		store: store,
		wake:  make(chan struct{}, 1),
		client: &http.Client{
			Timeout: 6 * time.Second,
			Transport: &http.Transport{
				// In a homelab, self-signed certificates are the norm.
				TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
				MaxIdleConnsPerHost: 2,
			},
			// A redirect (e.g. to a login page) means the service is responding.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		results: map[string]Result{},
	}
}

// Wake brings forward the next check round (e.g. after a configuration change).
func (c *Checker) Wake() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

func (c *Checker) Results() map[string]Result {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make(map[string]Result, len(c.results))
	for k, v := range c.results {
		out[k] = v
	}
	return out
}

func (c *Checker) Run(ctx context.Context) {
	for {
		c.checkAll(ctx)
		interval := time.Duration(c.store.Get().Settings.CheckInterval) * time.Second
		t := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-c.wake:
			t.Stop()
		case <-t.C:
		}
	}
}

func (c *Checker) checkAll(ctx context.Context) {
	var items []config.Item
	for _, g := range c.store.Get().Groups {
		for _, it := range g.Items {
			if it.Check {
				items = append(items, it)
			}
		}
	}

	// Each result is published as soon as it's ready: a slow service doesn't delay the others.
	active := make(map[string]bool, len(items))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 16)
	for _, it := range items {
		active[it.ID] = true
		wg.Add(1)
		sem <- struct{}{}
		go func(it config.Item) {
			defer wg.Done()
			defer func() { <-sem }()
			r := c.probe(ctx, it.URL)
			c.mu.Lock()
			c.results[it.ID] = r
			c.mu.Unlock()
		}(it)
	}
	wg.Wait()

	// Removes results for services that were deleted or have checks disabled.
	c.mu.Lock()
	for id := range c.results {
		if !active[id] {
			delete(c.results, id)
		}
	}
	c.mu.Unlock()
}

func (c *Checker) probe(ctx context.Context, url string) Result {
	start := time.Now()
	r := Result{CheckedAt: start}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		r.Error = "invalid address"
		return r
	}
	req.Header.Set("User-Agent", "homie-status")
	resp, err := c.client.Do(req)
	r.LatencyMs = time.Since(start).Milliseconds()
	if err != nil {
		var timeout interface{ Timeout() bool }
		if errors.As(err, &timeout) && timeout.Timeout() {
			r.Error = "no response within 6 seconds"
		} else {
			r.Error = "connection failed"
		}
		return r
	}
	resp.Body.Close()
	r.Code = resp.StatusCode
	// 401/403 = the service is responding; 502/503 = usually a proxy with no backend.
	r.Up = resp.StatusCode < 500
	return r
}
