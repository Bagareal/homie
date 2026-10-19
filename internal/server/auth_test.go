package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/Bagareal/homie/internal/config"
	"github.com/Bagareal/homie/internal/status"
)

func newGuarded(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	dir := t.TempDir()
	store, err := config.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	ui := fstest.MapFS{"index.html": {Data: []byte("<html>Homie</html>")}}
	s := New(store, status.New(store), ui, dir, "test")
	return s, s.Handler()
}

func call(h http.Handler, method, path string, body any, mod func(*http.Request)) *httptest.ResponseRecorder {
	var r *http.Request
	if body != nil {
		b, _ := json.Marshal(body)
		r = httptest.NewRequest(method, path, bytes.NewReader(b))
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	if mod != nil {
		mod(r)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func cookieOf(w *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range w.Result().Cookies() {
		if c.Name == cookieName {
			return c
		}
	}
	return nil
}

func withCookie(c *http.Cookie) func(*http.Request) {
	return func(r *http.Request) { r.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value}) }
}

func TestEverythingPrivateNeedsALogin(t *testing.T) {
	_, h := newGuarded(t)
	// The interface loads, so the login page can show.
	if w := call(h, "GET", "/", nil, nil); w.Code != 200 {
		t.Fatalf("the interface: %d", w.Code)
	}
	for _, path := range []string{"/api/config", "/api/widgets", "/api/status", "/api/releases", "/api/version", "/icons/x.png", "/backgrounds/x.png", "/cdn/a/b/c.png", "/api/auth/devices"} {
		w := call(h, "GET", path, nil, nil)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("GET %s without a login: %d", path, w.Code)
		}
	}
	if w := call(h, "PUT", "/api/config", map[string]any{}, nil); w.Code != http.StatusUnauthorized {
		t.Errorf("PUT /api/config without a login: %d", w.Code)
	}
	var st authStatus
	_ = json.Unmarshal(call(h, "GET", "/api/auth/status", nil, nil).Body.Bytes(), &st)
	if st.State != "setup" {
		t.Errorf("a new Homie is %q", st.State)
	}
}

func TestSetupLoginCookieAndLogout(t *testing.T) {
	_, h := newGuarded(t)
	w := call(h, "POST", "/api/auth/setup", map[string]string{"username": "ivan", "password": "a-long-enough-password"}, nil)
	if w.Code != 200 {
		t.Fatalf("setup: %d %s", w.Code, w.Body)
	}
	c := cookieOf(w)
	if c == nil || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.MaxAge < 24*3600 {
		t.Fatalf("the cookie is not what it should be: %+v", c)
	}
	if c.Secure {
		t.Error("the cookie is Secure on plain http (the browser would drop it)")
	}
	if w := call(h, "GET", "/api/config", nil, withCookie(c)); w.Code != 200 {
		t.Fatalf("with the cookie: %d", w.Code)
	}
	// A second setup is refused.
	if w := call(h, "POST", "/api/auth/setup", map[string]string{"username": "x", "password": "another-long-password"}, nil); w.Code != http.StatusConflict {
		t.Errorf("a second account: %d", w.Code)
	}
	// Behind an https proxy the cookie is Secure.
	w = call(h, "POST", "/api/auth/login", map[string]string{"username": "ivan", "password": "a-long-enough-password"}, func(r *http.Request) {
		r.Header.Set("X-Forwarded-Proto", "https")
	})
	if c2 := cookieOf(w); w.Code != 200 || c2 == nil || !c2.Secure {
		t.Errorf("login behind https: %d %+v", w.Code, c2)
	}
	if w := call(h, "POST", "/api/auth/logout", nil, withCookie(c)); w.Code != 200 {
		t.Fatalf("logout: %d", w.Code)
	}
	if w := call(h, "GET", "/api/config", nil, withCookie(c)); w.Code != http.StatusUnauthorized {
		t.Fatalf("after logout: %d", w.Code)
	}
}

