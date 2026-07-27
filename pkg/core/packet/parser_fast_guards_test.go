package packet

import (
	"encoding/xml"
	"fmt"
	"strings"
	"testing"
)

func packetWithRow(rowValue string) []byte {
	return []byte(`<?xml version="1.0" encoding="UTF-8"?>` +
		`<DataPacket protocol="TDTP" version="1.0">` +
		`<Header><Type>reference</Type><TableName>T</TableName>` +
		`<MessageID>m1</MessageID><Timestamp>2026-07-27T00:00:00Z</Timestamp></Header>` +
		`<Schema><Field name="a" type="string"/></Schema>` +
		`<Data><R>` + rowValue + `</R></Data>` +
		`</DataPacket>`)
}

// Ни один вход не должен приниматься быстрым путём, если encoding/xml его
// отвергает: контракт parser_fast.go — «не та форма, отдай в fallback», а не
// «прими то, что эталон не примет».
func TestFastPathNeverAcceptsWhatEncodingXMLRejects(t *testing.T) {
	refs := []string{
		"&#0;", "&#x0;", "&#1;", "&#x8;", "&#xB;", "&#xC;", "&#x1F;",
		"&#xD800;", "&#xDFFF;", "&#xFFFE;", "&#xFFFF;", "&#x110000;",
	}

	for _, ref := range refs {
		t.Run(ref, func(t *testing.T) {
			data := packetWithRow("a" + ref + "b")

			var pkt DataPacket
			refErr := xml.Unmarshal(data, &pkt)
			if refErr == nil {
				t.Skipf("encoding/xml accepts %s — nothing to guard here", ref)
			}

			if _, ok := tryFastParse(data); ok {
				t.Errorf("fast path accepted %s, which encoding/xml rejects (%v)", ref, refErr)
			}
		})
	}
}

// Разрешённые продукцией Char ссылки должны по-прежнему раскрываться.
func TestFastPathKeepsValidCharRefs(t *testing.T) {
	cases := []struct {
		ref  string
		want string
	}{
		{"&#x9;", "\t"},
		{"&#xA;", "\n"},
		{"&#xD;", "\r"},
		{"&#32;", " "},
		{"&#x41;", "A"},
		{"&#x44F;", "я"},
		{"&#xFFFD;", "�"},
		{"&#x1F600;", "\U0001F600"},
	}

	for _, tc := range cases {
		t.Run(tc.ref, func(t *testing.T) {
			got, ok := decodeChardata([]byte(tc.ref))
			if !ok {
				t.Fatalf("decodeChardata rejected the valid reference %s", tc.ref)
			}
			if got != tc.want {
				t.Errorf("got %q want %q", got, tc.want)
			}

			// И то же самое сквозь весь быстрый путь, против эталона.
			data := packetWithRow(tc.ref)
			var pkt DataPacket
			if err := xml.Unmarshal(data, &pkt); err != nil {
				t.Fatalf("reference unmarshal: %v", err)
			}
			fast, okFast := tryFastParse(data)
			if !okFast {
				t.Fatal("fast path bailed out on a valid reference")
			}
			if fast.Data.Rows[0].Value != pkt.Data.Rows[0].Value {
				t.Errorf("diverged from reference: fast=%q ref=%q",
					fast.Data.Rows[0].Value, pkt.Data.Rows[0].Value)
			}
		})
	}
}

// Элемент с префиксом Schema в имени не должен прекращать поиск — иначе
// проверка MaxSchemaBytesRead молча выключается.
func TestSchemaSpanSizeSkipsPrefixedElements(t *testing.T) {
	schema := `<Schema><Field name="a" type="string"/></Schema>`
	cases := []struct {
		name string
		data string
	}{
		{"plain", `<DataPacket>` + schema + `<Data></Data></DataPacket>`},
		{"prefixed element first", `<DataPacket><SchemaVersion>1</SchemaVersion>` + schema + `<Data></Data></DataPacket>`},
		{"two prefixed first", `<DataPacket><SchemaX/><SchemaY>2</SchemaY>` + schema + `<Data></Data></DataPacket>`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			size, ok := schemaSpanSize([]byte(tc.data))
			if !ok {
				t.Fatal("schema span not found — the size check would be skipped")
			}
			if size != len(schema) {
				t.Errorf("size = %d, want %d (the <Schema> element itself)", size, len(schema))
			}
		})
	}
}

func TestSchemaSpanSizeSelfClosing(t *testing.T) {
	const empty = `<Schema/>`
	size, ok := schemaSpanSize([]byte(`<DataPacket>` + empty + `<Data></Data></DataPacket>`))
	if !ok {
		t.Fatal("self-closing Schema not found")
	}
	if size != len(empty) {
		t.Errorf("size = %d, want %d", size, len(empty))
	}
}

// Предел на чтении обязан срабатывать и тогда, когда перед Schema стоит
// элемент с префиксом — раньше это было обходом.
func TestReadLimitNotBypassedByPrefixedElement(t *testing.T) {
	huge := strings.Repeat(`<Field name="`+strings.Repeat("x", 100)+`" type="string"/>`, 12000)
	data := []byte(`<?xml version="1.0"?><DataPacket protocol="TDTP" version="1.0">` +
		`<SchemaVersion>1</SchemaVersion>` +
		`<Header><Type>reference</Type><TableName>T</TableName>` +
		`<MessageID>m1</MessageID><Timestamp>2026-07-27T00:00:00Z</Timestamp></Header>` +
		`<Schema>` + huge + `</Schema><Data></Data></DataPacket>`)

	size, ok := schemaSpanSize(data)
	if !ok {
		t.Fatal("schema span not found")
	}
	if size <= MaxSchemaBytesRead {
		t.Fatalf("test fixture too small: schema is %d bytes, need > %d", size, MaxSchemaBytesRead)
	}

	_, err := NewParser().ParseBytes(data)
	if err == nil {
		t.Fatal("oversized schema was parsed despite the read limit")
	}
	if !strings.Contains(err.Error(), "refusing to parse") {
		t.Errorf("expected the read-limit error, got: %v", err)
	}
}

// Контроль: тот же пакет без элемента с префиксом отвергался и раньше.
func TestReadLimitStillAppliesWithoutPrefixedElement(t *testing.T) {
	huge := strings.Repeat(fmt.Sprintf(`<Field name="%s" type="string"/>`, strings.Repeat("x", 100)), 12000)
	data := []byte(`<?xml version="1.0"?><DataPacket protocol="TDTP" version="1.0">` +
		`<Header><Type>reference</Type><TableName>T</TableName>` +
		`<MessageID>m1</MessageID><Timestamp>2026-07-27T00:00:00Z</Timestamp></Header>` +
		`<Schema>` + huge + `</Schema><Data></Data></DataPacket>`)

	if _, err := NewParser().ParseBytes(data); err == nil {
		t.Fatal("oversized schema was parsed despite the read limit")
	}
}
