package commands

// v14_security_test.go — security gate tests for v1.4 TDTP packets.
//
// Verifies that ConvertTDTPToCSV, ConvertTDTPToXLSX, and ConvertTDTPToHTML
// all apply the same VerifyAndPrepare pre-flight for v1.4 packets:
//
//  1. v1.0 packet            → pass-through (no security gate)
//  2. v1.4 + valid hash      → local integrity passes
//  3. v1.4 + tampered hash   → blocked (VerifyIntegrity error)
//  4. v1.4 + Mercury OK      → Mercury check passes
//  5. v1.4 + Mercury tampered → blocked (ErrHashTampered)
//  6. v1.4 + Mercury not-registered → blocked (ErrHashNotRegistered)

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// ── helpers ───────────────────────────────────────────────────────────────────

// makeV14File creates a temp file containing a valid v1.4 TDTP XML packet
// with XXH3 integrity stamps. Returns the file path.
func makeV14File(t *testing.T) string {
	t.Helper()
	schema := packet.Schema{
		Fields: []packet.Field{
			{Name: "id", Type: "INTEGER", Key: true},
			{Name: "name", Type: "TEXT"},
			{Name: "salary", Type: "DECIMAL"},
		},
		// Dictionary auto-bumps version to 1.4
		Dictionary: &packet.Dictionary{
			Entries: []packet.DictEntry{
				{Short: "@ENG", Full: "Engineer"},
			},
		},
	}
	gen := packet.NewGenerator()
	pkts, err := gen.GenerateReference("employees", schema, [][]string{
		{"1", "@ENG", "95000.00"},
		{"2", "Manager", "120000.00"},
		{"3", "@ENG", "87500.00"},
	})
	if t.Failed() {
		t.FailNow()
	}
	if err != nil {
		t.Fatalf("GenerateReference: %v", err)
	}
	pkt := pkts[0]

	// Stamp XXH3 integrity hashes
	if _, err := packet.ComputeIntegrity(pkt); err != nil {
		t.Fatalf("ComputeIntegrity: %v", err)
	}

	// Serialize to XML
	xmlData, err := packet.NewGenerator().ToXML(pkt, true)
	if err != nil {
		t.Fatalf("ToXML: %v", err)
	}

	f, err := os.CreateTemp(t.TempDir(), "v14-*.tdtp.xml")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer func() { _ = f.Close() }()

	if _, err := f.Write(xmlData); err != nil {
		t.Fatalf("Write: %v", err)
	}
	return f.Name()
}

// makeTamperedV14File creates a v1.4 file whose row data was modified after
// integrity stamping — VerifyIntegrity should detect the mismatch.
func makeTamperedV14File(t *testing.T) string {
	t.Helper()
	path := makeV14File(t)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	// Replace a salary value to tamper with the payload
	tampered := strings.ReplaceAll(string(raw), "95000.00", "9999999.00")
	if tampered == string(raw) {
		t.Fatal("tampering produced no change — test invariant broken")
	}
	if err := os.WriteFile(path, []byte(tampered), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

// makeV10File creates a temp file containing a plain v1.0 TDTP packet (no hash).
func makeV10File(t *testing.T) string {
	t.Helper()
	schema := packet.Schema{
		Fields: []packet.Field{
			{Name: "id", Type: "INTEGER", Key: true},
			{Name: "name", Type: "TEXT"},
		},
	}
	gen := packet.NewGenerator()
	pkts, err := gen.GenerateReference("users", schema, [][]string{
		{"1", "Alice"},
		{"2", "Bob"},
	})
	if err != nil {
		t.Fatalf("GenerateReference: %v", err)
	}
	pkt := pkts[0]

	xmlData, err := gen.ToXML(pkt, true)
	if err != nil {
		t.Fatalf("ToXML: %v", err)
	}

	f, err := os.CreateTemp(t.TempDir(), "v10-*.tdtp.xml")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Write(xmlData); err != nil {
		t.Fatalf("Write: %v", err)
	}
	return f.Name()
}

// mercuryOKServer returns a Mercury-like httptest server that always confirms
// the hash as registered and matching. The caller provides the XXH3 it will
// return so the response body can be crafted correctly.
func mercuryOKServer(t *testing.T, xxh3 string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/hashes/") {
			http.Error(w, "unexpected endpoint", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"registered":         true,
			"match":              true,
			"uuid":               "test-uuid",
			"part":               0,
			"stored_xxh3":        xxh3,
			"table":              "employees",
			"sender":             "svc_tdtp",
			"packet_version":     "1.4",
			"expires_in_seconds": 86400,
		})
	}))
}

