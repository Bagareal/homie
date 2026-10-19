// Package auth is Homie's login: one account (a user name and a password),
// sessions that a cookie keeps for a long time, an optional biometric lock for
// a device (a passkey, see webauthn.go) and the recovery from the terminal.
//
// Everything lives in <data>/auth.json (mode 0600), apart from config.json:
// the configuration can be exported and imported, the login must never be.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	// SessionDays is how long a login lasts without being used again; every use
	// renews it, so a device that is opened now and then stays signed in.
	SessionDays = 30
	maxSessions = 50
	// A device that has the biometric lock is locked again when it has been out of
	// sight for LockAfter seconds. The page says it is visible every heartbeat; a
	// missing beat for longer than slack means it was not (a phone that fell asleep).
	slack = 45 * time.Second
	// How long a sign-in page's challenge stays valid.
	challengeTTL = 3 * time.Minute

	minPassword = 10
	maxPassword = 200
	maxUsername = 64
)

var (
	ErrNoAccount   = errors.New("there is no account yet")
	ErrHasAccount  = errors.New("an account already exists")
	ErrBadLogin    = errors.New("wrong user name or password")
	ErrBadCode     = errors.New("wrong recovery code")
	ErrNeedCode    = errors.New("enter the recovery code printed by the reset command")
	ErrUnauth      = errors.New("not signed in")
	ErrLocked      = errors.New("locked: unlock with the biometric")
	ErrWeak        = fmt.Errorf("the password needs at least %d characters", minPassword)
	ErrNoSuchEntry = errors.New("not found")
)

// TooManyError says how long to wait after too many wrong passwords.
type TooManyError struct{ Wait time.Duration }

func (e *TooManyError) Error() string {
	return fmt.Sprintf("too many attempts: try again in %d seconds", int(e.Wait.Seconds())+1)
}

type account struct {
	Username string `json:"username"`
	UserID   []byte `json:"userId"` // the stable handle passkeys are made for
	Salt     []byte `json:"salt"`
	Hash     []byte `json:"hash"`
	Iter     int    `json:"iterations"`
	Created  time.Time
}

// Session is one signed-in device.
type Session struct {
	ID       string    `json:"id"` // sha256 of the cookie value: the cookie itself is never stored
	Created  time.Time `json:"created"`
	LastUsed time.Time `json:"lastUsed"`
	Expires  time.Time `json:"expires"`
	Label    string    `json:"label"`

	// The biometric lock of this device. Passkey is the credential that unlocks
	// it (empty = no lock).
	Passkey    string    `json:"passkey,omitempty"`
	LockAfter  int       `json:"lockAfter,omitempty"` // seconds out of sight before it locks
	LastActive time.Time `json:"lastActive,omitempty"`
	HiddenAt   time.Time `json:"hiddenAt,omitempty"`
	Locked     bool      `json:"locked,omitempty"`
}

type file struct {
	Account   *account   `json:"account,omitempty"`
	Sessions  []*Session `json:"sessions"`
	Passkeys  []*Passkey `json:"passkeys"`
	Origin    string     `json:"origin,omitempty"`    // where the passkeys were made, e.g. https://homie.example.com
	SetupCode string     `json:"setupCode,omitempty"` // sha256 of the code after a reset, empty on a first install
	// ResetUser is the user name of the account a reset from the terminal forgot: the
	// new password is set for the same one.
	ResetUser string `json:"resetUser,omitempty"`
}

// Manager keeps the account and the sessions.
type Manager struct {
	mu       sync.Mutex
	path     string
	dir      string
	f        file
	byID     map[string]*Session
	lastSave time.Time

	// Wrong passwords, for the wait between attempts.
	fails   int
	lastBad time.Time
	blocked time.Time

	challenges map[string]challenge
	now        func() time.Time
}

// Open reads <dir>/auth.json (a missing file is a Homie with no account yet).
func Open(dir string) (*Manager, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	m := &Manager{path: filepath.Join(dir, "auth.json"), dir: dir, byID: map[string]*Session{}, challenges: map[string]challenge{}, now: time.Now}
	raw, err := os.ReadFile(m.path)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		if err := json.Unmarshal(raw, &m.f); err != nil {
			return nil, fmt.Errorf("%s is not valid JSON: %w", m.path, err)
		}
	}
	for _, s := range m.f.Sessions {
		m.byID[s.ID] = s
	}
	return m, nil
}

// ---- state

// State is what the page needs to know first.
type State string

const (
	StateSetup  State = "setup"  // no account: the first visitor creates it
	StateLogin  State = "login"  // not signed in
	StateLocked State = "locked" // signed in, but this device wants its biometric
	StateOK     State = "ok"
)

