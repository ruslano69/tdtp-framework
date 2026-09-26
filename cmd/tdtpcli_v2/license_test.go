package main

// license_test.go — the license gate v2 lacked entirely: on the Community
// floor `tdtpcli_v2 --config pg.yaml export t` ran, and so did
// `pipeline --enc`. v1 refused both up front.

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/license"
)

// communityEnv makes sure nothing on the machine supplies a license: no
// TDTP_LICENSE, and the package directory has no tdtp.lic.
func communityEnv(t *testing.T) {
	t.Helper()
	t.Setenv("TDTP_LICENSE", "")
	if _, err := os.Stat("tdtp.lic"); err == nil {
		t.Skip("a tdtp.lic in the package directory would lift the Community floor")
	}
}

// pgConfig writes a config naming a PostgreSQL database nothing listens on:
// the gate must refuse before any connection is attempted.
func pgConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pg.yaml")
	yaml := "database:\n  type: postgres\n  host: 127.0.0.1\n  port: 1\n" +
		"  user: u\n  password: p\n  database: d\n  sslmode: disable\n" +
		"broker:\n  type: rabbitmq\n  host: 127.0.0.1\n  port: 1\n  queue: q\n"
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// dbCommandArgs: one Validate-passing invocation per command that reaches a
// database. TestLicense_DBCommandTableIsComplete keeps it complete.
//
// Inputs exist (empty): some commands check the file in Validate, before the
// gate, and a missing file would end the run before the license is asked.
func dbCommandArgs(t *testing.T) map[string][]string {
	in := touch(t, "in.xml")
	xlsx := touch(t, "in.xlsx")
	return map[string][]string{
		"list":          {"list"},
		"export":        {"export", "t"},
		"import":        {"import", in},
		"export-xlsx":   {"export-xlsx", "t"},
		"import-xlsx":   {"import-xlsx", xlsx},
		"export-broker": {"export-broker", "t"},
		"import-broker": {"import-broker"},
	}
}

func touch(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLicense_CommunityRefusesNonSQLiteAdapter(t *testing.T) {
	communityEnv(t)
	cfg := pgConfig(t)
	for name, args := range dbCommandArgs(t) {
		code, _, stderr := runApp(t, append([]string{"--config", cfg}, args...)...)
		if code != ExitFail || !strings.Contains(stderr, `database adapter "postgres" is not licensed`) {
			t.Errorf("%s: exit=%d, want %d with the license refusal; stderr:\n%s", name, code, ExitFail, stderr)
		}
	}
}

// The table above must name every command that can reach a database.
// Derived from the source, not listed by hand: any method that calls
// databaseConfig or loadConfigs marks its receiver as a DB command, and
// the registry maps receivers to command names.
func TestLicense_DBCommandTableIsComplete(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	dbReceivers := map[string]bool{}
	for _, pkg := range pkgs {
		for _, f := range pkg.Files {
			for _, decl := range f.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Recv == nil || fn.Body == nil {
					continue
				}
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					name := ""
					switch fun := call.Fun.(type) {
					case *ast.Ident:
						name = fun.Name
					case *ast.SelectorExpr:
						name = fun.Sel.Name
					}
					if name == "databaseConfig" || name == "loadConfigs" {
						dbReceivers[recvTypeName(fn.Recv.List[0].Type)] = true
					}
					return true
				})
			}
		}
	}
	delete(dbReceivers, "Deps") // databaseConfig itself
	table := dbCommandArgs(t)
	for _, c := range NewApp().commands {
		typ := strings.TrimPrefix(fmt.Sprintf("%T", c), "*main.")
		if dbReceivers[typ] {
			if _, ok := table[c.Name()]; !ok {
				t.Errorf("command %q (%s) reaches a database but is missing from dbCommandArgs", c.Name(), typ)
			}
			delete(dbReceivers, typ)
		}
	}
	for typ := range dbReceivers {
		t.Errorf("%s calls databaseConfig but is not a registered command", typ)
	}
}

