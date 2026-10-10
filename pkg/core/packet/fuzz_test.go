package packet

// Fuzz targets for the read side. Packets arrive from other parties, and
// everything that breaks the parse fires before any signature is checked
// (CLAUDE.md, "Reading someone else's packet") — so this is the code an
// untrusted input reaches first.
//
// Run one target at a time, e.g.:
//
//	go test ./pkg/core/packet -run '^$' -fuzz FuzzFastParseMatchesReference -fuzztime 60s
//
// Without -fuzz, `go test` runs every target over its seeds and over the
// corpus in testdata/fuzz, so a once-found failure stays a regression test.

import (
	"encoding/xml"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/xmlchar"
)

// fuzzRows turns one fuzzed string into rows: '\x1e' separates rows, '\x1f'
// separates fields. Every row is padded to the widest one — a packet's rows
// all have the schema's width. At most maxFields fields.
func fuzzRows(s string, maxFields int) (rows [][]string, nFields int) {
	for _, line := range strings.Split(s, "\x1e") {
		f := strings.Split(line, "\x1f")
		if len(f) > maxFields {
			f = f[:maxFields]
		}
		if len(f) > nFields {
			nFields = len(f)
		}
		rows = append(rows, f)
	}
	for i, r := range rows {
		for len(r) < nFields {
			r = append(r, "")
		}
		rows[i] = r
	}
	return rows, nFields
}

func textSchema(n int) Schema {
	fields := make([]Field, n)
	for i := range fields {
		fields[i] = Field{Name: "c" + strconv.Itoa(i), Type: "TEXT"}
	}
	return Schema{Fields: fields}
}

// seedPackets are real writer output in the shapes the parser meets: plain,
// escaped, compact, columnar, with SpecialValues.
func seedPackets(f *testing.F) [][]byte {
	f.Helper()
	gen := NewGenerator()
	var out [][]byte
	add := func(sch Schema, rows [][]string, mutate func(*DataPacket)) {
		pkts, err := gen.GenerateReference("t", sch, rows)
		if err != nil {
			f.Fatal(err)
		}
		if mutate != nil {
			mutate(pkts[0])
		}
		data, err := gen.ToXML(pkts[0], false)
		if err != nil {
			f.Fatal(err)
		}
		out = append(out, data)
	}
	add(textSchema(3), [][]string{{"1", "a", "b"}, {"2", "c|d", `e\f`}}, nil)
	add(textSchema(2), [][]string{{"x<y", "a&b"}, {"line\nbreak", `"q"`}}, nil)
	add(textSchema(2), [][]string{{"1", "\x00"}, {"2", "[NULL]"}}, nil)
	add(textSchema(3), [][]string{{"1", "Sales", "a"}, {"2", "Sales", "b"}, {"3", "IT", "c"}},
		func(p *DataPacket) { _ = ApplyCompact(p, []string{"c1"}, true) })
	add(textSchema(2), [][]string{{"1", "a|b"}, {"2", "c"}}, EnsureColumnar)
	return out
}

// The fast path must agree with xml.Unmarshal on every input it accepts. It
// is allowed to decline anything (ok=false sends the input down the ordinary
// path); it is not allowed to accept an input the reference rejects, nor to
// read one differently.
func FuzzFastParseMatchesReference(f *testing.F) {
	for _, p := range seedPackets(f) {
		f.Add(p)
	}
	f.Add([]byte(`<DataPacket><Data><R>a&amp;b</R><R>&#x41;</R></Data></DataPacket>`))
	f.Add([]byte(`<DataPacket><Data><R><![CDATA[x]]></R></Data></DataPacket>`))
	f.Add([]byte(`<DataPacket><Data></Data><Data><R>2</R></Data></DataPacket>`))

	f.Fuzz(func(t *testing.T, data []byte) {
		fast, ok := tryFastParse(data)
		if !ok {
			return
		}
		var ref DataPacket
		if err := xml.Unmarshal(data, &ref); err != nil {
			t.Fatalf("fast path accepted input the reference rejects (%v):\n%q", err, data)
		}
		if !reflect.DeepEqual(fast, &ref) {
			t.Fatalf("fast path diverged from xml.Unmarshal\n fast: %+v\n  ref: %+v\ninput: %q", fast, &ref, data)
		}
	})
}

// ParseBytes and what callers do next — row splitting, compact expansion —
// must return errors on garbage, never panic.
func FuzzParseBytes(f *testing.F) {
	for _, p := range seedPackets(f) {
		f.Add(p)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		pkt, err := NewParser().ParseBytes(data)
		if err != nil {
			return
		}
		_ = pkt.GetRows()
		if err := ExpandCompactRows(pkt); err == nil {
			_ = pkt.GetRows()
		}
	})
}

