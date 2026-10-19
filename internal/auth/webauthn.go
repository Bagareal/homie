package auth

import (
	"crypto"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"strings"
	"time"
)

// A passkey is a key pair kept by the phone or computer and opened with a
// fingerprint, a face or the device's PIN (WebAuthn). Homie keeps the public
// half and asks the device to sign a random challenge: the biometric never
// leaves the device.

// Passkey is a credential made on one device.
type Passkey struct {
	ID      string    `json:"id"` // base64url of the credential id
	Name    string    `json:"name"`
	Created time.Time `json:"created"`
	Alg     int       `json:"alg"` // COSE: -7 (ES256) or -257 (RS256)
	X       []byte    `json:"x,omitempty"`
	Y       []byte    `json:"y,omitempty"`
	N       []byte    `json:"n,omitempty"`
	E       []byte    `json:"e,omitempty"`
	Count   uint32    `json:"count"`
}

type challenge struct {
	purpose string // "register", "unlock" or "link"
	session string
	expires time.Time
}

var b64 = base64.RawURLEncoding

// Options are what the page hands to navigator.credentials.create() or .get();
// binary values are base64url strings the page decodes.
type Options struct {
	Challenge string `json:"challenge"`
	RP        *struct {
		Name string `json:"name"`
	} `json:"rp,omitempty"`
	User *struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		DisplayName string `json:"displayName"`
	} `json:"user,omitempty"`
	PubKeyCredParams []map[string]any `json:"pubKeyCredParams,omitempty"`
	Exclude          []map[string]any `json:"excludeCredentials,omitempty"`
	Allow            []map[string]any `json:"allowCredentials,omitempty"`
	Selection        map[string]any   `json:"authenticatorSelection,omitempty"`
	Attestation      string           `json:"attestation,omitempty"`
	UserVerification string           `json:"userVerification,omitempty"`
	Timeout          int              `json:"timeout"`
}

// Credential is what the page sends back (binary values in base64url).
type Credential struct {
	ID       string `json:"id"`
	Response struct {
		ClientDataJSON    string `json:"clientDataJSON"`
		AttestationObject string `json:"attestationObject,omitempty"`
		AuthenticatorData string `json:"authenticatorData,omitempty"`
		Signature         string `json:"signature,omitempty"`
	} `json:"response"`
}

func (m *Manager) newChallenge(purpose, session string) string {
	now := m.now()
	for k, c := range m.challenges {
		if now.After(c.expires) {
			delete(m.challenges, k)
		}
	}
	if len(m.challenges) > 100 {
		m.challenges = map[string]challenge{}
	}
	ch := b64.EncodeToString(random(32))
	m.challenges[ch] = challenge{purpose: purpose, session: session, expires: now.Add(challengeTTL)}
	return ch
}

// takeChallenge consumes a challenge: it works once, for the session and purpose it was made for.
func (m *Manager) takeChallenge(ch, purpose, session string) bool {
	c, ok := m.challenges[ch]
	delete(m.challenges, ch)
	return ok && c.purpose == purpose && c.session == session && m.now().Before(c.expires)
}

// BeginRegister starts making a passkey for the signed-in, unlocked session.
func (m *Manager) BeginRegister(token string) (*Options, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.sessionLocked(token)
	if s == nil || m.f.Account == nil {
		return nil, ErrUnauth
	}
	if s.Locked {
		return nil, ErrLocked
	}
	o := &Options{
		Challenge:        m.newChallenge("register", s.ID),
		PubKeyCredParams: []map[string]any{{"type": "public-key", "alg": -7}, {"type": "public-key", "alg": -257}},
		Selection:        map[string]any{"authenticatorAttachment": "platform", "userVerification": "required", "residentKey": "discouraged"},
		Attestation:      "none",
		Timeout:          60000,
	}
	o.RP = &struct {
		Name string `json:"name"`
	}{Name: "Homie"}
	o.User = &struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		DisplayName string `json:"displayName"`
	}{ID: b64.EncodeToString(m.f.Account.UserID), Name: m.f.Account.Username, DisplayName: m.f.Account.Username}
	// A device that already has a passkey is not offered to make a second one.
	for _, p := range m.f.Passkeys {
		o.Exclude = append(o.Exclude, map[string]any{"type": "public-key", "id": p.ID})
	}
	return o, nil
}

