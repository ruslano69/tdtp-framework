package packet

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"testing/iotest"
)

// parseReference — эталон: полный разбор через reflection, как было до fast path.
func parseReference(t *testing.T, data []byte) *DataPacket {
	t.Helper()
	var pkt DataPacket
	if err := xml.Unmarshal(data, &pkt); err != nil {
		t.Fatalf("reference unmarshal failed: %v", err)
	}
	return &pkt
}

// assertFastMatchesReference требует, чтобы быстрый путь сработал и дал
// результат, неотличимый от эталонного.
func assertFastMatchesReference(t *testing.T, data []byte) {
	t.Helper()
	want := parseReference(t, data)

	got, ok := tryFastParse(data)
	if !ok {
		t.Fatalf("fast path bailed out on a packet it should handle:\n%s", data)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("fast path diverged from reference\n got: %+v\nwant: %+v", got, want)
		if len(got.Data.Rows) == len(want.Data.Rows) {
			for i := range got.Data.Rows {
				if got.Data.Rows[i] != want.Data.Rows[i] {
					t.Errorf("row %d: got %q, want %q", i, got.Data.Rows[i].Value, want.Data.Rows[i].Value)
				}
			}
		} else {
			t.Errorf("row count: got %d, want %d", len(got.Data.Rows), len(want.Data.Rows))
		}
	}
}

// buildPacketBytes прогоняет строки через настоящий writer, чтобы тест ходил по
// тому же формату, который система реально производит.
func buildPacketBytes(t *testing.T, rows [][]string, nFields int) []byte {
	t.Helper()
	fields := make([]Field, nFields)
	for i := range fields {
		fields[i] = Field{Name: "col" + strconv.Itoa(i), Type: "TEXT"}
	}
	pkts, err := NewGenerator().GenerateReference("t", Schema{Fields: fields}, rows)
	if err != nil || len(pkts) == 0 {
		t.Fatalf("generate: %v", err)
	}
	b, err := packetToBytes(pkts[0])
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	return b
}

// TestFastParse_MatchesReference_RealWriter — эквивалентность на пакетах,
// произведённых настоящим writer'ом, включая значения со спецсимволами.
func TestFastParse_MatchesReference_RealWriter(t *testing.T) {
	cases := []struct {
		name string
		rows [][]string
	}{
		{"plain", [][]string{{"a", "b", "c"}, {"d", "e", "f"}}},
		{"empty values", [][]string{{"", "", ""}, {"x", "", "z"}}},
		{"xml specials", [][]string{{"a<b", "c>d", "e&f"}}},
		{"quotes", [][]string{{`say "hi"`, "it's", "both\"'"}}},
		{"tdtp escapes", [][]string{{"a|b", `c\d`, `e\|f`}}},
		{"unicode", [][]string{{"Привіт", "日本語", "emoji 🚀"}}},
		{"whitespace", [][]string{{" lead", "trail ", "in\tside"}}},
		{"long", [][]string{{strings.Repeat("x", 5000), "b", "c"}}},
		{"single row", [][]string{{"only", "one", "row"}}},
		{"all specials", [][]string{{"<>&\"'|\\", "&amp;", "&#60;"}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertFastMatchesReference(t, buildPacketBytes(t, tc.rows, 3))
		})
	}
}

// TestFastParse_MatchesReference_ManyRows — объёмный пакет.
func TestFastParse_MatchesReference_ManyRows(t *testing.T) {
	rows := make([][]string, 2000)
	for i := range rows {
		rows[i] = []string{"id" + strconv.Itoa(i), "value <&> " + strconv.Itoa(i), "tail"}
	}
	assertFastMatchesReference(t, buildPacketBytes(t, rows, 3))
}