// mercuryNotRegisteredServer returns a server that says the hash slot is empty.
func mercuryNotRegisteredServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"registered": false,
			"match":      false,
		})
	}))
}

// mercuryTamperedServer returns a server that confirms the slot exists but
// reports a hash mismatch (tampered packet).
func mercuryTamperedServer(t *testing.T, storedXXH3 string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"registered":         true,
			"match":              false,
			"uuid":               "test-uuid",
			"part":               0,
			"stored_xxh3":        storedXXH3,
			"table":              "employees",
			"sender":             "svc_tdtp",
			"packet_version":     "1.4",
			"expires_in_seconds": 86400,
		})
	}))
}

// readXXH3FromFile parses the TDTP XML and returns pkt.XXH3.
func readXXH3FromFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	pkt, err := packet.NewParser().ParseBytes(data)
	if err != nil {
		t.Fatalf("ParseBytes: %v", err)
	}
	return pkt.XXH3
}

// ── CSV tests ─────────────────────────────────────────────────────────────────

func TestCSV_V10_PassThrough(t *testing.T) {
	src := makeV10File(t)
	out := filepath.Join(t.TempDir(), "out.csv")
	err := ConvertTDTPToCSV(context.Background(), CSVOptions{
		InputFile:  src,
		OutputFile: out,
	})
	if err != nil {
		t.Fatalf("v1.0 should pass through, got: %v", err)
	}
}

func TestCSV_V14_LocalIntegrity_Valid(t *testing.T) {
	src := makeV14File(t)
	out := filepath.Join(t.TempDir(), "out.csv")
	// No MercuryURL → local integrity only (FallbackDegrade)
	err := ConvertTDTPToCSV(context.Background(), CSVOptions{
		InputFile:  src,
		OutputFile: out,
	})
	if err != nil {
		t.Fatalf("valid v1.4 local integrity should pass, got: %v", err)
	}
}

