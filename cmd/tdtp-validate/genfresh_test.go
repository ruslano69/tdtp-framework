package main

// genfresh_test.go — the embedded schema must equal docs/tdtp.xsd.
//
// tdtpXSDText (spec_xsd_gen.go) is a synced copy, not a fork: this test
// compares it byte-for-byte against the file and tells exactly how to
// refresh it. Regeneration is deterministic (verbatim copy), so any diff
// means someone edited one side without the other.

import (
	"bytes"
	"os"
	"testing"
)

func TestEmbeddedSchemaMatchesFile(t *testing.T) {
	raw, err := os.ReadFile("../../docs/tdtp.xsd")
	if err != nil {
		t.Fatalf("read docs/tdtp.xsd: %v", err)
	}
	// Same CRLF→LF normalization as genxsd: checkout line endings must not
	// count as drift.
	raw = bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n"))
	if string(raw) != tdtpXSDText {
		t.Fatalf("spec_xsd_gen.go drifted from docs/tdtp.xsd — run: go generate ./cmd/tdtp-validate/")
	}
	if len(tdtpXSD) != len(raw) {
		t.Fatalf("tdtpXSD has %d bytes, docs/tdtp.xsd has %d", len(tdtpXSD), len(raw))
	}
}
