package packet

import (
	"testing"
)

func makeEncryptionTestPacket(t *testing.T) *DataPacket {
	t.Helper()
	schema := Schema{
		Fields: []Field{
			{Name: "id", Type: "INTEGER", Key: true},
			{Name: "name", Type: "TEXT"},
			{Name: "balance", Type: "DECIMAL"},
		},
	}
	rows := [][]string{
		{"1", "John Doe", "1000.50"},
		{"2", "Jane Roe", "2500.00"},
	}
	gen := NewGenerator()
	pkts, err := gen.GenerateReference("customers", schema, rows)
	if err != nil {
		t.Fatal(err)
	}
	pkt := pkts[0]
	pkt.MaterializeRows()
	pkt.QueryContext = &QueryContext{
		OriginalQuery: Query{
			Language: "TDTQL",
			Version:  "1.0",
			Filters: &Filters{
				And: &LogicalGroup{
					Filters: []Filter{{Field: "balance", Operator: ">=", Value: "1000"}},
				},
			},
		},
		ExecutionResults: ExecutionResults{
			TotalRecordsInTable: 100,
			RecordsAfterFilters: 2,
			RecordsReturned:     2,
		},
	}
	return pkt
}

func testKey() []byte {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	return key
}

func TestEncryptDecryptSections_RoundTrip(t *testing.T) {
	pkt := makeEncryptionTestPacket(t)
	if _, err := ComputeIntegrity(pkt); err != nil {
		t.Fatalf("ComputeIntegrity: %v", err)
	}

	wantSchemaXXH3 := pkt.Schema.XXH3
	wantDataXXH3 := pkt.Data.XXH3
	wantFields := pkt.Schema.Fields
	wantRows := pkt.Data.Rows
	wantFilter := pkt.QueryContext.OriginalQuery.Filters.And.Filters[0]

	key := testKey()
	if err := EncryptSections(pkt, key); err != nil {
		t.Fatalf("EncryptSections: %v", err)
	}

	// Version must flip to 1.5.
	if pkt.Version != "1.5" {
		t.Errorf("Version = %q, want %q", pkt.Version, "1.5")
	}

	// All three sections must now be marked encrypted, with opaque content.
	if pkt.QueryContext.Encryption == "" || pkt.QueryContext.Encrypted == "" {
		t.Error("QueryContext not encrypted")
	}
	if pkt.QueryContext.OriginalQuery.Language != "" {
		t.Error("QueryContext.OriginalQuery still populated after encryption — plaintext leaked")
	}
	if pkt.Schema.Encryption == "" || pkt.Schema.Encrypted == "" {
		t.Error("Schema not encrypted")
	}
	if len(pkt.Schema.Fields) != 0 {
		t.Error("Schema.Fields still populated after encryption — plaintext leaked")
	}
	if pkt.Data.Encryption == "" {
		t.Error("Data not encrypted")
	}
	if len(pkt.Data.Rows) != 1 {
		t.Fatalf("Data.Rows after encryption = %d rows, want exactly 1 opaque row", len(pkt.Data.Rows))
	}

	// xxh3 attrs must survive encryption unchanged (computed pre-encrypt, never touched).
	if pkt.Schema.XXH3 != wantSchemaXXH3 {
		t.Errorf("Schema.XXH3 changed: got %q, want %q", pkt.Schema.XXH3, wantSchemaXXH3)
	}
	if pkt.Data.XXH3 != wantDataXXH3 {
		t.Errorf("Data.XXH3 changed: got %q, want %q", pkt.Data.XXH3, wantDataXXH3)
	}

	// Now decrypt and verify everything comes back exactly.
	if err := DecryptSections(pkt, key); err != nil {
		t.Fatalf("DecryptSections: %v", err)
	}

	if pkt.QueryContext.Encryption != "" {
		t.Error("QueryContext.Encryption not cleared after decrypt")
	}
	if pkt.QueryContext.OriginalQuery.Filters == nil ||
		pkt.QueryContext.OriginalQuery.Filters.And == nil ||
		len(pkt.QueryContext.OriginalQuery.Filters.And.Filters) != 1 ||
		pkt.QueryContext.OriginalQuery.Filters.And.Filters[0] != wantFilter {
		t.Errorf("QueryContext filter not restored correctly: got %+v", pkt.QueryContext.OriginalQuery)
	}

	if pkt.Schema.Encryption != "" {
		t.Error("Schema.Encryption not cleared after decrypt")
	}
	if len(pkt.Schema.Fields) != len(wantFields) {
		t.Fatalf("Schema.Fields count = %d, want %d", len(pkt.Schema.Fields), len(wantFields))
	}
	for i, f := range wantFields {
		if pkt.Schema.Fields[i] != f {
			t.Errorf("Schema.Fields[%d] = %+v, want %+v", i, pkt.Schema.Fields[i], f)
		}
	}
	if pkt.Schema.XXH3 != wantSchemaXXH3 {
		t.Errorf("Schema.XXH3 after decrypt = %q, want %q", pkt.Schema.XXH3, wantSchemaXXH3)
	}

	if pkt.Data.Encryption != "" {
		t.Error("Data.Encryption not cleared after decrypt")
	}
	if len(pkt.Data.Rows) != len(wantRows) {
		t.Fatalf("Data.Rows count = %d, want %d", len(pkt.Data.Rows), len(wantRows))
	}
	for i, r := range wantRows {
		if pkt.Data.Rows[i].Value != r.Value {
			t.Errorf("Data.Rows[%d] = %q, want %q", i, pkt.Data.Rows[i].Value, r.Value)
		}
	}
}

