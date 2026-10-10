package base

import (
	"fmt"
	"sync"
	"unicode/utf8"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// ParseFailure counts the values of one field that the converter could not
// handle as the field's declared type, and so wrote to the packet as they
// came (or, for an unserialisable JSON value, replaced).
//
// This used to be one log line per cell and nothing else: an export with a
// wrong (p,s) on a DECIMAL column, or a date column holding text, finished
// successfully, and the only trace was a log nobody read. That is how the
// MySQL export declaring every DECIMAL as (18,2) went unnoticed — the values
// still matched on a round trip, because they were passed through raw.
type ParseFailure struct {
	Field    string
	Type     string
	Count    int
	Sample   string // the first offending value, cut to sampleLen runes
	Err      string // the first error
	Replaced bool   // the value was substituted (JSON → "{}"/"[]"), not passed through
}

// ParseFailureReporter is implemented by adapters whose converter keeps a
// ParseFailure tally. A caller exporting through an adapter asks it once the
// export is done, and reports what it gets.
type ParseFailureReporter interface {
	// ParseFailures returns the failures recorded since the previous call,
	// in the order the fields first failed, and clears the tally.
	ParseFailures() []ParseFailure
}

const sampleLen = 32

// parseTally is the converter's failure record. Only the failure path takes
// the lock; a value that converts costs nothing extra.
type parseTally struct {
	mu      sync.Mutex
	byField map[string]*ParseFailure
	order   []string
}

func (t *parseTally) record(field packet.Field, value string, err error, replaced bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	f := t.byField[field.Name]
	if f == nil {
		if t.byField == nil {
			t.byField = map[string]*ParseFailure{}
		}
		f = &ParseFailure{Field: field.Name, Type: field.Type, Sample: cutRunes(value, sampleLen), Replaced: replaced}
		if err != nil {
			f.Err = err.Error()
		}
		t.byField[field.Name] = f
		t.order = append(t.order, field.Name)
	}
	f.Count++
}

func (t *parseTally) take() []ParseFailure {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.order) == 0 {
		return nil
	}
	out := make([]ParseFailure, len(t.order))
	for i, name := range t.order {
		out[i] = *t.byField[name]
	}
	t.byField, t.order = nil, nil
	return out
}

func cutRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n]) + "…"
}

// TakeParseFailures returns and clears the failures recorded by
// ConvertValueToTDTP and the JSON branches of DBValueToString.
func (c *UniversalTypeConverter) TakeParseFailures() []ParseFailure {
	return c.failures.take()
}

// String renders the failure as one warning line, the same for every caller.
func (f ParseFailure) String() string {
	what := "written to the packet unchanged"
	if f.Replaced {
		what = "replaced"
	}
	return fmt.Sprintf("%s (%s): %d value(s) did not convert and were %s — first %q: %s",
		f.Field, f.Type, f.Count, what, f.Sample, f.Err)
}
