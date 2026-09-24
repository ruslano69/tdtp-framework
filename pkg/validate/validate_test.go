package validate

// validate_test.go — tdtp-validate conformance tests.
//
// Positives are built with packet.Generator (the real producer); negatives
// mutate that output, so every failure is a deviation from a known-good
// packet rather than a hand-written fixture that could itself be wrong.

import (
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/processors"
)

var validSchema = packet.Schema{
	Fields: []packet.Field{
		{Name: "id", Type: "INTEGER", Key: true},
		{Name: "note", Type: "TEXT"},
	},
}

var validRows = [][]string{
	{"1", "first"},
	{"2", "second"},
}

// validXML returns a known-good plain reference packet.
func validXML(t *testing.T) string {
	t.Helper()
	gen := packet.NewGenerator()
	pkts, err := gen.GenerateReference("orders", validSchema, validRows)
	if err != nil {
		t.Fatalf("GenerateReference: %v", err)
	}
	xmlData, err := gen.ToXML(pkts[0], true)
	if err != nil {
		t.Fatalf("ToXML: %v", err)
	}
	return string(xmlData)
}

// mustBeValid fails the test when the report holds any error.
func mustBeValid(t *testing.T, name, doc string) {
	t.Helper()
	rep := Validate([]byte(doc), name)
	if !rep.Valid() {
		t.Fatalf("expected VALID, got errors:\n  %s", strings.Join(rep.Errors, "\n  "))
	}
}

// mustBeInvalid fails when no reported error contains the want substring.
func mustBeInvalid(t *testing.T, name, doc, want string) {
	t.Helper()
	rep := Validate([]byte(doc), name)
	if rep.Valid() {
		t.Fatalf("expected INVALID containing %q, got VALID", want)
	}
	for _, e := range rep.Errors {
		if strings.Contains(e, want) {
			return
		}
	}
	t.Errorf("expected error containing %q, got:\n  %s", want, strings.Join(rep.Errors, "\n  "))
}

func TestValidate_PlainOK(t *testing.T) {
	rep := Validate([]byte(validXML(t)), "plain.xml")
	if !rep.Valid() {
		t.Fatalf("errors:\n  %s", strings.Join(rep.Errors, "\n  "))
	}
	if rep.Rows != 2 || rep.Cols != 2 {
		t.Errorf("rows=%d cols=%d, want 2/2", rep.Rows, rep.Cols)
	}
	if rep.Version != "1.0" {
		t.Errorf("version=%q, want 1.0", rep.Version)
	}
}

// mutate replaces old with new exactly once; the test fails if the anchor
// is missing (a silently non-mutating mutation proves nothing).
func mutate(t *testing.T, doc, old, new string) string {
	t.Helper()
	if strings.Count(doc, old) != 1 {
		t.Fatalf("mutation anchor %q found %d times, want exactly 1", old, strings.Count(doc, old))
	}
	return strings.Replace(doc, old, new, 1)
}

