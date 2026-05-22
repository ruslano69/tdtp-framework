package packet

import (
	"bytes"
	"strings"
	"testing"
)

func makeIntegrityPacket(t *testing.T) *DataPacket {
	t.Helper()
	schema := Schema{
		Fields: []Field{
			{Name: "id", Type: "INTEGER", Key: true},
			{Name: "ns", Type: "TEXT"},
		},
		Dictionary: &Dictionary{
			Entries: []DictEntry{
				{Short: "@W3", Full: "http://www.w3.org/2000/svg"},
			},
		},
	}
	rows := [][]string{{"1", "@W3"}, {"2", "plain"}}
	gen := NewGenerator()
	pkts, err := gen.GenerateReference("tbl", schema, rows)
	if err != nil {
		t.Fatal(err)
	}
	return pkts[0]
}

// TestComputeIntegrity_Stamps verifies that ComputeIntegrity sets all three
// hash fields and that they are non-empty 32-char hex strings.
func TestComputeIntegrity_Stamps(t *testing.T) {
	pkt := makeIntegrityPacket(t)
	result, err := ComputeIntegrity(pkt)
	if err != nil {
		t.Fatalf("ComputeIntegrity: %v", err)
	}

	for name, v := range map[string]string{
		"SchemaXXH3":    result.SchemaXXH3,
		"DataXXH3":      result.DataXXH3,
		"PacketXXH3":    result.PacketXXH3,
		"pkt.Schema.XXH3": pkt.Schema.XXH3,
		"pkt.Data.XXH3":   pkt.Data.XXH3,
		"pkt.XXH3":        pkt.XXH3,
	} {
		if len(v) != 32 {
			t.Errorf("%s: expected 32-char hex, got %q (len=%d)", name, v, len(v))
		}
	}

	// result fields must equal the stamped packet fields
	if result.SchemaXXH3 != pkt.Schema.XXH3 {
		t.Errorf("result.SchemaXXH3 != pkt.Schema.XXH3")
	}
	if result.DataXXH3 != pkt.Data.XXH3 {
		t.Errorf("result.DataXXH3 != pkt.Data.XXH3")
	}
	if result.PacketXXH3 != pkt.XXH3 {
		t.Errorf("result.PacketXXH3 != pkt.XXH3")
	}
}

// TestComputeIntegrity_Idempotent verifies that calling ComputeIntegrity twice
// produces the same hashes (schema hash must ignore its own attr).
func TestComputeIntegrity_Idempotent(t *testing.T) {
	pkt := makeIntegrityPacket(t)

	r1, _ := ComputeIntegrity(pkt)
	r2, _ := ComputeIntegrity(pkt) // second call — Schema.XXH3 now set from r1

	if r1.SchemaXXH3 != r2.SchemaXXH3 {
		t.Errorf("schema hash not idempotent: %s vs %s", r1.SchemaXXH3, r2.SchemaXXH3)
	}
	if r1.DataXXH3 != r2.DataXXH3 {
		t.Errorf("data hash not idempotent: %s vs %s", r1.DataXXH3, r2.DataXXH3)
	}
	if r1.PacketXXH3 != r2.PacketXXH3 {
		t.Errorf("packet hash not idempotent: %s vs %s", r1.PacketXXH3, r2.PacketXXH3)
	}
}

// TestVerifyIntegrity_Pass verifies that a freshly stamped packet passes verification.
func TestVerifyIntegrity_Pass(t *testing.T) {
	pkt := makeIntegrityPacket(t)
	if _, err := ComputeIntegrity(pkt); err != nil {
		t.Fatal(err)
	}
	if err := VerifyIntegrity(pkt); err != nil {
		t.Errorf("expected pass, got: %v", err)
	}
}

// TestVerifyIntegrity_NoHash verifies that a packet without hashes passes silently.
func TestVerifyIntegrity_NoHash(t *testing.T) {
	pkt := makeIntegrityPacket(t)
	// No ComputeIntegrity called — XXH3 fields are empty
	if err := VerifyIntegrity(pkt); err != nil {
		t.Errorf("no-hash packet should pass silently, got: %v", err)
	}
}

