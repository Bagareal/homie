package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"strings"
	"testing"
)

// ---- a tiny CBOR writer and a fake authenticator, enough to play a device

func cborUint(major byte, n uint64) []byte {
	switch {
	case n < 24:
		return []byte{major<<5 | byte(n)}
	case n < 256:
		return []byte{major<<5 | 24, byte(n)}
	default:
		return []byte{major<<5 | 25, byte(n >> 8), byte(n)}
	}
}

func cborInt(n int64) []byte {
	if n >= 0 {
		return cborUint(0, uint64(n))
	}
	return cborUint(1, uint64(-1-n))
}
func cborBytes(b []byte) []byte { return append(cborUint(2, uint64(len(b))), b...) }
func cborText(s string) []byte  { return append(cborUint(3, uint64(len(s))), s...) }
func cborMap(pairs ...[]byte) []byte {
	out := cborUint(5, uint64(len(pairs)/2))
	for _, p := range pairs {
		out = append(out, p...)
	}
	return out
}

type device struct {
	key   *ecdsa.PrivateKey
	id    []byte
	count uint32
}

func newDevice(t *testing.T) *device {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return &device{key: k, id: []byte("credential-id-0123456789")}
}

func (d *device) clientData(typ, challenge, origin string) []byte {
	b, _ := json.Marshal(map[string]any{"type": typ, "challenge": challenge, "origin": origin})
	return b
}

func (d *device) create(host, challenge, origin string, flags byte) Credential {
	x := d.key.PublicKey.X.FillBytes(make([]byte, 32))
	y := d.key.PublicKey.Y.FillBytes(make([]byte, 32))
	cose := cborMap(cborInt(1), cborInt(2), cborInt(3), cborInt(-7), cborInt(-1), cborInt(1), cborInt(-2), cborBytes(x), cborInt(-3), cborBytes(y))
	rp := sha256.Sum256([]byte(host))
	ad := append([]byte(nil), rp[:]...)
	ad = append(ad, flags, 0, 0, 0, 0)
	ad = append(ad, make([]byte, 16)...) // aaguid
	ad = append(ad, byte(len(d.id)>>8), byte(len(d.id)))
	ad = append(ad, d.id...)
	ad = append(ad, cose...)
	att := cborMap(cborText("fmt"), cborText("none"), cborText("attStmt"), cborMap(), cborText("authData"), cborBytes(ad))
	var c Credential
	c.ID = b64.EncodeToString(d.id)
	c.Response.ClientDataJSON = b64.EncodeToString(d.clientData("webauthn.create", challenge, origin))
	c.Response.AttestationObject = b64.EncodeToString(att)
	return c
}

func (d *device) get(host, challenge, origin string, flags byte) Credential {
	d.count++
	rp := sha256.Sum256([]byte(host))
	ad := append([]byte(nil), rp[:]...)
	ad = append(ad, flags)
	ad = binary.BigEndian.AppendUint32(ad, d.count)
	cd := d.clientData("webauthn.get", challenge, origin)
	h := sha256.Sum256(cd)
	sum := sha256.Sum256(append(append([]byte(nil), ad...), h[:]...))
	sig, err := ecdsa.SignASN1(rand.Reader, d.key, sum[:])
	if err != nil {
		panic(err)
	}
	var c Credential
	c.ID = b64.EncodeToString(d.id)
	c.Response.ClientDataJSON = b64.EncodeToString(cd)
	c.Response.AuthenticatorData = b64.EncodeToString(ad)
	c.Response.Signature = b64.EncodeToString(sig)
	return c
}

const (
	upuv    = 0x01 | 0x04
	upuvat  = 0x01 | 0x04 | 0x40
	origin  = "https://homie.example.com"
	hostIDs = "homie.example.com"
)

func signedIn(t *testing.T) (*Manager, string) {
	m, _ := open(t)
	tok, err := m.Setup("ivan", "a-long-enough-password", "", "Phone")
	if err != nil {
		t.Fatal(err)
	}
	return m, tok
}

