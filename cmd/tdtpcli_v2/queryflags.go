package main

import (
	"fmt"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/cliquery"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/core/tdtql"
	"github.com/spf13/pflag"
)

// queryflags.go — the TDTQL flag bundle shared by every command that
// filters/projects/sorts file content (--to-csv, --to-xlsx, ...).
// One definition, one help text, one builder — mirrors how v1's main
// builds the query (BuildTDTQLQuery + --fields injection).

// stringList is a repeatable string flag: --where A --where B.
type stringList []string

func (s *stringList) String() string     { return strings.Join(*s, ", ") }
func (s *stringList) Set(v string) error { *s = append(*s, v); return nil }

// Type satisfies pflag.Value (used in help/usage rendering).
func (s *stringList) Type() string { return "stringSlice" }

// queryFlags holds WHERE/ORDER BY/LIMIT/OFFSET/FIELDS values.
type queryFlags struct {
	wheres  stringList
	orderBy string
	limit   int
	offset  int
	fields  string
}

// addQueryFlags registers the bundle on fs with v1-identical flag names,
// shorthands and defaults.
func addQueryFlags(fs *pflag.FlagSet, q *queryFlags) {
	fs.VarP(&q.wheres, "where", "w", "TDTQL WHERE clause; repeatable, combined with AND")
	fs.StringVar(&q.orderBy, "order-by", "", "ORDER BY clause (e.g. 'name ASC, age DESC')")
	fs.IntVarP(&q.limit, "limit", "l", 0, "LIMIT rows: positive = first N, negative = last N (tail)")
	fs.IntVar(&q.offset, "offset", 0, "OFFSET rows to skip")
	fs.StringVar(&q.fields, "fields", "", "column projection: comma-separated list")
}

// build constructs the *packet.Query exactly the way v1 does.
func (q *queryFlags) build() (*packet.Query, error) {
	query, err := cliquery.BuildQuery([]string(q.wheres), q.orderBy, q.limit, q.offset)
	if err != nil {
		return nil, err
	}
	if q.fields != "" {
		if query == nil {
			query = packet.NewQuery()
		}
		query.Fields = tdtql.SplitFieldList(q.fields)
	}
	return query, nil
}

// fieldsList splits the --fields projection the way v1 does
// (splitCommaSeparated → tdtql.SplitFieldList, bracket-quoting aware).
func (q *queryFlags) fieldsList() []string {
	if q.fields == "" {
		return nil
	}
	return tdtql.SplitFieldList(q.fields)
}

// parseExpectVars parses repeatable name=value flags (same grammar as v1:
// name non-empty, value may be empty).
func parseExpectVars(vars []string) (map[string]string, error) {
	out := map[string]string{}
	for _, s := range vars {
		eq := strings.IndexByte(s, '=')
		if eq < 1 {
			return nil, fmt.Errorf("--expect-var requires name=value format, got: %s", s)
		}
		out[s[:eq]] = s[eq+1:]
	}
	return out, nil
}

// splitFields splits any comma-separated flag value the v1 way.
func splitFields(s string) []string {
	if s == "" {
		return nil
	}
	return tdtql.SplitFieldList(s)
}

// outputFile mirrors v1's determineOutputFile: explicit --output wins,
// otherwise <input>.<ext> next to the source.
func outputFile(output, input, ext string) string {
	if output != "" {
		return output
	}
	if strings.HasSuffix(input, "."+ext) {
		return input
	}
	return input + "." + ext
}
