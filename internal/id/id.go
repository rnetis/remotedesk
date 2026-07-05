// Package id generates the human-friendly connection IDs and one-time PINs
// shown in the host tray, in the style of AnyDesk/TeamViewer.
package id

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

// NewID returns a 9-digit connection ID formatted as "NNN-NNN-NNN".
func NewID() string {
	n := randDigits(9)
	return fmt.Sprintf("%s-%s-%s", n[0:3], n[3:6], n[6:9])
}

// NewPIN returns a 6-digit one-time session PIN.
func NewPIN() string {
	return randDigits(6)
}

// randDigits returns a cryptographically-random string of n decimal digits,
// zero-padded (so it never loses leading zeros).
func randDigits(n int) string {
	max := big.NewInt(1)
	ten := big.NewInt(10)
	for i := 0; i < n; i++ {
		max.Mul(max, ten)
	}
	v, err := rand.Int(rand.Reader, max)
	if err != nil {
		// rand.Reader failing is catastrophic; there is no safe fallback for
		// security-relevant IDs, so surface it loudly.
		panic("id: crypto/rand unavailable: " + err.Error())
	}
	return fmt.Sprintf("%0*d", n, v)
}