// TestFastParse_MatchesReference_HandWritten — формы, которые writer не
// производит, но которые обязаны разбираться одинаково.
func TestFastParse_MatchesReference_HandWritten(t *testing.T) {
	const head = `<?xml version="1.0" encoding="UTF-8"?>` +
		`<DataPacket protocol="TDTP" version="1.4">` +
		`<Header><Type>reference</Type><TableName>t</TableName>` +
		`<MessageID>M1</MessageID><PartNumber>1</PartNumber>` +
		`<TotalParts>1</TotalParts><RecordsInPart>1</RecordsInPart></Header>` +
		`<Schema><Field name="c0" type="TEXT"></Field></Schema>`

	cases := []struct {
		name string
		xml  string
	}{
		{"no rows", head + `<Data></Data></DataPacket>`},
		{"whitespace between rows", head + "<Data>\n  <R>a</R>\n  <R>b</R>\n</Data></DataPacket>"},
		{"data attributes", head + `<Data compression="zstd" checksum="ab12" compact="true" tail="true" carry="c">` +
			`<R>blob</R></Data></DataPacket>`},
		{"v15 encryption attr", head + `<Data encryption="aes-256-gcm"><R>YmFzZTY0</R></Data></DataPacket>`},
		{"numeric char refs", head + `<Data><R>a&#60;b&#x3E;c&#38;d</R></Data></DataPacket>`},
		{"apos entity", head + `<Data><R>it&apos;s &quot;q&quot;</R></Data></DataPacket>`},
		{"empty row element", head + `<Data><R></R><R>x</R></Data></DataPacket>`},
		{"xxh3 on root and data", `<?xml version="1.0" encoding="UTF-8"?>` +
			`<DataPacket protocol="TDTP" version="1.4" xxh3="deadbeef">` +
			`<Header><Type>reference</Type><TableName>t</TableName><MessageID>M1</MessageID>` +
			`<PartNumber>1</PartNumber><TotalParts>1</TotalParts><RecordsInPart>1</RecordsInPart></Header>` +
			`<Schema><Field name="c0" type="TEXT"></Field></Schema>` +
			`<Data xxh3="cafe"><R>v</R></Data></DataPacket>`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertFastMatchesReference(t, []byte(tc.xml))
		})
	}
}

// TestFastParse_FallsBack — формы, на которых быстрый путь обязан отказаться,
// чтобы разбор ушёл в reflection. Проверяется и то, что публичный ParseBytes
// при этом всё равно даёт эталонный результат.
func TestFastParse_FallsBack(t *testing.T) {
	const head = `<?xml version="1.0" encoding="UTF-8"?>` +
		`<DataPacket protocol="TDTP" version="1.4">` +
		`<Header><Type>reference</Type><TableName>t</TableName>` +
		`<MessageID>M1</MessageID><PartNumber>1</PartNumber>` +
		`<TotalParts>1</TotalParts><RecordsInPart>1</RecordsInPart></Header>` +
		`<Schema><Field name="c0" type="TEXT"></Field></Schema>`

	cases := []struct {
		name string
		xml  string
	}{
		{"cdata", head + `<Data><R><![CDATA[raw]]></R></Data></DataPacket>`},
		{"comment inside data", head + `<Data><!-- c --><R>a</R></Data></DataPacket>`},
		{"self-closing row", head + `<Data><R/></Data></DataPacket>`},
		{"self-closing data", head + `<Data/></DataPacket>`},
		{"unknown entity", head + `<Data><R>a&nbsp;b</R></Data></DataPacket>`},
		{"no data section", `<?xml version="1.0"?><DataPacket protocol="TDTP" version="1.4"></DataPacket>`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := tryFastParse([]byte(tc.xml)); ok {
				t.Errorf("fast path accepted input it should have rejected")
			}

			// Публичный API обязан отработать через fallback ровно как эталон.
			var want DataPacket
			refErr := xml.Unmarshal([]byte(tc.xml), &want)

			got, err := NewParser().ParseBytes([]byte(tc.xml))
			if refErr != nil {
				return // эталон сам не парсит — достаточно что fast path отказался
			}
			if err != nil {
				// расхождения нет только если ошибка от валидации, не от разбора
				if !strings.Contains(err.Error(), "validation failed") {
					t.Fatalf("ParseBytes failed where reference succeeded: %v", err)
				}
				return
			}
			if !reflect.DeepEqual(got, &want) {
				t.Errorf("fallback diverged\n got: %+v\nwant: %+v", got, &want)
			}
		})
	}
}

