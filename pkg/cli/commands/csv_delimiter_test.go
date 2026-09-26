package commands

import (
	"context"
	"encoding/csv"
	"os"
	"path/filepath"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// makeCSVTestFile creates a v1.0 TDTP packet with rows that include
// values containing commas, semicolons, tabs and quotes — to exercise
// the csv encoder's quoting behaviour across delimiter variants.
func makeCSVTestFile(t *testing.T) string {
	t.Helper()
	schema := packet.Schema{
		Fields: []packet.Field{
			{Name: "id", Type: "INTEGER", Key: true},
			{Name: "name", Type: "TEXT"},
			{Name: "note", Type: "TEXT"},
		},
	}
	gen := packet.NewGenerator()
	pkts, err := gen.GenerateReference("orders", schema, [][]string{
		{"1", "Alice", "simple value"},
		{"2", "Bob; Jr.", "contains semicolon"},
		{"3", "Carol,Inc", "contains comma"},
		{"4", "Dave\tX", "contains tab"},
		{"5", `Eve "quoted"`, "contains double-quote"},
	})
	if err != nil {
		t.Fatalf("GenerateReference: %v", err)
	}
	xmlData, err := gen.ToXML(pkts[0], true)
	if err != nil {
		t.Fatalf("ToXML: %v", err)
	}
	f, err := os.CreateTemp(t.TempDir(), "delim-*.tdtp.xml")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Write(xmlData); err != nil {
		t.Fatalf("Write: %v", err)
	}
	return f.Name()
}

// readCSV parses a CSV file with the given delimiter and returns all records.
func readCSV(t *testing.T, path string, comma rune) [][]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open csv: %v", err)
	}
	defer func() { _ = f.Close() }()
	r := csv.NewReader(f)
	r.Comma = comma
	records, err := r.ReadAll()
	if err != nil {
		t.Fatalf("read csv: %v", err)
	}
	return records
}

func TestCSVDelimiter_Semicolon(t *testing.T) {
	in := makeCSVTestFile(t)
	out := filepath.Join(t.TempDir(), "out.csv")

	err := ConvertTDTPToCSV(context.Background(), CSVOptions{
		InputFile:  in,
		OutputFile: out,
		Delimiter:  ';',
	})
	if err != nil {
		t.Fatalf("ConvertTDTPToCSV: %v", err)
	}

	records := readCSV(t, out, ';')
	if len(records) != 6 { // header + 5 rows
		t.Fatalf("expected 6 records, got %d", len(records))
	}
	// Header
	if records[0][1] != "name" {
		t.Errorf("header[1]=%q, want %q", records[0][1], "name")
	}
	// Row 2: "Bob; Jr." must survive semicolon delimiter (csv quoting)
	if records[2][1] != "Bob; Jr." {
		t.Errorf("row2 name=%q, want %q", records[2][1], "Bob; Jr.")
	}
}

func TestCSVDelimiter_Tab(t *testing.T) {
	in := makeCSVTestFile(t)
	out := filepath.Join(t.TempDir(), "out.tsv")

	err := ConvertTDTPToCSV(context.Background(), CSVOptions{
		InputFile:  in,
		OutputFile: out,
		Delimiter:  '\t',
	})
	if err != nil {
		t.Fatalf("ConvertTDTPToCSV: %v", err)
	}

	records := readCSV(t, out, '\t')
	if len(records) != 6 {
		t.Fatalf("expected 6 records, got %d", len(records))
	}
	// Row 3: "Carol,Inc" — comma not special in TSV, no quoting needed
	if records[3][1] != "Carol,Inc" {
		t.Errorf("row3 name=%q, want %q", records[3][1], "Carol,Inc")
	}
	// Row 4: "Dave\tX" — tab in value must be quoted by encoder
	if records[4][1] != "Dave\tX" {
		t.Errorf("row4 name=%q, want %q (tab in value)", records[4][1], "Dave\tX")
	}
}

func TestCSVDelimiter_Pipe(t *testing.T) {
	in := makeCSVTestFile(t)
	out := filepath.Join(t.TempDir(), "out.csv")

	err := ConvertTDTPToCSV(context.Background(), CSVOptions{
		InputFile:  in,
		OutputFile: out,
		Delimiter:  '|',
	})
	if err != nil {
		t.Fatalf("ConvertTDTPToCSV: %v", err)
	}

	records := readCSV(t, out, '|')
	if len(records) != 6 {
		t.Fatalf("expected 6 records, got %d", len(records))
	}
	if records[0][0] != "id" || records[0][1] != "name" || records[0][2] != "note" {
		t.Errorf("unexpected header: %v", records[0])
	}
}

func TestCSVDelimiter_CommaDefault(t *testing.T) {
	in := makeCSVTestFile(t)
	out := filepath.Join(t.TempDir(), "out.csv")

	// Delimiter=0 → should default to ','
	err := ConvertTDTPToCSV(context.Background(), CSVOptions{
		InputFile:  in,
		OutputFile: out,
		Delimiter:  0,
	})
	if err != nil {
		t.Fatalf("ConvertTDTPToCSV: %v", err)
	}

	records := readCSV(t, out, ',')
	if len(records) != 6 {
		t.Fatalf("expected 6 records, got %d", len(records))
	}
	// Row 3: "Carol,Inc" must be quoted in comma-CSV
	if records[3][1] != "Carol,Inc" {
		t.Errorf("row3 name=%q, want %q", records[3][1], "Carol,Inc")
	}
}

func TestCSVDelimiter_DoubleQuoteInValue(t *testing.T) {
	in := makeCSVTestFile(t)
	out := filepath.Join(t.TempDir(), "out.csv")

	err := ConvertTDTPToCSV(context.Background(), CSVOptions{
		InputFile:  in,
		OutputFile: out,
		Delimiter:  ',',
	})
	if err != nil {
		t.Fatalf("ConvertTDTPToCSV: %v", err)
	}

	records := readCSV(t, out, ',')
	// Row 5: `Eve "quoted"` — double-quote in value must round-trip
	if records[5][1] != `Eve "quoted"` {
		t.Errorf("row5 name=%q, want %q", records[5][1], `Eve "quoted"`)
	}
}