func TestValidate_Structure(t *testing.T) {
	base := validXML(t)
	cases := []struct {
		name string
		doc  func(t *testing.T) string
		want string
	}{
		{"wrong root", func(t *testing.T) string {
			doc := mutate(t, base, "<DataPacket", "<Packet")
			return mutate(t, doc, "</DataPacket>", "</Packet>")
		}, "root element is not declared: Packet"},
		{"missing MessageID", func(t *testing.T) string {
			i := strings.Index(base, "<MessageID>")
			j := strings.Index(base, "</MessageID>") + len("</MessageID>")
			return base[:i] + base[j:]
		}, "missing required child element"},
		{"unknown packet type", func(t *testing.T) string {
			return mutate(t, base, ">reference<", ">teleport<")
		}, "enumeration facet failed"},
		{"query and context", func(t *testing.T) string {
			return mutate(t, base, "<Schema>", "<Query language=\"tdtql\" version=\"1.0\"></Query><QueryContext><OriginalQuery language=\"tdtql\" version=\"1.0\"></OriginalQuery><ExecutionResults><TotalRecordsInTable>2</TotalRecordsInTable><RecordsAfterFilters>2</RecordsAfterFilters><RecordsReturned>2</RecordsReturned><MoreDataAvailable>false</MoreDataAvailable></ExecutionResults></QueryContext><Schema>")
		}, "unexpected child element QueryContext"},
		{"field without type", func(t *testing.T) string {
			return mutate(t, base, `name="note" type="TEXT"`, `name="note"`)
		}, "missing required attribute"},
		{"unknown header element", func(t *testing.T) string {
			return mutate(t, base, "</MessageID>", "</MessageID><Mood>x</Mood>")
		}, "unexpected child element Mood"},
		{"unknown attribute", func(t *testing.T) string {
			return mutate(t, base, "<Schema>", `<Schema mood="happy">`)
		}, "attribute is not declared: mood"},
		{"bad xxh3 format", func(t *testing.T) string {
			return mutate(t, base, "<DataPacket", `<DataPacket xxh3="zzz"`)
		}, "pattern facet failed"},
		{"bad layout", func(t *testing.T) string {
			return mutate(t, base, "<Data>", `<Data layout="rows">`)
		}, "enumeration facet failed"},
		{"negative limit", func(t *testing.T) string {
			return mutate(t, base, "<Schema>", `<Query language="tdtql" version="1.0"><Limit>-1</Limit></Query><Schema>`)
		}, "lower bound facet failed"},
		{"unknown operator", func(t *testing.T) string {
			return mutate(t, base, "<Schema>", `<Query language="tdtql" version="1.0"><Filters><And><Filter field="id" operator="around" value="1"/></And></Filters></Query><Schema>`)
		}, "enumeration facet failed"},
		{"bare filter", func(t *testing.T) string {
			return mutate(t, base, "<Schema>", `<Query language="tdtql" version="1.0"><Filters><Filter field="id" operator="eq" value="1"/></Filters></Query><Schema>`)
		}, "unexpected child element Filter"},
		{"encrypted querycontext with children", func(t *testing.T) string {
			return mutate(t, base, "<Schema>", `<QueryContext encryption="aes-256-gcm"><OriginalQuery language="tdtql" version="1.0"></OriginalQuery></QueryContext><Schema>`)
		}, "must not carry structured children"},
		{"not xml at all", func(t *testing.T) string {
			return "this is not xml <"
		}, "structure: document:"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mustBeInvalid(t, c.name+".xml", c.doc(t), c.want)
		})
	}
}

