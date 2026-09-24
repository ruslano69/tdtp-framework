// tdtp-validate checks TDTP files for conformance to the protocol
// specification. Thin frontend over pkg/validate (shared with tdtpcli_v2) —
// all logic lives there; this main only parses flags and prints reports.
//
// Usage:
//
//	tdtp-validate file.tdtp.xml [more files...]
//	tdtp-validate -q file.tdtp.xml   # quiet: exit code only
//
//	tdtp-validate --stamp-integrity --output stamped.xml file.tdtp.xml
//	tdtp-validate --strip-integrity --output plain.xml file.tdtp.xml
//
// Exit code is 0 when every file validates, 1 otherwise.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/ruslano69/tdtp-framework/pkg/validate"
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

	if *stamp || *strip {
		rep, err := validate.ProcessFile(files[0], *output, *stamp, *strip, *maxMB)
		if !*quiet {
			fmt.Print(rep.Format())
		}
		return err
	}

	// Pure validation reads through ProcessFile too (no output written
	// without mutation flags) so both frontends share one code path.
	failed := false
	for _, f := range files {
		rep, err := validate.ProcessFile(f, "", false, false, *maxMB)
		if !*quiet {
			fmt.Print(rep.Format())
		}
		if err != nil || !rep.Valid() {
			failed = true
		}
	}
	if failed {
		return fmt.Errorf("validation failed")
	}
	return nil
}