// TestFastParse_DataPacketPrefixNotConfused — <DataPacket> не должен приниматься
// за <Data>.
func TestFastParse_DataPacketPrefixNotConfused(t *testing.T) {
	data := buildPacketBytes(t, [][]string{{"a", "b", "c"}}, 3)

	bodyStart, bodyEnd, _, ok := findDataSection(data)
	if !ok {
		t.Fatal("findDataSection did not find the Data element")
	}
	body := string(data[bodyStart:bodyEnd])
	if !strings.HasPrefix(body, "<R>") {
		t.Errorf("Data body should start with a row, got %q", body[:min(20, len(body))])
	}
	if strings.Contains(body, "<Header>") {
		t.Error("findDataSection matched <DataPacket> instead of <Data>")
	}
}

// TestFastParse_TableNamedData — таблица с именем "Data" не ломает поиск секции.
func TestFastParse_TableNamedData(t *testing.T) {
	x := `<?xml version="1.0" encoding="UTF-8"?>` +
		`<DataPacket protocol="TDTP" version="1.4">` +
		`<Header><Type>reference</Type><TableName>Data</TableName>` +
		`<MessageID>M1</MessageID><PartNumber>1</PartNumber>` +
		`<TotalParts>1</TotalParts><RecordsInPart>1</RecordsInPart></Header>` +
		`<Schema><Field name="Data" type="TEXT"></Field></Schema>` +
		`<Data><R>v</R></Data></DataPacket>`
	assertFastMatchesReference(t, []byte(x))
}

// TestFastParse_BrokenXMLStillErrors — битый XML обязан по-прежнему давать ошибку.
func TestFastParse_BrokenXMLStillErrors(t *testing.T) {
	broken := [][]byte{
		[]byte(`<DataPacket protocol="TDTP"><Data><R>a</R></Data>`), // не закрыт корень
		[]byte(`not xml at all`),
		[]byte(``),
	}
	for i, b := range broken {
		if _, err := NewParser().ParseBytes(b); err == nil {
			t.Errorf("case %d: expected an error, got nil", i)
		}
	}
}

