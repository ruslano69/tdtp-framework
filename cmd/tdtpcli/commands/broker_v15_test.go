package commands

// broker_v15_test.go — dual-format decrypt dispatch for the broker import
// path (parseAndDecryptBrokerMessage). Broker-transport-independent by
// design: the function only takes raw bytes, so these tests exercise it
// directly without needing a live RabbitMQ/Kafka/MSMQ connection — that
// coverage is exercised separately via a live smoke run against a real
// broker (see session notes; not part of the committed suite, same as
// this project's existing convention of mocking Mercury over HTTP rather
// than requiring live infrastructure for the standard test run).

import (
	"context"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

func makeBrokerTestPacket(t *testing.T) *packet.DataPacket {
	t.Helper()
	schema := packet.Schema{
		Fields: []packet.Field{
			{Name: "id", Type: "INTEGER", Key: true},
			{Name: "name", Type: "TEXT"},
		},
	}
	pkts, err := packet.NewGenerator().GenerateReference("broker_v15_table", schema, [][]string{
		{"1", "Alice"},
		{"2", "Bob"},
	})
	if err != nil {
		t.Fatalf("GenerateReference: %v", err)
	}
	return pkts[0]
}

func TestParseAndDecryptBrokerMessage_PlainPassthrough(t *testing.T) {
	pkt := makeBrokerTestPacket(t)
	pkt.MaterializeRows()
	xmlData, err := packet.NewGenerator().ToXML(pkt, true)
	if err != nil {
		t.Fatalf("ToXML: %v", err)
	}

	got, err := parseAndDecryptBrokerMessage(context.Background(), xmlData, "")
	if err != nil {
		t.Fatalf("parseAndDecryptBrokerMessage: %v", err)
	}
	if len(got.Data.Rows) != 2 || got.Data.Rows[0].Value != "1|Alice" {
		t.Errorf("rows = %+v", got.Data.Rows)
	}
}

func TestParseAndDecryptBrokerMessage_V15(t *testing.T) {
	t.Setenv("MERCURY_SERVER_SECRET", "dev-mode")
	srv := newMercuryEncMock(t)
	defer srv.Close()

	ctx := context.Background()
	pkt := makeBrokerTestPacket(t)
	if _, err := packet.ComputeIntegrity(pkt); err != nil {
		t.Fatalf("ComputeIntegrity: %v", err)
	}

	xmlData, _, err := EncryptPacketV15(ctx, pkt, srv.URL, "test-pipeline")
	if err != nil {
		t.Fatalf("EncryptPacketV15: %v", err)
	}
	if !strings.HasPrefix(string(xmlData), "<?xml") {
		t.Fatal("v1.5 encrypted output must still be valid XML on the wire")
	}

	got, err := parseAndDecryptBrokerMessage(ctx, xmlData, srv.URL)
	if err != nil {
		t.Fatalf("parseAndDecryptBrokerMessage: %v", err)
	}
	if len(got.Data.Rows) != 2 || got.Data.Rows[0].Value != "1|Alice" {
		t.Errorf("decrypted rows = %+v", got.Data.Rows)
	}
	if got.Schema.Encryption != "" {
		t.Error("Schema.Encryption should be cleared after decrypt")
	}
}

func TestParseAndDecryptBrokerMessage_V13Legacy(t *testing.T) {
	t.Setenv("MERCURY_SERVER_SECRET", "dev-mode")
	srv := newMercuryEncMock(t)
	defer srv.Close()

	ctx := context.Background()
	pkt := makeBrokerTestPacket(t)

	blob, _, err := EncryptPacket(ctx, pkt, srv.URL, "test-pipeline")
	if err != nil {
		t.Fatalf("EncryptPacket: %v", err)
	}
	if strings.HasPrefix(string(blob), "<?xml") {
		t.Fatal("legacy v1.3 output should be a binary blob, not XML")
	}

	got, err := parseAndDecryptBrokerMessage(ctx, blob, srv.URL)
	if err != nil {
		t.Fatalf("parseAndDecryptBrokerMessage: %v", err)
	}
	if len(got.Data.Rows) != 2 || got.Data.Rows[0].Value != "1|Alice" {
		t.Errorf("decrypted rows = %+v", got.Data.Rows)
	}
}

func TestParseAndDecryptBrokerMessage_V15WithCompression(t *testing.T) {
	// Guards the decrypt-then-decompress order: a v1.5 packet whose Data
	// was ALSO compressed must decompress AFTER decrypting, never before
	// (ciphertext isn't valid zstd).
	t.Setenv("MERCURY_SERVER_SECRET", "dev-mode")
	srv := newMercuryEncMock(t)
	defer srv.Close()

	ctx := context.Background()
	pkt := makeBrokerTestPacket(t)
	if err := compressPacketData(pkt, 3, "zstd", true); err != nil {
		t.Fatalf("compressPacketData: %v", err)
	}
	if _, err := packet.ComputeIntegrity(pkt); err != nil {
		t.Fatalf("ComputeIntegrity: %v", err)
	}

	xmlData, _, err := EncryptPacketV15(ctx, pkt, srv.URL, "test-pipeline")
	if err != nil {
		t.Fatalf("EncryptPacketV15: %v", err)
	}
	if !strings.Contains(string(xmlData), `compression="zstd"`) {
		t.Fatal("expected compression attribute to survive encryption")
	}

	got, err := parseAndDecryptBrokerMessage(ctx, xmlData, srv.URL)
	if err != nil {
		t.Fatalf("parseAndDecryptBrokerMessage: %v", err)
	}
	if got.Data.Compression != "" {
		t.Error("Compression flag should be cleared after decompression")
	}
	if len(got.Data.Rows) != 2 || got.Data.Rows[0].Value != "1|Alice" {
		t.Errorf("decrypted+decompressed rows = %+v", got.Data.Rows)
	}
}

func TestParseAndDecryptBrokerMessage_V15_NoMercuryURL(t *testing.T) {
	t.Setenv("MERCURY_SERVER_SECRET", "dev-mode")
	srv := newMercuryEncMock(t)
	defer srv.Close()

	ctx := context.Background()
	pkt := makeBrokerTestPacket(t)
	xmlData, _, err := EncryptPacketV15(ctx, pkt, srv.URL, "test-pipeline")
	if err != nil {
		t.Fatalf("EncryptPacketV15: %v", err)
	}

	_, err = parseAndDecryptBrokerMessage(ctx, xmlData, "")
	if err == nil {
		t.Fatal("expected error decrypting a v1.5 packet with no mercury URL")
	}
}
