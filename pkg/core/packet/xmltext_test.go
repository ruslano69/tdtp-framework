package packet

import (
	"encoding/xml"
	"strings"
	"testing"
)

func genOne(t *testing.T, table string, sch Schema, rows [][]string) *DataPacket {
	t.Helper()
	pkts, err := NewGenerator().GenerateReference(table, sch, rows)
	if err != nil {
		t.Fatal(err)
	}
	return pkts[0]
}

// The writer refuses text XML 1.0 cannot carry and says where it is. It used
// to write rows raw (not XML) and Header/Schema through xml.Marshal, which
// replaced the character with U+FFFD — renaming tables and columns silently.
func TestWriter_RefusesTextXMLCannotCarry(t *testing.T) {
	sch := Schema{Fields: []Field{{Name: "id", Type: "INTEGER"}, {Name: "note", Type: "TEXT"}}}
	for _, tc := range []struct {
		name, table string
		sch         Schema
		rows        [][]string
		mutate      func(*DataPacket)
		want        []string
	}{
		{"control char in a row", "t", sch, [][]string{{"1", "ok"}, {"2", "a\x01b"}}, nil,
			[]string{`row 2, field "note"`, "U+0001"}},
		{"invalid UTF-8 in a row", "t", sch, [][]string{{"1", "caf\xe9"}}, nil,
			[]string{`row 1, field "note"`, "invalid UTF-8"}},
		{"U+FFFE in a row", "t", sch, [][]string{{"1", "a\uFFFEb"}}, nil,
			[]string{"U+FFFE"}},
		{"control char in the table name", "ta\x01ble", sch, [][]string{{"1", "x"}}, nil,
			[]string{"Header.TableName", "U+0001"}},
		{"invalid UTF-8 in a field name", "t",
			Schema{Fields: []Field{{Name: "id", Type: "INTEGER"}, {Name: "caf\xe9", Type: "TEXT"}}},
			[][]string{{"1", "x"}}, nil, []string{"Schema.Fields[1].Name", "invalid UTF-8"}},
		{"in a compact row", "t", sch, [][]string{{"1", "a"}, {"2", "a\x02"}},
			func(p *DataPacket) { _ = ApplyCompact(p, []string{"id"}, false) },
			[]string{`row 2, field "note"`, "U+0002"}},
		{"in a columnar column", "t", sch, [][]string{{"1", "a"}, {"2", "b\x03"}}, EnsureColumnar,
			[]string{`column "note"`, "U+0003"}},
		{"in a Query value", "t", sch, [][]string{{"1", "x"}},
			func(p *DataPacket) {
				p.Query = &Query{Language: "TDTQL", Version: "1.0",
					Filters: &Filters{And: &LogicalGroup{Filters: []Filter{{Field: "note", Operator: "eq", Value: "v\x04"}}}}}
			},
			[]string{"Query.", "U+0004"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := genOne(t, tc.table, tc.sch, tc.rows)
			if tc.mutate != nil {
				tc.mutate(p)
			}
			_, err := NewGenerator().ToXML(p, false)
			if err == nil {
				t.Fatal("writer accepted it")
			}
			for _, w := range tc.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q does not mention %q", err, w)
				}
			}
		})
	}
}

// --fast skips DetectAndApply, so NULL reaches the writer as the 0x00
// sentinel. It was written as a raw 0x00 byte — not XML, and PostgreSQL
// refuses 0x00 in text on import. With no marker declared it is the empty
// value, and the file is XML any reader accepts.
func TestWriter_FastModeNullIsEmpty(t *testing.T) {
	g := NewGenerator()
	g.SetSkipSpecialValues(true)
	pkts, err := g.GenerateReference("t", Schema{Fields: []Field{{Name: "id", Type: "INTEGER"}, {Name: "v", Type: "TEXT"}}},
		[][]string{{"1", nullSentinel}, {"2", "x"}})
	if err != nil {
		t.Fatal(err)
	}
	for name, p := range map[string]*DataPacket{"rawRows": pkts[0]} {
		data, err := g.ToXML(p, false)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if strings.ContainsRune(string(data), 0) {
			t.Errorf("%s: a raw 0x00 byte reached the file", name)
		}
		var ref DataPacket
		if err := xml.Unmarshal(data, &ref); err != nil {
			t.Errorf("%s: not XML: %v", name, err)
		}
		back, err := NewParser().ParseBytes(data)
		if err != nil {
			t.Fatal(err)
		}
		if got := back.GetRows(); got[0][1] != "" {
			t.Errorf("%s: NULL came back as %q, want the empty value", name, got[0][1])
		}
	}
	// The materialised path (compact, columnar, compression) goes through
	// escapeValue, and must agree.
	if got := escapeValue(nullSentinel); got != "" {
		t.Errorf("escapeValue(sentinel) = %q", got)
	}
}

// A packet from another producer carrying such text is refused on the fast
// path exactly as encoding/xml refuses it on the ordinary one.
func TestReader_RefusesTextXMLCannotCarry(t *testing.T) {
	head := `<DataPacket protocol="TDTP" version="1.0"><Header><Type>reference</Type><TableName>t</TableName>` +
		`<MessageID>m</MessageID><Timestamp>2026-10-10T00:00:00Z</Timestamp></Header>` +
		`<Schema><Field name="v" type="TEXT"></Field></Schema><Data>`
	for name, row := range map[string]string{
		"C0 control":   "a\x01b",
		"invalid utf8": "caf\xe9",
		"U+FFFF":       "a\uFFFF",
	} {
		in := []byte(head + "<R>" + row + "</R></Data></DataPacket>")
		if _, ok := tryFastParse(in); ok {
			t.Errorf("%s: fast path accepted it", name)
		}
		if _, err := NewParser().ParseBytes(in); err == nil {
			t.Errorf("%s: ParseBytes accepted it", name)
		}
	}
}
