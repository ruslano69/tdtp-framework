// tdtp-validate checks TDTP files for conformance to the protocol
// specification (docs/tdtp.xsd: structure; packet semantics: row shapes,
// counters, version-vs-features, xxh3 integrity) and can raise or lower
// the stamped version (applying or removing the xxh3 integrity hashes).
//
// Usage:
//
//	tdtp-validate file.tdtp.xml [more files...]
//	tdtp-validate -q file.tdtp.xml   # quiet: exit code only
//
//	tdtp-validate --stamp-integrity --output stamped.xml file.tdtp.xml
//	tdtp-validate --strip-integrity --output plain.xml file.tdtp.xml
//
// Mutation flags require exactly one input file plus --output (never
// in-place), are mutually exclusive, and refuse unsound input: only the
// version-predates error is excused, since restoring the version is the
// point. Stamps are local-only (no Mercury registration) — same as
// tdtpcli --integrity without --mercury-url.
//
// Exit code is 0 when every file validates, 1 otherwise (including IO
// errors). Encrypted (.tdtp.enc) and legacy whole-packet-encrypted files
// are opaque binary, not XML — no XSD can validate those, and neither
// can this tool: it reports them as unparsable.
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	quiet := flag.Bool("q", false, "quiet: report by exit code only")
	maxMB := flag.Int("max-mb", 256, "reject files larger than this (XML entity expansion guard)")
	stamp := flag.Bool("stamp-integrity", false, "compute xxh3 hashes, raise version to 1.4, write to --output")
	strip := flag.Bool("strip-integrity", false, "remove xxh3 hashes, lower version to remaining features, write to --output")
	output := flag.String("output", "", "output file for --stamp/--strip-integrity (required with either)")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: tdtp-validate [-q] [--max-mb N] file.tdtp.xml [...]")
		fmt.Fprintln(os.Stderr, "       tdtp-validate --stamp-integrity|--strip-integrity --output out.xml file.tdtp.xml")
		flag.PrintDefaults()
	}
	flag.Parse()

	files := flag.Args()
	if len(files) == 0 {
		flag.Usage()
		return fmt.Errorf("no input files")
	}
	if *stamp && *strip {
		return fmt.Errorf("--stamp-integrity and --strip-integrity are mutually exclusive")
	}
	if (*stamp || *strip) && (len(files) != 1 || *output == "") {
		return fmt.Errorf("mutation flags need exactly one input file plus --output")
	}

	failed := false
	if *stamp || *strip {
		rep, err := processFile(files[0], *output, *stamp, *strip, *maxMB)
		if !*quiet {
			fmt.Print(rep.Format())
		}
		return err
	}
	for _, f := range files {
		if !validateFile(f, *quiet, *maxMB) {
			failed = true
		}
	}
	if failed {
		return fmt.Errorf("validation failed")
	}
	return nil
}

// validateFile returns true when the file conforms.
func validateFile(path string, quiet bool, maxMB int) bool {
	data, err := readCapped(path, maxMB)
	if err != nil {
		if !quiet {
			fmt.Printf("%s: INVALID\n  x %v\n", path, err)
		}
		return false
	}
	rep := Validate(data, path)
	if !quiet {
		fmt.Print(rep.Format())
	}
	return rep.Valid()
}

// readCapped reads the file, refusing absurd sizes up front: encoding/xml
// expands internal entities without a billion-laughs guard, so a validator
// handed a hostile file should not be the one to find out.
func readCapped(path string, maxMB int) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if maxMB > 0 && info.Size() > int64(maxMB)<<20 {
		return nil, fmt.Errorf("file is %d bytes, over the --max-mb %d limit", info.Size(), maxMB)
	}
	return os.ReadFile(path)
}

// writeFile stores mutated output with owner-only permissions, matching
// the rest of the framework (export paths use 0o600 for packet files).
func writeFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o600)
}
