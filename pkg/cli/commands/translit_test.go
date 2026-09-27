package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// cyrillicFixture writes a TDTP file with Cyrillic field names.
func cyrillicFixture(t *testing.T, name string) string {
	t.Helper()
	gen := packet.NewGenerator()
	pkts, err := gen.GenerateReference("staff",
		packet.Schema{Fields: []packet.Field{
			{Name: "Имя", Type: "TEXT"},
			{Name: "Фамилия", Type: "TEXT"},
			{Name: "Возраст", Type: "INTEGER"},
		}},
		[][]string{
			{"Иван", "Иванов", "30"},
		})
	if err != nil {
		t.Fatalf("GenerateReference: %v", err)
	}
	xmlData, err := gen.ToXML(pkts[0], true)
	if err != nil {
		t.Fatalf("ToXML: %v", err)
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, xmlData, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestTranslitCSV(t *testing.T) {
	in := cyrillicFixture(t, "ru.xml")
	plain := filepath.Join(t.TempDir(), "plain.csv")
	if err := ConvertTDTPToCSV(t.Context(), CSVOptions{InputFile: in, OutputFile: plain}); err != nil {
		t.Fatalf("plain: %v", err)
	}
	data, _ := os.ReadFile(plain)
	header := strings.SplitN(string(data), "\n", 2)[0]
	if !strings.Contains(header, "Имя") {
		t.Errorf("without --translit the header must stay Cyrillic, got %q", header)
	}

	ascii := filepath.Join(t.TempDir(), "ascii.csv")
	if err := ConvertTDTPToCSV(t.Context(), CSVOptions{InputFile: in, OutputFile: ascii, Translit: true}); err != nil {
		t.Fatalf("translit: %v", err)
	}
	data, _ = os.ReadFile(ascii)
	header = strings.SplitN(string(data), "\n", 2)[0]
	if strings.Contains(header, "Имя") || strings.Contains(header, "Фамилия") {
		t.Errorf("translit header must be ASCII, got %q", header)
	}
	// Values are positional and never transliterated.
	if !strings.Contains(string(data), "Иван") {
		t.Errorf("row values must keep Cyrillic, got:\n%s", data)
	}
}

func TestTranslitXLSX(t *testing.T) {
	in := cyrillicFixture(t, "ru.xml")
	out := filepath.Join(t.TempDir(), "ru.xlsx")
	if err := ConvertTDTPToXLSX(t.Context(), XLSXOptions{InputFile: in, OutputFile: out, SheetName: "S", Translit: true}); err != nil {
		t.Fatalf("translit: %v", err)
	}
	// The sheet XML carries the header row: no Cyrillic field names may remain.
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	// XLSX is a zip; header strings live in sharedStrings or inline.
	// Cheap robust check: the output must differ from the non-translit one.
	plain := filepath.Join(t.TempDir(), "plain.xlsx")
	if err := ConvertTDTPToXLSX(t.Context(), XLSXOptions{InputFile: in, OutputFile: plain, SheetName: "S"}); err != nil {
		t.Fatalf("plain: %v", err)
	}
	praw, _ := os.ReadFile(plain)
	if string(raw) == string(praw) {
		t.Error("translit output must differ from plain output")
	}
	if strings.Contains(string(raw), "Фамилия") {
		t.Error("translit xlsx must not contain the Cyrillic header")
	}
}
