package license

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"sync"
)

// vendorPublicKeyPEM is the TDTP vendor's Ed25519 public key in PKIX PEM form.
//
// Licenses (tdtp.lic) are signed by the matching vendor private key, which is
// kept offline/HSM and NEVER ships with the framework. tdtpcli verifies every
// license against this embedded key — no network call, fully offline.
//
// To rotate: replace this block with a new public key and re-sign all licenses.
// Vendor builds may override this at build time via -ldflags.
var vendorPublicKeyPEM = `-----BEGIN PUBLIC KEY-----
MCowBQYDK2VwAyEAQN8aC7mWhGV33DFE3gri/bC9YEqfrlj3adKJ/Lc6vXQ=
-----END PUBLIC KEY-----`

var (
	vendorKeyOnce sync.Once
	vendorKey     ed25519.PublicKey
	vendorKeyErr  error
)

// VendorPublicKey returns the embedded vendor Ed25519 public key.
// Parsed once and cached.
func VendorPublicKey() (ed25519.PublicKey, error) {
	vendorKeyOnce.Do(func() {
		vendorKey, vendorKeyErr = parsePKIXEd25519(vendorPublicKeyPEM)
	})
	return vendorKey, vendorKeyErr
}

// parsePKIXEd25519 parses a PKIX (SubjectPublicKeyInfo) PEM into an Ed25519 key.
// This is the format OpenSSL produces with `openssl pkey -pubout`.
func parsePKIXEd25519(pemStr string) (ed25519.PublicKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, fmt.Errorf("license: invalid public key PEM")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("license: parse public key: %w", err)
	}
	edPub, ok := pub.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("license: public key is not Ed25519")
	}
	return edPub, nil
}
