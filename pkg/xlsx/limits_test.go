package xlsx

// limits_test.go — Excel worksheet hard limits enforced by ToXLSX.
//
// A sheet holds at most 1_048_576 rows (header included), 16_384 columns
// (XFD) and 32_767 characters per cell. Past any of them Excel reports the
// file as corrupt and drops data, so the writer must refuse loudly.
// The limits are package vars precisely so these tests can shrink them and
// exercise the real ToXLSX path without building million-row packets.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// withExcelLimits temporarily overrides the worksheet limits.
func withExcelLimits(rows, cols, chars int) func() {
	oldRows, oldCols, oldChars := maxExcelRows, maxExcelCols, maxExcelCellChars
	maxExcelRows, maxExcelCols, maxExcelCellChars = rows, cols, chars
	return func() {
		maxExcelRows, maxExcelCols, maxExcelCellChars = oldRows, oldCols, oldChars
	}
}

func writeTemp(t *testing.T, pkt *packet.DataPacket, sheet string) error {
	t.Helper()
	path := filepath.Join(t.TempDir(), "limits.xlsx")
	return ToXLSX(pkt, path, sheet)
}

// ── row limit ──────────────────────────────────────────────────────────────

func TestLimits_RowsWithinLimitOK(t *testing.T) {
	defer withExcelLimits(4, 16_384, 32_767)() // 1 header + 3 data rows fit
	pkt := makePacket(
		[]packet.Field{{Name: "id", Type: "INTEGER"}},
		[]string{"1", "2", "3"},
	)
	if err := writeTemp(t, pkt, "s"); err != nil {
		t.Fatalf("3 data rows with limit 1+3 should convert: %v", err)
	}
}

func TestLimits_RowsOverLimit(t *testing.T) {
	defer withExcelLimits(4, 16_384, 32_767)() // 1 header + 3 data rows fit
	pkt := makePacket(
		[]packet.Field{{Name: "id", Type: "INTEGER"}},
		[]string{"1", "2", "3", "4"}, // one past the limit
	)
	err := writeTemp(t, pkt, "s")
	if err == nil {
		t.Fatal("expected error for 4 data rows with limit 1+3, got nil")
	}
	if !strings.Contains(err.Error(), "data rows") {
		t.Fatalf("error should name the row limit, got: %v", err)
	}
}

// ── column limit ───────────────────────────────────────────────────────────

func TestLimits_ColsOverLimit(t *testing.T) {
	// Real spec value: 16_385 fields, no rows — fails before any writing.
	fields := make([]packet.Field, maxExcelCols+1)
	for i := range fields {
		fields[i] = packet.Field{Name: "c", Type: "TEXT"}
	}
	pkt := makePacket(fields, nil)
	err := writeTemp(t, pkt, "s")
	if err == nil {
		t.Fatal("expected error for 16385 columns, got nil")
	}
	if !strings.Contains(err.Error(), "columns") {
		t.Fatalf("error should name the column limit, got: %v", err)
	}
}

func TestLimits_ColsWithinLimitOK(t *testing.T) {
	defer withExcelLimits(1_048_576, 3, 32_767)()
	pkt := makePacket(
		[]packet.Field{
			{Name: "a", Type: "TEXT"},
			{Name: "b", Type: "TEXT"},
			{Name: "c", Type: "TEXT"},
		},
		[]string{"x|y|z"},
	)
	if err := writeTemp(t, pkt, "s"); err != nil {
		t.Fatalf("3 columns with limit 3 should convert: %v", err)
	}
}

// ── per-cell character limit ───────────────────────────────────────────────

func TestLimits_CellTooLong(t *testing.T) {
	defer withExcelLimits(1_048_576, 16_384, 10)()
	pkt := makePacket(
		// Header "n (TEXT)" is 8 chars and fits; the 11-char value must not.
		[]packet.Field{{Name: "n", Type: "TEXT"}},
		[]string{"12345678901"}, // 11 chars > 10
	)
	err := writeTemp(t, pkt, "s")
	if err == nil {
		t.Fatal("expected error for 11-char cell with limit 10, got nil")
	}
	if !strings.Contains(err.Error(), "per-cell limit") || !strings.Contains(err.Error(), "A2") {
		t.Fatalf("error should name the per-cell limit and cell A2, got: %v", err)
	}
}

func TestLimits_CellExactLengthOK(t *testing.T) {
	defer withExcelLimits(1_048_576, 16_384, 10)()
	pkt := makePacket(
		[]packet.Field{{Name: "n", Type: "TEXT"}},
		[]string{"1234567890"}, // exactly 10 chars
	)
	if err := writeTemp(t, pkt, "s"); err != nil {
		t.Fatalf("cell at exactly the limit should convert: %v", err)
	}
}

// ── multi-byte characters count as characters, not bytes ───────────────────

func TestLimits_CellCountsRunesNotBytes(t *testing.T) {
	// Header "n (TEXT)" is 8 chars, so the limit must cover it; the value
	// "日本語" is 3 runes but 9 bytes — must fit a limit of 8.
	defer withExcelLimits(1_048_576, 16_384, 8)()
	pkt := makePacket(
		[]packet.Field{{Name: "n", Type: "TEXT"}},
		[]string{"日本語"},
	)
	if err := writeTemp(t, pkt, "s"); err != nil {
		t.Fatalf("3-rune cell should fit a 3-char limit: %v", err)
	}
}

// ── refusal writes no file ─────────────────────────────────────────────────

func TestLimits_NoFileOnRefusal(t *testing.T) {
	defer withExcelLimits(2, 16_384, 32_767)() // header + 1 row only
	pkt := makePacket(
		[]packet.Field{{Name: "id", Type: "INTEGER"}},
		[]string{"1", "2"},
	)
	path := filepath.Join(t.TempDir(), "refused.xlsx")
	if err := ToXLSX(pkt, path, "s"); err == nil {
		t.Fatal("expected error, got nil")
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("refused conversion must not leave a file behind: %v", statErr)
	}
}