// Escaping a row's values and splitting the row again gives the values back.
func FuzzRowEscapeRoundTrip(f *testing.F) {
	for _, s := range []string{"a\x1fb", `a\|b` + "\x1f\\", "\x1f\x1f", "x\ny\x1f|", `\n` + "\x1f" + `\\n`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		values := strings.Split(s, "\x1f")
		escaped := make([]string, len(values))
		for i, v := range values {
			escaped[i] = escapeValue(v)
		}
		got := NewParser().GetRowValues(Row{Value: strings.Join(escaped, "|")})
		// A value that is exactly the adapters' NULL sentinel is written as
		// the empty value (NULL with no marker declared); anything else
		// comes back unchanged.
		want := make([]string, len(values))
		for i, v := range values {
			if v != nullSentinel {
				want[i] = v
			}
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("round trip changed the row\n got %q\nwant %q", got, want)
		}
	})
}

// Compact encoding followed by expansion gives the rows back, whatever the
// values — including "" in a requested fixed field, which ApplyCompactReport
// must keep out of fixed rather than let it decode as "same as above".
func FuzzCompactRoundTrip(f *testing.F) {
	f.Add("1\x1fSales\x1fa\x1e2\x1fSales\x1fb\x1e3\x1f\x1fc", uint8(0b010), false)
	f.Add("1\x1fa|b\x1e2\x1fa|b\x1e3\x1fc", uint8(0b011), true)
	f.Add("\x1e\x1e", uint8(0xff), true)
	f.Fuzz(func(t *testing.T, s string, fixedMask uint8, tail bool) {
		if strings.ContainsRune(s, 0) {
			t.Skip("\\x00 is the adapters' NULL sentinel, turned into a marker by the generator")
		}
		rows, n := fuzzRows(s, 6)
		pkts, err := NewGenerator().GenerateReference("t", textSchema(n), rows)
		if err != nil {
			t.Skip(err)
		}
		pkt := pkts[0]
		var fixed []string
		for i := 0; i < n; i++ {
			if fixedMask&(1<<i) != 0 {
				fixed = append(fixed, "c"+strconv.Itoa(i))
			}
		}
		if _, err := ApplyCompactReport(pkt, fixed, tail); err != nil {
			t.Fatal(err)
		}
		if err := ExpandCompactRows(pkt); err != nil {
			t.Fatalf("expanding our own compact output: %v", err)
		}
		if got := pkt.GetRows(); !reflect.DeepEqual(got, rows) {
			t.Fatalf("compact round trip changed the rows (fixed %v, tail %v)\n got %q\nwant %q", fixed, tail, got, rows)
		}
	})
}

// The columnar layout followed by its expansion gives the rows back.
func FuzzColumnarRoundTrip(f *testing.F) {
	f.Add("1\x1fa|b\x1e2\x1fc\\d")
	f.Add("\x1f\x1e\x1f")
	f.Add("x\ny")
	f.Fuzz(func(t *testing.T, s string) {
		if strings.ContainsRune(s, 0) {
			t.Skip("\\x00 is the NULL sentinel")
		}
		rows, n := fuzzRows(s, 6)
		pkts, err := NewGenerator().GenerateReference("t", textSchema(n), rows)
		if err != nil {
			t.Skip(err)
		}
		pkt := pkts[0]
		EnsureColumnar(pkt)
		if err := ExpandColumnarRows(pkt); err != nil {
			t.Fatalf("expanding our own columnar output: %v", err)
		}
		if got := pkt.GetRows(); !reflect.DeepEqual(got, rows) {
			t.Fatalf("columnar round trip changed the rows\n got %q\nwant %q", got, rows)
		}
	})
}

// What the writer produces, the parser reads back unchanged — through XML,
// both parse paths. Values XML 1.0 cannot carry at all are out of scope here.
func FuzzWriteParseRoundTrip(f *testing.F) {
	f.Add("1\x1fa&b<c>\x1e2\x1f\"q\" 'q'")
	f.Add("x\r\ny\x1f\t \x1f]]>")
	f.Add("\x1f\x1e\x1f")
	f.Fuzz(func(t *testing.T, s string) {
		if strings.ContainsRune(s, 0) {
			t.Skip("\\x00 is the NULL sentinel")
		}
		rows, n := fuzzRows(s, 6)
		gen := NewGenerator()
		pkts, err := gen.GenerateReference("t", textSchema(n), rows)
		if err != nil {
			t.Skip(err)
		}
		data, err := gen.ToXML(pkts[0], false)
		// The writer refuses exactly the text XML 1.0 cannot carry.
		clean := true
		for _, r := range rows {
			for _, v := range r {
				clean = clean && xmlchar.Clean(v)
			}
		}
		if !clean {
			if err == nil {
				t.Fatalf("writer accepted text XML cannot carry: %q", rows)
			}
			return
		}
		if err != nil {
			t.Fatalf("ToXML: %v", err)
		}
		back, err := NewParser().ParseBytes(data)
		if err != nil {
			t.Fatalf("parsing our own output: %v\n%s", err, data)
		}
		if got := back.GetRows(); !reflect.DeepEqual(got, rows) {
			t.Fatalf("write→parse changed the rows\n got %q\nwant %q\n xml %s", got, rows, data)
		}
	})
}
