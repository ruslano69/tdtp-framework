package xlsx

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

func writtenCell(t *testing.T, field packet.Field, value string) string {
	t.Helper()
	field.Name = "v"
	path := filepath.Join(t.TempDir(), "v.xlsx")
	if err := ToXLSX(makePacket([]packet.Field{field}, []string{value}), path, "s"); err != nil {
		t.Fatal(err)
	}
	rows, err := readSheet(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || len(rows[1]) != 1 {
		t.Fatalf("unexpected sheet: %q", rows)
	}
	return rows[1][0]
}

// Excel держит 15 значащих цифр. Всё, что длиннее, обязано дойти текстом без
// потерь, а не числом, округлённым по пути.
func TestLimits_NumberPrecision(t *testing.T) {
	decimal := func(precision, scale int) packet.Field {
		return packet.Field{Type: "DECIMAL", Precision: precision, Scale: scale}
	}
	cases := []struct {
		field packet.Field
		in    string
		want  string
	}{
		{decimal(19, 4), "123456789012345.6789", "123456789012345.6789"},
		{decimal(19, 4), "-123456789012345.6789", "-123456789012345.6789"},
		{decimal(19, 0), "9007199254740993", "9007199254740993"},
		{decimal(20, 18), "0.123456789012345678", "0.123456789012345678"},
		// В пределах 15 цифр значение остаётся числом.
		{decimal(12, 2), "1234567.89", "1234567.89"},
		{decimal(19, 4), "12345678901.2345", "12345678901.2345"},
		{packet.Field{Type: "INTEGER"}, "9007199254740993", "9007199254740993"},
		{packet.Field{Type: "REAL"}, "1.7e308", "1.7e+308"},
	}
	for _, tc := range cases {
		if got := writtenCell(t, tc.field, tc.in); got != tc.want {
			t.Errorf("%s %s: cell %q, want %q", tc.field.Type, tc.in, got, tc.want)
		}
	}
}

func TestLimits_LongDecimalIsTextNotNumber(t *testing.T) {
	for value, wantText := range map[string]bool{
		"123456789012345.6789": true,  // 19 значащих цифр
		"1234567.89":           false, // 9
		// Нули по краям точности не стоят.
		"1000000000000000000.0000": false,
		"0.0000000000000000012300": false,
	} {
		got := significantDigits(value) > maxExcelDigits
		if got != wantText {
			t.Errorf("%s: text=%v, want %v", value, got, wantText)
		}
	}
}

// Лимит ячейки — в единицах UTF-16: эмодзи вне BMP занимает две.
func TestLimits_CellCountsUTF16Units(t *testing.T) {
	defer withExcelLimits(1_048_576, 16_384, 8)()
	field := []packet.Field{{Name: "n", Type: "TEXT"}} // заголовок "n (TEXT)" — 8

	if err := writeTemp(t, makePacket(field, []string{strings.Repeat("😀", 4)}), "s"); err != nil {
		t.Fatalf("4 emoji are 8 units and must fit: %v", err)
	}
	err := writeTemp(t, makePacket(field, []string{strings.Repeat("😀", 5)}), "s")
	if err == nil || !strings.Contains(err.Error(), "10 characters") {
		t.Fatalf("5 emoji are 10 units and must be refused, got %v", err)
	}
}

func TestSanitizeSheetName_ExcelRules(t *testing.T) {
	for in, want := range map[string]string{
		"":                             "Sheet1",
		"   ":                          "Sheet1",
		"'":                            "Sheet1",
		"'quoted'":                     "quoted",
		"O'Brien":                      "O'Brien",
		"History":                      "History_",
		"history":                      "history_",
		"History 2026":                 "History 2026",
		`a:b/c\d?e*f[g]`:               "a_b_c_d_e_f_g_",
		strings.Repeat("a", 40):        strings.Repeat("a", 31),
		strings.Repeat("😀", 31):        strings.Repeat("😀", 15), // 30 единиц; 16-й дал бы 32
		strings.Repeat("a", 30) + "'b": strings.Repeat("a", 30), // обрезка оставила апостроф
		"Звіт за листопад 2026 по підрозділах": "Звіт за листопад 2026 по підроз",
	} {
		if got := sanitizeSheetName(in); got != want {
			t.Errorf("sanitizeSheetName(%q) = %q, want %q", in, got, want)
		}
	}
}

// Настоящие границы формата, без подмены лимитов.
func TestLimits_RealRowBoundary(t *testing.T) {
	if err := checkExcelLimits("t", 1, 1_048_575); err != nil {
		t.Fatalf("1 048 575 data rows fit: %v", err)
	}
	if err := checkExcelLimits("t", 1, 1_048_576); err == nil {
		t.Fatal("1 048 576 data rows plus the header do not fit")
	}
	if err := checkExcelLimits("t", 16_384, 0); err != nil {
		t.Fatalf("16 384 columns fit: %v", err)
	}
}
