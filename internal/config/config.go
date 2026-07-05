// Package config handles per-agent key material and settings persisted under
// the user's OS config directory.
package config

import (
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"

	"remotedesk/internal/wire"

	"golang.org/x/crypto/ssh"
)

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
