//go:build ignore

// genxsd regenerates spec_xsd_gen.go from docs/tdtp.xsd.
//
// go:embed cannot reach outside the package directory (.. is forbidden),
// so the schema travels into the binary as a generated string constant
// instead. docs/tdtp.xsd stays the single source of truth: genfresh_test.go
// fails the build if the checked-in copy drifts from it.
//
// Regenerate: go generate ./cmd/tdtp-validate/
package main

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
)

const (
	in  = "../../docs/tdtp.xsd"
	out = "spec_xsd_gen.go"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "genxsd:", err)
		os.Exit(1)
	}
}

func run() error {
	raw, err := os.ReadFile(in)
	if err != nil {
		return err
	}
	// Normalize line endings: the generator runs on Windows (CRLF checkout)
	// and CI on Linux (LF) — the embedded copy must not depend on that.
	raw = bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n"))
	if len(raw) == 0 {
		return fmt.Errorf("%s is empty", in)
	}
	code := "// Code generated from docs/tdtp.xsd by genxsd. DO NOT EDIT.\n" +
		"// Regenerate: go generate ./cmd/tdtp-validate/\n\n" +
		"package main\n\n" +
		"// tdtpXSDText is docs/tdtp.xsd verbatim. The validator compiles it\n" +
		"// once (specEngine) and validates every file against it — the schema\n" +
		"// file is the single source of truth, this constant its synced copy.\n" +
		"const tdtpXSDText = " + strconv.Quote(string(raw)) + "\n"
	//nolint:gosec // generated file inside the repo, permissions follow umask
	if err := os.WriteFile(out, []byte(code), 0o644); err != nil {
		return err
	}
	fmt.Printf("genxsd: wrote %s (%d bytes of schema)\n", out, len(raw))
	return nil
}
