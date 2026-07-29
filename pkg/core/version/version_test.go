package version

import (
	"regexp"
	"testing"
)

// TestVersionIsSet exists because this file has been truncated to zero bytes
// twice by the same bad edit — `open(p,'w').write(open(p).read()...)`, where
// the outer open empties the file before the inner read runs. Both times it
// was committed and pushed, and both times the whole binary stopped building.
//
// A compile error is a loud failure, but only for whoever builds next. This
// makes the version itself the thing under test, so a truncated or malformed
// file fails here rather than in someone else's checkout.
func TestVersionIsSet(t *testing.T) {
	if Version == "" {
		t.Fatal("Version is empty — version.go was likely truncated")
	}
	if !regexp.MustCompile(`^\d+\.\d+\.\d+(-[\w.]+)?$`).MatchString(Version) {
		t.Errorf("Version = %q, want semver", Version)
	}
}
