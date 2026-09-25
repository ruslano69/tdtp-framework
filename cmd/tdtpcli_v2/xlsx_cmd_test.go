package main

// xlsx_cmd_test.go — xlsx trio through the dispatcher (sqlite round-trips).

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/xlsx"
	_ "modernc.org/sqlite"
)

// writeXlsxDB creates a sqlite database with a staff table plus config.
func writeXlsxDB(t *testing.T) (dir, cfg, db string) {
	t.Helper()
	dir = t.TempDir()
	db = filepath.Join(dir, "staff.db")
	sdb, err := sql.Open("sqlite", db)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sdb.Close() }()
	for _, q := range []string{
		`CREATE TABLE staff (ID INTEGER PRIMARY KEY, Name TEXT, City TEXT)`,
		`INSERT INTO staff VALUES (1,'Ann','Moscow'),(2,'Ben','SPb')`,
	} {
		if _, err := sdb.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	cfg = filepath.Join(dir, "staff.yaml")
	yaml := "database:\n  type: sqlite\n  database: " + db + "\n"
	if err := os.WriteFile(cfg, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, cfg, db
}

func TestExportXLSX_Basic(t *testing.T) {
	dir, cfg, _ := writeXlsxDB(t)
	out := filepath.Join(dir, "staff.xlsx")
	code, _, _ := runApp(t, "--config", cfg, "export-xlsx", "staff", "--output", out)
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	// Content, not just existence: the rawRows trap once shipped empty sheets.
	pkt, err := xlsx.FromXLSX(out, "Sheet1")
	if err != nil {
		t.Fatalf("FromXLSX: %v", err)
	}
	if len(pkt.Data.Rows) != 2 {
		t.Errorf("rows = %d, want 2", len(pkt.Data.Rows))
	}
}

func TestExportXLSX_Filter(t *testing.T) {
	dir, cfg, _ := writeXlsxDB(t)
	out := filepath.Join(dir, "f.xlsx")
	code, _, _ := runApp(t, "--config", cfg, "export-xlsx", "staff",
		"--output", out, "--where", "City = Moscow")
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
}

func TestExportXLSX_NoConfig(t *testing.T) {
	code, _, _ := runApp(t, "export-xlsx", "staff")
	if code != ExitUsage {
		t.Errorf("exit = %d, want %d", code, ExitUsage)
	}
}

func TestFromXLSX_RoundTrip(t *testing.T) {
	dir, cfg, _ := writeXlsxDB(t)
	xlsxPath := filepath.Join(dir, "staff.xlsx")
	if code, _, _ := runApp(t, "--config", cfg, "export-xlsx", "staff", "--output", xlsxPath); code != ExitOK {
		t.Fatalf("export exit = %d", code)
	}
	tdtpPath := filepath.Join(dir, "staff.tdtp.xml")
	code, _, _ := runApp(t, "from-xlsx", xlsxPath, "--output", tdtpPath)
	if code != ExitOK {
		t.Fatalf("from-xlsx exit = %d", code)
	}
	data, _ := os.ReadFile(tdtpPath)
	s := string(data)
	// TableName comes from the sheet name (v1 behavior: FromXLSX names
	// the packet after the worksheet, not the source table).
	if !strings.Contains(s, "Ann") || !strings.Contains(s, "Sheet1") {
		t.Errorf("round-trip lost data:\n%.400s", s)
	}
}

func TestFromXLSX_MissingFile(t *testing.T) {
	code, _, _ := runApp(t, "from-xlsx", filepath.Join(t.TempDir(), "nope.xlsx"))
	if code != ExitFail {
		t.Errorf("exit = %d, want %d", code, ExitFail)
	}
}

func TestImportXLSX_RoundTrip(t *testing.T) {
	dir, cfg, db := writeXlsxDB(t)
	xlsxPath := filepath.Join(dir, "staff.xlsx")
	if code, _, _ := runApp(t, "--config", cfg, "export-xlsx", "staff", "--output", xlsxPath); code != ExitOK {
		t.Fatalf("export exit = %d", code)
	}
	code, _, _ := runApp(t, "--config", cfg, "import-xlsx", xlsxPath)
	if code != ExitOK {
		t.Fatalf("import-xlsx exit = %d", code)
	}
	sdb, err := sql.Open("sqlite", db)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sdb.Close() }()
	var n int
	if err := sdb.QueryRow("SELECT COUNT(*) FROM staff").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("staff rows after replace-import = %d, want 2", n)
	}
}

func TestImportXLSX_BadStrategy(t *testing.T) {
	dir, cfg, _ := writeXlsxDB(t)
	xlsxPath := filepath.Join(dir, "staff.xlsx")
	if code, _, _ := runApp(t, "--config", cfg, "export-xlsx", "staff", "--output", xlsxPath); code != ExitOK {
		t.Fatalf("export exit = %d", code)
	}
	code, _, _ := runApp(t, "--config", cfg, "import-xlsx", xlsxPath, "--strategy", "teleport")
	if code != ExitUsage {
		t.Errorf("exit = %d, want %d", code, ExitUsage)
	}
}
