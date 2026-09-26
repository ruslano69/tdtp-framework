package main

// export_cmd_test.go — export through the dispatcher against sqlite.
// File equivalence with v1 is proven E2E (same engine); here the
// contract: table selection, filters, formats, exit codes.

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// writeExportDB creates a sqlite database with an orders table plus a
// v1-format config file. Returns dir and config path.
func writeExportDB(t *testing.T) (dir, cfg string) {
	t.Helper()
	dir = t.TempDir()
	dbPath := filepath.Join(dir, "shop.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	for _, q := range []string{
		`CREATE TABLE orders (OrderID INTEGER PRIMARY KEY, Product TEXT, Amount REAL, Status TEXT)`,
		`INSERT INTO orders VALUES (1,'Laptop',1500,'completed'),(2,'Mouse',50,'pending'),(3,'Tablet',600,'completed')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	cfg = filepath.Join(dir, "shop.yaml")
	yaml := "database:\n  type: sqlite\n  database: " + dbPath + "\n"
	if err := os.WriteFile(cfg, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, cfg
}

func TestExportCmd_Basic(t *testing.T) {
	dir, cfg := writeExportDB(t)
	out := filepath.Join(dir, "orders.tdtp.xml")
	code, _, _ := runApp(t, "--config", cfg, "export", "orders", "--output", out)
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	s := string(data)
	for _, want := range []string{"OrderID", "Laptop", `version="1.0"`} {
		if !strings.Contains(s, want) {
			t.Errorf("output should contain %q", want)
		}
	}
}

func TestExportCmd_TableFlag(t *testing.T) {
	_, cfg := writeExportDB(t)
	out := filepath.Join(t.TempDir(), "o.xml")
	code, _, _ := runApp(t, "--config", cfg, "export", "--table", "orders", "--output", out)
	if code != ExitOK {
		t.Errorf("exit = %d", code)
	}
}

func TestExportCmd_Filter(t *testing.T) {
	dir, cfg := writeExportDB(t)
	out := filepath.Join(dir, "f.tdtp.xml")
	code, _, _ := runApp(t, "--config", cfg, "export", "orders",
		"--output", out, "--where", "Status = completed", "--fields", "OrderID,Amount")
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	data, _ := os.ReadFile(out)
	s := string(data)
	if strings.Contains(s, "Mouse") {
		t.Error("pending row must be filtered out")
	}
	if strings.Contains(s, "Product") {
		t.Error("Product column must be projected out")
	}
}

func TestExportCmd_Compress(t *testing.T) {
	dir, cfg := writeExportDB(t)
	out := filepath.Join(dir, "c.tdtp.xml")
	code, _, _ := runApp(t, "--config", cfg, "export", "orders", "--output", out, "--compress")
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	data, _ := os.ReadFile(out)
	s := string(data)
	if !strings.Contains(s, `compression="zstd"`) {
		t.Error("output should be zstd-compressed")
	}
	if !strings.Contains(s, `version="1.2"`) {
		t.Error("compressed export must declare version 1.2")
	}
}

func TestExportCmd_Compact(t *testing.T) {
	dir, cfg := writeExportDB(t)
	out := filepath.Join(dir, "k.tdtp.xml")
	code, _, _ := runApp(t, "--config", cfg, "export", "orders",
		"--output", out, "--compact", "--fixed-fields", "Status")
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	data, _ := os.ReadFile(out)
	if !strings.Contains(string(data), `version="1.3.1"`) {
		t.Error("compact export must declare version 1.3.1")
	}
}

func TestExportCmd_Integrity(t *testing.T) {
	dir, cfg := writeExportDB(t)
	out := filepath.Join(dir, "i.tdtp.xml")
	code, _, _ := runApp(t, "--config", cfg, "export", "orders", "--output", out, "--integrity")
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	data, _ := os.ReadFile(out)
	if !strings.Contains(string(data), `version="1.4"`) {
		t.Error("integrity export must declare version 1.4")
	}
}

func TestExportCmd_NoTable(t *testing.T) {
	_, cfg := writeExportDB(t)
	code, _, _ := runApp(t, "--config", cfg, "export")
	if code != ExitUsage {
		t.Errorf("exit = %d, want %d (table required)", code, ExitUsage)
	}
}

func TestExportCmd_NoConfig(t *testing.T) {
	code, _, _ := runApp(t, "export", "orders")
	if code != ExitUsage {
		t.Errorf("exit = %d, want %d (config required)", code, ExitUsage)
	}
}

func TestExportCmd_MissingTable(t *testing.T) {
	_, cfg := writeExportDB(t)
	out := filepath.Join(t.TempDir(), "m.xml")
	code, _, _ := runApp(t, "--config", cfg, "export", "ghost", "--output", out)
	if code != ExitFail {
		t.Errorf("exit = %d, want %d (database failure is operational)", code, ExitFail)
	}
}

func TestExportCmd_JSON(t *testing.T) {
	dir, cfg := writeExportDB(t)
	out := filepath.Join(dir, "j.tdtp.xml")
	code, stdout, _ := runApp(t, "--config", cfg, "--json", "export", "orders", "--output", out)
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(stdout, `"table":"orders"`) {
		t.Errorf("JSON should name the table, got %q", stdout)
	}
}

// An explicitly given compression flag beats the config's export: section.
// v1 compared against the default instead, so --compress=false could not
// switch off config compress: true, and an explicit --compress-algo zstd
// (the default) lost to a config algo.
func TestExportCmd_FlagBeatsConfigCompression(t *testing.T) {
	dir, cfg := writeExportDB(t)
	data, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	extra := "export:\n  compress: true\n  compress_algo: kanzi\n"
	if err := os.WriteFile(cfg, append(data, extra...), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		args []string
		want string // expected compression attribute, "" = none
	}{
		{"config applies", nil, `compression="kanzi"`},
		{"--compress=false", []string{"--compress=false"}, ""},
		{"--compress-algo zstd", []string{"--compress-algo", "zstd"}, `compression="zstd"`},
	}
	for _, tc := range cases {
		out := filepath.Join(dir, strings.ReplaceAll(tc.name, " ", "_")+".xml")
		argv := append([]string{"--config", cfg, "export", "orders", "--output", out}, tc.args...)
		if code, _, stderr := runApp(t, argv...); code != ExitOK {
			t.Fatalf("%s: exit = %d: %s", tc.name, code, stderr)
		}
		got, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		s := string(got)
		if tc.want == "" {
			if strings.Contains(s, "compression=") {
				t.Errorf("%s: output must be uncompressed", tc.name)
			}
		} else if !strings.Contains(s, tc.want) {
			t.Errorf("%s: want %s in output", tc.name, tc.want)
		}
	}
}
