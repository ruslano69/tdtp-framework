package commands

// enc_tier_test.go — tests for the standalone --enc encryption tier.
//
// Verifies:
//  1. IsEncryptedFile extension detection
//  2. encOutputKey extension rewrite
//  3. EncryptPacket + DecryptEncBlob round-trip (mock Mercury)
//  4. DecryptEncFile pass-through for plain TDTP files
//  5. DecryptEncBlob without mercury URL → error
//  6. ConvertTDTPToCSV with encrypted input → correct CSV output
//  7. ConvertTDTPToXLSX with encrypted input → succeeds
//  8. ConvertTDTPToHTML with encrypted input → succeeds
//  9. Burn-on-read: second retrieve must fail

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/mercury"
)

// ── mock Mercury server ───────────────────────────────────────────────────────

// newMercuryEncMock returns an httptest.Server that implements the minimal subset
// of the xZMercury API used by the --enc tier:
//
//	POST /api/keys/bind     → stores key, returns KeyBinding
//	POST /api/keys/retrieve → returns key and deletes it (burn-on-read)
func newMercuryEncMock(t *testing.T) *httptest.Server {
	t.Helper()
	type store struct {
		mu   sync.Mutex
		keys map[string]string // uuid → keyB64
	}
	s := &store{keys: make(map[string]string)}
	mux := http.NewServeMux()

	mux.HandleFunc("/api/keys/bind", func(w http.ResponseWriter, r *http.Request) {
		var req mercury.BindKeyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		// Deterministic test key derived from UUID prefix (32 bytes).
		raw := make([]byte, 32)
		copy(raw, []byte(req.PackageUUID))
		keyB64 := base64.StdEncoding.EncodeToString(raw)

		s.mu.Lock()
		s.keys[req.PackageUUID] = keyB64
		s.mu.Unlock()

		resp := mercury.KeyBinding{KeyB64: keyB64, HMAC: "test-hmac", Mode: "dev"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/api/keys/retrieve", func(w http.ResponseWriter, r *http.Request) {
		var req mercury.RetrieveKeyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		keyB64, ok := s.keys[req.PackageUUID]
		if ok {
			delete(s.keys, req.PackageUUID) // burn-on-read
		}
		s.mu.Unlock()

		if !ok {
			http.Error(w, "key not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"key_b64": keyB64})
	})

	return httptest.NewServer(mux)
}

// ── helper: simple test packet ────────────────────────────────────────────────

func makeEncTestPacket(t *testing.T) *packet.DataPacket {
	t.Helper()
	schema := packet.Schema{
		Fields: []packet.Field{
			{Name: "id", Type: "INTEGER", Key: true},
			{Name: "name", Type: "TEXT"},
		},
	}
	pkts, err := packet.NewGenerator().GenerateReference("enc_table", schema, [][]string{
		{"1", "Alice"},
		{"2", "Bob"},
	})
	if err != nil {
		t.Fatalf("GenerateReference: %v", err)
	}
	return pkts[0]
}

// ── IsEncryptedFile ───────────────────────────────────────────────────────────

func TestIsEncryptedFile(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"file.tdtp.enc", true},
		{"FILE.TDTP.ENC", true},
		{"data.enc", true},
		{"file.tdtp.xml", false},
		{"file.xml", false},
		{"file.csv", false},
		{"", false},
	}
	for _, tc := range cases {
		got := IsEncryptedFile(tc.path)
		if got != tc.want {
			t.Errorf("IsEncryptedFile(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

// ── encOutputKey ─────────────────────────────────────────────────────────────

func TestEncOutputKey(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"output.tdtp.xml", "output.tdtp.enc"},
		{"output.tdtp", "output.tdtp.enc"},
		{"output.xml", "output.tdtp.enc"},
		{"output.tdtp.enc", "output.tdtp.enc"}, // already correct
		{"output", "output.tdtp.enc"},
		{"dir/file.tdtp.xml", "dir/file.tdtp.enc"},
	}
	for _, tc := range cases {
		got := encOutputKey(tc.in)
		if got != tc.want {
			t.Errorf("encOutputKey(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// ── EncryptPacket + DecryptEncBlob round-trip ─────────────────────────────────

func TestEncryptDecryptRoundTrip(t *testing.T) {
	// "dev-mode" sentinel skips HMAC verification — correct for tests with mock Mercury.
	t.Setenv("MERCURY_SERVER_SECRET", "dev-mode")
	srv := newMercuryEncMock(t)
	defer srv.Close()

	ctx := context.Background()
	pkt := makeEncTestPacket(t)

	blob, uuid, err := EncryptPacket(ctx, pkt, srv.URL, "test-pipeline")
	if err != nil {
		t.Fatalf("EncryptPacket: %v", err)
	}
	if uuid == "" {
		t.Fatal("EncryptPacket returned empty uuid")
	}
	if len(blob) == 0 {
		t.Fatal("EncryptPacket returned empty blob")
	}

	plaintext, err := DecryptEncBlob(ctx, blob, srv.URL)
	if err != nil {
		t.Fatalf("DecryptEncBlob: %v", err)
	}

	decoded, err := packet.NewParser().ParseBytes(plaintext)
	if err != nil {
		t.Fatalf("ParseBytes after decrypt: %v", err)
	}
	if decoded.Header.TableName != "enc_table" {
		t.Errorf("TableName = %q, want enc_table", decoded.Header.TableName)
	}
	if len(decoded.Data.Rows) != 2 {
		t.Errorf("Rows = %d, want 2", len(decoded.Data.Rows))
	}
}

// ── DecryptEncBlob without mercury URL ───────────────────────────────────────

func TestDecryptEncBlob_NoMercuryURL(t *testing.T) {
	ctx := context.Background()
	_, err := DecryptEncBlob(ctx, []byte("dummy"), "")
	if err == nil {
		t.Fatal("expected error when mercuryURL is empty, got nil")
	}
	if !strings.Contains(err.Error(), "--mercury-url") {
		t.Errorf("error should mention --mercury-url, got: %v", err)
	}
}

// ── DecryptEncFile pass-through for plain TDTP ────────────────────────────────

func TestDecryptEncFile_PlainPassthrough(t *testing.T) {
	dir := t.TempDir()
	pkt := makeEncTestPacket(t)
	xmlData, err := packet.NewGenerator().ToXML(pkt, true)
	if err != nil {
		t.Fatalf("ToXML: %v", err)
	}
	plain := filepath.Join(dir, "plain.tdtp.xml")
	if err := os.WriteFile(plain, xmlData, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := DecryptEncFile(context.Background(), plain, "")
	if err != nil {
		t.Fatalf("DecryptEncFile: %v", err)
	}
	if string(got) != string(xmlData) {
		t.Error("DecryptEncFile modified plain file — expected pass-through")
	}
}

// ── ConvertTDTPToCSV with .tdtp.enc input ────────────────────────────────────

func TestConvertTDTPToCSV_EncryptedInput(t *testing.T) {
	t.Setenv("MERCURY_SERVER_SECRET", "dev-mode")
	srv := newMercuryEncMock(t)
	defer srv.Close()

	ctx := context.Background()
	pkt := makeEncTestPacket(t)
	dir := t.TempDir()

	blob, _, err := EncryptPacket(ctx, pkt, srv.URL, "test")
	if err != nil {
		t.Fatalf("EncryptPacket: %v", err)
	}
	encPath := filepath.Join(dir, "data.tdtp.enc")
	if err := os.WriteFile(encPath, blob, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	csvPath := filepath.Join(dir, "out.csv")
	err = ConvertTDTPToCSV(ctx, CSVOptions{
		InputFile:  encPath,
		OutputFile: csvPath,
		Delimiter:  ',',
		MercuryURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("ConvertTDTPToCSV: %v", err)
	}

	csv, err := os.ReadFile(csvPath)
	if err != nil {
		t.Fatalf("ReadFile csv: %v", err)
	}
	content := string(csv)
	if !strings.Contains(content, "Alice") {
		t.Errorf("CSV missing 'Alice': %s", content)
	}
	if !strings.Contains(content, "Bob") {
		t.Errorf("CSV missing 'Bob': %s", content)
	}
}

// ── ConvertTDTPToXLSX with .tdtp.enc input ───────────────────────────────────

func TestConvertTDTPToXLSX_EncryptedInput(t *testing.T) {
	t.Setenv("MERCURY_SERVER_SECRET", "dev-mode")
	srv := newMercuryEncMock(t)
	defer srv.Close()

	ctx := context.Background()
	pkt := makeEncTestPacket(t)
	dir := t.TempDir()

	blob, _, err := EncryptPacket(ctx, pkt, srv.URL, "test")
	if err != nil {
		t.Fatalf("EncryptPacket: %v", err)
	}
	encPath := filepath.Join(dir, "data.tdtp.enc")
	if err := os.WriteFile(encPath, blob, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	xlsxPath := filepath.Join(dir, "out.xlsx")
	err = ConvertTDTPToXLSX(ctx, XLSXOptions{
		InputFile:  encPath,
		OutputFile: xlsxPath,
		SheetName:  "Sheet1",
		MercuryURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("ConvertTDTPToXLSX: %v", err)
	}

	info, err := os.Stat(xlsxPath)
	if err != nil {
		t.Fatalf("xlsx not created: %v", err)
	}
	if info.Size() == 0 {
		t.Error("xlsx file is empty")
	}
}

// ── ConvertTDTPToHTML with .tdtp.enc input ───────────────────────────────────

func TestConvertTDTPToHTML_EncryptedInput(t *testing.T) {
	t.Setenv("MERCURY_SERVER_SECRET", "dev-mode")
	srv := newMercuryEncMock(t)
	defer srv.Close()

	ctx := context.Background()
	pkt := makeEncTestPacket(t)
	dir := t.TempDir()

	blob, _, err := EncryptPacket(ctx, pkt, srv.URL, "test")
	if err != nil {
		t.Fatalf("EncryptPacket: %v", err)
	}
	encPath := filepath.Join(dir, "data.tdtp.enc")
	if err := os.WriteFile(encPath, blob, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	htmlPath := filepath.Join(dir, "out.html")
	err = ConvertTDTPToHTML(ctx, HTMLOptions{
		InputFile:  encPath,
		OutputFile: htmlPath,
		MercuryURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("ConvertTDTPToHTML: %v", err)
	}

	html, err := os.ReadFile(htmlPath)
	if err != nil {
		t.Fatalf("ReadFile html: %v", err)
	}
	content := string(html)
	if !strings.Contains(content, "Alice") {
		n := len(content)
		if n > 200 {
			n = 200
		}
		t.Errorf("HTML missing 'Alice': %s", content[:n])
	}
}

// ── writeEncryptedBlobToFile + encOutputKey integration ──────────────────────

func TestEncOutputKey_WriteFile(t *testing.T) {
	t.Setenv("MERCURY_SERVER_SECRET", "dev-mode")
	srv := newMercuryEncMock(t)
	defer srv.Close()

	ctx := context.Background()
	dir := t.TempDir()
	pkt := makeEncTestPacket(t)

	outPath := filepath.Join(dir, "export.tdtp.xml")
	encPath := encOutputKey(outPath)
	if !strings.HasSuffix(encPath, ".tdtp.enc") {
		t.Fatalf("encOutputKey(%q) = %q, want .tdtp.enc suffix", outPath, encPath)
	}

	blob, uuid, err := EncryptPacket(ctx, pkt, srv.URL, "test-pipeline")
	if err != nil {
		t.Fatalf("EncryptPacket: %v", err)
	}
	if err := writeEncryptedBlobToFile(blob, encPath); err != nil {
		t.Fatalf("writeEncryptedBlobToFile: %v", err)
	}

	raw, err := os.ReadFile(encPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.HasPrefix(string(raw), "<?xml") {
		t.Error("encrypted file starts with <?xml — not encrypted")
	}

	plain, err := DecryptEncBlob(ctx, raw, srv.URL)
	if err != nil {
		t.Fatalf("DecryptEncBlob: %v", err)
	}
	decoded, err := packet.NewParser().ParseBytes(plain)
	if err != nil {
		t.Fatalf("ParseBytes: %v", err)
	}
	if decoded.Header.TableName != "enc_table" {
		t.Errorf("TableName = %q, want enc_table", decoded.Header.TableName)
	}
	_ = uuid
}

// ── Burn-on-read: second retrieve must fail ───────────────────────────────────

func TestDecryptEncBlob_BurnOnRead(t *testing.T) {
	t.Setenv("MERCURY_SERVER_SECRET", "dev-mode")
	srv := newMercuryEncMock(t)
	defer srv.Close()

	ctx := context.Background()
	pkt := makeEncTestPacket(t)

	blob, _, err := EncryptPacket(ctx, pkt, srv.URL, "test")
	if err != nil {
		t.Fatalf("EncryptPacket: %v", err)
	}

	// First retrieve → success.
	if _, err := DecryptEncBlob(ctx, blob, srv.URL); err != nil {
		t.Fatalf("first DecryptEncBlob: %v", err)
	}

	// Second retrieve → key gone (burn-on-read).
	_, err = DecryptEncBlob(ctx, blob, srv.URL)
	if err == nil {
		t.Fatal("second DecryptEncBlob should fail (burn-on-read), but got nil")
	}
	t.Logf("second retrieve correctly failed: %v", err)
}