func TestEncryptSections_WithCompression(t *testing.T) {
	pkt := makeEncryptionTestPacket(t)
	if _, err := ComputeIntegrity(pkt); err != nil {
		t.Fatalf("ComputeIntegrity: %v", err)
	}
	wantRows := pkt.Data.Rows

	// Simulate compression having already run (as ToXML would do before
	// encryption, per the fixed hash->compress->encrypt order): collapse
	// to a single compressed-marked row. We don't need real zstd here —
	// only that EncryptSections treats whatever's in Data.Rows as final.
	pkt.Data.Compression = "zstd"
	pkt.Data.Rows = []Row{{Value: "FAKE_COMPRESSED_BLOB"}}

	key := testKey()
	if err := EncryptSections(pkt, key); err != nil {
		t.Fatalf("EncryptSections: %v", err)
	}
	if pkt.Data.Compression != "zstd" {
		t.Error("Compression attr lost after encryption — must stay visible for decrypt-then-decompress order")
	}

	if err := DecryptSections(pkt, key); err != nil {
		t.Fatalf("DecryptSections: %v", err)
	}
	if len(pkt.Data.Rows) != 1 || pkt.Data.Rows[0].Value != "FAKE_COMPRESSED_BLOB" {
		t.Errorf("Data.Rows after decrypt = %+v, want the single compressed blob row", pkt.Data.Rows)
	}
	_ = wantRows // compression collapses row count; only the blob content matters here
}

func TestEncryptSections_UniqueNoncePerSection(t *testing.T) {
	// One key, three sections — ciphertexts must all differ even though
	// two of them (QueryContext, Schema) are short and could collide if
	// nonces were reused.
	pkt := makeEncryptionTestPacket(t)
	key := testKey()
	if err := EncryptSections(pkt, key); err != nil {
		t.Fatalf("EncryptSections: %v", err)
	}
	if pkt.QueryContext.Encrypted == pkt.Schema.Encrypted {
		t.Error("QueryContext and Schema ciphertexts identical — possible nonce reuse")
	}
	if pkt.Schema.Encrypted == pkt.Data.Rows[0].Value {
		t.Error("Schema and Data ciphertexts identical — possible nonce reuse")
	}
}

func TestDecryptSections_WrongKey(t *testing.T) {
	pkt := makeEncryptionTestPacket(t)
	if _, err := ComputeIntegrity(pkt); err != nil {
		t.Fatalf("ComputeIntegrity: %v", err)
	}
	key := testKey()
	if err := EncryptSections(pkt, key); err != nil {
		t.Fatalf("EncryptSections: %v", err)
	}

	wrongKey := make([]byte, 32)
	for i := range wrongKey {
		wrongKey[i] = byte(255 - i)
	}
	if err := DecryptSections(pkt, wrongKey); err == nil {
		t.Error("DecryptSections with wrong key should fail (GCM auth)")
	}
}

func TestEncryptSections_InvalidKeyLength(t *testing.T) {
	pkt := makeEncryptionTestPacket(t)
	if err := EncryptSections(pkt, make([]byte, 16)); err == nil {
		t.Error("EncryptSections expected error for invalid key length")
	}
}

func TestDecryptSections_InvalidKeyLength(t *testing.T) {
	pkt := makeEncryptionTestPacket(t)
	if err := DecryptSections(pkt, make([]byte, 16)); err == nil {
		t.Error("DecryptSections expected error for invalid key length")
	}
}

func TestEncryptSections_NoQueryContext(t *testing.T) {
	// Reference packets (not response) often have no QueryContext at all —
	// must not panic, Schema/Data still encrypt normally.
	pkt := makeEncryptionTestPacket(t)
	pkt.QueryContext = nil

	key := testKey()
	if err := EncryptSections(pkt, key); err != nil {
		t.Fatalf("EncryptSections: %v", err)
	}
	if pkt.QueryContext != nil {
		t.Error("QueryContext should remain nil when it started nil")
	}
	if err := DecryptSections(pkt, key); err != nil {
		t.Fatalf("DecryptSections: %v", err)
	}
}

