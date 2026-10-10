package packet

import (
	"encoding/xml"
	"reflect"
	"testing"
)

// Findings of the fuzz targets in fuzz_test.go, pinned as named tests.

// The fast path took the first <Data> it found in the bytes — inside a
// comment, a CDATA section, <Query>, or as the root — and read its rows,
// while encoding/xml (and any XSD validator) saw none. ParseBytes must read
// exactly what encoding/xml reads.
func TestParseBytes_DataOutsideRootChildIsNotRows(t *testing.T) {
	head := `<DataPacket protocol="TDTP" version="1.0"><Header><Type>reference</Type><TableName>t</TableName>` +
		`<MessageID>m</MessageID><Timestamp>2026-10-10T00:00:00Z</Timestamp></Header>`
	schema := `<Schema><Field name="a" type="TEXT"></Field></Schema>`
	for name, in := range map[string]string{
		"in a comment": head + schema + `<!-- <Data><R>hidden</R></Data> --></DataPacket>`,
		"inside Query": head + `<Query language="TDTQL" version="1.0"><Data><R>hidden</R></Data></Query>` + schema + `</DataPacket>`,
		"in CDATA":     head + schema + `<AlarmDetails><![CDATA[<Data><R>hidden</R></Data>]]></AlarmDetails></DataPacket>`,
		"root is Data": `<Data><R>hidden</R></Data>`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, ok := tryFastParse([]byte(in)); ok {
				t.Error("fast path accepted a <Data> that is not the root's child")
			}
			var ref DataPacket
			if err := xml.Unmarshal([]byte(in), &ref); err != nil {
				t.Fatalf("reference: %v", err)
			}
			pkt, err := NewParser().ParseBytes([]byte(in))
			if err != nil {
				return // refused outright is fine; reading "hidden" is not
			}
			if !reflect.DeepEqual(pkt.Data.Rows, ref.Data.Rows) {
				t.Errorf("ParseBytes rows %q, encoding/xml rows %q", pkt.Data.Rows, ref.Data.Rows)
			}
		})
	}
}

// The real <Data> next to a decoy still takes the fast path — the check
// must not turn every packet with a comment into a slow parse.
func TestParseBytes_RootDataStillFast(t *testing.T) {
	in := `<DataPacket protocol="TDTP" version="1.0"><!-- note --><Header><Type>reference</Type><TableName>t</TableName>` +
		`<MessageID>m</MessageID><Timestamp>2026-10-10T00:00:00Z</Timestamp></Header>` +
		`<Schema><Field name="a" type="TEXT"></Field></Schema><Data><R>x</R></Data></DataPacket>`
	pkt, ok := tryFastParse([]byte(in))
	if !ok || len(pkt.Data.Rows) != 1 {
		t.Fatalf("ok=%v pkt=%+v", ok, pkt)
	}
}

// One row whose values are all empty lays out by column exactly like no rows
// at all. It was read back as no rows — and VerifyRowCount then refused the
// packet. RecordsInPart tells them apart.
func TestColumnar_SingleAllEmptyRow(t *testing.T) {
	for _, n := range []int{1, 3} {
		row := make([]string, n)
		pkts, err := NewGenerator().GenerateReference("t", textSchema(n), [][]string{row})
		if err != nil {
			t.Fatal(err)
		}
		pkt := pkts[0]
		EnsureColumnar(pkt)
		if err := ExpandColumnarRows(pkt); err != nil {
			t.Fatal(err)
		}
		if got := pkt.GetRows(); len(got) != 1 {
			t.Errorf("%d field(s): %d row(s) back, want 1", n, len(got))
		}
		if err := VerifyRowCount(pkt); err != nil {
			t.Errorf("%d field(s): %v", n, err)
		}
	}
	// And no rows still means no rows.
	pkts, _ := NewGenerator().GenerateReference("t", textSchema(2), nil)
	if len(pkts) > 0 {
		EnsureColumnar(pkts[0])
		if err := ExpandColumnarRows(pkts[0]); err != nil {
			t.Fatal(err)
		}
		if got := pkts[0].GetRows(); len(got) != 0 {
			t.Errorf("empty table came back as %d row(s)", len(got))
		}
	}
}

// encoding/xml binds the first top-level element and ignores the rest; a
// <Data> inside a second one is not the packet's Data, at whatever depth.
func TestParseBytes_DataInSecondTopLevelElementIsNotRows(t *testing.T) {
	in := []byte(`<PartNumber></PartNumber><A><Data><R>hidden</R></Data></A>`)
	if _, ok := tryFastParse(in); ok {
		t.Error("fast path accepted <Data> under a second top-level element")
	}
}
