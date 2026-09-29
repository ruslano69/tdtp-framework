package tdtpjson

// json_test.go — typed object output, projection, query, edge values.

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

var testSchema = packet.Schema{Fields: []packet.Field{
	{Name: "id", Type: "INTEGER", Key: true},
	{Name: "name", Type: "TEXT"},
	{Name: "balance", Type: "DECIMAL"},
	{Name: "active", Type: "BOOLEAN"},
}}

func testPacket(t *testing.T, rows [][]string) *packet.DataPacket {
	t.Helper()
	gen := packet.NewGenerator()
	pkts, err := gen.GenerateReference("t", testSchema, rows)
	if err != nil {
		t.Fatalf("GenerateReference: %v", err)
	}
	pkt := pkts[0]
	pkt.MaterializeRows()
	return pkt
}

// decodeObjects parses the streamed array back for assertions.
func decodeObjects(t *testing.T, doc string) []map[string]any {
	t.Helper()
	var out []map[string]any
	if err := json.Unmarshal([]byte(doc), &out); err != nil {
		t.Fatalf("invalid JSON %q: %v", doc, err)
	}
	return out
}

func TestWrite_TypedValues(t *testing.T) {
	pkt := testPacket(t, [][]string{{"1", "Alice", "1500.50", "1"}})
	var b bytes.Buffer
	n, err := Write(context.Background(), &b, pkt, nil, false)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != 1 {
		t.Fatalf("n = %d, want 1", n)
	}
	objs := decodeObjects(t, b.String())
	if len(objs) != 1 {
		t.Fatalf("objects = %d, want 1", len(objs))
	}
	o := objs[0]
	if o["id"] != float64(1) { // encoding/json decodes numbers to float64
		t.Errorf("id = %v (%T), want number 1", o["id"], o["id"])
	}
	if o["name"] != "Alice" {
		t.Errorf("name = %v", o["name"])
	}
	if o["balance"] != 1500.5 {
		t.Errorf("balance = %v", o["balance"])
	}
	if o["active"] != true {
		t.Errorf("active = %v", o["active"])
	}
}

func TestWrite_KeyOrder(t *testing.T) {
	pkt := testPacket(t, [][]string{{"1", "Alice", "10", "1"}})
	var b bytes.Buffer
	if _, err := Write(context.Background(), &b, pkt, nil, false); err != nil {
		t.Fatal(err)
	}
	doc := b.String()
	order := []string{`"id"`, `"name"`, `"balance"`, `"active"`}
	last := -1
	for _, k := range order {
		i := strings.Index(doc, k)
		if i < 0 || i < last {
			t.Fatalf("keys out of schema order in %s", doc)
		}
		last = i
	}
}

func TestWrite_NullAndSpecials(t *testing.T) {
	pkt := testPacket(t, [][]string{
		{"2", "[NULL]", "NaN", "0"},
	})
	var b bytes.Buffer
	if _, err := Write(context.Background(), &b, pkt, nil, false); err != nil {
		t.Fatal(err)
	}
	objs := decodeObjects(t, b.String())
	o := objs[0]
	if o["name"] != nil {
		t.Errorf("name [NULL] should be null, got %v", o["name"])
	}
	if o["balance"] != nil {
		t.Errorf("balance NaN should be null, got %v", o["balance"])
	}
	if o["active"] != false {
		t.Errorf("active = %v, want false", o["active"])
	}
}

func TestWrite_BigIntAsString(t *testing.T) {
	// 19-digit BIGINT past the JS safe range must not round-trip as a number.
	pkt := testPacket(t, [][]string{{"1234567890123456789", "x", "0", "1"}})
	var b bytes.Buffer
	if _, err := Write(context.Background(), &b, pkt, nil, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), `"id":"1234567890123456789"`) {
		t.Errorf("bigint should be a quoted string, got %s", b.String())
	}
}

func TestWrite_ProjectionAndFilter(t *testing.T) {
	pkt := testPacket(t, [][]string{
		{"1", "Alice", "100", "1"},
		{"2", "Bob", "2000", "1"},
	})
	q := &packet.Query{
		Fields: []string{"name", "balance"},
		Filters: &packet.Filters{And: &packet.LogicalGroup{Filters: []packet.Filter{
			{Field: "balance", Operator: "gt", Value: "1000"},
		}}},
		OrderBy: &packet.OrderBy{Field: "balance", Direction: "DESC"},
		Limit:   1,
	}
	var b bytes.Buffer
	n, err := Write(context.Background(), &b, pkt, q, false)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != 1 {
		t.Fatalf("n = %d, want 1", n)
	}
	doc := b.String()
	if !strings.Contains(doc, `"name":"Bob"`) || !strings.Contains(doc, `"balance":2000`) {
		t.Errorf("unexpected projection/filter result: %s", doc)
	}
	if strings.Contains(doc, `"id"`) {
		t.Errorf("id should be projected out: %s", doc)
	}
}

func TestWrite_UnknownField(t *testing.T) {
	pkt := testPacket(t, [][]string{{"1", "Alice", "10", "1"}})
	q := &packet.Query{Fields: []string{"ghost"}}
	var b bytes.Buffer
	if _, err := Write(context.Background(), &b, pkt, q, false); err == nil {
		t.Error("unknown --fields column must fail")
	}
}

func TestWrite_ManyRowsStayValid(t *testing.T) {
	rows := make([][]string, 2000)
	for i := range rows {
		rows[i] = []string{"1", "n", "2.5", "1"}
	}
	pkt := testPacket(t, rows)
	var b bytes.Buffer
	n, err := Write(context.Background(), &b, pkt, nil, false)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != 2000 {
		t.Fatalf("n = %d, want 2000", n)
	}
	if objs := decodeObjects(t, b.String()); len(objs) != 2000 {
		t.Fatalf("objects = %d, want 2000", len(objs))
	}
}

// Pretty output has no blank line after "[": one object per line.
func TestWrite_PrettyNoBlankLine(t *testing.T) {
	pkt := &packet.DataPacket{Schema: packet.Schema{Fields: []packet.Field{{Name: "id", Type: "INTEGER"}}}}
	pkt.Data.Rows = []packet.Row{{Value: "1"}, {Value: "2"}}
	var buf bytes.Buffer
	if _, err := Write(context.Background(), &buf, pkt, nil, true); err != nil {
		t.Fatal(err)
	}
	if got, want := buf.String(), "[\n  {\"id\":1},\n  {\"id\":2}\n]"; got != want {
		t.Errorf("pretty = %q, want %q", got, want)
	}
}
