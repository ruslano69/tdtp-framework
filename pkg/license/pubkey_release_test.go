//go:build !tdtp_devkey

package license

import (
	"bytes"
	"testing"
)

// A release build must never embed the development key. Before the
// tdtp_devkey tag, the development key was pasted into pubkey.go by hand
// with a "DO NOT COMMIT" note; if that edit is ever committed, this fails
// in CI instead of shipping a binary that accepts development licenses.
func TestReleaseBuildDoesNotEmbedDevKey(t *testing.T) {
	const devKey = `-----BEGIN PUBLIC KEY-----
MCowBQYDK2VwAyEAd0JW1t1Bi0MSUuM35Wi764fMNUgpPUcJvsU4xgmAAp8=
-----END PUBLIC KEY-----`
	dev, err := parsePKIXEd25519(devKey)
	if err != nil {
		t.Fatal(err)
	}
	got, err := VendorPublicKey()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(got, dev) {
		t.Fatal("pubkey.go embeds the development key; use -tags tdtp_devkey for dev builds instead")
	}
}