func TestPasskeyRegisterAndUnlock(t *testing.T) {
	m, tok := signedIn(t)
	d := newDevice(t)

	o, err := m.BeginRegister(tok)
	if err != nil {
		t.Fatal(err)
	}
	if o.Attestation != "none" || o.Selection["userVerification"] != "required" {
		t.Fatalf("options: %+v", o)
	}
	if err := m.FinishRegister(tok, d.create(hostIDs, o.Challenge, origin, upuvat), "My phone", 300); err != nil {
		t.Fatal(err)
	}
	// The challenge works once.
	if err := m.FinishRegister(tok, d.create(hostIDs, o.Challenge, origin, upuvat), "again", 60); err == nil {
		t.Fatal("a challenge was used twice")
	}
	devs, keys := m.Devices(tok)
	if len(keys) != 1 || keys[0].Name != "My phone" || !devs[0].Lock {
		t.Fatalf("devices %+v keys %+v", devs, keys)
	}

	// Lock it, then unlock with the biometric.
	m.mu.Lock()
	m.sessionLocked(tok).Locked = true
	m.mu.Unlock()
	if m.Check(tok).State != StateLocked {
		t.Fatal("not locked")
	}
	a, err := m.BeginAssert(tok, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Allow) != 1 {
		t.Fatalf("allowed credentials: %v", a.Allow)
	}
	if err := m.FinishAssert(tok, d.get(hostIDs, a.Challenge, origin, upuv), false, 0); err != nil {
		t.Fatal(err)
	}
	if m.Check(tok).State != StateOK {
		t.Fatal("still locked after the biometric")
	}
}

func TestPasskeyRefusals(t *testing.T) {
	m, tok := signedIn(t)
	d := newDevice(t)
	reg := func(host, origin string, flags byte) error {
		o, _ := m.BeginRegister(tok)
		return m.FinishRegister(tok, d.create(host, o.Challenge, origin, flags), "x", 60)
	}
	if err := reg(hostIDs, "http://homie.example.com", upuvat); err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("plain http origin: %v", err)
	}
	if err := reg("other.example.com", origin, upuvat); err == nil {
		t.Fatal("a passkey made for another address was accepted")
	}
	if err := reg(hostIDs, origin, 0x01|0x40); err == nil {
		t.Fatal("a registration without user verification was accepted")
	}
	if err := reg(hostIDs, origin, upuvat); err != nil {
		t.Fatalf("a good registration: %v", err)
	}

	// Assertions.
	lock := func() {
		m.mu.Lock()
		m.sessionLocked(tok).Locked = true
		m.mu.Unlock()
	}
	try := func(mut func(c *Credential), host, org string, flags byte) error {
		lock()
		a, err := m.BeginAssert(tok, false)
		if err != nil {
			t.Fatal(err)
		}
		c := d.get(host, a.Challenge, org, flags)
		if mut != nil {
			mut(&c)
		}
		return m.FinishAssert(tok, c, false, 0)
	}
	if err := try(nil, hostIDs, "https://evil.example.org", upuv); err == nil {
		t.Fatal("an assertion from another origin was accepted")
	}
	if err := try(nil, "evil.example.org", origin, upuv); err == nil {
		t.Fatal("an assertion for another rp id was accepted")
	}
	if err := try(nil, hostIDs, origin, 0x01); err == nil {
		t.Fatal("an assertion without user verification was accepted")
	}
	if err := try(func(c *Credential) { c.Response.Signature = b64.EncodeToString(make([]byte, 70)) }, hostIDs, origin, upuv); err == nil {
		t.Fatal("a bad signature was accepted")
	}
	other := newDevice(t)
	other.id = d.id
	lock()
	a, _ := m.BeginAssert(tok, false)
	if err := m.FinishAssert(tok, other.get(hostIDs, a.Challenge, origin, upuv), false, 0); err == nil {
		t.Fatal("a signature from another key was accepted")
	}
	if err := try(nil, hostIDs, origin, upuv); err != nil {
		t.Fatalf("a good assertion after the bad ones: %v", err)
	}
	// A counter that goes back is a cloned key.
	d.count = 0
	if err := try(nil, hostIDs, origin, upuv); err == nil {
		t.Fatal("a counter that went backwards was accepted")
	}
	// The same challenge cannot be replayed.
	d.count = 100
	lock()
	a, _ = m.BeginAssert(tok, false)
	c := d.get(hostIDs, a.Challenge, origin, upuv)
	if err := m.FinishAssert(tok, c, false, 0); err != nil {
		t.Fatal(err)
	}
	lock()
	if err := m.FinishAssert(tok, c, false, 0); err == nil {
		t.Fatal("a replayed assertion was accepted")
	}
}