// Info describes a request's session.
type Info struct {
	State    State
	Username string
	Session  *Session // a copy; nil unless signed in
	// NeedCode: the account was reset from the terminal, the new one needs its code.
	NeedCode bool
}

// Check looks at the session cookie of a request.
func (m *Manager) Check(token string) Info {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pollReset()
	if m.f.Account == nil {
		return Info{State: StateSetup, NeedCode: m.f.SetupCode != "", Username: m.f.ResetUser}
	}
	s := m.sessionLocked(token)
	if s == nil {
		return Info{State: StateLogin}
	}
	now := m.now()
	m.lockIfDue(s, now)
	cp := *s
	st := StateOK
	if s.Locked {
		st = StateLocked
	} else {
		m.touch(s, now)
	}
	return Info{State: st, Username: m.f.Account.Username, Session: &cp}
}

func (m *Manager) sessionLocked(token string) *Session {
	if token == "" {
		return nil
	}
	s := m.byID[tokenID(token)]
	if s == nil || m.now().After(s.Expires) {
		return nil
	}
	return s
}

func tokenID(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// lockIfDue locks a device with the biometric lock that has been out of sight
// for too long.
func (m *Manager) lockIfDue(s *Session, now time.Time) {
	if s.Passkey == "" || s.Locked {
		return
	}
	after := time.Duration(s.LockAfter) * time.Second
	gone := !s.HiddenAt.IsZero() && now.Sub(s.HiddenAt) > after
	silent := now.Sub(s.LastActive) > after+slack
	if gone || silent {
		s.Locked = true
		m.save()
	}
}

// touch renews a session; the file is rewritten at most every ten minutes.
func (m *Manager) touch(s *Session, now time.Time) {
	s.LastUsed = now
	s.Expires = now.Add(SessionDays * 24 * time.Hour)
	if now.Sub(m.lastSave) > 10*time.Minute {
		m.save()
	}
}

// Active says the page is in front of the user: it keeps the biometric lock from
// closing. It does nothing to a locked session.
func (m *Manager) Active(token string) Info {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.sessionLocked(token)
	if s == nil || m.f.Account == nil {
		return Info{State: StateLogin}
	}
	now := m.now()
	m.lockIfDue(s, now)
	if s.Locked {
		return Info{State: StateLocked, Username: m.f.Account.Username}
	}
	s.LastActive, s.HiddenAt = now, time.Time{}
	m.touch(s, now)
	return Info{State: StateOK, Username: m.f.Account.Username}
}

// Hidden says the page went out of sight: the lock counts from now.
func (m *Manager) Hidden(token string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s := m.sessionLocked(token); s != nil && s.Passkey != "" && !s.Locked {
		s.HiddenAt = m.now()
		m.save()
	}
}

// ---- account

func validUsername(u string) error {
	if u == "" || utf8.RuneCountInString(u) > maxUsername {
		return fmt.Errorf("the user name needs 1 to %d characters", maxUsername)
	}
	for _, r := range u {
		if r < 0x20 || r == 0x7f {
			return errors.New("the user name has a character that is not allowed")
		}
	}
	return nil
}

func validPassword(p, user string) error {
	if utf8.RuneCountInString(p) < minPassword {
		return ErrWeak
	}
	if len(p) > maxPassword {
		return fmt.Errorf("the password is too long (%d characters max)", maxPassword)
	}
	if strings.EqualFold(p, user) {
		return errors.New("the password cannot be the same as the user name")
	}
	return nil
}

func random(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("auth: no randomness: " + err.Error())
	}
	return b
}

// HasAccount tells whether Homie is set up.
func (m *Manager) HasAccount() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pollReset()
	return m.f.Account != nil
}

// Setup creates the account and signs in. Allowed only while there is none; after
// a reset from the terminal it also needs the code that was printed there, and the
// user name stays the one the account had.
func (m *Manager) Setup(username, password, code, label string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pollReset()
	if m.f.Account != nil {
		return "", ErrHasAccount
	}
	if m.f.ResetUser != "" {
		username = m.f.ResetUser
	}
	username = strings.TrimSpace(username)
	if err := validUsername(username); err != nil {
		return "", err
	}
	if err := validPassword(password, username); err != nil {
		return "", err
	}
	if err := m.throttle(); err != nil {
		return "", err
	}
	if m.f.SetupCode != "" {
		code = strings.ToUpper(strings.TrimSpace(code))
		if code == "" {
			return "", ErrNeedCode
		}
		if !equal([]byte(codeHash(code)), []byte(m.f.SetupCode)) {
			m.fail()
			return "", ErrBadCode
		}
	}
	m.succeed()
	salt := random(16)
	m.f.Account = &account{Username: username, UserID: random(16), Salt: salt, Hash: hashPassword(password, salt, hashIterations), Iter: hashIterations, Created: m.now()}
	m.f.SetupCode, m.f.ResetUser = "", ""
	m.f.Sessions, m.f.Passkeys, m.f.Origin = nil, nil, ""
	m.byID = map[string]*Session{}
	return m.newSession(label)
}

