//go:build tdtp_devkey

package license

// Development builds: `go build -tags tdtp_devkey ./cmd/...` accepts licenses
// signed with the local, throwaway development key instead of the vendor key,
// so offline end-to-end runs (licensed adapters such as oracle, --enc) work
// without the vendor's offline signing key.
//
// Release builds never set the tag and keep vendorPublicKeyPEM from
// pubkey.go. This replaces editing pubkey.go by hand with a "DO NOT COMMIT"
// note, where one careless `git add` would have shipped a release accepting
// development licenses. Only the PUBLIC half lives here; the private half
// stays outside Git, like vendor.priv.
const devPublicKeyPEM = `-----BEGIN PUBLIC KEY-----
MCowBQYDK2VwAyEAd0JW1t1Bi0MSUuM35Wi764fMNUgpPUcJvsU4xgmAAp8=
-----END PUBLIC KEY-----`

func init() { vendorPublicKeyPEM = devPublicKeyPEM }