func TestPasskeyOriginIsPinned(t *testing.T) {
	m, tok := signedIn(t)
	d := newDevice(t)
	o, _ := m.BeginRegister(tok)
	if err := m.FinishRegister(tok, d.create(hostIDs, o.Challenge, origin, upuvat), "x", 60); err != nil {
		t.Fatal(err)
	}
	d2 := newDevice(t)
	d2.id = []byte("another-credential-id-xx")
	o, _ = m.BeginRegister(tok)
	err := m.FinishRegister(tok, d2.create("lan.example.com", o.Challenge, "https://lan.example.com", upuvat), "y", 60)
	if err == nil || !strings.Contains(err.Error(), origin) {
		t.Fatalf("a second address was accepted, or the error does not say where to go: %v", err)
	}
	// Removing the last passkey frees the address.
	_, keys := m.Devices(tok)
	if err := m.RemovePasskey(keys[0].ID); err != nil {
		t.Fatal(err)
	}
	o, _ = m.BeginRegister(tok)
	if err := m.FinishRegister(tok, d2.create("lan.example.com", o.Challenge, "https://lan.example.com", upuvat), "y", 60); err != nil {
		t.Fatalf("after removing the passkeys: %v", err)
	}
}

func TestLinkExistingPasskey(t *testing.T) {
	m, tok := signedIn(t)
	d := newDevice(t)
	o, _ := m.BeginRegister(tok)
	if err := m.FinishRegister(tok, d.create(hostIDs, o.Challenge, origin, upuvat), "x", 60); err != nil {
		t.Fatal(err)
	}
	// The same phone signs in again (a new session) and takes its passkey back.
	tok2, err := m.Login("ivan", "a-long-enough-password", "Phone again")
	if err != nil {
		t.Fatal(err)
	}
	o2, err := m.BeginRegister(tok2)
	if err != nil || len(o2.Exclude) != 1 {
		t.Fatalf("the device is not told it already has a passkey: %v %+v", err, o2)
	}
	a, err := m.BeginAssert(tok2, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.FinishAssert(tok2, d.get(hostIDs, a.Challenge, origin, upuv), true, 900); err != nil {
		t.Fatal(err)
	}
	devs, _ := m.Devices(tok2)
	if !devs[0].Current || !devs[0].Lock {
		t.Fatalf("the session was not linked: %+v", devs)
	}
	// A session with no lock has nothing to unlock.
	tok3, _ := m.Login("ivan", "a-long-enough-password", "Desktop")
	if _, err := m.BeginAssert(tok3, false); err == nil {
		t.Fatal("a session without a lock can ask to be unlocked")
	}
}

func TestCBORLimits(t *testing.T) {
	if _, _, err := cborDecode([]byte{0x9f, 0x01, 0xff}, 0); err == nil {
		t.Fatal("an indefinite array was accepted")
	}
	deep := make([]byte, 20)
	for i := range deep {
		deep[i] = 0x81
	}
	if _, _, err := cborDecode(deep, 0); err == nil {
		t.Fatal("a deeply nested item was accepted")
	}
	if _, _, err := cborDecode([]byte{0x5a, 0xff, 0xff, 0xff, 0xff}, 0); err == nil {
		t.Fatal("a huge byte string with no data was accepted")
	}
	v, rest, err := cborDecode(append(cborMap(cborInt(1), cborInt(-7)), 0xaa), 0)
	if err != nil || len(rest) != 1 || v.(map[any]any)[int64(1)] != int64(-7) {
		t.Fatalf("%v %v %v", v, rest, err)
	}
}
