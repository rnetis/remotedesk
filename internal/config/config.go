// Package config handles per-agent key material and settings persisted under
// the user's OS config directory.
package config

import (
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"remotedesk/internal/wire"

	"golang.org/x/crypto/ssh"
)

// ParseRelayKey resolves a pinned relay host key from a flag value. The value
// may be an inline authorized-keys line ("ssh-ed25519 AAAA...") or a path to a
// file containing one. An empty value returns (nil, nil) — no pinning.
func ParseRelayKey(value string) (ssh.PublicKey, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	data := []byte(value)
	if !strings.HasPrefix(value, "ssh-") && !strings.HasPrefix(value, "ecdsa-") {
		// Treat it as a path.
		b, err := os.ReadFile(value)
		if err != nil {
			return nil, fmt.Errorf("read relay key %s: %w", value, err)
		}
		data = b
	}
	key, _, _, _, err := ssh.ParseAuthorizedKey(data)
	if err != nil {
		return nil, fmt.Errorf("parse relay key: %w", err)
	}
	return key, nil
}

// Dir returns (and creates) the remotedesk config directory.
func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "remotedesk")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// LoadOrCreateSigner loads an ed25519 SSH key from path, creating and
// persisting a new one (0600) if the file does not exist.
func LoadOrCreateSigner(path string) (ssh.Signer, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		signer, perr := ssh.ParsePrivateKey(data)
		if perr != nil {
			return nil, fmt.Errorf("parse key %s: %w", path, perr)
		}
		return signer, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}

	// Generate, persist, and return a fresh key.
	signer, priv, err := wire.NewSigner()
	if err != nil {
		return nil, err
	}
	block, err := ssh.MarshalPrivateKey(priv, "remotedesk")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		return nil, err
	}
	return signer, nil
}
