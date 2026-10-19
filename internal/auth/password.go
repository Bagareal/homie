package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"hash"
)

// hashIterations is the PBKDF2 work factor (OWASP's figure for HMAC-SHA256).
// It is stored with each account, so raising it later does not lock anyone
// out; tests lower it to stay fast.
var hashIterations = 600_000

// pbkdf2 derives a key with PBKDF2 (RFC 8018). It is here, and not in
// golang.org/x/crypto, because Homie depends on the standard library alone.
func pbkdf2(h func() hash.Hash, password, salt []byte, iter, keyLen int) []byte {
	prf := hmac.New(h, password)
	hLen := prf.Size()
	blocks := (keyLen + hLen - 1) / hLen
	out := make([]byte, 0, blocks*hLen)
	var counter [4]byte
	for b := 1; b <= blocks; b++ {
		binary.BigEndian.PutUint32(counter[:], uint32(b))
		prf.Reset()
		prf.Write(salt)
		prf.Write(counter[:])
		u := prf.Sum(nil)
		t := append([]byte(nil), u...)
		for i := 1; i < iter; i++ {
			prf.Reset()
			prf.Write(u)
			u = prf.Sum(u[:0])
			for j := range t {
				t[j] ^= u[j]
			}
		}
		out = append(out, t...)
	}
	return out[:keyLen]
}

func hashPassword(password string, salt []byte, iter int) []byte {
	return pbkdf2(sha256.New, []byte(password), salt, iter, 32)
}

// equal compares two secrets without leaking where they differ.
func equal(a, b []byte) bool { return subtle.ConstantTimeCompare(a, b) == 1 }
