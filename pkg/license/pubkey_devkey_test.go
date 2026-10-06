//go:build tdtp_devkey

package license

import (
	"bytes"
	"testing"
)

// With the tag, the embedded key is the development key.
func TestDevKeyBuildUsesDevKey(t *testing.T) {
	got, err := VendorPublicKey()
	if err != nil {
		t.Fatal(err)
	}
	want, err := parsePKIXEd25519(devPublicKeyPEM)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("tdtp_devkey build does not embed the development key")
	}
}
