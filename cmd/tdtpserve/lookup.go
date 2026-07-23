package main

// lookup.go — GET /api/lookup/<name>?param=value: a parameterized query run
// live against its own DB connection at request time, unlike sources (which
// preload everything into memory at startup). For data that's expensive or
// pointless to preload for every row — an employee's photo, their checkpoint
// access history — fetched by one key, on demand, only when actually asked
// for. See LookupConfig's doc comment (config.go) for the design rationale.

import (
	"database/sql"
	"fmt"
	"net/http"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver, for lookups.type: postgres
)

// lookupDriverNames maps LookupConfig.Type to the database/sql driver name
// registered for it in this binary. Mirrors cmd/tdtpcli/production.go's
// auditDBDriverNames — same reasoning, same set of adapters.
var lookupDriverNames = map[string]string{
	"sqlite":   "sqlite",
	"mysql":    "mysql",
	"mssql":    "mssql",
	"postgres": "pgx",
}

// Lookup is one configured live query, with its own connection opened once
// at startup and reused (via database/sql's pool) across requests.
type Lookup struct {
	cfg LookupConfig
	db  *sql.DB
}

// loadLookups opens one *sql.DB per configured lookup. A failure here is
// fatal at startup (same as a bad source) rather than deferred to first
// request — a typo'd DSN should surface immediately, not on someone's first
// click of "Show".
func loadLookups(cfgs []LookupConfig) (map[string]*Lookup, error) {
	out := make(map[string]*Lookup, len(cfgs))
	for _, cfg := range cfgs {
		driverName, ok := lookupDriverNames[cfg.Type]
		if !ok {
			return nil, fmt.Errorf("lookup %q: unsupported type %q", cfg.Name, cfg.Type)
		}
		db, err := sql.Open(driverName, cfg.DSN)
		if err != nil {
			return nil, fmt.Errorf("lookup %q: open connection: %w", cfg.Name, err)
		}
		if err := db.Ping(); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("lookup %q: connection test failed: %w", cfg.Name, err)
		}
		out[cfg.Name] = &Lookup{cfg: cfg, db: db}
	}
	return out, nil
}

// handleAPILookup serves GET /api/lookup/<name>?<param>=<value>[&...].
// Every name in the lookup's configured Params must be present in the query
// string, bound positionally in that exact order — this is deliberately not
// a general WHERE-clause API like /api/data: a lookup is a fixed query with
// a small, explicit set of bind variables, which is most of why it's a
// meaningfully smaller injection/authorization surface than /api/data.
func (s *Server) handleAPILookup(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/api/lookup/")
	name = strings.TrimSuffix(name, "/")

	lk, ok := s.lookups[name]
	if !ok {
		writeAPIError(w, http.StatusNotFound, "lookup not found: "+name)
		return
	}

	q := r.URL.Query()
	args := make([]any, len(lk.cfg.Params))
	for i, p := range lk.cfg.Params {
		v := q.Get(p)
		if v == "" {
			writeAPIError(w, http.StatusBadRequest, "missing required parameter: "+p)
			return
		}
		args[i] = v
	}

	rows, err := lk.db.QueryContext(r.Context(), lk.cfg.Query, args...)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "lookup query failed: "+err.Error())
		return
	}
	defer func() { _ = rows.Close() }()

	switch lk.cfg.Result {
	case "binary":
		serveBinaryLookup(w, rows, lk.cfg)
	case "row":
		serveRowLookup(w, rows, false)
	default: // "rows"
		serveRowLookup(w, rows, true, withMaxRows(lk.cfg.MaxRows))
	}
}

// scanColumns reads the current row into a map[string]any keyed by column
// name. []byte values are converted to string — most driver/column
// combinations here are effectively text (varchar/text columns commonly
// surface as []byte under a generic `any` scan target), and json.Marshal
// would otherwise silently base64-encode raw []byte, which is virtually
// never what a JSON row/rows consumer wants. result: binary bypasses this
// path entirely and reads its one column as true raw bytes.
func scanColumns(rows *sql.Rows, cols []string) (map[string]any, error) {
	values := make([]any, len(cols))
	scanArgs := make([]any, len(cols))
	for i := range values {
		scanArgs[i] = &values[i]
	}
	if err := rows.Scan(scanArgs...); err != nil {
		return nil, err
	}
	out := make(map[string]any, len(cols))
	for i, col := range cols {
		if b, ok := values[i].([]byte); ok {
			out[col] = string(b)
		} else {
			out[col] = values[i]
		}
	}
	return out, nil
}

type rowLookupOpts struct{ maxRows int }

func withMaxRows(n int) func(*rowLookupOpts) { return func(o *rowLookupOpts) { o.maxRows = n } }

// serveRowLookup handles result: row (many=false, exactly one row expected)
// and result: rows (many=true, 0..maxRows rows).
func serveRowLookup(w http.ResponseWriter, rows *sql.Rows, many bool, opts ...func(*rowLookupOpts)) {
	o := rowLookupOpts{maxRows: 100}
	for _, fn := range opts {
		fn(&o)
	}

	cols, err := rows.Columns()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "reading columns: "+err.Error())
		return
	}

	results := make([]map[string]any, 0, 1)
	for rows.Next() {
		row, err := scanColumns(rows, cols)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "scanning row: "+err.Error())
			return
		}
		results = append(results, row)
		if many && len(results) >= o.maxRows {
			break
		}
		if !many && len(results) > 1 {
			writeAPIError(w, http.StatusInternalServerError, "result: row expects exactly one row, got more than one")
			return
		}
	}
	if err := rows.Err(); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "iterating rows: "+err.Error())
		return
	}

	if !many {
		if len(results) == 0 {
			writeAPIError(w, http.StatusNotFound, "no matching row")
			return
		}
		writeAPIJSON(w, http.StatusOK, results[0])
		return
	}
	writeAPIJSON(w, http.StatusOK, results)
}

// serveBinaryLookup handles result: binary — exactly one row, one column,
// written as a raw byte response instead of JSON.
func serveBinaryLookup(w http.ResponseWriter, rows *sql.Rows, cfg LookupConfig) {
	cols, err := rows.Columns()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "reading columns: "+err.Error())
		return
	}
	if len(cols) != 1 {
		writeAPIError(w, http.StatusInternalServerError, fmt.Sprintf("result: binary expects exactly one column, got %d", len(cols)))
		return
	}

	if !rows.Next() {
		writeAPIError(w, http.StatusNotFound, "no matching row")
		return
	}
	var payload []byte
	if err := rows.Scan(&payload); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "scanning binary column: "+err.Error())
		return
	}
	if rows.Next() {
		writeAPIError(w, http.StatusInternalServerError, "result: binary expects exactly one row, got more than one")
		return
	}
	if err := rows.Err(); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "iterating rows: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", cfg.ContentType)
	_, _ = w.Write(payload)
}