// TestVerifyIntegrity_SchemaModified verifies tampered schema is detected.
func TestVerifyIntegrity_SchemaModified(t *testing.T) {
	pkt := makeIntegrityPacket(t)
	ComputeIntegrity(pkt)

	// Tamper: add a field after signing
	pkt.Schema.Fields = append(pkt.Schema.Fields, Field{Name: "evil", Type: "TEXT"})

	if err := VerifyIntegrity(pkt); err == nil {
		t.Error("expected schema hash mismatch, got nil")
	} else if !strings.Contains(err.Error(), "schema") {
		t.Errorf("expected 'schema' in error, got: %v", err)
	}
}

// TestVerifyIntegrity_DataModified verifies tampered row data is detected.
func TestVerifyIntegrity_DataModified(t *testing.T) {
	pkt := makeIntegrityPacket(t)
	ComputeIntegrity(pkt)

	// Tamper: modify a row after signing
	pkt.Data.Rows[0].Value = "999|injected"

	if err := VerifyIntegrity(pkt); err == nil {
		t.Error("expected data hash mismatch, got nil")
	} else if !strings.Contains(err.Error(), "data") {
		t.Errorf("expected 'data' in error, got: %v", err)
	}
}

// TestVerifyIntegrity_PacketHashTampered verifies that direct XXH3 attr tampering is caught.
func TestVerifyIntegrity_PacketHashTampered(t *testing.T) {
	pkt := makeIntegrityPacket(t)
	ComputeIntegrity(pkt)

	// Tamper: flip one char in the packet fingerprint
	orig := pkt.XXH3
	pkt.XXH3 = orig[:31] + "x"

	if err := VerifyIntegrity(pkt); err == nil {
		t.Error("expected packet hash mismatch, got nil")
	}
}

// TestRoundTrip_IntegrityXML verifies that ComputeIntegrity → ToXML → Parse → VerifyIntegrity
// round-trips correctly: hashes survive XML serialization and parsing.
func TestRoundTrip_IntegrityXML(t *testing.T) {
	pkt := makeIntegrityPacket(t)
	if _, err := ComputeIntegrity(pkt); err != nil {
		t.Fatal(err)
	}

	gen := NewGenerator()
	xmlBytes, err := gen.ToXML(pkt, false)
	if err != nil {
		t.Fatalf("ToXML: %v", err)
	}

	// Verify XML contains the three xxh3 attributes
	if !bytes.Contains(xmlBytes, []byte(`xxh3="`)) {
		t.Error("ToXML output missing xxh3 attributes")
	}

	parser := NewParser()
	parsed, err := parser.Parse(bytes.NewReader(xmlBytes))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if parsed.XXH3 == "" {
		t.Error("parsed DataPacket.XXH3 is empty")
	}
	if parsed.Schema.XXH3 == "" {
		t.Error("parsed Schema.XXH3 is empty")
	}
	if parsed.Data.XXH3 == "" {
		t.Error("parsed Data.XXH3 is empty")
	}

	// The critical check: verify integrity of the parsed packet
	if err := VerifyIntegrity(parsed); err != nil {
		t.Errorf("VerifyIntegrity after round-trip: %v", err)
	}
}

// TestHasIntegrity verifies the fast pre-flight check.
func TestHasIntegrity(t *testing.T) {
	pkt := makeIntegrityPacket(t)
	if HasIntegrity(pkt) {
		t.Error("expected false before ComputeIntegrity")
	}
	ComputeIntegrity(pkt)
	if !HasIntegrity(pkt) {
		t.Error("expected true after ComputeIntegrity")
	}
}

