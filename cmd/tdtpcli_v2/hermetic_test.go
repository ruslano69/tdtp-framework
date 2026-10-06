package main

import (
	"os"
	"testing"
)

// TestMain keeps the developer's license out of the tests. ResolveLicense
// reads TDTP_LICENSE, and a value set user-wide (for another tool's license,
// signed by another key) made every license-resolving test fail here with
// "signature verification failed" — for v2 an invalid license is fatal, so
// every command exited 1. Tests that need a license inject their own.
func TestMain(m *testing.M) {
	_ = os.Unsetenv("TDTP_LICENSE")
	os.Exit(m.Run())
}
