package processors

import (
	"context"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/mercury"
)

// mockBinder is a minimal MercuryBinder returning a fixed 32-byte key,
// enough to exercise bindAndDecodeKey/EncryptSectionsV15 without a live
// xZMercury instance.
type mockBinder struct {
	keyB64  string
	hmac    string
	mode    string
	bindErr error
	calls   int
}

func (m *mockBinder) BindKey(ctx context.Context, packageUUID, pipelineName string) (*mercury.KeyBinding, error) {
	m.calls++
	if m.bindErr != nil {
		return nil, m.bindErr
	}
	return &mercury.KeyBinding{KeyB64: m.keyB64, HMAC: m.hmac, Mode: m.mode}, nil
}

func makeProcEncryptTestPacket(t *testing.T) *packet.DataPacket {
	t.Helper()
	schema := packet.Schema{
		Fields: []packet.Field{
			{Name: "id", Type: "INTEGER", Key: true},
			{Name: "name", Type: "TEXT"},
		},
	}
	pkts, err := packet.NewGenerator().GenerateReference("proc_enc_table", schema, [][]string{
		{"1", "Alice"},
		{"2", "Bob"},
	})
	if err != nil {
		t.Fatalf("GenerateReference: %v", err)
	}
	pkt := pkts[0]
	pkt.MaterializeRows()
	return pkt
}

func TestEncryptSectionsV15_RoundTrip(t *testing.T) {
	pkt := makeProcEncryptTestPacket(t)
	binder := &mockBinder{
		keyB64: "AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA=", // 32 bytes
		mode:   "dev",
	}
	fe := NewFileEncryptor(binder, "dev-mode", pkt.Header.MessageID, "test-pipeline")

	errCode, err := fe.EncryptSectionsV15(context.Background(), pkt)
	if err != nil {
		t.Fatalf("EncryptSectionsV15: %v (errCode=%s)", err, errCode)
	}
	if errCode != "" {
		t.Errorf("errCode = %q, want empty on success", errCode)
	}
	if binder.calls != 1 {
		t.Errorf("BindKey called %d times, want 1", binder.calls)
	}
	if pkt.Version != "1.5" {
		t.Errorf("pkt.Version = %q, want 1.5", pkt.Version)
	}
	if pkt.Schema.Encryption == "" {
		t.Error("Schema not encrypted")
	}
	if pkt.Data.Encryption == "" {
		t.Error("Data not encrypted")
	}

	key, err := mercury.DecodeKey(binder.keyB64)
	if err != nil {
		t.Fatalf("DecodeKey: %v", err)
	}
	if err := packet.DecryptSections(pkt, key); err != nil {
		t.Fatalf("DecryptSections: %v", err)
	}
	if len(pkt.Data.Rows) != 2 || pkt.Data.Rows[0].Value != "1|Alice" {
		t.Errorf("decrypted rows = %+v", pkt.Data.Rows)
	}
}

func TestEncryptSectionsV15_BindKeyFails(t *testing.T) {
	pkt := makeProcEncryptTestPacket(t)
	binder := &mockBinder{bindErr: mercury.ErrMercuryUnavailable}
	fe := NewFileEncryptor(binder, "dev-mode", pkt.Header.MessageID, "test-pipeline")

	_, err := fe.EncryptSectionsV15(context.Background(), pkt)
	if err == nil {
		t.Fatal("expected error when BindKey fails")
	}
	// pkt must remain untouched — no partial encryption on failure.
	if pkt.Version == "1.5" || pkt.Schema.Encryption != "" {
		t.Error("packet was mutated despite BindKey failure")
	}
}

func TestEncryptSectionsV15_MissingServerSecret(t *testing.T) {
	pkt := makeProcEncryptTestPacket(t)
	binder := &mockBinder{keyB64: "AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA=", mode: "dev"}
	fe := NewFileEncryptor(binder, "", pkt.Header.MessageID, "test-pipeline")

	_, err := fe.EncryptSectionsV15(context.Background(), pkt)
	if err == nil {
		t.Fatal("expected error when serverSecret is empty")
	}
}
