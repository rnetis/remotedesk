package id

import (
	"regexp"
	"testing"
)

func TestNewIDFormat(t *testing.T) {
	re := regexp.MustCompile(`^\d{3}-\d{3}-\d{3}$`)
	seen := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		v := NewID()
		if !re.MatchString(v) {
			t.Fatalf("ID %q does not match NNN-NNN-NNN", v)
		}
		seen[v] = true
	}
	// 1000 draws from a 9-digit space should essentially never collide.
	if len(seen) < 999 {
		t.Fatalf("unexpectedly many duplicate IDs: %d unique of 1000", len(seen))
	}
}

func TestNewPINFormat(t *testing.T) {
	re := regexp.MustCompile(`^\d{6}$`)
	for i := 0; i < 1000; i++ {
		v := NewPIN()
		if !re.MatchString(v) {
			t.Fatalf("PIN %q is not 6 digits", v)
		}
	}
}

func TestRandDigitsPreservesLeadingZeros(t *testing.T) {
	// Every result must be exactly n characters, even when the number is small.
	for i := 0; i < 5000; i++ {
		if got := randDigits(6); len(got) != 6 {
			t.Fatalf("randDigits(6) = %q, len %d", got, len(got))
		}
	}
}
