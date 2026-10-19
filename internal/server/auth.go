package server

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Bagareal/homie/internal/auth"
)

const cookieName = "homie_session"

// guard puts the login in front of everything but the interface itself (its
// files hold nothing private, and the login page has to load): the API, the
// icons and the background images need a session. It also refuses a request that
// changes something and was started from another site.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		// Nobody may put Homie in a frame (a page that tricks a click), and the browser
		// must not guess what a file is.
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Content-Security-Policy", "frame-ancestors 'none'")
		h.Set("Referrer-Policy", "same-origin")
		if s.auth == nil {
			// Closed rather than open when the login cannot be read.
			if strings.HasPrefix(p, "/api/") {
				writeError(w, http.StatusInternalServerError, "the login cannot be read ("+s.authErr.Error()+"): check the files in the data folder")
				return
			}
		}
		private := strings.HasPrefix(p, "/api/") || strings.HasPrefix(p, "/icons/") || strings.HasPrefix(p, "/cdn/") || strings.HasPrefix(p, "/backgrounds/")
		if !private {
			next.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions && !sameOrigin(r) {
			writeError(w, http.StatusForbidden, "the request came from another site")
			return
		}
		if strings.HasPrefix(p, "/api/auth/") {
			next.ServeHTTP(w, r) // each of these decides for itself
			return
		}
		info := s.auth.Check(token(r))
		switch info.State {
		case auth.StateOK:
			next.ServeHTTP(w, r)
		case auth.StateLocked:
			writeJSON(w, http.StatusLocked, map[string]string{"error": "locked: unlock with the biometric", "state": string(info.State)})
		default:
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "sign in first", "state": string(info.State)})
		}
	})
}

func token(r *http.Request) string {
	if c, err := r.Cookie(cookieName); err == nil {
		return c.Value
	}
	return ""
}

// sameOrigin tells whether a browser request that changes something came from
// Homie's own page. A request without an Origin (not from a browser page) passes:
// without the cookie it gets nowhere, and the cookie is SameSite=Strict.
func sameOrigin(r *http.Request) bool {
	// Every current browser says where the request comes from, and a page cannot forge it.
	switch r.Header.Get("Sec-Fetch-Site") {
	case "same-origin", "none":
		return true
	case "":
		// An older browser, or not a browser: fall back on the Origin.
	default:
		return false
	}
	o := r.Header.Get("Origin")
	if o == "" || o == "null" {
		return o == ""
	}
	u, err := url.Parse(o)
	if err != nil {
		return false
	}
	// Behind a proxy the Host may be rewritten: the forwarded one counts too.
	for _, h := range []string{r.Host, firstValue(r.Header.Get("X-Forwarded-Host"))} {
		if h != "" && strings.EqualFold(u.Host, h) {
			return true
		}
	}
	return false
}

func firstValue(v string) string {
	if i := strings.IndexByte(v, ','); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSpace(v)
}

func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(firstValue(r.Header.Get("X-Forwarded-Proto")), "https")
}

func setCookie(w http.ResponseWriter, r *http.Request, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   isHTTPS(r),
		SameSite: http.SameSiteStrictMode,
	})
}

// deviceLabel names a device from its browser, for the list of signed-in devices.
func deviceLabel(r *http.Request) string {
	ua := r.UserAgent()
	browser := "Browser"
	switch {
	case strings.Contains(ua, "Edg/"):
		browser = "Edge"
	case strings.Contains(ua, "OPR/"), strings.Contains(ua, "Opera"):
		browser = "Opera"
	case strings.Contains(ua, "Firefox/"), strings.Contains(ua, "FxiOS"):
		browser = "Firefox"
	case strings.Contains(ua, "CriOS"), strings.Contains(ua, "Chrome/"):
		browser = "Chrome"
	case strings.Contains(ua, "Safari/"):
		browser = "Safari"
	}
	system := ""
	switch {
	case strings.Contains(ua, "iPhone"):
		system = "iPhone"
	case strings.Contains(ua, "iPad"):
		system = "iPad"
	case strings.Contains(ua, "Android"):
		system = "Android"
	case strings.Contains(ua, "Windows"):
		system = "Windows"
	case strings.Contains(ua, "Mac OS X"), strings.Contains(ua, "Macintosh"):
		system = "Mac"
	case strings.Contains(ua, "CrOS"):
		system = "ChromeOS"
	case strings.Contains(ua, "Linux"):
		system = "Linux"
	}
	if system == "" {
		return browser
	}
	return browser + " on " + system
}

