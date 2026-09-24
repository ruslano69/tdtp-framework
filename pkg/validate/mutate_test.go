package validate

// mutate_test.go — stamp/strip integrity round-trips.
//
// Like validate_test.go, positives are built with the real producer
// (Generator, compressor, ComputeIntegrity); assertions check the version,
// the hash attributes, strict re-validation, and byte-level data identity.

import (
	"context"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/processors"
)

// parseRows parses output bytes and returns plain row values
// (decompressing first when needed).
func parseRows(t *testing.T, doc string) [][]string {
	t.Helper()
	pkt, err := packet.NewParser().ParseBytes([]byte(doc))
	if err != nil {
		t.Fatalf("ParseBytes: %v", err)
	}
	if pkt.Data.Compression != "" {
		if err := processors.DecompressPacket(context.Background(), pkt); err != nil {
			t.Fatalf("DecompressPacket: %v", err)
		}
	}
	got := pkt.GetRows()
	plain := make([][]string, len(got))
	for i, r := range got {
		plain[i] = append([]string{}, r...)
	}
	return plain
}

func equalRows(a, b [][]string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if len(a[i]) != len(b[i]) {
			return false
		}
		for j := range a[i] {
			if a[i][j] != b[i][j] {
				return false
			}
		}
	}
	return true
}

func mustContain(t *testing.T, doc, want string) {
	t.Helper()
	if !strings.Contains(doc, want) {
		t.Fatalf("output does not contain %q", want)
	}
}

func mustNotContain(t *testing.T, doc, want string) {
	t.Helper()
	if strings.Contains(doc, want) {
		t.Fatalf("output must not contain %q", want)
	}
}

func TestStamp_Plain(t *testing.T) {
	out, notes, err := stampIntegrity([]byte(validXML(t)))
	if err != nil {
		t.Fatalf("stampIntegrity: %v", err)
	}
	if len(notes) != 0 {
		t.Errorf("plain stamp should need no round-trip, got notes %v", notes)
	}
	doc := string(out)
	mustContain(t, doc, `version="1.4"`)
	mustContain(t, doc, "xxh3=")
	mustBeValid(t, "stamped.xml", doc)
	if !equalRows(parseRows(t, doc), validRows) {
		t.Errorf("stamped data differs from input rows")
	}
}

func TestStamp_Idempotent(t *testing.T) {
	once, _, err := stampIntegrity([]byte(validXML(t)))
	if err != nil {
		t.Fatalf("first stamp: %v", err)
	}
	twice, _, err := stampIntegrity(once)
	if err != nil {
		t.Fatalf("second stamp: %v", err)
	}
	if string(once) != string(twice) {
		t.Error("stamping twice must be byte-identical (same MessageID salt)")
	}
}

func TestStamp_Compressed(t *testing.T) {
	out, _, err := stampIntegrity([]byte(compressedXML(t)))
	if err != nil {
		t.Fatalf("stampIntegrity: %v", err)
	}
	doc := string(out)
	mustContain(t, doc, `version="1.4"`)
	mustContain(t, doc, `compression="zstd"`)
	mustBeValid(t, "stamped-zstd.xml", doc)
	if !equalRows(parseRows(t, doc), validRows) {
		t.Errorf("stamped+recompressed data differs from input rows")
	}
}

func TestStamp_RefusesInvalid(t *testing.T) {
	bad := strings.Replace(validXML(t), "<R>2|second</R>", "<R>2</R>", 1)
	if _, _, err := stampIntegrity([]byte(bad)); err == nil {
		// stampIntegrity itself does not validate — refusal happens in
		// processFile. Stamping here must at least not produce VALID output.
		t.Log("stamp ran (refusal is ProcessFile's job, tested below)")
	}
	rep, err := processFileContent("bad.xml", bad, true, false)
	if err == nil {
		t.Fatal("ProcessFile should refuse an invalid file")
	}
	if rep.Valid() {
		t.Error("report for refused file must not claim VALID")
	}
}

// processFileContent is ProcessFile without the disk IO, for tests.
func processFileContent(name, doc string, stamp, strip bool) (Report, error) {
	data := []byte(doc)
	if pre := validate(data, name, true); !pre.Valid() {
		return pre, errMutationRefused
	}
	var out []byte
	var notes []string
	var err error
	if stamp {
		out, notes, err = stampIntegrity(data)
	} else {
		out, err = stripIntegrity(data)
	}
	if err != nil {
		return Report{File: name}, err
	}
	rep := Validate(out, name)
	rep.Notes = notes
	if !rep.Valid() {
		return rep, errMutationInvalid
	}
	return rep, nil
}

