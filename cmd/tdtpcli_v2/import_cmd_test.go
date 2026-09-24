package main

// import_cmd_test.go — import through the dispatcher against sqlite:
// full round-trip (export → import), strategies, whitelist, exit codes.

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// writeImportDB creates a sqlite database with a source table plus config.
// Returns dir, config path and db path.
func writeImportDB(t *testing.T) (dir, cfg, db string) {
	t.Helper()
	dir = t.TempDir()
	db = filepath.Join(dir, "shop.db")
	sdb, err := sql.Open("sqlite", db)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sdb.Close() }()
	for _, q := range []string{
		`CREATE TABLE goods (ID INTEGER PRIMARY KEY, Name TEXT, Price REAL)`,
		`INSERT INTO goods VALUES (1,'Laptop',1500),(2,'Mouse',50)`,
	} {
		if _, err := sdb.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	cfg = filepath.Join(dir, "shop.yaml")
	yaml := "database:\n  type: sqlite\n  database: " + db + "\n"
	if err := os.WriteFile(cfg, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, cfg, db
}

// exportGoods exports the goods table to a TDTP file via v2 itself.
func exportGoods(t *testing.T, cfg, dir, name string) string {
	t.Helper()
	out := filepath.Join(dir, name)
	if code, _, _ := runApp(t, "--config", cfg, "export", "goods", "--output", out); code != ExitOK {
		t.Fatalf("export exit = %d", code)
	}
	return out
}

func countRows(t *testing.T, db, table string) int {
	t.Helper()
	sdb, err := sql.Open("sqlite", db)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sdb.Close() }()
	var n int
	if err := sdb.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestImportCmd_RoundTrip(t *testing.T) {
	dir, cfg, db := writeImportDB(t)
	src := exportGoods(t, cfg, dir, "goods.xml")
	code, _, _ := runApp(t, "--config", cfg, "import", src, "--table", "goods_copy")
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	if n := countRows(t, db, "goods_copy"); n != 2 {
		t.Errorf("goods_copy rows = %d, want 2", n)
	}
}

func TestImportCmd_Replace(t *testing.T) {
	dir, cfg, db := writeImportDB(t)
	src := exportGoods(t, cfg, dir, "goods.xml")
	for i := 0; i < 2; i++ {
		code, _, _ := runApp(t, "--config", cfg, "import", src,
			"--table", "goods_rt", "--strategy", "replace")
		if code != ExitOK {
			t.Fatalf("pass %d: exit = %d", i, code)
		}
	}
	if n := countRows(t, db, "goods_rt"); n != 2 {
		t.Errorf("replace must not duplicate: rows = %d, want 2", n)
	}
}

func TestImportCmd_FailOnDuplicate(t *testing.T) {
	dir, cfg, _ := writeImportDB(t)
	src := exportGoods(t, cfg, dir, "goods.xml")
	code, _, _ := runApp(t, "--config", cfg, "import", src, "--table", "goods_f")
	if code != ExitOK {
		t.Fatalf("first import exit = %d", code)
	}
	code, _, _ = runApp(t, "--config", cfg, "import", src,
		"--table", "goods_f", "--strategy", "fail")
	if code != ExitFail {
		t.Errorf("duplicate with fail strategy: exit = %d, want %d", code, ExitFail)
	}
}

func TestImportCmd_FieldsWhitelist(t *testing.T) {
	dir, cfg, db := writeImportDB(t)
	src := exportGoods(t, cfg, dir, "goods.xml")
	code, _, _ := runApp(t, "--config", cfg, "import", src,
		"--table", "goods_slim", "--fields", "ID,Name")
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	sdb, err := sql.Open("sqlite", db)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sdb.Close() }()
	rows, err := sdb.Query("SELECT sql FROM sqlite_master WHERE name='goods_slim'")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	if rows.Next() {
		var ddl string
		if err := rows.Scan(&ddl); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(ddl, "Price") {
			t.Errorf("Price column must be whitelisted out, ddl: %s", ddl)
		}
	}
}

func TestImportCmd_BadStrategy(t *testing.T) {
	dir, cfg, _ := writeImportDB(t)
	src := exportGoods(t, cfg, dir, "goods.xml")
	code, _, _ := runApp(t, "--config", cfg, "import", src, "--strategy", "teleport")
	if code != ExitUsage {
		t.Errorf("exit = %d, want %d (unknown strategy)", code, ExitUsage)
	}
}

func TestImportCmd_BadExpectVar(t *testing.T) {
	dir, cfg, _ := writeImportDB(t)
	src := exportGoods(t, cfg, dir, "goods.xml")
	code, _, _ := runApp(t, "--config", cfg, "import", src, "--expect-var", "novalue")
	if code != ExitUsage {
		t.Errorf("exit = %d, want %d (malformed expect-var)", code, ExitUsage)
	}
}

func TestImportCmd_MissingFile(t *testing.T) {
	_, cfg, _ := writeImportDB(t)
	code, _, _ := runApp(t, "--config", cfg, "import", filepath.Join(t.TempDir(), "nope.xml"))
	if code != ExitFail {
		t.Errorf("exit = %d, want %d", code, ExitFail)
	}
}

func TestImportCmd_NoConfig(t *testing.T) {
	// Missing input is checked before config: unreadable input is operational.
	code, _, _ := runApp(t, "import", "f.xml")
	if code != ExitFail {
		t.Errorf("exit = %d, want %d", code, ExitFail)
	}
}