// TestEncryptedPacket_WireRoundTrip proves the full wire format works, not
// just the in-memory structs: serialize an encrypted packet to actual XML
// bytes (ToXML, the same path a producer sends over the wire), parse those
// bytes back with the standard Parser (the same path a consumer receives
// them with, before any decrypt call), and verify Header is readable while
// Schema/Data are opaque — then decrypt and confirm full content recovery.
func TestEncryptedPacket_WireRoundTrip(t *testing.T) {
	pkt := makeEncryptionTestPacket(t)
	if _, err := ComputeIntegrity(pkt); err != nil {
		t.Fatalf("ComputeIntegrity: %v", err)
	}
	wantFields := pkt.Schema.Fields
	wantRows := pkt.Data.Rows

	key := testKey()
	if err := EncryptSections(pkt, key); err != nil {
		t.Fatalf("EncryptSections: %v", err)
	}

	gen := NewGenerator()
	xmlBytes, err := gen.ToXML(pkt, true)
	if err != nil {
		t.Fatalf("ToXML: %v", err)
	}

	// Must be well-formed XML — a consumer's decoder must not choke on it
	// before any decrypt call happens.
	parser := NewParser()
	parsed, err := parser.ParseBytes(xmlBytes)
	if err != nil {
		t.Fatalf("ParseBytes on encrypted packet failed (wire format broken): %v\n--- XML ---\n%s", err, xmlBytes)
	}

	// Header must be fully readable without any key.
	if parsed.Header.MessageID != pkt.Header.MessageID {
		t.Errorf("Header.MessageID = %q, want %q (Header must stay plain)", parsed.Header.MessageID, pkt.Header.MessageID)
	}
	if parsed.Header.TableName != "customers" {
		t.Errorf("Header.TableName = %q, want %q", parsed.Header.TableName, "customers")
	}

	// Schema/Data must be opaque, not structurally readable.
	if parsed.Schema.Encryption != EncryptionAlgoAESGCM {
		t.Errorf("parsed Schema.Encryption = %q, want %q", parsed.Schema.Encryption, EncryptionAlgoAESGCM)
	}
	if len(parsed.Schema.Fields) != 0 {
		t.Errorf("parsed Schema.Fields not empty: %+v (should be opaque pre-decrypt)", parsed.Schema.Fields)
	}
	if parsed.Data.Encryption != EncryptionAlgoAESGCM {
		t.Errorf("parsed Data.Encryption = %q, want %q", parsed.Data.Encryption, EncryptionAlgoAESGCM)
	}
	if len(parsed.Data.Rows) != 1 {
		t.Fatalf("parsed Data.Rows = %d, want exactly 1 opaque row", len(parsed.Data.Rows))
	}

	// Decrypt the freshly-parsed packet (simulating the consumer side) and
	// confirm full content recovery.
	if err := DecryptSections(parsed, key); err != nil {
		t.Fatalf("DecryptSections on wire-parsed packet: %v", err)
	}
	if len(parsed.Schema.Fields) != len(wantFields) {
		t.Fatalf("decrypted Schema.Fields count = %d, want %d", len(parsed.Schema.Fields), len(wantFields))
	}
	for i, f := range wantFields {
		if parsed.Schema.Fields[i] != f {
			t.Errorf("decrypted Schema.Fields[%d] = %+v, want %+v", i, parsed.Schema.Fields[i], f)
		}
	}
	if len(parsed.Data.Rows) != len(wantRows) {
		t.Fatalf("decrypted Data.Rows count = %d, want %d", len(parsed.Data.Rows), len(wantRows))
	}
	for i, r := range wantRows {
		if parsed.Data.Rows[i].Value != r.Value {
			t.Errorf("decrypted Data.Rows[%d] = %q, want %q", i, parsed.Data.Rows[i].Value, r.Value)
		}
	}
}

func TestRenderParseDataRowsFragment_RoundTrip(t *testing.T) {
	rows := []Row{
		{Value: "1|John Doe|1000.50"},
		{Value: "2|special <chars> & \"quotes\""},
	}
	fragment := renderDataRowsFragment(rows)
	got, err := parseDataRowsFragment(fragment)
	if err != nil {
		t.Fatalf("parseDataRowsFragment: %v", err)
	}
	if len(got) != len(rows) {
		t.Fatalf("got %d rows, want %d", len(got), len(rows))
	}
	for i, r := range rows {
		if got[i].Value != r.Value {
			t.Errorf("row[%d] = %q, want %q", i, got[i].Value, r.Value)
		}
	}
}