func TestWrongPasswordsSlowDown(t *testing.T) {
	_, h := newGuarded(t)
	call(h, "POST", "/api/auth/setup", map[string]string{"username": "ivan", "password": "a-long-enough-password"}, nil)
	for i := 0; i < 5; i++ {
		if w := call(h, "POST", "/api/auth/login", map[string]string{"username": "ivan", "password": "wrong-wrong-wrong"}, nil); w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: %d", i, w.Code)
		}
	}
	w := call(h, "POST", "/api/auth/login", map[string]string{"username": "ivan", "password": "a-long-enough-password"}, nil)
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") == "" {
		t.Fatalf("no wait: %d %v", w.Code, w.Header())
	}
}

func TestRequestsFromOtherSitesAreRefused(t *testing.T) {
	_, h := newGuarded(t)
	c := cookieOf(call(h, "POST", "/api/auth/setup", map[string]string{"username": "ivan", "password": "a-long-enough-password"}, nil))
	put := func(mod func(*http.Request)) int {
		return call(h, "PUT", "/api/config", map[string]any{}, func(r *http.Request) {
			withCookie(c)(r)
			mod(r)
		}).Code
	}
	if code := put(func(r *http.Request) { r.Header.Set("Origin", "https://evil.example.org") }); code != http.StatusForbidden {
		t.Errorf("another origin: %d", code)
	}
	if code := put(func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }); code != http.StatusForbidden {
		t.Errorf("cross-site fetch: %d", code)
	}
	if code := put(func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "same-site") }); code != http.StatusForbidden {
		t.Errorf("a request from another port or subdomain of the same site: %d", code)
	}
	if code := put(func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "same-origin") }); code == http.StatusForbidden {
		t.Errorf("a same-origin fetch was refused")
	}
	if code := put(func(r *http.Request) { r.Header.Set("Origin", "null") }); code != http.StatusForbidden {
		t.Errorf("null origin: %d", code)
	}
	// The page itself, directly or behind a proxy that rewrites the Host.
	if code := put(func(r *http.Request) { r.Header.Set("Origin", "http://"+r.Host) }); code == http.StatusForbidden {
		t.Errorf("the same origin was refused")
	}
	if code := put(func(r *http.Request) {
		r.Header.Set("Origin", "https://homie.example.com")
		r.Header.Set("X-Forwarded-Host", "homie.example.com")
	}); code == http.StatusForbidden {
		t.Errorf("the forwarded host was refused")
	}
}

func TestPasskeyEndpointsNeedALogin(t *testing.T) {
	_, h := newGuarded(t)
	for _, path := range []string{"/api/auth/passkey/register/begin", "/api/auth/passkey/assert/begin", "/api/auth/lock/off"} {
		if w := call(h, "POST", path, nil, nil); w.Code != http.StatusUnauthorized {
			t.Errorf("%s without a login: %d", path, w.Code)
		}
	}
	c := cookieOf(call(h, "POST", "/api/auth/setup", map[string]string{"username": "ivan", "password": "a-long-enough-password"}, nil))
	if w := call(h, "POST", "/api/auth/passkey/register/begin", nil, withCookie(c)); w.Code != 200 {
		t.Errorf("register/begin signed in: %d %s", w.Code, w.Body)
	}
	// There is no passkey yet, so there is nothing to unlock with.
	if w := call(h, "POST", "/api/auth/passkey/assert/begin", nil, withCookie(c)); w.Code != http.StatusBadRequest {
		t.Errorf("assert/begin with no passkey: %d", w.Code)
	}
}

func TestAuthFileIsNotPartOfTheConfig(t *testing.T) {
	_, h := newGuarded(t)
	c := cookieOf(call(h, "POST", "/api/auth/setup", map[string]string{"username": "ivan", "password": "a-long-enough-password"}, nil))
	w := call(h, "GET", "/api/config", nil, withCookie(c))
	for _, secret := range []string{"a-long-enough-password", "hash", "salt", c.Value} {
		if bytes.Contains(w.Body.Bytes(), []byte(secret)) {
			t.Errorf("the configuration mentions %q", secret)
		}
	}
}