// TestDecodeChardata — таблица сущностей.
func TestDecodeChardata(t *testing.T) {
	ok := []struct{ in, want string }{
		{"plain", "plain"},
		{"", ""},
		{"a&lt;b", "a<b"},
		{"a&gt;b", "a>b"},
		{"a&amp;b", "a&b"},
		{"a&quot;b", `a"b`},
		{"a&apos;b", "a'b"},
		{"&#60;&#62;", "<>"},
		{"&#x3C;&#x3e;", "<>"},
		{"&amp;amp;", "&amp;"},
		{"mix &lt;a&gt; and &amp; tail", "mix <a> and & tail"},
		{"&#1055;", "П"},
	}
	for _, tc := range ok {
		got, good := decodeChardata([]byte(tc.in))
		if !good {
			t.Errorf("decodeChardata(%q) bailed out unexpectedly", tc.in)
			continue
		}
		if got != tc.want {
			t.Errorf("decodeChardata(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	bad := []string{"a&nbsp;b", "a&b", "&#xZZ;", "&#;", "&"}
	for _, in := range bad {
		if _, good := decodeChardata([]byte(in)); good {
			t.Errorf("decodeChardata(%q) should have bailed out", in)
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ─── Parse / ParseFile ──────────────────────────────────────────────────────

// TestParse_MatchesParseBytes — Parse(io.Reader) должен давать то же, что
// ParseBytes на тех же байтах.
func TestParse_MatchesParseBytes(t *testing.T) {
	data := buildPacketBytes(t, [][]string{
		{"a<b", "c&d", "e|f"},
		{"Привіт", `back\slash`, `q"uote`},
	}, 3)

	p := NewParser()
	fromBytes, err := p.ParseBytes(data)
	if err != nil {
		t.Fatalf("ParseBytes: %v", err)
	}
	fromReader, err := p.Parse(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !reflect.DeepEqual(fromReader, fromBytes) {
		t.Errorf("Parse diverged from ParseBytes\n got: %+v\nwant: %+v", fromReader, fromBytes)
	}
}

// TestParse_NonSizedReader — reader без Len() (нет преаллокации) обрабатывается
// так же.
func TestParse_NonSizedReader(t *testing.T) {
	data := buildPacketBytes(t, [][]string{{"x", "y", "z"}}, 3)

	p := NewParser()
	want, err := p.ParseBytes(data)
	if err != nil {
		t.Fatalf("ParseBytes: %v", err)
	}
	// iotest-подобная обёртка: только Read, без Len.
	got, err := p.Parse(struct{ io.Reader }{bytes.NewReader(data)})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Parse over a plain reader diverged\n got: %+v\nwant: %+v", got, want)
	}
}

// TestParseFile_MatchesParseBytes — ParseFile читает файл и даёт тот же результат.
func TestParseFile_MatchesParseBytes(t *testing.T) {
	data := buildPacketBytes(t, [][]string{{"1", "2", "3"}, {"4", "5", "6"}}, 3)

	path := filepath.Join(t.TempDir(), "p.tdtp.xml")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	p := NewParser()
	want, err := p.ParseBytes(data)
	if err != nil {
		t.Fatalf("ParseBytes: %v", err)
	}
	got, err := p.ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseFile diverged\n got: %+v\nwant: %+v", got, want)
	}
}

// TestParse_ExpandsCompact — Parse/ParseFile разворачивают compact-формат,
// а ParseBytes намеренно нет. Эта асимметрия должна сохраниться.
func TestParse_ExpandsCompact(t *testing.T) {
	const compactXML = `<?xml version="1.0" encoding="UTF-8"?>` +
		`<DataPacket protocol="TDTP" version="1.3.1">` +
		`<Header><Type>reference</Type><TableName>t</TableName><MessageID>M1</MessageID>` +
		`<PartNumber>1</PartNumber><TotalParts>1</TotalParts><RecordsInPart>2</RecordsInPart>` +
		`<Timestamp>2026-01-01T00:00:00Z</Timestamp></Header>` +
		`<Schema><Field name="dept" type="TEXT" fixed="true"></Field>` +
		`<Field name="name" type="TEXT"></Field></Schema>` +
		`<Data compact="true"><R>HR|Ann</R><R>|Bob</R></Data></DataPacket>`

	p := NewParser()

	viaBytes, err := p.ParseBytes([]byte(compactXML))
	if err != nil {
		t.Fatalf("ParseBytes: %v", err)
	}
	viaReader, err := p.Parse(bytes.NewReader([]byte(compactXML)))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	// ParseBytes оставляет compact как есть.
	if viaBytes.Data.Rows[1].Value != "|Bob" {
		t.Errorf("ParseBytes should leave compact rows untouched, got %q", viaBytes.Data.Rows[1].Value)
	}
	// Parse разворачивает carry-forward.
	if viaReader.Data.Rows[1].Value != "HR|Bob" {
		t.Errorf("Parse should expand compact rows, got %q", viaReader.Data.Rows[1].Value)
	}
}

// TestParse_ErrorsPreserved — ошибки чтения и разбора не теряются.
func TestParse_ErrorsPreserved(t *testing.T) {
	p := NewParser()

	if _, err := p.Parse(bytes.NewReader([]byte("not xml"))); err == nil {
		t.Error("expected an error on garbage input")
	}
	if _, err := p.Parse(bytes.NewReader(nil)); err == nil {
		t.Error("expected an error on empty input")
	}
	if _, err := p.ParseFile(filepath.Join(t.TempDir(), "missing.xml")); err == nil {
		t.Error("expected an error on a missing file")
	}
	if _, err := p.Parse(iotest.ErrReader(errors.New("boom"))); err == nil {
		t.Error("expected the reader error to propagate")
	}
}

// ─── Порог буферизации ──────────────────────────────────────────────────────

// withLoweredParseCap временно опускает порог, чтобы обычный пакет пошёл
// потоковой веткой.
func withLoweredParseCap(t *testing.T, limit int64) {
	t.Helper()
	old := maxBufferedParse
	maxBufferedParse = limit
	t.Cleanup(func() { maxBufferedParse = old })
}

// TestParse_OverCap_StreamsAndMatches — вход больше порога разбирается
// потоковым декодером и даёт тот же результат.
func TestParse_OverCap_StreamsAndMatches(t *testing.T) {
	data := buildPacketBytes(t, [][]string{
		{"a<b", "c&d", "e|f"},
		{"Привіт", `back\slash`, "plain"},
	}, 3)

	p := NewParser()
	want, err := p.ParseBytes(data)
	if err != nil {
		t.Fatalf("ParseBytes: %v", err)
	}

	withLoweredParseCap(t, 64) // пакет заведомо больше

	got, err := p.Parse(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Parse over cap: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("streaming path diverged\n got: %+v\nwant: %+v", got, want)
	}
}

// TestParse_OverCap_NonSizedReader — та же ветка для reader без Len():
// уже прочитанный кусок должен подставляться обратно через MultiReader.
func TestParse_OverCap_NonSizedReader(t *testing.T) {
	data := buildPacketBytes(t, [][]string{{"x", "y", "z"}, {"1", "2", "3"}}, 3)

	p := NewParser()
	want, err := p.ParseBytes(data)
	if err != nil {
		t.Fatalf("ParseBytes: %v", err)
	}

	withLoweredParseCap(t, 32)

	got, err := p.Parse(struct{ io.Reader }{bytes.NewReader(data)})
	if err != nil {
		t.Fatalf("Parse over cap on a plain reader: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("MultiReader hand-off lost data\n got: %+v\nwant: %+v", got, want)
	}
}

// TestParseFile_OverCap_Streams — файл больше порога тоже идёт потоковой веткой.
func TestParseFile_OverCap_Streams(t *testing.T) {
	data := buildPacketBytes(t, [][]string{{"f1", "f2", "f3"}}, 3)
	path := filepath.Join(t.TempDir(), "big.tdtp.xml")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	p := NewParser()
	want, err := p.ParseBytes(data)
	if err != nil {
		t.Fatalf("ParseBytes: %v", err)
	}

	withLoweredParseCap(t, 16)

	got, err := p.ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile over cap: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseFile streaming path diverged\n got: %+v\nwant: %+v", got, want)
	}
}

// TestParse_OverCap_ExpandsCompact — compact разворачивается и на потоковой ветке.
func TestParse_OverCap_ExpandsCompact(t *testing.T) {
	const compactXML = `<?xml version="1.0" encoding="UTF-8"?>` +
		`<DataPacket protocol="TDTP" version="1.3.1">` +
		`<Header><Type>reference</Type><TableName>t</TableName><MessageID>M1</MessageID>` +
		`<PartNumber>1</PartNumber><TotalParts>1</TotalParts><RecordsInPart>2</RecordsInPart>` +
		`<Timestamp>2026-01-01T00:00:00Z</Timestamp></Header>` +
		`<Schema><Field name="dept" type="TEXT" fixed="true"></Field>` +
		`<Field name="name" type="TEXT"></Field></Schema>` +
		`<Data compact="true"><R>HR|Ann</R><R>|Bob</R></Data></DataPacket>`

	withLoweredParseCap(t, 32)

	got, err := NewParser().Parse(bytes.NewReader([]byte(compactXML)))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Data.Rows[1].Value != "HR|Bob" {
		t.Errorf("streaming path should expand compact rows, got %q", got.Data.Rows[1].Value)
	}
}

// TestParse_OverCap_ErrorsPreserved — на битом входе сверх порога ошибка не теряется.
func TestParse_OverCap_ErrorsPreserved(t *testing.T) {
	withLoweredParseCap(t, 8)

	if _, err := NewParser().Parse(bytes.NewReader([]byte("this is definitely not xml at all"))); err == nil {
		t.Error("expected an error from the streaming path")
	}
}