// clientData is the part of clientDataJSON that is checked.
type clientData struct {
	Type      string `json:"type"`
	Challenge string `json:"challenge"`
	Origin    string `json:"origin"`
}

func parseClientData(raw []byte) (clientData, error) {
	var cd clientData
	if err := json.Unmarshal(raw, &cd); err != nil {
		return cd, errors.New("unreadable client data")
	}
	return cd, nil
}

// originHost checks an origin and returns its host name (what the passkey is bound to).
// A passkey needs https; plain http is only tolerated for localhost, which
// browsers treat as secure.
func originHost(origin string) (string, error) {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || u.Path != "" && u.Path != "/" {
		return "", errors.New("unexpected origin")
	}
	host := u.Hostname()
	if u.Scheme == "https" || (u.Scheme == "http" && (host == "localhost" || host == "127.0.0.1")) {
		return host, nil
	}
	return "", errors.New("a passkey needs Homie to be opened over https")
}

// checkOrigin: the origin of the page must be the one the passkeys were made for.
// The first passkey decides it (it is whatever the browser reported: only a
// signed-in user gets here).
func (m *Manager) checkOrigin(origin string) (string, error) {
	host, err := originHost(origin)
	if err != nil {
		return "", err
	}
	if m.f.Origin != "" && m.f.Origin != origin {
		return "", fmt.Errorf("passkeys are set for %s: open Homie from that address", m.f.Origin)
	}
	return host, nil
}

// FinishRegister verifies the new credential and turns the biometric lock on for the session.
func (m *Manager) FinishRegister(token string, c Credential, name string, lockAfter int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.sessionLocked(token)
	if s == nil || m.f.Account == nil {
		return ErrUnauth
	}
	if s.Locked {
		return ErrLocked
	}
	cdRaw, err := b64.DecodeString(c.Response.ClientDataJSON)
	if err != nil {
		return errors.New("unreadable client data")
	}
	cd, err := parseClientData(cdRaw)
	if err != nil {
		return err
	}
	if cd.Type != "webauthn.create" || !m.takeChallenge(cd.Challenge, "register", s.ID) {
		return errors.New("the request expired: try again")
	}
	host, err := m.checkOrigin(cd.Origin)
	if err != nil {
		return err
	}
	att, err := b64.DecodeString(c.Response.AttestationObject)
	if err != nil {
		return errors.New("unreadable attestation")
	}
	pk, err := parseAttestation(att, host)
	if err != nil {
		return err
	}
	for _, p := range m.f.Passkeys {
		if p.ID == pk.ID {
			return errors.New("this device already has a passkey")
		}
	}
	if name = strings.TrimSpace(name); name == "" {
		name = s.Label
	}
	if len(name) > 60 {
		name = name[:60]
	}
	pk.Name, pk.Created = name, m.now()
	if m.f.Origin == "" {
		m.f.Origin = cd.Origin
	}
	m.f.Passkeys = append(m.f.Passkeys, pk)
	m.lockOn(s, pk.ID, lockAfter)
	return m.save()
}

// lockOn gives the session its biometric lock (unlocked now: it was just proven).
func (m *Manager) lockOn(s *Session, passkey string, lockAfter int) {
	s.Passkey, s.LockAfter = passkey, ValidLockAfter(lockAfter)
	s.Locked, s.HiddenAt, s.LastActive = false, time.Time{}, m.now()
}

// ValidLockAfter limits the time out of sight to the choices the settings offer.
func ValidLockAfter(sec int) int {
	switch sec {
	case 60, 300, 900:
		return sec
	}
	return 60
}

