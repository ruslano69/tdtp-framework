package packet

import (
	"bytes"
	"encoding/xml"
	"strings"
	"testing"
)

// Значения с виндовыми и юниксовыми переносами обязаны пережить запись и
// чтение без изменений. До экранирования CR (см. escCR) любой XML-парсер
// приводил CRLF к LF по §2.11, и экспорт из MSSQL с полем-примечанием
// возвращался из импорта уже другим.
func TestNewlinesSurviveRoundTrip(t *testing.T) {
	values := []string{
		"windows\r\nstyle",
		"unix\nstyle",
		"lone\rcarriage",
		"mixed\r\nand\nand\rall",
		"trailing\r\n",
		"\r\nleading",
		"no newlines at all",
		"кириллица\r\nз переносом",
	}

	schema := Schema{Fields: []Field{{Name: "note", Type: "string"}}}
	rows := make([][]string, len(values))
	for i, v := range values {
		rows[i] = []string{v}
	}

	packets, err := NewGenerator().GenerateReference("notes", schema, rows)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(packets) != 1 {
		t.Fatalf("expected a single part, got %d", len(packets))
	}

	data, err := NewGenerator().ToXML(packets[0], false)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Сырой CR в файле означал бы, что мы снова полагаемся на то, что парсер
	// его не тронет — а он тронет.
	if bytes.ContainsRune(data, '\r') {
		t.Error("serialized packet contains a raw CR; it must be written as &#xD;")
	}

	parsed, err := NewParser().ParseBytes(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	got := parsed.GetRows()
	if len(got) != len(values) {
		t.Fatalf("row count: got %d want %d", len(got), len(values))
	}
	for i, want := range values {
		if got[i][0] != want {
			t.Errorf("row %d: got %q want %q", i, got[i][0], want)
		}
	}
}

// Быстрый путь и encoding/xml обязаны совпадать на том, что мы пишем сами.
func TestFastPathMatchesReferenceOnNewlines(t *testing.T) {
	schema := Schema{Fields: []Field{{Name: "a", Type: "string"}, {Name: "b", Type: "string"}}}
	rows := [][]string{
		{"x\r\ny", "plain"},
		{"lone\rcr", "tab\there"},
		{"amp&and<lt>", "quote\"q"},
	}

	packets, err := NewGenerator().GenerateReference("t", schema, rows)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	data, err := NewGenerator().ToXML(packets[0], false)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var ref DataPacket
	if err := xml.Unmarshal(data, &ref); err != nil {
		t.Fatalf("reference unmarshal: %v", err)
	}
	fast, ok := tryFastParse(data)
	if !ok {
		t.Fatal("fast path bailed out on a packet we produced ourselves")
	}

	if len(fast.Data.Rows) != len(ref.Data.Rows) {
		t.Fatalf("row count differs: fast %d, reference %d", len(fast.Data.Rows), len(ref.Data.Rows))
	}
	for i := range ref.Data.Rows {
		if fast.Data.Rows[i].Value != ref.Data.Rows[i].Value {
			t.Errorf("row %d diverged:\n fast=%q\n  ref=%q",
				i, fast.Data.Rows[i].Value, ref.Data.Rows[i].Value)
		}
	}
}

// Файлы, записанные до экранирования CR, содержат его сырым. Такие должны
// читаться ровно как читались раньше — то есть с нормализацией в LF, как
// того требует §2.11 и как всегда делал encoding/xml. Иначе один и тот же
// файл менял бы содержимое при обновлении tdtpcli.
func TestLegacyRawCRNormalizesLikeEncodingXML(t *testing.T) {
	legacy := []byte(`<?xml version="1.0" encoding="UTF-8"?>` +
		`<DataPacket protocol="TDTP" version="1.0">` +
		`<Header><Type>reference</Type><TableName>T</TableName>` +
		`<MessageID>m1</MessageID><Timestamp>2026-07-27T00:00:00Z</Timestamp></Header>` +
		`<Schema><Field name="a" type="string"/></Schema>` +
		"<Data><R>line1\r\nline2\rline3</R></Data>" +
		`</DataPacket>`)

	var ref DataPacket
	if err := xml.Unmarshal(legacy, &ref); err != nil {
		t.Fatalf("reference unmarshal: %v", err)
	}
	fast, ok := tryFastParse(legacy)
	if !ok {
		t.Fatal("fast path bailed out")
	}

	const want = "line1\nline2\nline3"
	if ref.Data.Rows[0].Value != want {
		t.Fatalf("reference itself changed: got %q want %q", ref.Data.Rows[0].Value, want)
	}
	if fast.Data.Rows[0].Value != want {
		t.Errorf("fast path got %q, want %q (same as encoding/xml)", fast.Data.Rows[0].Value, want)
	}
}

// &#xD; должен раскрываться именно в CR, а не попадать под нормализацию —
// иначе экранирование не имело бы смысла.
func TestCharRefCRIsNotNormalized(t *testing.T) {
	got, ok := decodeChardata([]byte("a&#xD;b&#xD;&#xA;c"))
	if !ok {
		t.Fatal("decodeChardata refused a valid char ref")
	}
	if want := "a\rb\r\nc"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// В значении атрибута XML заменяет на пробел не только CR, но также LF и TAB
// (§3.3.3). Атрибут carry несёт значения полей, поэтому все три экранируются.
func TestAttrValueEscapesWhitespace(t *testing.T) {
	pkt := NewDataPacket(TypeReference, "t")
	pkt.Data.Compact = true
	pkt.Data.Carry = "a\r\nb\tc|d"
	pkt.Schema = Schema{Fields: []Field{{Name: "a", Type: "string"}}}

	data, err := NewGenerator().ToXML(pkt, false)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	for _, raw := range []struct{ name, s string }{{"CR", "\r"}, {"LF", "\n"}, {"TAB", "\t"}} {
		if strings.Contains(string(data), `carry="`) && bytes.Contains(data, []byte(raw.s)) {
			// Проверяем именно внутри значения атрибута.
			start := bytes.Index(data, []byte(`carry="`)) + len(`carry="`)
			end := start + bytes.IndexByte(data[start:], '"')
			if bytes.Contains(data[start:end], []byte(raw.s)) {
				t.Errorf("attribute value contains a raw %s; it must be escaped", raw.name)
			}
		}
	}

	var ref DataPacket
	if err := xml.Unmarshal(data, &ref); err != nil {
		t.Fatalf("reference unmarshal: %v", err)
	}
	if ref.Data.Carry != pkt.Data.Carry {
		t.Errorf("carry did not survive: got %q want %q", ref.Data.Carry, pkt.Data.Carry)
	}
}