func TestStrip_PlainV14(t *testing.T) {
	out, err := stripIntegrity([]byte(integrityXML(t)))
	if err != nil {
		t.Fatalf("stripIntegrity: %v", err)
	}
	doc := string(out)
	mustContain(t, doc, `version="1.0"`)
	mustNotContain(t, doc, "xxh3=")
	mustBeValid(t, "stripped.xml", doc)
	if !equalRows(parseRows(t, doc), validRows) {
		t.Errorf("stripped data differs from input rows")
	}
}

func TestStrip_CompressedV14(t *testing.T) {
	doc := integrityXML(t) // plain v1.4 base; compress path covered via stamp test data
	_ = doc
	// Build compressed v1.4: stamp covers plaintext, then compress by hand
	// the way the export chain would (integrity BEFORE compress).
	gen := packet.NewGenerator()
	pkts, err := gen.GenerateReference("orders", validSchema, validRows)
	if err != nil {
		t.Fatalf("GenerateReference: %v", err)
	}
	pkt := pkts[0]
	packet.BumpVersion(pkt, "1.4")
	if _, err := packet.ComputeIntegrity(pkt); err != nil {
		t.Fatalf("ComputeIntegrity: %v", err)
	}
	pkt.MaterializeRows()
	rows := make([]string, len(pkt.Data.Rows))
	for i, r := range pkt.Data.Rows {
		rows[i] = r.Value
	}
	blob, _, err := processors.CompressDataForTdtpAlgo(rows, "zstd", 3)
	if err != nil {
		t.Fatalf("compress: %v", err)
	}
	pkt.Data.Compression = "zstd"
	pkt.Data.Checksum = processors.ComputeChecksum([]byte(blob))
	pkt.Data.Rows = []packet.Row{{Value: blob}}
	raw, err := gen.ToXML(pkt, true)
	if err != nil {
		t.Fatalf("ToXML: %v", err)
	}
	mustBeValid(t, "compressed-v14.xml", string(raw))

	out, err := stripIntegrity(raw)
	if err != nil {
		t.Fatalf("stripIntegrity: %v", err)
	}
	sdoc := string(out)
	mustContain(t, sdoc, `version="1.2"`)
	mustContain(t, sdoc, `compression="zstd"`)
	mustNotContain(t, sdoc, "xxh3=")
	mustBeValid(t, "stripped-zstd.xml", sdoc)
	if !equalRows(parseRows(t, sdoc), validRows) {
		t.Errorf("stripped data differs from input rows")
	}
}

func TestStrip_NoHashesNoop(t *testing.T) {
	out, err := stripIntegrity([]byte(validXML(t)))
	if err != nil {
		t.Fatalf("stripIntegrity: %v", err)
	}
	doc := string(out)
	mustContain(t, doc, `version="1.0"`)
	mustBeValid(t, "stripped-plain.xml", doc)
}

func TestResolveVersion(t *testing.T) {
	cases := []struct {
		name string
		pkt  packet.DataPacket
		want string
	}{
		{"plain", packet.DataPacket{Version: "1.4"}, "1.0"},
		{"compressed", packet.DataPacket{Version: "1.4",
			Data: packet.Data{Compression: "zstd"}}, "1.2"},
		{"compact", packet.DataPacket{Version: "1.4",
			Data: packet.Data{Compact: true}}, "1.3.1"},
		{"compact+compressed", packet.DataPacket{Version: "1.4",
			Data: packet.Data{Compact: true, Compression: "zstd"}}, "1.3.1"},
		{"dictionary", packet.DataPacket{Version: "1.0",
			Schema: packet.Schema{Dictionary: &packet.Dictionary{Entries: []packet.DictEntry{
				{Short: "@MRC", Full: "http://x"},
			}}}}, "1.4"},
		{"encrypted", packet.DataPacket{Version: "1.4",
			Data: packet.Data{Encryption: "aes-256-gcm"}}, "1.5"},
	}
	for _, c := range cases {
		if got := resolveVersion(&c.pkt); got != c.want {
			t.Errorf("%s: resolveVersion = %q, want %q", c.name, got, c.want)
		}
	}
}
