package etl

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/security"
	"golang.org/x/text/encoding/charmap"
)

func TestCSVSourcePreservesRawCells(t *testing.T) {
	path := filepath.Join(t.TempDir(), "input.csv")
	content := "\ufeffNUM_LN;FIO;DATE_START\r\n" +
		"1;\"Іван | Петро\nТест\";2026-02-31\r\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	source := SourceConfig{Name: "leaves", Type: "csv", DSN: path,
		CSV: &CSVSourceConfig{Columns: []string{"NUM_LN", "FIO", "DATE_START"}, Delimiter: ";"}}
	pkt, err := loadCSVFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if pkt.Header.RecordsInPart != 1 || pkt.Header.TableName != "leaves" {
		t.Fatalf("wrong CSV packet header: %+v", pkt.Header)
	}
	values := packet.NewParser().GetRowValues(pkt.Data.Rows[0])
	if len(values) != 3 || values[1] != "Іван | Петро\nТест" || values[2] != "2026-02-31" {
		t.Fatalf("CSV cells were changed: %#v", values)
	}
}

func TestCSVSourceChecksShapeAndEncoding(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "input.csv")
	source := SourceConfig{Name: "leaves", Type: "csv", DSN: path,
		CSV: &CSVSourceConfig{Columns: []string{"ID", "FIO"}}}
	if err := os.WriteFile(path, []byte("WRONG,FIO\n1,Name\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadCSVFile(source); err == nil || !strings.Contains(err.Error(), "header column 1") {
		t.Fatalf("expected header mismatch, got %v", err)
	}
	if err := os.WriteFile(path, []byte("ID,FIO\n1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadCSVFile(source); err == nil || !strings.Contains(err.Error(), "wrong number of fields") {
		t.Fatalf("expected row width error, got %v", err)
	}
	encoded, err := charmap.Windows1251.NewEncoder().Bytes([]byte("ID,FIO\n1,Іван\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	source.CSV.Encoding = "windows-1251"
	pkt, err := loadCSVFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if got := packet.NewParser().GetRowValues(pkt.Data.Rows[0])[1]; got != "Іван" {
		t.Fatalf("wrong decoded name %q", got)
	}
}

func TestPFUCSVSQLKeepsInvalidRowsInOneTDTP(t *testing.T) {
	config, err := LoadConfig(filepath.Join("..", "..", "examples", "pfu-csv", "pipeline.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if _, err := ApplyVariables(config, map[string]string{
		"csv_path": filepath.Join(dir, "export.csv"), "output_path": filepath.Join(dir, "leaves.tdtp.xml"),
		"reference_date": "2026-09-29", "max_days": "30",
	}); err != nil {
		t.Fatal(err)
	}
	if err := security.NewSQLValidator(true).Validate(config.Transform.SQL); err != nil {
		t.Fatalf("example SQL is not accepted in safe mode: %v", err)
	}
	content := "NUM_LN;NUM_CASE;VERSION;RN_OKP;FIO;DATE_START;DATE_END;STATUS\n" +
		"1;10;1;0012345678;Іван | Петро;2026-09-01;2026-09-05;Готово\n" +
		"2;20;1;0012345679;Олена;2026-02-31;2026-03-05;Готово\n" +
		"3;30;1;0012345680;Марія;2026-09-01;2026-10-05;Готово\n" +
		"4;40;1;0012345681;Петро;2026-07-01;2026-07-02;Готово\n" +
		"5;50;bad;0012345682;Ганна;2026-09-01;2026-09-02;Готово\n"
	if err := os.WriteFile(config.Sources[0].DSN, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := NewProcessor(config).Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	pkt, err := packet.NewParser().ParseFile(config.Output.TDTP.Destination)
	if err != nil {
		t.Fatal(err)
	}
	if err := packet.VerifyIntegrity(pkt); err != nil {
		t.Fatal(err)
	}
	if len(pkt.Data.Rows) != 5 {
		t.Fatalf("expected all five rows, got %d", len(pkt.Data.Rows))
	}
	indices := map[string]int{}
	for i, field := range pkt.Schema.Fields {
		indices[field.Name] = i
	}
	status := []string{"OK", "ERROR", "ERROR", "ERROR", "ERROR"}
	reasons := []string{"", "DATE_START is not a valid", "duration exceeds", "outside", "VERSION must be"}
	for i, row := range pkt.Data.Rows {
		values := packet.NewParser().GetRowValues(row)
		if got := values[indices["validation_status"]]; got != status[i] {
			t.Fatalf("row %d status %q, want %q", i, got, status[i])
		}
		if !strings.Contains(values[indices["validation_error"]], reasons[i]) {
			t.Fatalf("row %d error %q, want %q", i, values[indices["validation_error"]], reasons[i])
		}
	}
}

func TestPFUCSVSQLClampsCalendarMonthWindow(t *testing.T) {
	config, err := LoadConfig(filepath.Join("..", "..", "examples", "pfu-csv", "pipeline.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if _, err := ApplyVariables(config, map[string]string{
		"csv_path": filepath.Join(dir, "export.csv"), "output_path": filepath.Join(dir, "result.tdtp.xml"),
		"reference_date": "2026-03-31", "max_days": "30",
	}); err != nil {
		t.Fatal(err)
	}
	content := "NUM_LN;NUM_CASE;VERSION;RN_OKP;FIO;DATE_START;DATE_END;STATUS\n" +
		"1;10;1;0012345678;А;2026-02-28;2026-03-01;Готово\n" +
		"2;20;1;0012345679;Б;2026-02-27;2026-02-28;Готово\n" +
		"3;30;1;0012345680;В;2026-04-30;2026-05-01;Готово\n" +
		"4;40;1;0012345681;Г;2026-05-01;2026-05-02;Готово\n"
	if err := os.WriteFile(config.Sources[0].DSN, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := NewProcessor(config).Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	pkt, err := packet.NewParser().ParseFile(config.Output.TDTP.Destination)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkt.Data.Rows) != 4 {
		t.Fatalf("expected four rows, got %d", len(pkt.Data.Rows))
	}
	statusColumn := -1
	for i, field := range pkt.Schema.Fields {
		if field.Name == "validation_status" {
			statusColumn = i
			break
		}
	}
	if statusColumn < 0 {
		t.Fatal("validation_status column missing")
	}
	want := []string{"OK", "ERROR", "OK", "ERROR"}
	for i, row := range pkt.Data.Rows {
		got := packet.NewParser().GetRowValues(row)[statusColumn]
		if got != want[i] {
			t.Fatalf("row %d status = %s, want %s", i, got, want[i])
		}
	}
}