// Login checks the credentials and opens a session; the cookie value comes back.
func (m *Manager) Login(username, password, label string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pollReset()
	if m.f.Account == nil {
		return "", ErrNoAccount
	}
	if err := m.throttle(); err != nil {
		return "", err
	}
	a := m.f.Account
	// The hash is always computed, so a wrong user name takes as long as a wrong password.
	got := hashPassword(password, a.Salt, a.Iter)
	userOK := equal([]byte(strings.TrimSpace(username)), []byte(a.Username))
	if !equal(got, a.Hash) || !userOK {
		m.fail()
		return "", ErrBadLogin
	}
	m.succeed()
	return m.newSession(label)
}

// throttle is the wait after too many wrong passwords: from the fifth in a row
// on, 5 seconds doubling up to five minutes. It is for the whole server, not per
// address (behind a proxy every request seems to come from it), so someone
// guessing slows the owner down too: a short, bounded price for it.
func (m *Manager) throttle() error {
	now := m.now()
	if now.Before(m.blocked) {
		return &TooManyError{Wait: m.blocked.Sub(now)}
	}
	return nil
}

func (m *Manager) fail() {
	now := m.now()
	if now.Sub(m.lastBad) > 15*time.Minute {
		m.fails = 0
	}
	m.fails++
	m.lastBad = now
	if m.fails >= 5 {
		wait := 5 * time.Second << (m.fails - 5)
		if wait > 5*time.Minute || wait <= 0 {
			wait = 5 * time.Minute
		}
		m.blocked = now.Add(wait)
	}
}

func (m *Manager) succeed() { m.fails, m.blocked = 0, time.Time{} }

// newSession must be called with the lock held.
func (m *Manager) newSession(label string) (string, error) {
	token := base64.RawURLEncoding.EncodeToString(random(32))
	now := m.now()
	s := &Session{ID: tokenID(token), Created: now, LastUsed: now, Expires: now.Add(SessionDays * 24 * time.Hour), Label: label, LastActive: now}
	m.byID[s.ID] = s
	m.f.Sessions = append(m.f.Sessions, s)
	m.prune(now)
	return token, m.save()
}

// prune forgets the sessions that ran out, and the oldest ones beyond the limit.
func (m *Manager) prune(now time.Time) {
	keep := m.f.Sessions[:0]
	for _, s := range m.f.Sessions {
		if now.After(s.Expires) {
			delete(m.byID, s.ID)
			continue
		}
		keep = append(keep, s)
	}
	m.f.Sessions = keep
	if len(m.f.Sessions) > maxSessions {
		sort.Slice(m.f.Sessions, func(i, j int) bool { return m.f.Sessions[i].LastUsed.After(m.f.Sessions[j].LastUsed) })
		for _, s := range m.f.Sessions[maxSessions:] {
			delete(m.byID, s.ID)
		}
		m.f.Sessions = m.f.Sessions[:maxSessions]
	}
}

// Logout ends the session of a cookie.
func (m *Manager) Logout(token string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if token != "" {
		m.removeSession(tokenID(token))
	}
}

func (m *Manager) removeSession(id string) bool {
	if _, ok := m.byID[id]; !ok {
		return false
	}
	delete(m.byID, id)
	keep := m.f.Sessions[:0]
	for _, s := range m.f.Sessions {
		if s.ID != id {
			keep = append(keep, s)
		}
	}
	m.f.Sessions = keep
	m.save()
	return true
}

// ChangePassword needs the current password; every other device is signed out.
func (m *Manager) ChangePassword(token, current, next string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a := m.f.Account
	if a == nil || m.sessionLocked(token) == nil {
		return ErrUnauth
	}
	if err := m.throttle(); err != nil {
		return err
	}
	if !equal(hashPassword(current, a.Salt, a.Iter), a.Hash) {
		m.fail()
		return errors.New("the current password is wrong")
	}
	m.succeed()
	if err := validPassword(next, a.Username); err != nil {
		return err
	}
	a.Salt = random(16)
	a.Iter = hashIterations
	a.Hash = hashPassword(next, a.Salt, a.Iter)
	keep := tokenID(token)
	for _, s := range append([]*Session(nil), m.f.Sessions...) {
		if s.ID != keep {
			m.removeSession(s.ID)
		}
	}
	return m.save()
}

