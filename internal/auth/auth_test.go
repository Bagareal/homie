package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func init() { hashIterations = 50 } // the real figure is slow on purpose

func open(t *testing.T) (*Manager, string) {
	t.Helper()
	dir := t.TempDir()
	m, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return m, dir
}

func TestPBKDF2Vectors(t *testing.T) {
	// RFC 7914 section 11 (PBKDF2-HMAC-SHA256).
	for _, c := range []struct {
		iter int
		want string
	}{
		{1, "120fb6cffcf8b32c43e7225256c4f837a86548c92ccc35480805987cb70be17b"},
		{2, "ae4d0c95af6b46d32d0adff928f06dd02a303f8ef3c251dfd6e2d85a95474c43"},
		{4096, "c5e478d59288c841aa530db6845c4c8d962893a001ce4e11a4963873aa98134a"},
	} {
		got := hex.EncodeToString(pbkdf2(sha256.New, []byte("password"), []byte("salt"), c.iter, 32))
		if got != c.want {
			t.Errorf("iter %d: got %s", c.iter, got)
		}
	}
}

func TestSetupLoginLogout(t *testing.T) {
	m, dir := open(t)
	if got := m.Check("").State; got != StateSetup {
		t.Fatalf("a new Homie is in %q", got)
	}
	if _, err := m.Setup("ivan", "short", "", "x"); !errors.Is(err, ErrWeak) {
		t.Fatalf("a short password was accepted: %v", err)
	}
	if _, err := m.Setup("", "a-long-enough-password", "", "x"); err == nil {
		t.Fatal("an empty user name was accepted")
	}
	tok, err := m.Setup("ivan", "a-long-enough-password", "", "Edge on Windows")
	if err != nil {
		t.Fatal(err)
	}
	if info := m.Check(tok); info.State != StateOK || info.Username != "ivan" {
		t.Fatalf("after setup: %+v", info)
	}
	if _, err := m.Setup("other", "another-long-password", "", "x"); !errors.Is(err, ErrHasAccount) {
		t.Fatalf("a second account was created: %v", err)
	}
	if m.Check("not-a-token").State != StateLogin {
		t.Fatal("an unknown cookie is signed in")
	}
	if _, err := m.Login("ivan", "wrong-password-here", "x"); !errors.Is(err, ErrBadLogin) {
		t.Fatalf("a wrong password: %v", err)
	}
	if _, err := m.Login("nobody", "a-long-enough-password", "x"); !errors.Is(err, ErrBadLogin) {
		t.Fatalf("a wrong user name: %v", err)
	}
	tok2, err := m.Login("ivan", "a-long-enough-password", "Phone")
	if err != nil {
		t.Fatal(err)
	}
	m.Logout(tok)
	if m.Check(tok).State != StateLogin || m.Check(tok2).State != StateOK {
		t.Fatal("logout ended the wrong session")
	}

	// The file is private and holds neither the password nor the cookie.
	raw, err := os.ReadFile(filepath.Join(dir, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"a-long-enough-password", tok, tok2} {
		if contains(raw, secret) {
			t.Fatalf("auth.json contains a secret: %q", secret)
		}
	}
	if fi, _ := os.Stat(filepath.Join(dir, "auth.json")); fi.Mode().Perm()&0o077 != 0 && !isWindows() {
		t.Fatalf("auth.json is readable by others: %v", fi.Mode())
	}

	// It survives a restart.
	m2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m2.Check(tok2).State != StateOK {
		t.Fatal("the session was lost on restart")
	}
	if _, err := m2.Login("ivan", "a-long-enough-password", "x"); err != nil {
		t.Fatal(err)
	}
}

func contains(b []byte, s string) bool {
	return len(s) > 0 && len(b) >= len(s) && indexOf(string(b), s) >= 0
}

func indexOf(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}

func isWindows() bool { return os.PathSeparator == '\\' }

func TestSessionExpiresAndSlides(t *testing.T) {
	m, _ := open(t)
	now := time.Now()
	m.now = func() time.Time { return now }
	tok, _ := m.Setup("ivan", "a-long-enough-password", "", "x")
	now = now.Add(20 * 24 * time.Hour)
	if m.Check(tok).State != StateOK {
		t.Fatal("expired after 20 days")
	}
	now = now.Add(20 * 24 * time.Hour) // 40 days after login, but 20 since its last use
	if m.Check(tok).State != StateOK {
		t.Fatal("the session did not renew with use")
	}
	now = now.Add(31 * 24 * time.Hour)
	if m.Check(tok).State != StateLogin {
		t.Fatal("a session unused for 31 days is still valid")
	}
}

func TestThrottle(t *testing.T) {
	m, _ := open(t)
	now := time.Now()
	m.now = func() time.Time { return now }
	if _, err := m.Setup("ivan", "a-long-enough-password", "", "x"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if _, err := m.Login("ivan", "nope-nope-nope", "x"); !errors.Is(err, ErrBadLogin) {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	var tm *TooManyError
	if _, err := m.Login("ivan", "a-long-enough-password", "x"); !errors.As(err, &tm) {
		t.Fatalf("no wait after five failures: %v", err)
	}
	now = now.Add(6 * time.Second)
	if _, err := m.Login("ivan", "a-long-enough-password", "x"); err != nil {
		t.Fatalf("still blocked after the wait: %v", err)
	}
	if _, err := m.Login("ivan", "nope-nope-nope", "x"); !errors.Is(err, ErrBadLogin) {
		t.Fatalf("the counter was not reset: %v", err)
	}
}

func TestChangePasswordSignsOthersOut(t *testing.T) {
	m, _ := open(t)
	a, _ := m.Setup("ivan", "a-long-enough-password", "", "a")
	b, _ := m.Login("ivan", "a-long-enough-password", "b")
	if err := m.ChangePassword(a, "wrong-current-pass", "a-brand-new-password"); err == nil {
		t.Fatal("changed with a wrong current password")
	}
	if err := m.ChangePassword(a, "a-long-enough-password", "short"); err == nil {
		t.Fatal("accepted a short new password")
	}
	if err := m.ChangePassword(a, "a-long-enough-password", "a-brand-new-password"); err != nil {
		t.Fatal(err)
	}
	if m.Check(a).State != StateOK || m.Check(b).State != StateLogin {
		t.Fatal("the other device was not signed out, or this one was")
	}
	if _, err := m.Login("ivan", "a-long-enough-password", "x"); err == nil {
		t.Fatal("the old password still works")
	}
	if _, err := m.Login("ivan", "a-brand-new-password", "x"); err != nil {
		t.Fatal(err)
	}
}

func TestResetFromTerminal(t *testing.T) {
	m, dir := open(t)
	tok, _ := m.Setup("ivan", "a-long-enough-password", "", "x")
	code, err := Reset(dir)
	if err != nil {
		t.Fatal(err)
	}
	info := m.Check(tok)
	if info.State != StateSetup || !info.NeedCode {
		t.Fatalf("after a reset: %+v", info)
	}
	if _, err := os.Stat(filepath.Join(dir, resetFile)); err == nil {
		t.Fatal("the reset file was not consumed")
	}
	if _, err := m.Setup("ivan", "a-long-enough-password", "", "x"); !errors.Is(err, ErrNeedCode) {
		t.Fatalf("setup without the code: %v", err)
	}
	if _, err := m.Setup("ivan", "a-long-enough-password", "WRONGCODE", "x"); !errors.Is(err, ErrBadCode) {
		t.Fatalf("setup with a wrong code: %v", err)
	}
	if _, err := m.Setup("ivan", "a-brand-new-password", code, "x"); err != nil {
		t.Fatalf("setup with the code: %v", err)
	}
	if m.Check(tok).State != StateLogin {
		t.Fatal("an old session survived the reset")
	}
}

func TestBiometricLockTiming(t *testing.T) {
	m, _ := open(t)
	now := time.Now()
	m.now = func() time.Time { return now }
	tok, _ := m.Setup("ivan", "a-long-enough-password", "", "Phone")
	m.mu.Lock()
	s := m.sessionLocked(tok)
	m.lockOn(s, "pk1", 60)
	m.mu.Unlock()

	// In front of the user, beating: never locks.
	for i := 0; i < 10; i++ {
		now = now.Add(20 * time.Second)
		if m.Active(tok).State != StateOK {
			t.Fatal("locked while visible")
		}
	}
	// Out of sight for less than the time: back without a prompt.
	m.Hidden(tok)
	now = now.Add(40 * time.Second)
	if m.Active(tok).State != StateOK {
		t.Fatal("locked after 40 s out of sight (limit 60)")
	}
	// Out of sight for longer.
	m.Hidden(tok)
	now = now.Add(61 * time.Second)
	if m.Check(tok).State != StateLocked {
		t.Fatal("not locked after 61 s out of sight")
	}
	if m.Active(tok).State != StateLocked {
		t.Fatal("a heartbeat unlocked it")
	}
	// A page that never said it was hidden (killed app) locks on the missing beats.
	m2, _ := open(t)
	now2 := time.Now()
	m2.now = func() time.Time { return now2 }
	tok2, _ := m2.Setup("ivan", "a-long-enough-password", "", "Phone")
	m2.mu.Lock()
	m2.lockOn(m2.sessionLocked(tok2), "pk1", 60)
	m2.mu.Unlock()
	now2 = now2.Add(2 * time.Minute)
	if m2.Check(tok2).State != StateLocked {
		t.Fatal("a device with no heartbeat for 2 minutes is not locked")
	}
}