// BeginAssert asks the device to prove it has one of the passkeys: to unlock a
// locked session, or to link this session to a passkey the device already has.
func (m *Manager) BeginAssert(token string, link bool) (*Options, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.sessionLocked(token)
	if s == nil || m.f.Account == nil {
		return nil, ErrUnauth
	}
	if len(m.f.Passkeys) == 0 {
		return nil, errors.New("no passkey has been made yet")
	}
	if !link && s.Passkey == "" {
		return nil, errors.New("this device has no biometric lock")
	}
	purpose := "unlock"
	if link {
		purpose = "link"
		if s.Locked {
			return nil, ErrLocked
		}
	}
	o := &Options{Challenge: m.newChallenge(purpose, s.ID), UserVerification: "required", Timeout: 60000}
	if link || s.Passkey == "" {
		for _, p := range m.f.Passkeys {
			o.Allow = append(o.Allow, map[string]any{"type": "public-key", "id": p.ID})
		}
	} else {
		o.Allow = []map[string]any{{"type": "public-key", "id": s.Passkey}}
	}
	return o, nil
}

// FinishAssert verifies a signature. For an unlock the session is unlocked; for a
// link the session gets the biometric lock with that passkey.
func (m *Manager) FinishAssert(token string, c Credential, link bool, lockAfter int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.sessionLocked(token)
	if s == nil || m.f.Account == nil {
		return ErrUnauth
	}
	purpose := "unlock"
	if link {
		purpose = "link"
	}
	cdRaw, err := b64.DecodeString(c.Response.ClientDataJSON)
	if err != nil {
		return errors.New("unreadable client data")
	}
	cd, err := parseClientData(cdRaw)
	if err != nil {
		return err
	}
	if cd.Type != "webauthn.get" || !m.takeChallenge(cd.Challenge, purpose, s.ID) {
		return errors.New("the request expired: try again")
	}
	host, err := m.checkOrigin(cd.Origin)
	if err != nil {
		return err
	}
	var key *Passkey
	for _, p := range m.f.Passkeys {
		if p.ID == c.ID {
			key = p
		}
	}
	if key == nil {
		return errors.New("this passkey is not known to Homie")
	}
	if !link && s.Passkey != "" && s.Passkey != key.ID {
		return errors.New("this passkey does not belong to this device")
	}
	authData, err := b64.DecodeString(c.Response.AuthenticatorData)
	if err != nil || len(authData) < 37 {
		return errors.New("unreadable authenticator data")
	}
	sig, err := b64.DecodeString(c.Response.Signature)
	if err != nil {
		return errors.New("unreadable signature")
	}
	rp := sha256.Sum256([]byte(host))
	if !equal(authData[:32], rp[:]) {
		return errors.New("the passkey is for another address")
	}
	if authData[32]&0x01 == 0 || authData[32]&0x04 == 0 {
		return errors.New("the device did not verify you")
	}
	hash := sha256.Sum256(cdRaw)
	if err := key.verify(append(append([]byte(nil), authData...), hash[:]...), sig); err != nil {
		return errors.New("the signature does not match")
	}
	// A counter that does not grow means the key was copied.
	count := binary.BigEndian.Uint32(authData[33:37])
	if (count != 0 || key.Count != 0) && count <= key.Count {
		return errors.New("the passkey was refused: its counter went backwards")
	}
	key.Count = count
	now := m.now()
	if link {
		m.lockOn(s, key.ID, lockAfter)
	} else {
		s.Locked, s.HiddenAt, s.LastActive = false, time.Time{}, now
	}
	return m.save()
}

