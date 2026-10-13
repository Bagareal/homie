package updates

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCompare(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{
		{"v1.23.4", "v1.23.4", 0},
		{"v1.23.4", "v1.23.10", -1},
		{"v1.24.0", "v1.23.99", 1},
		{"v2.0.0", "v1.99.99", 1},
		{"dev", "v1.0.0", -1},
		{"v1.0.0", "dev", 1},
		{"dev", "dev", 0},
	} {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestReleasesAreFilteredSortedAndCached(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`[
			{"tag_name":"v1.9.0","html_url":"https://github.com/x/y/releases/tag/v1.9.0","body":"old","published_at":"2026-10-01T10:00:00Z"},
			{"tag_name":"v1.10.0","html_url":"https://github.com/x/y/releases/tag/v1.10.0","body":"### New thing\n\nIt does stuff.","published_at":"2026-10-12T10:00:00Z"},
			{"tag_name":"v1.11.0-rc1","html_url":"https://github.com/x/y/releases/tag/v1.11.0-rc1","body":"rc"},
			{"tag_name":"v1.12.0","html_url":"https://github.com/x/y/releases/tag/v1.12.0","body":"draft","draft":true},
			{"tag_name":"v1.13.0","html_url":"javascript:alert(1)","body":"bad link"},
			{"tag_name":"nightly","html_url":"https://github.com/x/y/releases/tag/nightly","body":"not a version"}
		]`))
	}))
	defer srv.Close()
	c := New(srv.URL)
	rels, at, errMsg := c.Get(context.Background(), false)
	if errMsg != "" || at.IsZero() {
		t.Fatalf("got error %q", errMsg)
	}
	if len(rels) != 2 || rels[0].Version != "v1.10.0" || rels[1].Version != "v1.9.0" {
		t.Fatalf("expected v1.10.0 then v1.9.0, got %+v", rels)
	}
	if !strings.Contains(rels[0].Notes, "New thing") {
		t.Errorf("notes lost: %q", rels[0].Notes)
	}
	// Asking again soon does not ask GitHub again, not even when forced twice in a row.
	c.Get(context.Background(), false)
	c.Get(context.Background(), true)
	if calls.Load() != 1 {
		t.Errorf("GitHub was asked %d times, want 1", calls.Load())
	}
}

func TestFailureKeepsTheLastGoodAnswer(t *testing.T) {
	ok := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !ok {
			http.Error(w, "rate limited", http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte(`[{"tag_name":"v1.0.0","html_url":"https://github.com/x/y/releases/tag/v1.0.0","body":"first"}]`))
	}))
	defer srv.Close()
	c := New(srv.URL)
	c.Get(context.Background(), false)
	ok = false
	c.tried = c.tried.Add(-forceEvery * 2) // long enough ago to be asked again
	rels, _, errMsg := c.Get(context.Background(), true)
	if errMsg == "" || len(rels) != 1 {
		t.Fatalf("want the old release and an error, got %+v %q", rels, errMsg)
	}
}

func TestUnreachable(t *testing.T) {
	c := New("http://127.0.0.1:1")
	rels, _, errMsg := c.Get(context.Background(), false)
	if len(rels) != 0 || errMsg == "" {
		t.Fatalf("expected an error, got %+v %q", rels, errMsg)
	}
}