// ---- sessions

// DeviceInfo is a session as the settings list it.
type DeviceInfo struct {
	ID       string    `json:"id"`
	Label    string    `json:"label"`
	Created  time.Time `json:"created"`
	LastUsed time.Time `json:"lastUsed"`
	Current  bool      `json:"current"`
	Lock     bool      `json:"lock"`
}

// PasskeyInfo is a passkey as the settings list it.
type PasskeyInfo struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Created time.Time `json:"created"`
	// Used is the number of sessions that are locked with it.
	Used int `json:"used"`
}

// Devices lists the signed-in devices, the current one first, and the passkeys.
func (m *Manager) Devices(token string) ([]DeviceInfo, []PasskeyInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	cur := tokenID(token)
	var devs []DeviceInfo
	for _, s := range m.f.Sessions {
		if now.After(s.Expires) {
			continue
		}
		devs = append(devs, DeviceInfo{ID: s.ID, Label: s.Label, Created: s.Created, LastUsed: s.LastUsed, Current: s.ID == cur, Lock: s.Passkey != ""})
	}
	sort.SliceStable(devs, func(i, j int) bool {
		if devs[i].Current != devs[j].Current {
			return devs[i].Current
		}
		return devs[i].LastUsed.After(devs[j].LastUsed)
	})
	keys := []PasskeyInfo{}
	for _, p := range m.f.Passkeys {
		n := 0
		for _, s := range m.f.Sessions {
			if s.Passkey == p.ID {
				n++
			}
		}
		keys = append(keys, PasskeyInfo{ID: p.ID, Name: p.Name, Created: p.Created, Used: n})
	}
	return devs, keys
}

// Revoke signs a device out.
func (m *Manager) Revoke(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.removeSession(id) {
		return ErrNoSuchEntry
	}
	return nil
}

// RevokeOthers signs every device out but the current one.
func (m *Manager) RevokeOthers(token string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	keep := tokenID(token)
	for _, s := range append([]*Session(nil), m.f.Sessions...) {
		if s.ID != keep {
			m.removeSession(s.ID)
		}
	}
}

// ---- recovery

const resetFile = "reset-password"

// Reset is the terminal command "homie reset-password": it asks the running
// server, through a file, to forget the password (the user name stays), and returns
// the code the new password will need (so that nobody else on the network can create it first).
// Going through the server keeps the files with their owner (the service runs as
// its own user, the command as root).
func Reset(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	raw := random(5)
	code := strings.ToUpper(codeText(raw))
	// The server must read it (as another user), so 0644; it holds no secret but a hash.
	if err := os.WriteFile(filepath.Join(dir, resetFile), []byte(codeHash(code)), 0o644); err != nil {
		return "", err
	}
	return code, nil
}

func codeText(b []byte) string {
	const alphabet = "ABCDEFGHJKMNPQRSTVWXYZ23456789" // no look-alikes
	out := make([]byte, 0, 2*len(b))
	for _, x := range b {
		out = append(out, alphabet[int(x)%len(alphabet)], alphabet[int(x>>3)%len(alphabet)])
	}
	return string(out)
}

func codeHash(code string) string {
	sum := sha256.Sum256([]byte("homie-reset:" + code))
	return hex.EncodeToString(sum[:])
}

// pollReset carries out a reset asked for from the terminal. Called with the lock held.
func (m *Manager) pollReset() {
	p := filepath.Join(m.dir, resetFile)
	raw, err := os.ReadFile(p)
	if err != nil {
		return
	}
	_ = os.Remove(p)
	h := strings.TrimSpace(string(raw))
	if len(h) != 64 {
		return
	}
	user := m.f.ResetUser
	if m.f.Account != nil {
		user = m.f.Account.Username
	}
	m.f = file{Sessions: []*Session{}, Passkeys: []*Passkey{}, SetupCode: h, ResetUser: user}
	m.byID = map[string]*Session{}
	m.fails, m.blocked = 0, time.Time{}
	m.save()
}

// ---- storage

// save writes auth.json atomically with mode 0600. Called with the lock held.
func (m *Manager) save() error {
	m.lastSave = m.now()
	if m.f.Sessions == nil {
		m.f.Sessions = []*Session{}
	}
	if m.f.Passkeys == nil {
		m.f.Passkeys = []*Passkey{}
	}
	data, err := json.MarshalIndent(m.f, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(m.dir, ".auth-*.json")
	if err != nil {
		return err
	}
	_ = os.Chmod(tmp.Name(), 0o600)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), m.path); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}