// verify checks a signature over data with the public key.
func (p *Passkey) verify(data, sig []byte) error {
	sum := sha256.Sum256(data)
	switch p.Alg {
	case -7:
		pub, err := ecPublic(p.X, p.Y)
		if err != nil {
			return err
		}
		if !ecdsa.VerifyASN1(pub, sum[:], sig) {
			return errors.New("bad signature")
		}
		return nil
	case -257:
		e := new(big.Int).SetBytes(p.E)
		if !e.IsInt64() || e.Int64() < 3 || e.Int64() > 1<<31-1 {
			return errors.New("bad key")
		}
		pub := &rsa.PublicKey{N: new(big.Int).SetBytes(p.N), E: int(e.Int64())}
		return rsa.VerifyPKCS1v15(pub, crypto.SHA256, sum[:], sig)
	}
	return errors.New("unsupported key")
}

func ecPublic(x, y []byte) (*ecdsa.PublicKey, error) {
	if len(x) != 32 || len(y) != 32 {
		return nil, errors.New("bad key")
	}
	// ecdh refuses a point that is not on the curve.
	if _, err := ecdh.P256().NewPublicKey(append(append([]byte{4}, x...), y...)); err != nil {
		return nil, errors.New("bad key")
	}
	return &ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(x), Y: new(big.Int).SetBytes(y)}, nil
}

// parseAttestation reads the new credential out of an attestation object. The
// attestation statement is not checked: Homie asks for "none", and what it needs
// is the public key and proof that this origin and the user were involved.
func parseAttestation(raw []byte, host string) (*Passkey, error) {
	v, rest, err := cborDecode(raw, 0)
	if err != nil || len(rest) != 0 {
		return nil, errors.New("unreadable attestation")
	}
	top, ok := v.(map[any]any)
	if !ok {
		return nil, errors.New("unreadable attestation")
	}
	authData, ok := top["authData"].([]byte)
	if !ok || len(authData) < 55 {
		return nil, errors.New("unreadable authenticator data")
	}
	rp := sha256.Sum256([]byte(host))
	if !equal(authData[:32], rp[:]) {
		return nil, errors.New("the passkey is for another address")
	}
	flags := authData[32]
	if flags&0x01 == 0 || flags&0x04 == 0 {
		return nil, errors.New("the device did not verify you")
	}
	if flags&0x40 == 0 {
		return nil, errors.New("no credential in the answer")
	}
	idLen := int(binary.BigEndian.Uint16(authData[53:55]))
	if idLen == 0 || idLen > 1023 || len(authData) < 55+idLen {
		return nil, errors.New("unreadable credential id")
	}
	id := authData[55 : 55+idLen]
	kv, _, err := cborDecode(authData[55+idLen:], 0)
	if err != nil {
		return nil, errors.New("unreadable public key")
	}
	key, ok := kv.(map[any]any)
	if !ok {
		return nil, errors.New("unreadable public key")
	}
	pk := &Passkey{ID: b64.EncodeToString(id), Count: binary.BigEndian.Uint32(authData[33:37])}
	kty, _ := key[int64(1)].(int64)
	alg, _ := key[int64(3)].(int64)
	switch {
	case kty == 2 && alg == -7:
		crv, _ := key[int64(-1)].(int64)
		x, _ := key[int64(-2)].([]byte)
		y, _ := key[int64(-3)].([]byte)
		if crv != 1 {
			return nil, errors.New("unsupported curve")
		}
		if _, err := ecPublic(x, y); err != nil {
			return nil, err
		}
		pk.Alg, pk.X, pk.Y = -7, x, y
	case kty == 3 && alg == -257:
		n, _ := key[int64(-1)].([]byte)
		e, _ := key[int64(-2)].([]byte)
		if len(n) < 256 || len(n) > 1024 || len(e) == 0 || len(e) > 8 {
			return nil, errors.New("unsupported key")
		}
		pk.Alg, pk.N, pk.E = -257, n, e
	default:
		return nil, errors.New("this device uses a kind of key Homie does not support")
	}
	return pk, nil
}

