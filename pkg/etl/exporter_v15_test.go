package etl

// exporter_v15_test.go — verifies Exporter.Export routes TDTP encryption to
// the right wire format based on TDTPOutputConfig.EncryptionV13:
// unset/false → v1.5 section-level (default since v1.5), true → legacy
// v1.3 whole-blob (--enc13). Both share the same BindKey/HMAC flow via a
// mock MercuryBinder — no live xZMercury needed.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/mercury"
)

type exporterMockBinder struct {
	keyB64         string
	mode           string
	calls          int
	registeredHash string
	registerCalls  int
}

func (m *exporterMockBinder) BindKey(ctx context.Context, packageUUID, pipelineName string) (*mercury.KeyBinding, error) {
	m.calls++
	return &mercury.KeyBinding{KeyB64: m.keyB64, HMAC: "unused", Mode: m.mode}, nil
}

// RegisterHash satisfies pipeline.HashRegistrar — the mandatory v1.4
// integrity registration TDTP v1.5 encryption now requires ahead of every
// export (see pkg/pipeline/produce.go). Without this method, Exporter's
// resolveHashRegistrar would fall through to a real mercury.Client
// pointed at this test's placeholder SecurityConfig.MercuryURL.
func (m *exporterMockBinder) RegisterHash(ctx context.Context, uuid string, part int, xxh3, tableName, sender, packetVersion string) error {
	m.registerCalls++
	m.registeredHash = xxh3
	return nil
}

func makeExporterTestPacket(t *testing.T) *packet.DataPacket {
	t.Helper()
	schema := packet.Schema{
		Fields: []packet.Field{
			{Name: "id", Type: "INTEGER", Key: true},
			{Name: "name", Type: "TEXT"},
		},
	}
	pkts, err := packet.NewGenerator().GenerateReference("exporter_v15_table", schema, [][]string{
		{"1", "Alice"},
		{"2", "Bob"},
	})
	if err != nil {
		t.Fatalf("GenerateReference: %v", err)
	}
	return pkts[0]
}

func TestExporter_ExportToTDTP_V15Default(t *testing.T) {
	t.Setenv("MERCURY_SERVER_SECRET", "dev-mode")
	dir := t.TempDir()
	dest := filepath.Join(dir, "out.tdtp.xml")

	cfg := OutputConfig{
		Type: "tdtp",
		TDTP: &TDTPOutputConfig{Destination: dest}, // EncryptionV13 unset → default v1.5
	}
	cfg.TDTP.Encryption = true

	binder := &exporterMockBinder{keyB64: "AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA=", mode: "dev"}
	exp := NewExporter(cfg).
		WithSecurity(SecurityConfig{MercuryURL: "unused-mock-overrides-this"}, "pkg-uuid-unused-for-v15", "test-pipeline").
		WithMercuryBinder(binder)

	pkt := makeExporterTestPacket(t)

	if _, err := exp.Export(context.Background(), pkt); err != nil {
		t.Fatalf("Export: %v", err)
	}

	raw, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(raw)
	if !strings.HasPrefix(content, "<?xml") {
		t.Fatalf("output is not valid XML (expected v1.5 to stay XML): %.200s", content)
	}
	if strings.Contains(content, "Alice") {
		t.Fatalf("output contains plaintext row data — Data section not encrypted:\n%s", content)
	}
	if !strings.Contains(content, `encryption="aes-256-gcm"`) {
		t.Error("output missing encryption attribute — not v1.5 format")
	}

	parsed, err := packet.NewParser().ParseBytes(raw)
	if err != nil {
		t.Fatalf("ParseBytes: %v", err)
	}
	// exportToTDTP regenerates parts internally (GenerateReference is
	// called again inside Export, with a fresh MessageID) — so the
	// self-consistency check is against the parsed output's own Header,
	// not the MessageID captured before Export ran.
	if parsed.Header.MessageID == "" {
		t.Fatal("parsed Header.MessageID is empty")
	}
	if !strings.Contains(content, parsed.Header.MessageID) {
		t.Error("output missing plaintext MessageID — v1.5 Header must stay readable")
	}

	key, err := mercury.DecodeKey(binder.keyB64)
	if err != nil {
		t.Fatalf("DecodeKey: %v", err)
	}
	if err := packet.DecryptSections(parsed, key); err != nil {
		t.Fatalf("DecryptSections: %v", err)
	}
	if len(parsed.Data.Rows) != 2 || parsed.Data.Rows[0].Value != "1|Alice" {
		t.Errorf("decrypted rows = %+v", parsed.Data.Rows)
	}

	// BindKey must have been called with this part's own MessageID, not a
	// separately generated UUID — the whole point of v1.5's key binding.
	if binder.calls != 1 {
		t.Errorf("BindKey called %d times, want 1", binder.calls)
	}

	// The mandatory pre-compression integrity step must have registered a
	// hash — without this, VerifyAndPrepare would block the packet with
	// HASH_NOT_REGISTERED the moment a consumer sets --mercury-url (which
	// v1.5 decryption always requires). Caught by a live smoke test before
	// this fix landed.
	if binder.registerCalls != 1 {
		t.Errorf("RegisterHash called %d times, want 1", binder.registerCalls)
	}
	if binder.registeredHash == "" {
		t.Error("RegisterHash called with empty xxh3")
	}
}

func TestExporter_ExportToTDTP_LegacyV13(t *testing.T) {
	t.Setenv("MERCURY_SERVER_SECRET", "dev-mode")
	dir := t.TempDir()
	dest := filepath.Join(dir, "out.tdtp.enc")

	cfg := OutputConfig{
		Type: "tdtp",
		TDTP: &TDTPOutputConfig{Destination: dest, EncryptionV13: true},
	}
	cfg.TDTP.Encryption = true

	binder := &exporterMockBinder{keyB64: "AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA=", mode: "dev"}
	exp := NewExporter(cfg).
		WithSecurity(SecurityConfig{}, "aaaaaaaa-0000-0000-0000-000000000001", "test-pipeline").
		WithMercuryBinder(binder)

	pkt := makeExporterTestPacket(t)

	if _, err := exp.Export(context.Background(), pkt); err != nil {
		t.Fatalf("Export: %v", err)
	}

	raw, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.HasPrefix(string(raw), "<?xml") {
		t.Error("legacy v1.3 output should be an opaque binary blob, not XML")
	}
}