func TestCSV_V14_LocalIntegrity_Tampered(t *testing.T) {
	src := makeTamperedV14File(t)
	out := filepath.Join(t.TempDir(), "out.csv")
	err := ConvertTDTPToCSV(context.Background(), CSVOptions{
		InputFile:  src,
		OutputFile: out,
	})
	if err == nil {
		t.Fatal("tampered v1.4 should be blocked, but export succeeded")
	}
	if !strings.Contains(err.Error(), "security check failed") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCSV_V14_Mercury_OK(t *testing.T) {
	src := makeV14File(t)
	xxh3 := readXXH3FromFile(t, src)
	srv := mercuryOKServer(t, xxh3)
	defer srv.Close()

	out := filepath.Join(t.TempDir(), "out.csv")
	err := ConvertTDTPToCSV(context.Background(), CSVOptions{
		InputFile:  src,
		OutputFile: out,
		MercuryURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("Mercury OK → should pass, got: %v", err)
	}
}

func TestCSV_V14_Mercury_NotRegistered(t *testing.T) {
	src := makeV14File(t)
	srv := mercuryNotRegisteredServer(t)
	defer srv.Close()

	out := filepath.Join(t.TempDir(), "out.csv")
	err := ConvertTDTPToCSV(context.Background(), CSVOptions{
		InputFile:  src,
		OutputFile: out,
		MercuryURL: srv.URL,
	})
	if err == nil {
		t.Fatal("not-registered hash should be blocked, but export succeeded")
	}
	if !strings.Contains(err.Error(), "security check failed") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCSV_V14_Mercury_Tampered(t *testing.T) {
	src := makeV14File(t)
	srv := mercuryTamperedServer(t, "deadbeef00000000deadbeef00000000")
	defer srv.Close()

	out := filepath.Join(t.TempDir(), "out.csv")
	err := ConvertTDTPToCSV(context.Background(), CSVOptions{
		InputFile:  src,
		OutputFile: out,
		MercuryURL: srv.URL,
	})
	if err == nil {
		t.Fatal("tampered Mercury response should be blocked, but export succeeded")
	}
	if !strings.Contains(err.Error(), "security check failed") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ── XLSX tests ────────────────────────────────────────────────────────────────

func TestXLSX_V10_PassThrough(t *testing.T) {
	src := makeV10File(t)
	out := filepath.Join(t.TempDir(), "out.xlsx")
	err := ConvertTDTPToXLSX(context.Background(), XLSXOptions{
		InputFile:  src,
		OutputFile: out,
		SheetName:  "Sheet1",
	})
	if err != nil {
		t.Fatalf("v1.0 should pass through, got: %v", err)
	}
}

func TestXLSX_V14_LocalIntegrity_Valid(t *testing.T) {
	src := makeV14File(t)
	out := filepath.Join(t.TempDir(), "out.xlsx")
	err := ConvertTDTPToXLSX(context.Background(), XLSXOptions{
		InputFile:  src,
		OutputFile: out,
		SheetName:  "Sheet1",
	})
	if err != nil {
		t.Fatalf("valid v1.4 local integrity should pass, got: %v", err)
	}
}

func TestXLSX_V14_LocalIntegrity_Tampered(t *testing.T) {
	src := makeTamperedV14File(t)
	out := filepath.Join(t.TempDir(), "out.xlsx")
	err := ConvertTDTPToXLSX(context.Background(), XLSXOptions{
		InputFile:  src,
		OutputFile: out,
		SheetName:  "Sheet1",
	})
	if err == nil {
		t.Fatal("tampered v1.4 should be blocked by XLSX converter, but export succeeded")
	}
	if !strings.Contains(err.Error(), "security check failed") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestXLSX_V14_Mercury_OK(t *testing.T) {
	src := makeV14File(t)
	xxh3 := readXXH3FromFile(t, src)
	srv := mercuryOKServer(t, xxh3)
	defer srv.Close()

	out := filepath.Join(t.TempDir(), "out.xlsx")
	err := ConvertTDTPToXLSX(context.Background(), XLSXOptions{
		InputFile:  src,
		OutputFile: out,
		SheetName:  "Sheet1",
		MercuryURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("Mercury OK → should pass, got: %v", err)
	}
}

func TestXLSX_V14_Mercury_NotRegistered(t *testing.T) {
	src := makeV14File(t)
	srv := mercuryNotRegisteredServer(t)
	defer srv.Close()

	out := filepath.Join(t.TempDir(), "out.xlsx")
	err := ConvertTDTPToXLSX(context.Background(), XLSXOptions{
		InputFile:  src,
		OutputFile: out,
		SheetName:  "Sheet1",
		MercuryURL: srv.URL,
	})
	if err == nil {
		t.Fatal("not-registered hash should block XLSX export, but export succeeded")
	}
	if !strings.Contains(err.Error(), "security check failed") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestXLSX_V14_Mercury_Tampered(t *testing.T) {
	src := makeV14File(t)
	srv := mercuryTamperedServer(t, "deadbeef00000000deadbeef00000000")
	defer srv.Close()

	out := filepath.Join(t.TempDir(), "out.xlsx")
	err := ConvertTDTPToXLSX(context.Background(), XLSXOptions{
		InputFile:  src,
		OutputFile: out,
		SheetName:  "Sheet1",
		MercuryURL: srv.URL,
	})
	if err == nil {
		t.Fatal("tampered Mercury response should block XLSX export, but export succeeded")
	}
	if !strings.Contains(err.Error(), "security check failed") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ── HTML tests ────────────────────────────────────────────────────────────────

func TestHTML_V10_PassThrough(t *testing.T) {
	src := makeV10File(t)
	out := filepath.Join(t.TempDir(), "out.html")
	err := ConvertTDTPToHTML(context.Background(), HTMLOptions{
		InputFile:  src,
		OutputFile: out,
	})
	if err != nil {
		t.Fatalf("v1.0 should pass through, got: %v", err)
	}
}

func TestHTML_V14_LocalIntegrity_Valid(t *testing.T) {
	src := makeV14File(t)
	out := filepath.Join(t.TempDir(), "out.html")
	err := ConvertTDTPToHTML(context.Background(), HTMLOptions{
		InputFile:  src,
		OutputFile: out,
	})
	if err != nil {
		t.Fatalf("valid v1.4 local integrity should pass, got: %v", err)
	}
}

func TestHTML_V14_LocalIntegrity_Tampered(t *testing.T) {
	src := makeTamperedV14File(t)
	out := filepath.Join(t.TempDir(), "out.html")
	err := ConvertTDTPToHTML(context.Background(), HTMLOptions{
		InputFile:  src,
		OutputFile: out,
	})
	if err == nil {
		t.Fatal("tampered v1.4 should be blocked by HTML converter, but export succeeded")
	}
	if !strings.Contains(err.Error(), "security check failed") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestHTML_V14_Mercury_OK(t *testing.T) {
	src := makeV14File(t)
	xxh3 := readXXH3FromFile(t, src)
	srv := mercuryOKServer(t, xxh3)
	defer srv.Close()

	out := filepath.Join(t.TempDir(), "out.html")
	err := ConvertTDTPToHTML(context.Background(), HTMLOptions{
		InputFile:  src,
		OutputFile: out,
		MercuryURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("Mercury OK → should pass, got: %v", err)
	}
}

func TestHTML_V14_Mercury_NotRegistered(t *testing.T) {
	src := makeV14File(t)
	srv := mercuryNotRegisteredServer(t)
	defer srv.Close()

	out := filepath.Join(t.TempDir(), "out.html")
	err := ConvertTDTPToHTML(context.Background(), HTMLOptions{
		InputFile:  src,
		OutputFile: out,
		MercuryURL: srv.URL,
	})
	if err == nil {
		t.Fatal("not-registered hash should block HTML export, but export succeeded")
	}
	if !strings.Contains(err.Error(), "security check failed") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestHTML_V14_Mercury_Tampered(t *testing.T) {
	src := makeV14File(t)
	srv := mercuryTamperedServer(t, "deadbeef00000000deadbeef00000000")
	defer srv.Close()

	out := filepath.Join(t.TempDir(), "out.html")
	err := ConvertTDTPToHTML(context.Background(), HTMLOptions{
		InputFile:  src,
		OutputFile: out,
		MercuryURL: srv.URL,
	})
	if err == nil {
		t.Fatal("tampered Mercury response should block HTML export, but export succeeded")
	}
	if !strings.Contains(err.Error(), "security check failed") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ── Cross-format parity test ──────────────────────────────────────────────────

// TestV14_AllFormats_TamperedBlocked verifies that all three converters block
// the same tampered packet — ensures no format can be used to bypass the gate.
func TestV14_AllFormats_TamperedBlocked(t *testing.T) {
	ctx := context.Background()

	formats := []struct {
		name string
		run  func(src, dst string) error
	}{
		{"CSV", func(src, dst string) error {
			return ConvertTDTPToCSV(ctx, CSVOptions{InputFile: src, OutputFile: dst})
		}},
		{"XLSX", func(src, dst string) error {
			return ConvertTDTPToXLSX(ctx, XLSXOptions{InputFile: src, OutputFile: dst, SheetName: "S"})
		}},
		{"HTML", func(src, dst string) error {
			return ConvertTDTPToHTML(ctx, HTMLOptions{InputFile: src, OutputFile: dst})
		}},
	}

	for _, f := range formats {
		f := f
		t.Run(f.name, func(t *testing.T) {
			// Each subtest gets its own tampered file (file is modified in place)
			src := makeTamperedV14File(t)
			dst := filepath.Join(t.TempDir(), "out")
			err := f.run(src, dst)
			if err == nil {
				t.Fatalf("[%s] tampered v1.4 packet was NOT blocked — security gate missing!", f.name)
			}
			if !strings.Contains(err.Error(), "security check failed") {
				t.Errorf("[%s] unexpected error format: %v", f.name, err)
			}
			t.Logf("[%s] correctly blocked: %v", f.name, err)
		})
	}
}

// TestV14_AllFormats_MercuryNotRegistered verifies all three converters block
// a v1.4 packet whose hash was never registered in Mercury.
func TestV14_AllFormats_MercuryNotRegistered(t *testing.T) {
	ctx := context.Background()
	srv := mercuryNotRegisteredServer(t)
	defer srv.Close()

	formats := []struct {
		name string
		run  func(src, dst string) error
	}{
		{"CSV", func(src, dst string) error {
			return ConvertTDTPToCSV(ctx, CSVOptions{InputFile: src, OutputFile: dst, MercuryURL: srv.URL})
		}},
		{"XLSX", func(src, dst string) error {
			return ConvertTDTPToXLSX(ctx, XLSXOptions{InputFile: src, OutputFile: dst, SheetName: "S", MercuryURL: srv.URL})
		}},
		{"HTML", func(src, dst string) error {
			return ConvertTDTPToHTML(ctx, HTMLOptions{InputFile: src, OutputFile: dst, MercuryURL: srv.URL})
		}},
	}

	for _, f := range formats {
		f := f
		t.Run(f.name, func(t *testing.T) {
			src := makeV14File(t)
			dst := filepath.Join(t.TempDir(), "out")
			err := f.run(src, dst)
			if err == nil {
				t.Fatalf("[%s] not-registered packet was NOT blocked — security gate missing!", f.name)
			}
			t.Logf("[%s] correctly blocked: %v", f.name, err)
		})
	}
}