// RemovePasskey forgets a passkey; the devices locked with it lose their lock.
func (m *Manager) RemovePasskey(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	keep := m.f.Passkeys[:0]
	found := false
	for _, p := range m.f.Passkeys {
		if p.ID == id {
			found = true
			continue
		}
		keep = append(keep, p)
	}
	if !found {
		return ErrNoSuchEntry
	}
	m.f.Passkeys = keep
	for _, s := range m.f.Sessions {
		if s.Passkey == id {
			s.Passkey, s.Locked, s.HiddenAt = "", false, time.Time{}
		}
	}
	if len(m.f.Passkeys) == 0 {
		m.f.Origin = ""
	}
	m.save()
	return nil
}

// LockOff turns the biometric lock off for the session of this cookie.
func (m *Manager) LockOff(token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.sessionLocked(token)
	if s == nil {
		return ErrUnauth
	}
	if s.Locked {
		return ErrLocked
	}
	s.Passkey, s.HiddenAt = "", time.Time{}
	m.save()
	return nil
}

// SetLockAfter changes how long the device may be out of sight before it locks.
func (m *Manager) SetLockAfter(token string, sec int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.sessionLocked(token)
	if s == nil {
		return ErrUnauth
	}
	if s.Locked {
		return ErrLocked
	}
	s.LockAfter = ValidLockAfter(sec)
	m.save()
	return nil
}

// ---- CBOR

// cborDecode reads one CBOR item (RFC 8949) and returns what follows it. It
// understands the subset WebAuthn uses: integers, byte and text strings, arrays,
// maps and the simple values; lengths that are not definite are refused.
func cborDecode(b []byte, depth int) (any, []byte, error) {
	if depth > 8 {
		return nil, nil, errors.New("cbor: too deep")
	}
	if len(b) == 0 {
		return nil, nil, errors.New("cbor: short")
	}
	major, info := b[0]>>5, b[0]&0x1f
	b = b[1:]
	var n uint64
	switch {
	case info < 24:
		n = uint64(info)
	case info >= 24 && info <= 27:
		size := 1 << (info - 24)
		if len(b) < size {
			return nil, nil, errors.New("cbor: short")
		}
		for _, c := range b[:size] {
			n = n<<8 | uint64(c)
		}
		b = b[size:]
	default:
		return nil, nil, errors.New("cbor: unsupported length")
	}
	switch major {
	case 0:
		if n > 1<<62 {
			return nil, nil, errors.New("cbor: too large")
		}
		return int64(n), b, nil
	case 1:
		if n > 1<<62 {
			return nil, nil, errors.New("cbor: too large")
		}
		return -1 - int64(n), b, nil
	case 2, 3:
		if n > uint64(len(b)) {
			return nil, nil, errors.New("cbor: short")
		}
		if major == 3 {
			return string(b[:n]), b[n:], nil
		}
		return append([]byte(nil), b[:n]...), b[n:], nil
	case 4:
		if n > 1024 {
			return nil, nil, errors.New("cbor: too long")
		}
		out := make([]any, 0, n)
		for i := uint64(0); i < n; i++ {
			var v any
			var err error
			v, b, err = cborDecode(b, depth+1)
			if err != nil {
				return nil, nil, err
			}
			out = append(out, v)
		}
		return out, b, nil
	case 5:
		if n > 1024 {
			return nil, nil, errors.New("cbor: too long")
		}
		out := make(map[any]any, n)
		for i := uint64(0); i < n; i++ {
			var k, v any
			var err error
			k, b, err = cborDecode(b, depth+1)
			if err != nil {
				return nil, nil, err
			}
			switch k.(type) {
			case int64, string:
			default:
				return nil, nil, errors.New("cbor: unsupported key")
			}
			v, b, err = cborDecode(b, depth+1)
			if err != nil {
				return nil, nil, err
			}
			out[k] = v
		}
		return out, b, nil
	case 7:
		switch info {
		case 20:
			return false, b, nil
		case 21:
			return true, b, nil
		case 22, 23:
			return nil, b, nil
		}
	}
	return nil, nil, errors.New("cbor: unsupported item")
}
