package main

// filter_test.go — a filter that cannot be applied must return nothing.
//
// queryDataset used to fail open: an unparsable where, or limit=abc, answered
// 200 with every row of the dataset and the error in a side field. These
// tests assert on the response body — rows present or absent — not on
// whether an error was recorded somewhere.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// filterTestServer serves one dataset, "users", with three rows whose
// secrets must never leak through a broken filter.
func filterTestServer(t *testing.T) *Server {
	t.Helper()
	schema := packet.Schema{Fields: []packet.Field{
		{Name: "id", Type: "INTEGER", Key: true},
		{Name: "dept", Type: "TEXT"},
		{Name: "secret", Type: "TEXT"},
	}}
	pkts, err := packet.NewGenerator().GenerateReference("users", schema, [][]string{
		{"1", "hr", "alpha-secret"}, {"2", "it", "bravo-secret"}, {"3", "it", "charlie-secret"},
	})
	if err != nil {
		t.Fatal(err)
	}
	pkts[0].MaterializeRows()
	return &Server{
		cfg:      &ServeConfig{},
		datasets: map[string]*Dataset{"users": {Name: "users", Type: "tdtp", Packet: pkts[0]}},
		order:    []string{"users"},
	}
}

func get(srv *Server, handler func(http.ResponseWriter, *http.Request), path string, q url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path+"?"+q.Encode(), nil)
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

// Each must be REJECTED. Trailing garbage after a complete expression
// ("dept = 'hr' AMD id > 5", "dept SIDEWAYS") is not here yet: the shared
// TDTQL translator still ignores it — a pkg/core/tdtql fix, separate.
var badFilters = map[string]url.Values{
	"unparsable where":  {"where": {"dept = = 'hr'"}},
	"dangling AND":      {"where": {"dept = 'hr' AND"}},
	"unknown column":    {"where": {"no_such_col = 'x'"}},
	"unparsable order":  {"order_by": {"dept,"}},
	"non-numeric limit": {"limit": {"abc"}},
	"negative limit":    {"limit": {"-5"}},
	"non-numeric off":   {"offset": {"x"}},
	"negative offset":   {"offset": {"-1"}},
}

func TestAPIData_BadFilterIs400WithNoRows(t *testing.T) {
	srv := filterTestServer(t)
	for name, q := range badFilters {
		rec := get(srv, srv.handleAPIData, "/api/data/users", q)
		body := rec.Body.String()
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400; body: %s", name, rec.Code, body)
		}
		if strings.Contains(body, "-secret") {
			t.Errorf("%s: rows leaked through a filter that was not applied: %s", name, body)
		}
		var e map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil || !strings.HasPrefix(e["error"], "filter: ") {
			t.Errorf("%s: want {\"error\": \"filter: ...\"}, got %s", name, body)
		}
	}
}

func TestDataPage_BadFilterIs400WithNoRows(t *testing.T) {
	srv := filterTestServer(t)
	for name, q := range badFilters {
		rec := get(srv, srv.handleData, "/data/users", q)
		body := rec.Body.String()
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", name, rec.Code)
		}
		if strings.Contains(body, "-secret") {
			t.Errorf("%s: the page rendered rows for a filter that was not applied", name)
		}
		if !strings.Contains(body, "Filter error") {
			t.Errorf("%s: the page should still say what was wrong", name)
		}
	}
}

// The fix must not cost the working path anything.
func TestAPIData_ValidFilterStillFilters(t *testing.T) {
	srv := filterTestServer(t)
	rec := get(srv, srv.handleAPIData, "/api/data/users", url.Values{
		"where": {"dept = 'it'"}, "order_by": {"id DESC"}, "limit": {"1"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var resp apiDataResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.RowCount != 1 || len(resp.Rows) != 1 || resp.Rows[0][0] != "3" {
		t.Errorf("rows = %v, want only id 3", resp.Rows)
	}
	// No filter at all: every row, 200.
	rec = get(srv, srv.handleAPIData, "/api/data/users", url.Values{})
	if rec.Code != http.StatusOK || strings.Count(rec.Body.String(), "-secret") != 3 {
		t.Errorf("unfiltered request: status %d, body %s", rec.Code, rec.Body.String())
	}
}

// The ad-hoc parser this replaced split on " AND " / " OR ": it refused the
// BETWEEN example from its own README and turned mixed AND/OR and
// parentheses into wrong filters without an error. The shared TDTQL gets
// them right.
func TestAPIData_SharedTDTQLSemantics(t *testing.T) {
	srv := filterTestServer(t)
	for where, want := range map[string]string{
		"id BETWEEN 2 AND 3":                      "2,3",
		"(dept = 'hr' OR dept = 'it') AND id > 1": "2,3",
		"dept = 'hr' OR dept = 'it' AND id > 2":   "1,3", // AND binds tighter
		"dept IN ('hr')":                          "1",
	} {
		rec := get(srv, srv.handleAPIData, "/api/data/users", url.Values{"where": {where}, "order_by": {"id"}})
		if rec.Code != http.StatusOK {
			t.Errorf("%q: status %d: %s", where, rec.Code, rec.Body.String())
			continue
		}
		var resp apiDataResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, r := range resp.Rows {
			ids = append(ids, r[0])
		}
		if got := strings.Join(ids, ","); got != want {
			t.Errorf("%q: ids %s, want %s", where, got, want)
		}
	}
}