type authStatus struct {
	State    string `json:"state"`
	Username string `json:"username,omitempty"`
	NeedCode bool   `json:"needCode,omitempty"`
	// HTTPS: a passkey needs it; Biometric: this device has the lock, and after how many seconds.
	Secure    bool   `json:"secure"`
	Biometric bool   `json:"biometric"`
	LockAfter int    `json:"lockAfter,omitempty"`
	Device    string `json:"device,omitempty"`
}

func (s *Server) statusOf(r *http.Request) authStatus {
	info := s.auth.Check(token(r))
	out := authStatus{State: string(info.State), Username: info.Username, NeedCode: info.NeedCode, Secure: isHTTPS(r) || isLocalhost(r)}
	if info.Session != nil {
		out.Biometric = info.Session.Passkey != ""
		out.LockAfter = info.Session.LockAfter
		out.Device = info.Session.Label
	}
	return out
}

func isLocalhost(r *http.Request) bool {
	h := r.Host
	if i := strings.LastIndexByte(h, ':'); i >= 0 && !strings.Contains(h[i:], "]") {
		h = h[:i]
	}
	return h == "localhost" || h == "127.0.0.1" || h == "[::1]"
}

func (s *Server) authStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.statusOf(r))
}

type credentialsRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Code     string `json:"code"`
}

// authFailure answers an error of the auth package with the right status.
func authFailure(w http.ResponseWriter, err error) {
	var tm *auth.TooManyError
	switch {
	case errors.As(err, &tm):
		w.Header().Set("Retry-After", strconv.Itoa(int(tm.Wait/time.Second)+1))
		writeError(w, http.StatusTooManyRequests, err.Error())
	case errors.Is(err, auth.ErrBadLogin), errors.Is(err, auth.ErrBadCode):
		writeError(w, http.StatusUnauthorized, err.Error())
	case errors.Is(err, auth.ErrUnauth):
		writeError(w, http.StatusUnauthorized, err.Error())
	case errors.Is(err, auth.ErrLocked):
		writeError(w, http.StatusLocked, err.Error())
	case errors.Is(err, auth.ErrNoSuchEntry):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, auth.ErrHasAccount):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
}

func (s *Server) authSetup(w http.ResponseWriter, r *http.Request) {
	var req credentialsRequest
	if !readJSON(w, r, &req) {
		return
	}
	tok, err := s.auth.Setup(req.Username, req.Password, req.Code, deviceLabel(r))
	if err != nil {
		authFailure(w, err)
		return
	}
	setCookie(w, r, tok, auth.SessionDays*24*3600)
	writeJSON(w, http.StatusOK, s.statusOf(withToken(r, tok)))
}

func (s *Server) authLogin(w http.ResponseWriter, r *http.Request) {
	var req credentialsRequest
	if !readJSON(w, r, &req) {
		return
	}
	tok, err := s.auth.Login(req.Username, req.Password, deviceLabel(r))
	if err != nil {
		authFailure(w, err)
		return
	}
	setCookie(w, r, tok, auth.SessionDays*24*3600)
	writeJSON(w, http.StatusOK, s.statusOf(withToken(r, tok)))
}

// withToken makes a request look like it already carries a fresh cookie.
func withToken(r *http.Request, tok string) *http.Request {
	r2 := r.Clone(r.Context())
	r2.Header.Del("Cookie")
	r2.AddCookie(&http.Cookie{Name: cookieName, Value: tok})
	return r2
}