func TestValidate_Semantics(t *testing.T) {
	base := validXML(t)
	cases := []struct {
		name string
		doc  func(t *testing.T) string
		want string
	}{
		{"short row", func(t *testing.T) string {
			return mutate(t, base, "<R>2|second</R>", "<R>2</R>")
		}, "1 value(s), Schema declares 2 field(s)"},
		{"row count mismatch", func(t *testing.T) string {
			return mutate(t, base, "<RecordsInPart>2</RecordsInPart>", "<RecordsInPart>99</RecordsInPart>")
		}, "RecordsInPart mismatch"},
		{"duplicate field", func(t *testing.T) string {
			return mutate(t, base, `name="note" type="TEXT"`, `name="id" type="TEXT"`)
		}, "duplicate field name"},
		{"query-context counters", func(t *testing.T) string {
			qc := `<QueryContext><OriginalQuery language="tdtql" version="1.0"></OriginalQuery>` +
				`<ExecutionResults><TotalRecordsInTable>2</TotalRecordsInTable>` +
				`<RecordsAfterFilters>1</RecordsAfterFilters><RecordsReturned>2</RecordsReturned>` +
				`<MoreDataAvailable>false</MoreDataAvailable></ExecutionResults></QueryContext><Schema>`
			return mutate(t, base, "<Schema>", qc)
		}, "RecordsAfterFilters (1) < RecordsReturned (2)"},
		{"checksum without compression", func(t *testing.T) string {
			return mutate(t, base, "<Data>", `<Data checksum="abcdef0123456789">`)
		}, "checksum present without compression"},
		{"empty MessageID", func(t *testing.T) string {
			i := strings.Index(base, "<MessageID>")
			j := strings.Index(base, "</MessageID>")
			if i < 0 || j < 0 {
				t.Fatal("MessageID element not found")
			}
			return base[:i+len("<MessageID>")] + base[j:]
			// Rejected by packet.Parser itself ("header.MessageID is required");
			// the validator reports the parse error.
		}, "header.MessageID is required"},
		{"part out of range", func(t *testing.T) string {
			return mutate(t, base, "<PartNumber>1</PartNumber>", "<PartNumber>5</PartNumber>")
			// Rejected by packet.Parser itself ("PartNumber cannot exceed
			// TotalParts"); the validator reports the parse error.
		}, "PartNumber cannot exceed TotalParts"},
		{"originalquery ghost field", func(t *testing.T) string {
			qc := `<QueryContext><OriginalQuery language="tdtql" version="1.0">` +
				`<Filters><And><Filter field="ghost" operator="eq" value="1"/></And></Filters>` +
				`</OriginalQuery>` +
				`<ExecutionResults><TotalRecordsInTable>2</TotalRecordsInTable>` +
				`<RecordsAfterFilters>2</RecordsAfterFilters><RecordsReturned>2</RecordsReturned>` +
				`<MoreDataAvailable>false</MoreDataAvailable></ExecutionResults></QueryContext><Schema>`
			return mutate(t, base, "<Schema>", qc)
		}, "filters unknown field"},
		{"bad dictionary token", func(t *testing.T) string {
			return mutate(t, base, "</Schema>", `<Dictionary><Entry short="no-at-sign" full="x"/></Dictionary></Schema>`)
		}, "invalid Dictionary"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mustBeInvalid(t, c.name+".xml", c.doc(t), c.want)
		})
	}
}

// compressedXML builds a valid zstd packet through the real compressor.
func compressedXML(t *testing.T) string {
	t.Helper()
	gen := packet.NewGenerator()
	pkts, err := gen.GenerateReference("orders", validSchema, validRows)
	if err != nil {
		t.Fatalf("GenerateReference: %v", err)
	}
	pkt := pkts[0]
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
	packet.BumpVersion(pkt, "1.2")
	xmlData, err := gen.ToXML(pkt, true)
	if err != nil {
		t.Fatalf("ToXML: %v", err)
	}
	return string(xmlData)
}

func TestValidate_CompressedOK(t *testing.T) {
	mustBeValid(t, "zstd.xml", compressedXML(t))
}

func TestValidate_CompressedOldVersion(t *testing.T) {
	doc := strings.Replace(compressedXML(t), `version="1.2"`, `version="1.0"`, 1)
	mustBeInvalid(t, "zstd-old.xml", doc, "predates compression")
}

// integrityXML builds a valid v1.4 packet through the real stamper.
func integrityXML(t *testing.T) string {
	t.Helper()
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
	xmlData, err := gen.ToXML(pkt, true)
	if err != nil {
		t.Fatalf("ToXML: %v", err)
	}
	return string(xmlData)
}

func TestValidate_IntegrityOK(t *testing.T) {
	mustBeValid(t, "v14.xml", integrityXML(t))
}

func TestValidate_IntegrityTampered(t *testing.T) {
	doc := mutate(t, integrityXML(t), "<R>2|second</R>", "<R>2|second!</R>")
	mustBeInvalid(t, "v14-tampered.xml", doc, "data hash mismatch")
}

func TestValidate_PartialIntegrityStamp(t *testing.T) {
	// Data xxh3 without the packet fingerprint: neither unstamped nor whole.
	doc := mutate(t, validXML(t), "<Data>", `<Data xxh3="0123456789abcdef0123456789abcdef">`)
	mustBeInvalid(t, "partial.xml", doc, "partial stamp")
}