// TestIntegrity_DifferentData verifies that mutating row data on the same packet
// (same UUID) changes DataXXH3 and PacketXXH3 but not SchemaXXH3.
func TestIntegrity_DifferentData(t *testing.T) {
	gen := NewGenerator()
	schema := Schema{Fields: []Field{{Name: "v", Type: "TEXT"}}}
	pkts, _ := gen.GenerateReference("t", schema, [][]string{{"hello"}})
	pkt := pkts[0]

	ComputeIntegrity(pkt)
	schemaHash := pkt.Schema.XXH3
	dataHash1 := pkt.Data.XXH3
	packet1 := pkt.XXH3

	// Mutate rows — same UUID, same schema, different data
	pkt.Data.Rows[0].Value = "world"
	ComputeIntegrity(pkt)

	if pkt.Data.XXH3 == dataHash1 {
		t.Error("different data produced same DataXXH3")
	}
	if pkt.XXH3 == packet1 {
		t.Error("different data produced same PacketXXH3")
	}
	// Schema unchanged, UUID unchanged → SchemaXXH3 must be identical
	if pkt.Schema.XXH3 != schemaHash {
		t.Errorf("schema unchanged but SchemaXXH3 changed: %s → %s",
			schemaHash, pkt.Schema.XXH3)
	}
}

// TestIntegrity_UUIDSalt verifies that two packets with identical schema and data
// but different MessageIDs (UUIDs) produce different hashes — salt is effective.
func TestIntegrity_UUIDSalt(t *testing.T) {
	gen := NewGenerator()
	schema := Schema{Fields: []Field{{Name: "v", Type: "TEXT"}}}
	rows := [][]string{{"hello"}}

	pkts1, _ := gen.GenerateReference("t", schema, rows)
	pkts2, _ := gen.GenerateReference("t", schema, rows)

	// GenerateReference assigns a fresh UUID each time — verify they differ
	if pkts1[0].Header.MessageID == pkts2[0].Header.MessageID {
		t.Skip("UUIDs happened to match (astronomically unlikely)")
	}

	r1, _ := ComputeIntegrity(pkts1[0])
	r2, _ := ComputeIntegrity(pkts2[0])

	if r1.SchemaXXH3 == r2.SchemaXXH3 {
		t.Error("UUID salt ineffective: same SchemaXXH3 for different message IDs")
	}
	if r1.DataXXH3 == r2.DataXXH3 {
		t.Error("UUID salt ineffective: same DataXXH3 for different message IDs")
	}
	if r1.PacketXXH3 == r2.PacketXXH3 {
		t.Error("UUID salt ineffective: same PacketXXH3 for different message IDs")
	}
}

// TestIntegrity_DifferentSchema verifies that different schemas produce different schema hashes.
// With UUID salt, DataXXH3 also differs (different MessageIDs) — that's correct and expected.
// The key property: SchemaXXH3 must differ when field definitions differ.
func TestIntegrity_DifferentSchema(t *testing.T) {
	gen := NewGenerator()
	rows := [][]string{{"42"}}

	schema1 := Schema{Fields: []Field{{Name: "id", Type: "INTEGER"}}}
	schema2 := Schema{Fields: []Field{{Name: "code", Type: "TEXT"}}}

	pkts1, _ := gen.GenerateReference("t", schema1, rows)
	pkts2, _ := gen.GenerateReference("t", schema2, rows)

	r1, _ := ComputeIntegrity(pkts1[0])
	r2, _ := ComputeIntegrity(pkts2[0])

	if r1.SchemaXXH3 == r2.SchemaXXH3 {
		t.Error("different schemas produced same SchemaXXH3")
	}

	// Same packet, mutate only the schema → schema hash must change, data hash must not
	pkt := pkts1[0]
	ComputeIntegrity(pkt)
	hashBefore := pkt.Data.XXH3

	pkt.Schema.Fields[0].Name = "renamed"
	ComputeIntegrity(pkt) // same UUID, same rows, different schema

	if pkt.Schema.XXH3 == r1.SchemaXXH3 {
		t.Error("schema hash did not change after field rename")
	}
	if pkt.Data.XXH3 != hashBefore {
		t.Error("data hash changed when only schema was modified (UUID and rows unchanged)")
	}
}
