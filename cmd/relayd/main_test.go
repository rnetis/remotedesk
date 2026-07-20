package main

import (
	"os"
	"path/filepath"
	"testing"

	"remotedesk/internal/wire"

	"golang.org/x/crypto/ssh"
)

func TestLoadAuthorizedKeys(t *testing.T) {
	allowed, _, _ := wire.NewSigner()
	other, _, _ := wire.NewSigner()

	path := filepath.Join(t.TempDir(), "authorized_keys")
	line := ssh.MarshalAuthorizedKey(allowed.PublicKey())
	// Include a comment and a blank line to mirror real files.
	content := append([]byte("# remotedesk agents\n\n"), line...)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	authorize, err := loadAuthorizedKeys(path)
	if err != nil {
		t.Fatalf("loadAuthorizedKeys: %v", err)
	}
	if !authorize(allowed.PublicKey()) {
		t.Error("listed key should be authorized")
	}
	if authorize(other.PublicKey()) {
		t.Error("unlisted key should not be authorized")
	}
}

func TestLoadAuthorizedKeysErrors(t *testing.T) {
	if _, err := loadAuthorizedKeys(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("expected error for missing file")
	}

	empty := filepath.Join(t.TempDir(), "empty")
	if err := os.WriteFile(empty, []byte("# no keys here\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadAuthorizedKeys(empty); err == nil {
		t.Error("expected error for file with no keys")
	}
}
