// Package envkey manages the environment's Ed25519 identity keypair.
//
// In production this should be TPM-sealed: the private key never leaves the chip
// and can only be used on the hardware where it was generated. This implementation
// is a file-based stub that provides the same interface — swap the backend for a
// real TPM (go-tpm) without changing callers.
//
// The keypair is generated once on first run and stored at keyPath.
// env_id_pub (the public key) is the hardware env-ID presented to the CA at enrollment.
// The private key signs challenge nonces — proof that the env is the original hardware.
package envkey

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	pubFile  = "env_id.pub"
	privFile = "env_id.key" // in prod: TPM-sealed; here: 0600 file
)

// Identity holds the environment's Ed25519 keypair.
type Identity struct {
	pub  ed25519.PublicKey
	priv ed25519.PrivateKey
}

// Load loads (or generates on first run) the env keypair from dir.
// In TPM mode: replace with tpm2.Load / tpm2.CreateKey.
func Load(dir string) (*Identity, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("envkey: mkdir %s: %w", dir, err)
	}

	privPath := filepath.Join(dir, privFile)
	pubPath := filepath.Join(dir, pubFile)

	_, errPriv := os.Stat(privPath)
	_, errPub := os.Stat(pubPath)

	if errors.Is(errPriv, os.ErrNotExist) || errors.Is(errPub, os.ErrNotExist) {
		return generate(privPath, pubPath)
	}

	return load(privPath, pubPath)
}

// PublicKey returns the env-ID public key (sent to CA at enrollment).
func (id *Identity) PublicKey() ed25519.PublicKey {
	return id.pub
}

// Sign signs msg with the env private key (TPM-stub: direct sign).
// In TPM mode: replace with tpm2.Sign(handle, msg).
func (id *Identity) Sign(msg []byte) ([]byte, error) {
	sig := ed25519.Sign(id.priv, msg)
	return sig, nil
}

// Verify checks that sig over msg was produced by this identity's private key.
func (id *Identity) Verify(msg, sig []byte) bool {
	return ed25519.Verify(id.pub, msg, sig)
}

// generate creates a new Ed25519 keypair and persists it.
func generate(privPath, pubPath string) (*Identity, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("envkey: generate: %w", err)
	}

	// Write private key (0600 — only owner can read)
	privPEM := pem.EncodeToMemory(&pem.Block{Type: "ED25519 PRIVATE KEY", Bytes: priv})
	if err := os.WriteFile(privPath, privPEM, 0o600); err != nil {
		return nil, fmt.Errorf("envkey: write priv: %w", err)
	}

	// Write public key (0644)
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "ED25519 PUBLIC KEY", Bytes: pub})
	if err := os.WriteFile(pubPath, pubPEM, 0o644); err != nil {
		return nil, fmt.Errorf("envkey: write pub: %w", err)
	}

	return &Identity{pub: pub, priv: priv}, nil
}

// load reads an existing keypair from disk.
func load(privPath, pubPath string) (*Identity, error) {
	privPEM, err := os.ReadFile(privPath)
	if err != nil {
		return nil, fmt.Errorf("envkey: read priv: %w", err)
	}
	pubPEM, err := os.ReadFile(pubPath)
	if err != nil {
		return nil, fmt.Errorf("envkey: read pub: %w", err)
	}

	privBlock, _ := pem.Decode(privPEM)
	if privBlock == nil {
		return nil, fmt.Errorf("envkey: invalid priv PEM")
	}
	pubBlock, _ := pem.Decode(pubPEM)
	if pubBlock == nil {
		return nil, fmt.Errorf("envkey: invalid pub PEM")
	}

	return &Identity{
		pub:  ed25519.PublicKey(pubBlock.Bytes),
		priv: ed25519.PrivateKey(privBlock.Bytes),
	}, nil
}
