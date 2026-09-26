package main

// processorflags_test.go — --mask/--validate/--normalize through the
// dispatcher. Every assertion is on OUTPUT: the file written, the table
// imported, the workbook produced. The v1 tests for these flags checked only
// that a processor had been configured, and three bugs lived underneath.

import (
	"database/sql"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	_ "modernc.org/sqlite"
)

// peopleDB: a sqlite table with addresses to mask and two invalid ones to
// filter, plus a config pointing at it.
func peopleDB(t *testing.T) (dir, cfg, dbPath string) {
	t.Helper()
	dir = t.TempDir()
	dbPath = filepath.Join(dir, "people.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`CREATE TABLE people (id INTEGER PRIMARY KEY, name TEXT, email TEXT);
		INSERT INTO people VALUES
		  (1,'Ivan','ivan.petrov@mail.ru'),(2,'Olga','not-an-email'),
		  (3,'Petr','petr.ivanov@mail.ru'),(4,'Anna','also bad')`); err != nil {
		t.Fatal(err)
	}
	cfg = filepath.Join(dir, "c.yaml")
	if err := os.WriteFile(cfg, []byte("database:\n  type: sqlite\n  database: "+dbPath+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, cfg, dbPath
}

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func parsedRows(t *testing.T, path string) (*packet.DataPacket, [][]string) {
	t.Helper()
	pkt, err := packet.NewParser().ParseFile(path)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return pkt, pkt.GetRows()
}

func TestProcessorFlags_ExportMask(t *testing.T) {
	dir, cfg, _ := peopleDB(t)
	out := filepath.Join(dir, "p.xml")
	if code, _, stderr := runApp(t, "--config", cfg, "export", "people", "-o", out, "--mask", "email"); code != ExitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	data, _ := os.ReadFile(out)
	if strings.Contains(string(data), "ivan.petrov@mail.ru") || strings.Contains(string(data), "petr.ivanov@mail.ru") {
		t.Errorf("addresses left in clear in the exported file")
	}
	if !strings.Contains(string(data), "Ivan") {
		t.Errorf("unmasked columns must survive")
	}
}

// on_error: filter removes the invalid rows from what is written, and the
// header counts what is left.
func TestProcessorFlags_ExportValidateFilter(t *testing.T) {
	dir, cfg, _ := peopleDB(t)
	rules := writeFile(t, dir, "v.yaml", "rules:\n  email: email\non_error: filter\n")
	out := filepath.Join(dir, "p.xml")
	if code, _, stderr := runApp(t, "--config", cfg, "export", "people", "-o", out, "--validate", rules); code != ExitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	pkt, rows := parsedRows(t, out)
	var ids []string
	for _, r := range rows {
		ids = append(ids, r[0])
	}
	if strings.Join(ids, ",") != "1,3" || pkt.Header.RecordsInPart != 2 {
		t.Errorf("rows=%v RecordsInPart=%d, want ids 1,3 and 2", ids, pkt.Header.RecordsInPart)
	}
}

func TestProcessorFlags_ImportMask(t *testing.T) {
	dir, cfg, dbPath := peopleDB(t)
	src := filepath.Join(dir, "src.xml")
	if code, _, stderr := runApp(t, "--config", cfg, "export", "people", "-o", src); code != ExitOK {
		t.Fatalf("export: exit %d: %s", code, stderr)
	}
	if code, _, stderr := runApp(t, "--config", cfg, "import", src, "--table", "people_masked", "--mask", "email"); code != ExitOK {
		t.Fatalf("import: exit %d: %s", code, stderr)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM people_masked WHERE email LIKE '%petrov%' OR email LIKE '%ivanov%'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("%d address(es) imported in clear", n)
	}
}

func TestProcessorFlags_ExportXLSXMask(t *testing.T) {
	dir, cfg, _ := peopleDB(t)
	book := filepath.Join(dir, "p.xlsx")
	if code, _, stderr := runApp(t, "--config", cfg, "export-xlsx", "people", "-o", book, "--mask", "email"); code != ExitOK {
		t.Fatalf("export-xlsx: exit %d: %s", code, stderr)
	}
	back := filepath.Join(dir, "back.xml")
	if code, _, stderr := runApp(t, "from-xlsx", book, "-o", back); code != ExitOK {
		t.Fatalf("from-xlsx: exit %d: %s", code, stderr)
	}
	data, _ := os.ReadFile(back)
	if strings.Contains(string(data), "ivan.petrov@mail.ru") {
		t.Errorf("address left in clear in the workbook")
	}
}

// A rules file without its section used to validate nothing, silently. Now a
// usage error, before the database is touched.
func TestProcessorFlags_RulesFileWithoutSection(t *testing.T) {
	dir, cfg, _ := peopleDB(t)
	wrong := writeFile(t, dir, "w.yaml", "fields:\n  email: lowercase\n") // a normalize file
	for _, cmd := range [][]string{
		{"export", "people", "-o", filepath.Join(dir, "x.xml")},
		{"export-broker", "people"},
		{"export-xlsx", "people", "-o", filepath.Join(dir, "x.xlsx")},
	} {
		argv := append(append([]string{"--config", cfg}, cmd...), "--validate", wrong)
		code, _, stderr := runApp(t, argv...)
		if code != ExitUsage || !strings.Contains(stderr, `no "rules:" section`) {
			t.Errorf("%s: exit %d, want %d with the missing-section error; stderr:\n%s", cmd[0], code, ExitUsage, stderr)
		}
	}
}

// pipeline takes processors from its YAML. v1's --pipeline accepted --mask
// and ignored it; here the flag does not parse.
func TestProcessorFlags_PipelineRefusesMask(t *testing.T) {
	p := writeFile(t, t.TempDir(), "p.yaml", "name: x\n")
	code, _, stderr := runApp(t, "pipeline", p, "--mask", "email")
	if code != ExitUsage || !strings.Contains(stderr, "mask") {
		t.Errorf("exit %d, want %d naming --mask; stderr:\n%s", code, ExitUsage, stderr)
	}
}

// A command that registers the bundle must build it in Run. This is the v1
// failure mode exactly: --export-broker and --pipeline listed --mask, and
// never read it. (That the built chain is then used, the compiler checks:
// an unused `procs` does not compile.)
func TestProcessorFlags_EveryHolderBuildsTheChain(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	holders := map[string]bool{}  // struct types with a processorFlags field
	builders := map[string]bool{} // receivers whose Run calls c.p.build()
	for _, pkg := range pkgs {
		for _, f := range pkg.Files {
			ast.Inspect(f, func(n ast.Node) bool {
				ts, ok := n.(*ast.TypeSpec)
				if !ok {
					return true
				}
				st, ok := ts.Type.(*ast.StructType)
				if !ok {
					return true
				}
				for _, fld := range st.Fields.List {
					if id, ok := fld.Type.(*ast.Ident); ok && id.Name == "processorFlags" {
						holders[ts.Name.Name] = true
					}
				}
				return true
			})
			for _, d := range f.Decls {
				fn, ok := d.(*ast.FuncDecl)
				if !ok || fn.Recv == nil || fn.Name.Name != "Run" || fn.Body == nil {
					continue
				}
				recv := recvTypeName(fn.Recv.List[0].Type)
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "build" {
						if inner, ok := sel.X.(*ast.SelectorExpr); ok && inner.Sel.Name == "p" {
							builders[recv] = true
						}
					}
					return true
				})
			}
		}
	}
	if len(holders) == 0 {
		t.Fatal("no command holds processorFlags — the scan is broken")
	}
	for h := range holders {
		if !builders[h] {
			t.Errorf("%s registers --mask/--validate/--normalize but its Run never builds them", h)
		}
	}
	// And the set itself: dropping the flags from one of these commands
	// would make v1 scripts using them fail to parse under the shim.
	for _, want := range []string{"exportCommand", "exportBrokerCommand", "exportXLSXCommand", "importCommand", "importXLSXCommand"} {
		if !holders[want] {
			t.Errorf("%s lost --mask/--validate/--normalize", want)
		}
	}
}
