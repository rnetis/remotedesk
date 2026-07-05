package config

import (
	"os"
	"path/filepath"
	"testing"

	"remotedesk/internal/wire"

	"golang.org/x/crypto/ssh"
)

func TestParseRelayKeyEmpty(t *testing.T) {
	key, err := ParseRelayKey("  ")
	if err != nil || key != nil {
		t.Fatalf("empty value should yield (nil,nil); got key=%v err=%v", key, err)
	}
}

func TestParseRelayKeyInlineAndFile(t *testing.T) {
	signer, _, err := wire.NewSigner()
	if err != nil {
		t.Fatal(err)
	}
	line := string(ssh.MarshalAuthorizedKey(signer.PublicKey()))
	want := ssh.FingerprintSHA256(signer.PublicKey())

	// Inline authorized-keys line.
	key, err := ParseRelayKey(line)
	if err != nil {
		t.Fatalf("inline parse: %v", err)
	}
	if ssh.FingerprintSHA256(key) != want {
		t.Fatal("inline key fingerprint mismatch")
	}

	// Same key via a file path.
	path := filepath.Join(t.TempDir(), "relay.pub")
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	key, err = ParseRelayKey(path)
	if err != nil {
		t.Fatalf("file parse: %v", err)
	}
	if ssh.FingerprintSHA256(key) != want {
		t.Fatal("file key fingerprint mismatch")
	}
}

func TestParseRelayKeyInvalid(t *testing.T) {
	if _, err := ParseRelayKey("not-a-key"); err == nil {
		t.Fatal("expected error for garbage input")
	}
}

func TestLoadOrCreateSignerPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent_key")
	s1, err := LoadOrCreateSigner(path)
	if err != nil {
		t.Fatal(err)
	}
	s2, err := LoadOrCreateSigner(path)
	if err != nil {
		t.Fatal(err)
	}
	if ssh.FingerprintSHA256(s1.PublicKey()) != ssh.FingerprintSHA256(s2.PublicKey()) {
		t.Fatal("signer not stable across loads")
	}
	// Key file must be private (0600).
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("key perms = %o, want 600", info.Mode().Perm())
	}
}
