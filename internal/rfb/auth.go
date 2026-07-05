package rfb

import (
	"crypto/des" //nolint:gosec // VNC authentication is specified to use DES.
	"crypto/subtle"
)

// vncAuthResponse computes the expected 16-byte VNC-auth response for a
// challenge, using the (bit-reversed) password as a DES key — the quirk baked
// into the RFB protocol. Passwords longer than 8 bytes are truncated.
func vncAuthResponse(challenge []byte, password string) []byte {
	key := make([]byte, 8)
	copy(key, password)
	for i := range key {
		key[i] = reverseBits(key[i])
	}
	block, err := des.NewCipher(key) //nolint:gosec
	if err != nil {
		return nil
	}
	out := make([]byte, 16)
	block.Encrypt(out[0:8], challenge[0:8])
	block.Encrypt(out[8:16], challenge[8:16])
	return out
}

// checkVNCAuth reports whether response matches the expected response for the
// challenge/password, in constant time.
func checkVNCAuth(challenge, response []byte, password string) bool {
	expected := vncAuthResponse(challenge, password)
	if expected == nil || len(response) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare(expected, response) == 1
}

func reverseBits(b byte) byte {
	var r byte
	for i := 0; i < 8; i++ {
		r = (r << 1) | (b & 1)
		b >>= 1
	}
	return r
}