func recvTypeName(e ast.Expr) string {
	if star, ok := e.(*ast.StarExpr); ok {
		e = star.X
	}
	if id, ok := e.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

// The adapter gate holds only while nothing builds adapters.Config around
// it. Two builders existed before; both dropped strict_schema, neither
// was gated.
func TestAdapterConfigBuiltOnlyInDeps(t *testing.T) {
	fset := token.NewFileSet()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			sel, ok := lit.Type.(*ast.SelectorExpr)
			if ok && sel.Sel.Name == "Config" {
				if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "adapters" && path != "deps.go" {
					t.Errorf("%s: adapters.Config built outside Deps.databaseConfig — it would bypass the license gate",
						fset.Position(lit.Pos()))
				}
			}
			return true
		})
	}
}

func TestLicense_CommunityRefusesLicensedFlags(t *testing.T) {
	communityEnv(t)
	cfg := pgConfig(t)
	p := touch(t, "p.yaml")
	for _, argv := range [][]string{
		{"pipeline", p, "--enc"},
		{"pipeline", p, "--enc13"},
		{"pipeline", p, "--unsafe"},
		{"--config", cfg, "export-broker", "t", "--enc"},
	} {
		code, _, stderr := runApp(t, argv...)
		if code != ExitFail || !strings.Contains(stderr, "is not licensed") {
			t.Errorf("%v: exit=%d, want %d with a feature refusal; stderr:\n%s", argv, code, ExitFail, stderr)
		}
	}
	// Flags off: nothing asked for, nothing refused on license grounds.
	_, _, stderr := runApp(t, "pipeline", p)
	if strings.Contains(stderr, "not licensed") {
		t.Errorf("pipeline without --enc/--unsafe must not hit the feature gate:\n%s", stderr)
	}
}

// appWithLicense is NewApp with the resolver replaced, so a paid license
// can be tested without the vendor's signing key.
func appWithLicense(lic *license.License) *App {
	a := &App{
		commands: map[string]Command{},
		middlewares: []Middleware{recoverMiddleware, licenseMiddleware(
			func(string) (*license.License, error) { return lic, nil })},
	}
	RegisterAll(a)
	return a
}

func TestLicense_PaidLicensePassesGate(t *testing.T) {
	lic := license.New("ACME", "2026-01-01", "2099-01-01", license.TierProfessional,
		[]string{"sqlite", "postgres"}, []string{"enc"}, license.Limits{})
	var stdout, stderr bytes.Buffer
	code := appWithLicense(lic).Run(context.Background(),
		[]string{"--config", pgConfig(t), "export", "t"}, &stdout, &stderr)
	if strings.Contains(stderr.String(), "not licensed") {
		t.Fatalf("licensed adapter refused:\n%s", stderr.String())
	}
	if code == ExitOK {
		t.Fatal("nothing listens on port 1: the export itself should fail, past the gate")
	}
	// The banner is a notice on stderr, never on stdout (the data channel).
	if !strings.Contains(stderr.String(), "License: ACME") || strings.Contains(stdout.String(), "License:") {
		t.Errorf("banner belongs on stderr only;\nstdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	}
	stderr.Reset()
	appWithLicense(lic).Run(context.Background(),
		[]string{"--quiet", "--config", pgConfig(t), "export", "t"}, &stdout, &stderr)
	if strings.Contains(stderr.String(), "License:") {
		t.Errorf("--quiet must silence the banner:\n%s", stderr.String())
	}
}

// A present-but-invalid license is fatal, not a quiet Community downgrade.
func TestLicense_InvalidFileIsFatal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.lic")
	if err := os.WriteFile(path, []byte(`{"licensee":"x","tier":"enterprise","signature":"AAAA"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := runApp(t, "--license", path, "inspect", "f.xml")
	if code != ExitFail || !strings.Contains(stderr, "license") {
		t.Errorf("exit=%d, want %d with a license error; stderr:\n%s", code, ExitFail, stderr)
	}
}

func TestTryCompat_HoistsLicense(t *testing.T) {
	got, _, ok := tryCompat([]string{"--export", "users", "--license", "x.lic", "--config", "c.yaml"})
	want := "--license x.lic --config c.yaml export users"
	if !ok || strings.Join(got, " ") != want {
		t.Errorf("rewrote to %v, want %s", got, want)
	}
}
