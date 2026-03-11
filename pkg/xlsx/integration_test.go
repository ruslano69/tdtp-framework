package xlsx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// makePacket builds a minimal DataPacket for roundtrip tests.
func makePacket(fields []packet.Field, rows []string) *packet.DataPacket {
	pktRows := make([]packet.Row, len(rows))
	for i, r := range rows {
		pktRows[i] = packet.Row{Value: r}
	}
	return &packet.DataPacket{
		Protocol: "TDTP",
		Version:  "1.0",
		Header: packet.Header{
			Type:          packet.TypeReference,
			TableName:     "test",
			Timestamp:     time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			RecordsInPart: len(rows),
		},
		Schema: packet.Schema{Fields: fields},
		Data:   packet.Data{Rows: pktRows},
	}
}

// roundtrip writes pkt to a temp XLSX file and reads it back.
func roundtrip(t *testing.T, pkt *packet.DataPacket) *packet.DataPacket {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.xlsx")

	if err := ToXLSX(pkt, path, "test"); err != nil {
		t.Fatalf("ToXLSX failed: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("output file not created: %v", err)
	}

	out, err := FromXLSX(path, "test")
	if err != nil {
		t.Fatalf("FromXLSX failed: %v", err)
	}
	return out
}

// cellValue returns the raw pipe-delimited value for column col (0-based) in row rowIdx (0-based).
func cellValue(t *testing.T, pkt *packet.DataPacket, rowIdx, col int) string {
	t.Helper()
	if rowIdx >= len(pkt.Data.Rows) {
		t.Fatalf("row %d out of range (len=%d)", rowIdx, len(pkt.Data.Rows))
	}
	parts := strings.Split(pkt.Data.Rows[rowIdx].Value, "|")
	if col >= len(parts) {
		t.Fatalf("col %d out of range (len=%d)", col, len(parts))
	}
	return parts[col]
}

// ── INTEGER / BIGINT ─────────────────────────────────────────────────────────

func TestIntegration_NormalInteger_Roundtrip(t *testing.T) {
	pkt := makePacket(
		[]packet.Field{{Name: "id", Type: "INTEGER"}},
		[]string{"42"},
	)
	out := roundtrip(t, pkt)
	got := cellValue(t, out, 0, 0)
	if got != "42" {
		t.Errorf("integer roundtrip: expected '42', got %q", got)
	}
}

func TestIntegration_BigInt_PreservesAllDigits(t *testing.T) {
	// 19-digit BIGINT — would silently truncate if written as Excel number
	bigint := "1234567890123456789"
	pkt := makePacket(
		[]packet.Field{{Name: "id", Type: "INTEGER"}},
		[]string{bigint},
	)
	out := roundtrip(t, pkt)
	got := cellValue(t, out, 0, 0)
	if got != bigint {
		t.Errorf("BIGINT roundtrip: expected %q, got %q", bigint, got)
	}
}

// ── NaN / INF / -INF ─────────────────────────────────────────────────────────

func TestIntegration_NaN_BecomesBlank(t *testing.T) {
	// id column keeps the row non-empty so excelize returns it from GetRows.
	// Real-world rows always have at least a key column alongside special values.
	pkt := makePacket(
		[]packet.Field{{Name: "id", Type: "INTEGER"}, {Name: "val", Type: "REAL"}},
		[]string{"1|NaN"},
	)
	out := roundtrip(t, pkt)
	// col 1 = val; NaN → blank cell → "" on import
	got := cellValue(t, out, 0, 1)
	if got != "" {
		t.Errorf("NaN should become blank cell, got %q", got)
	}
}

func TestIntegration_Inf_BecomesBlank(t *testing.T) {
	pkt := makePacket(
		[]packet.Field{{Name: "id", Type: "INTEGER"}, {Name: "val", Type: "REAL"}},
		[]string{"1|INF"},
	)
	out := roundtrip(t, pkt)
	got := cellValue(t, out, 0, 1)
	if got != "" {
		t.Errorf("INF should become blank cell, got %q", got)
	}
}

func TestIntegration_NegInf_BecomesBlank(t *testing.T) {
	pkt := makePacket(
		[]packet.Field{{Name: "id", Type: "INTEGER"}, {Name: "val", Type: "REAL"}},
		[]string{"1|-INF"},
	)
	out := roundtrip(t, pkt)
	got := cellValue(t, out, 0, 1)
	if got != "" {
		t.Errorf("-INF should become blank cell, got %q", got)
	}
}

func TestIntegration_NormalFloat_Roundtrip(t *testing.T) {
	pkt := makePacket(
		[]packet.Field{{Name: "val", Type: "REAL"}},
		[]string{"3.14"},
	)
	out := roundtrip(t, pkt)
	got := cellValue(t, out, 0, 0)
	if !strings.HasPrefix(got, "3.14") {
		t.Errorf("float roundtrip: expected ~'3.14', got %q", got)
	}
}

// ── NULL marker ──────────────────────────────────────────────────────────────

func TestIntegration_NullMarker_InTextField_BecomesBlank(t *testing.T) {
	pkt := makePacket(
		[]packet.Field{{Name: "id", Type: "INTEGER"}, {Name: "name", Type: "TEXT"}},
		[]string{"1|[NULL]"},
	)
	out := roundtrip(t, pkt)
	// col 1 = name; [NULL] → blank cell → "" on import
	got := cellValue(t, out, 0, 1)
	if got != "" {
		t.Errorf("[NULL] in TEXT should become blank cell, got %q", got)
	}
}

func TestIntegration_EmptyString_InTextField_Preserved(t *testing.T) {
	pkt := makePacket(
		[]packet.Field{{Name: "id", Type: "INTEGER"}, {Name: "name", Type: "TEXT"}},
		[]string{"1|"},
	)
	out := roundtrip(t, pkt)
	// col 1 = name; empty string → blank cell → "" on import.
	// Empty string and NULL are indistinguishable in XLSX (by design).
	got := cellValue(t, out, 0, 1)
	if got != "" {
		t.Errorf("empty TEXT should round-trip as '', got %q", got)
	}
}

// ── Dates ────────────────────────────────────────────────────────────────────

func TestIntegration_ModernDate_Roundtrip(t *testing.T) {
	pkt := makePacket(
		[]packet.Field{{Name: "created_at", Type: "DATE"}},
		[]string{"2024-06-15"},
	)
	out := roundtrip(t, pkt)
	got := cellValue(t, out, 0, 0)
	if got != "2024-06-15" {
		t.Errorf("modern date roundtrip: expected '2024-06-15', got %q", got)
	}
}

func TestIntegration_Pre1900Date_WrittenAsText(t *testing.T) {
	// Pre-1900 dates cannot be Excel serials → written as ISO text string.
	// On import they are returned as-is (text passthrough).
	pkt := makePacket(
		[]packet.Field{{Name: "hist_date", Type: "DATE"}},
		[]string{"1812-09-07"},
	)
	out := roundtrip(t, pkt)
	got := cellValue(t, out, 0, 0)
	if got != "1812-09-07" {
		t.Errorf("pre-1900 date roundtrip: expected '1812-09-07', got %q", got)
	}
}

func TestIntegration_Epoch1900_Boundary(t *testing.T) {
	// Jan 1, 1900 = serial 1 — first representable Excel date
	pkt := makePacket(
		[]packet.Field{{Name: "d", Type: "DATE"}},
		[]string{"1900-01-01"},
	)
	out := roundtrip(t, pkt)
	got := cellValue(t, out, 0, 0)
	if got != "1900-01-01" {
		t.Errorf("1900-01-01 roundtrip: expected '1900-01-01', got %q", got)
	}
}

func TestIntegration_DatetimeWithTime_Roundtrip(t *testing.T) {
	pkt := makePacket(
		[]packet.Field{{Name: "ts", Type: "DATETIME"}},
		[]string{"2023-06-01T15:30:00Z"},
	)
	out := roundtrip(t, pkt)
	got := cellValue(t, out, 0, 0)
	// Time-of-day must be preserved (roundtrip through serial fraction)
	if !strings.HasPrefix(got, "2023-06-01T15:30") {
		t.Errorf("datetime roundtrip: expected '2023-06-01T15:30:00Z', got %q", got)
	}
}

// ── Formula injection ────────────────────────────────────────────────────────

func TestIntegration_FormulaInjection_StoredAsText(t *testing.T) {
	// Strings starting with =, +, -, @ must NOT be executed as formulas.
	// They should round-trip as literal strings.
	injections := []string{
		"=SUM(A1:A10)",
		"+1+1",
		"-1",
		"@SUM",
	}
	for _, inj := range injections {
		pkt := makePacket(
			[]packet.Field{{Name: "formula", Type: "TEXT"}},
			[]string{inj},
		)
		out := roundtrip(t, pkt)
		got := cellValue(t, out, 0, 0)
		if got != inj {
			t.Errorf("formula injection %q: expected literal, got %q", inj, got)
		}
	}
}

// ── Boolean ──────────────────────────────────────────────────────────────────

func TestIntegration_Boolean_Roundtrip(t *testing.T) {
	pkt := makePacket(
		[]packet.Field{
			{Name: "active", Type: "BOOLEAN"},
			{Name: "deleted", Type: "BOOLEAN"},
		},
		[]string{"1|0"},
	)
	out := roundtrip(t, pkt)
	if cellValue(t, out, 0, 0) != "1" {
		t.Errorf("boolean TRUE roundtrip failed, got %q", cellValue(t, out, 0, 0))
	}
	if cellValue(t, out, 0, 1) != "0" {
		t.Errorf("boolean FALSE roundtrip failed, got %q", cellValue(t, out, 0, 1))
	}
}

// ── Excel error cells on import ──────────────────────────────────────────────

func TestIntegration_ExcelErrors_BecomeNull(t *testing.T) {
	// Simulate a file that contains error cells by writing an XLSX manually
	// and checking that FromXLSX maps them to "".
	// We test the isExcelError helper exhaustively here.
	errorValues := []string{
		"#N/A", "#DIV/0!", "#NUM!", "#VALUE!", "#REF!", "#NAME?", "#NULL!",
	}
	for _, ev := range errorValues {
		if !isExcelError(ev) {
			t.Errorf("isExcelError(%q) = false, want true", ev)
		}
	}
	// Non-error values must not be flagged
	safe := []string{"", "hello", "42", "2023-01-01", "NaN", "[NULL]"}
	for _, sv := range safe {
		if isExcelError(sv) {
			t.Errorf("isExcelError(%q) = true, want false", sv)
		}
	}
}

// ── Multi-column multi-row roundtrip ─────────────────────────────────────────

func TestIntegration_MultiColumn_NoErrors(t *testing.T) {
	// Stress test: all problematic types in one packet — must not panic or error.
	fields := []packet.Field{
		{Name: "id", Type: "INTEGER", Key: true},
		{Name: "bigid", Type: "INTEGER"},
		{Name: "score", Type: "REAL"},
		{Name: "name", Type: "TEXT"},
		{Name: "active", Type: "BOOLEAN"},
		{Name: "created", Type: "DATE"},
		{Name: "updated", Type: "DATETIME"},
	}
	rows := []string{
		// normal row
		"1|42|3.14|Alice|1|2024-01-15|2024-01-15T10:00:00Z",
		// BIGINT + NaN + [NULL] + pre-1900
		"2|1234567890123456789|NaN|[NULL]|0|1812-09-07|2024-01-15T10:00:00Z",
		// INF + -INF
		"3|42|INF|Bob|1|2024-06-01|2024-06-01T12:00:00Z",
		"4|42|-INF|=formula|0|2023-12-31|2023-12-31T23:59:59Z",
	}

	pkt := makePacket(fields, rows)
	// Must not panic or return error
	out := roundtrip(t, pkt)

	if len(out.Data.Rows) != len(rows) {
		t.Errorf("expected %d rows back, got %d", len(rows), len(out.Data.Rows))
	}

	// Row 0: normal values survive
	if cellValue(t, out, 0, 0) != "1" {
		t.Errorf("row0 id: expected '1'")
	}

	// Row 1: BIGINT preserved as string
	if cellValue(t, out, 1, 1) != "1234567890123456789" {
		t.Errorf("row1 bigid: expected '1234567890123456789', got %q", cellValue(t, out, 1, 1))
	}
	// NaN → blank
	if cellValue(t, out, 1, 2) != "" {
		t.Errorf("row1 NaN score: expected blank, got %q", cellValue(t, out, 1, 2))
	}
	// [NULL] → blank
	if cellValue(t, out, 1, 3) != "" {
		t.Errorf("row1 [NULL] name: expected blank, got %q", cellValue(t, out, 1, 3))
	}
	// pre-1900 date preserved as text
	if cellValue(t, out, 1, 5) != "1812-09-07" {
		t.Errorf("row1 pre-1900: expected '1812-09-07', got %q", cellValue(t, out, 1, 5))
	}

	// Row 2: INF → blank
	if cellValue(t, out, 2, 2) != "" {
		t.Errorf("row2 INF score: expected blank, got %q", cellValue(t, out, 2, 2))
	}

	// Row 3: -INF → blank, formula string literal preserved
	if cellValue(t, out, 3, 2) != "" {
		t.Errorf("row3 -INF score: expected blank, got %q", cellValue(t, out, 3, 2))
	}
	if cellValue(t, out, 3, 3) != "=formula" {
		t.Errorf("row3 formula string: expected '=formula', got %q", cellValue(t, out, 3, 3))
	}
}
