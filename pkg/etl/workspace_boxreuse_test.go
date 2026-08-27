package etl

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// Быстрый путь не переупаковывает значение, если в слоте уже лежит то же самое.
// Ошибка в учёте слотов не уронит вставку — она разложит значения по ЧУЖИМ
// ячейкам, поэтому проверяется каждая ячейка, а данные подобраны так, чтобы
// переиспользование срабатывало часто и по-разному.
//
// TestWorkspace_BatchInsertAlignment такую ошибку не поймает: там каждое
// значение уникально, и путь переиспользования не выполняется ни разу.
func TestWorkspace_RepeatedValuesLandInTheirOwnCells(t *testing.T) {
	ctx := context.Background()

	// Периоды намеренно взаимно не кратны размеру пачки (10 строк):
	// 1 — колонка-константа, 5 и 3 — справочники, 7 — почти уникальная,
	// 0 — уникальная, плюс колонка, где значения ПОВТОРЯЮТСЯ МЕЖДУ колонками.
	periods := []int{0, 1, 5, 3, 7, 2}
	const rows = 137 // не кратно пачке: хвост идёт по одной строке

	ws := newTestWorkspace(t, ctx)
	fields := make([]packet.Field, len(periods))
	for i := range fields {
		fields[i] = packet.Field{Name: fmt.Sprintf("C%d", i), Type: "TEXT"}
	}
	if err := ws.CreateTable(ctx, "t", fields); err != nil {
		t.Fatalf("create table: %v", err)
	}

	want := make([][]string, rows)
	pktRows := make([]packet.Row, rows)
	for r := 0; r < rows; r++ {
		vals := make([]string, len(periods))
		for c, p := range periods {
			if p == 0 {
				vals[c] = fmt.Sprintf("u%d", r) // уникальное
				continue
			}
			// Одинаковое написание в разных колонках: если слоты перепутаны,
			// значение всё равно «совпадёт» и ошибка спрячется. Поэтому
			// добавляем номер колонки — совпадение возможно только внутри неё.
			vals[c] = fmt.Sprintf("c%d-v%d", c, r%p)
		}
		want[r] = vals
		pktRows[r] = packet.Row{Value: strings.Join(vals, "|")}
	}

	pkt := packet.NewDataPacket(packet.TypeReference, "t")
	pkt.Schema = packet.Schema{Fields: fields}
	pkt.Header.RecordsInPart = rows
	pkt.Data.Rows = pktRows
	if err := ws.LoadData(ctx, "t", pkt); err != nil {
		t.Fatalf("load: %v", err)
	}

	res, err := ws.ExecuteSQL(ctx, `SELECT * FROM "t" ORDER BY CAST(substr("C0", 2) AS INTEGER)`, "r")
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	got := res.GetRows()
	if len(got) != rows {
		t.Fatalf("rows = %d, want %d", len(got), rows)
	}
	for r := range want {
		for c := range want[r] {
			if got[r][c] != want[r][c] {
				t.Fatalf("row %d column %d = %q, want %q", r, c, got[r][c], want[r][c])
			}
		}
	}
}

// Значение, повторяющееся во ВСЕЙ таблице, вместе с NULL и boolean: у этих
// convertValue возвращает не строку (nil и int64), и слот с ними обязан
// переживать переиспользование так же, как строковый.
func TestWorkspace_ReuseAcrossNullsAndBooleans(t *testing.T) {
	ctx := context.Background()
	ws := newTestWorkspace(t, ctx)

	fields := []packet.Field{
		{Name: "ID", Type: "INTEGER"},
		{Name: "Flag", Type: "BOOLEAN"},
		{Name: "Note", Type: "TEXT"},
	}
	if err := ws.CreateTable(ctx, "t", fields); err != nil {
		t.Fatalf("create table: %v", err)
	}

	const rows = 64
	pktRows := make([]packet.Row, rows)
	for r := 0; r < rows; r++ {
		flag := "false"
		if r%2 == 0 {
			flag = "true"
		}
		note := "same everywhere"
		if r%7 == 0 {
			// Период 7 не кратен пачке (20 строк) намеренно: иначе пустое
			// значение всегда попадало бы в один и тот же слот, устаревшего
			// соседа там не оказалось бы, и ошибка учёта прошла бы мимо.
			note = "" // NULL вперемешку с повторяющейся строкой
		}
		pktRows[r] = packet.Row{Value: strings.Join([]string{fmt.Sprintf("%d", r), flag, note}, "|")}
	}
	pkt := packet.NewDataPacket(packet.TypeReference, "t")
	pkt.Schema = packet.Schema{Fields: fields}
	pkt.Header.RecordsInPart = rows
	pkt.Data.Rows = pktRows
	if err := ws.LoadData(ctx, "t", pkt); err != nil {
		t.Fatalf("load: %v", err)
	}

	res, err := ws.ExecuteSQL(ctx, `SELECT * FROM "t" ORDER BY "ID"`, "r")
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	got := res.GetRows()
	if len(got) != rows {
		t.Fatalf("rows = %d, want %d", len(got), rows)
	}
	for r := range got {
		wantFlag := "0"
		if r%2 == 0 {
			wantFlag = "1"
		}
		wantNote := "same everywhere"
		if r%7 == 0 {
			wantNote = ""
		}
		if got[r][1] != wantFlag {
			t.Errorf("row %d flag = %q, want %q", r, got[r][1], wantFlag)
		}
		if got[r][2] != wantNote {
			t.Errorf("row %d note = %q, want %q", r, got[r][2], wantNote)
		}
	}
}