func (s *Server) authLogout(w http.ResponseWriter, r *http.Request) {
	s.auth.Logout(token(r))
	setCookie(w, r, "", -1)
	writeJSON(w, http.StatusOK, map[string]string{"state": string(auth.StateLogin)})
}

// signedIn lets a handler through only for a session that is signed in and, when
// the handler changes something, not locked.
func (s *Server) signedIn(w http.ResponseWriter, r *http.Request, allowLocked bool) bool {
	info := s.auth.Check(token(r))
	switch {
	case info.State == auth.StateOK, info.State == auth.StateLocked && allowLocked:
		return true
	case info.State == auth.StateLocked:
		authFailure(w, auth.ErrLocked)
	default:
		authFailure(w, auth.ErrUnauth)
	}
	return false
}

// authActive and authHidden are the page saying whether it is in front of the
// user: that is what the biometric lock counts from. They answer with the state,
// so the page learns it has been locked.
func (s *Server) authActive(w http.ResponseWriter, r *http.Request) {
	info := s.auth.Active(token(r))
	writeJSON(w, http.StatusOK, map[string]string{"state": string(info.State)})
}

func (s *Server) authHidden(w http.ResponseWriter, r *http.Request) {
	s.auth.Hidden(token(r))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) authPassword(w http.ResponseWriter, r *http.Request) {
	if !s.signedIn(w, r, false) {
		return
	}
	var req struct {
		Current string `json:"current"`
		Next    string `json:"next"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.auth.ChangePassword(token(r), req.Current, req.Next); err != nil {
		authFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) authDevices(w http.ResponseWriter, r *http.Request) {
	if !s.signedIn(w, r, false) {
		return
	}
	devs, keys := s.auth.Devices(token(r))
	writeJSON(w, http.StatusOK, map[string]any{"devices": devs, "passkeys": keys, "username": s.auth.Check(token(r)).Username})
}

func (s *Server) authRevoke(w http.ResponseWriter, r *http.Request) {
	if !s.signedIn(w, r, false) {
		return
	}
	if err := s.auth.Revoke(r.PathValue("id")); err != nil {
		authFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) authRevokeOthers(w http.ResponseWriter, r *http.Request) {
	if !s.signedIn(w, r, false) {
		return
	}
	s.auth.RevokeOthers(token(r))
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- passkeys

func (s *Server) passkeyRegisterBegin(w http.ResponseWriter, r *http.Request) {
	o, err := s.auth.BeginRegister(token(r))
	if err != nil {
		authFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, o)
}

func (s *Server) passkeyRegisterFinish(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Credential auth.Credential `json:"credential"`
		Name       string          `json:"name"`
		LockAfter  int             `json:"lockAfter"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.auth.FinishRegister(token(r), req.Credential, req.Name, req.LockAfter); err != nil {
		authFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.statusOf(r))
}

func (s *Server) passkeyAssertBegin(w http.ResponseWriter, r *http.Request) {
	o, err := s.auth.BeginAssert(token(r), r.URL.Query().Get("link") == "1")
	if err != nil {
		authFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, o)
}

func (s *Server) passkeyAssertFinish(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Credential auth.Credential `json:"credential"`
		Link       bool            `json:"link"`
		LockAfter  int             `json:"lockAfter"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.auth.FinishAssert(token(r), req.Credential, req.Link, req.LockAfter); err != nil {
		authFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.statusOf(r))
}

func (s *Server) passkeyRemove(w http.ResponseWriter, r *http.Request) {
	if !s.signedIn(w, r, false) {
		return
	}
	if err := s.auth.RemovePasskey(r.PathValue("id")); err != nil {
		authFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) lockOff(w http.ResponseWriter, r *http.Request) {
	if err := s.auth.LockOff(token(r)); err != nil {
		authFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.statusOf(r))
}

func (s *Server) lockAfter(w http.ResponseWriter, r *http.Request) {
	var req struct {
		LockAfter int `json:"lockAfter"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.auth.SetLockAfter(token(r), req.LockAfter); err != nil {
		authFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.statusOf(r))
}
