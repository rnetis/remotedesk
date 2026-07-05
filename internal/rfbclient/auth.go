package rfbclient

import "crypto/des" //nolint:gosec // VNC auth is specified to use DES.

// vncAuthResponse computes the DES challenge response with the bit-reversed
// password as key, per the RFB VNC-auth scheme.
func vncAuthResponse(challenge []byte, password string) []byte {
	key := make([]byte, 8)
	copy(key, password)
	for i := range key {
		key[i] = reverseBits(key[i])
	}
	block, err := des.NewCipher(key) //nolint:gosec
	if err != nil {
		return make([]byte, 16)
	}
	out := make([]byte, 16)
	block.Encrypt(out[0:8], challenge[0:8])
	block.Encrypt(out[8:16], challenge[8:16])
	return out
}

func reverseBits(b byte) byte {
	var r byte
	for i := 0; i < 8; i++ {
		r = (r << 1) | (b & 1)
		b >>= 1
	}
	return r
}
