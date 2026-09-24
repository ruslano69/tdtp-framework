package commands

import (
	"fmt"
	"io"
)

// reportf/reportln print report lines, ignoring write errors.
//
// Report streams (stdout, in-memory buffers) have no recovery path a
// caller could act on — a short write here means a broken pipe or a full
// disk, and failing the whole command over the progress line (while the
// data itself is fine) would be the wrong trade. This matches the
// previous Printf behavior, which errcheck ignored by default; the
// writer-parameterized Fprintf form needs the explicit marker instead.
//
//nolint:errcheck // report output is best-effort by design
func reportf(w io.Writer, format string, args ...any) {
	fmt.Fprintf(w, format, args...)
}

//nolint:errcheck // report output is best-effort by design, see reportf
func reportln(w io.Writer, args ...any) {
	fmt.Fprintln(w, args...)
}
