package etl

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// Две реализации одного чтения обязаны давать одно и то же — иначе быстрый путь
// не ускорение, а вторая правда о данных. Проверяются и схема, и каждая ячейка,
// на типах, где пути расходятся легче всего: даты, NULL, числа, булево.
func TestBulkRead_MatchesGenericPath(t *testing.T) {
	ctx := context.Background()
	ws := newTestWorkspace(t, ctx)

	fields := []packet.Field{
		{Name: "ID", Type: "INTEGER"},
		{Name: "Name", Type: "TEXT"},
		{Name: "Amount", Type: "REAL"},
		{Name: "Active", Type: "BOOLEAN"},
		{Name: "D", Type: "DATE"},
		{Name: "TS", Type: "TIMESTAMP"},
	}
	if err := ws.CreateTable(ctx, "t", fields); err != nil {
		t.Fatal(err)
	}
	rows := []packet.Row{
		{Value: "1|first|10.5|true|2020-01-02|2020-01-02 03:04:05"},
		{Value: "2||0.001|false||"},
		{Value: "3|with|pipe is impossible|1|1|1999-12-31|1999-12-31 23:59:59"},
	}
	pkt := packet.NewDataPacket(packet.TypeReference, "t")
	pkt.Schema = packet.Schema{Fields: fields}
	pkt.Header.RecordsInPart = len(rows)
	pkt.Data.Rows = rows[:2] // третья строка нарочно битая по числу колонок
	if err := ws.LoadData(ctx, "t", pkt); err != nil {
		t.Fatal(err)
	}

	fast, err := ws.bulkReadViaDriver(ctx, `SELECT * FROM "t" ORDER BY "ID"`, "r")
	if err != nil {
		t.Fatalf("fast path: %v", err)
	}
	generic, err := ws.executeSQLGeneric(ctx, `SELECT * FROM "t" ORDER BY "ID"`, "r")
	if err != nil {
		t.Fatalf("generic path: %v", err)
	}

	if len(fast.Schema.Fields) != len(generic.Schema.Fields) {
		t.Fatalf("schema length: fast %d, generic %d",
			len(fast.Schema.Fields), len(generic.Schema.Fields))
	}
	for i := range generic.Schema.Fields {
		g, f := generic.Schema.Fields[i], fast.Schema.Fields[i]
		if g.Name != f.Name || g.Type != f.Type {
			t.Errorf("field %d: fast %+v, generic %+v", i, f, g)
		}
	}

	fr, gr := fast.GetRows(), generic.GetRows()
	if len(fr) != len(gr) {
		t.Fatalf("rows: fast %d, generic %d", len(fr), len(gr))
	}
	for r := range gr {
		for c := range gr[r] {
			if fr[r][c] != gr[r][c] {
				t.Errorf("row %d column %d: fast %q, generic %q", r, c, fr[r][c], gr[r][c])
			}
		}
	}
	_ = fmt.Sprint(strings.TrimSpace(""))
}
