package etl

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// LoadData вставляет пачками, и опасность здесь не в скорости, а в выравнивании:
// ошибка в границе пачки или в хвосте не роняет вставку, а раскладывает
// значения по чужим колонкам. Поэтому проверяется каждая ячейка, а число строк
// намеренно НЕ кратно пачке — хвост идёт отдельным путём, по одной строке.
func TestWorkspace_BatchInsertAlignment(t *testing.T) {
	ctx := context.Background()

	for _, tc := range []struct {
		name string
		cols int
		rows int
	}{
		{"remainder", 6, 23},         // пачка 10: две полные + 3 в хвосте
		{"exact multiple", 6, 20},    // ровно две пачки, хвоста нет
		{"below one batch", 6, 4},    // до первой пачки не доходит вовсе
		{"single row", 6, 1},         // только хвост
		{"wide table", 80, 7},        // 80 колонок → пачка вырождается в одну строку
	} {
		t.Run(tc.name, func(t *testing.T) {
			ws := newTestWorkspace(t, ctx)

			fields := make([]packet.Field, tc.cols)
			for i := range fields {
				fields[i] = packet.Field{Name: fmt.Sprintf("C%d", i), Type: "TEXT"}
			}
			if err := ws.CreateTable(ctx, "t", fields); err != nil {
				t.Fatalf("create table: %v", err)
			}

			want := make([][]string, tc.rows)
			pktRows := make([]packet.Row, tc.rows)
			for r := 0; r < tc.rows; r++ {
				vals := make([]string, tc.cols)
				for c := range vals {
					// Значение несёт собственные координаты: если оно окажется
					// не в своей ячейке, это видно прямо в сообщении теста.
					vals[c] = fmt.Sprintf("r%dc%d", r, c)
				}
				want[r] = vals
				pktRows[r] = packet.Row{Value: strings.Join(vals, "|")}
			}

			pkt := packet.NewDataPacket(packet.TypeReference, "t")
			pkt.Schema = packet.Schema{Fields: fields}
			pkt.Header.RecordsInPart = tc.rows
			pkt.Data.Rows = pktRows

			if err := ws.LoadData(ctx, "t", pkt); err != nil {
				t.Fatalf("load: %v", err)
			}

			res, err := ws.ExecuteSQL(ctx, `SELECT * FROM "t" ORDER BY "C0"`, "r")
			if err != nil {
				t.Fatalf("read back: %v", err)
			}
			got := res.GetRows()
			if len(got) != tc.rows {
				t.Fatalf("rows = %d, want %d", len(got), tc.rows)
			}
			// ORDER BY по текстовому C0 даёт лексикографический порядок, так что
			// сверяем как множество строк, а не по позиции.
			index := map[string][]string{}
			for _, g := range got {
				index[g[0]] = g
			}
			for r := range want {
				g, ok := index[want[r][0]]
				if !ok {
					t.Fatalf("row %q did not come back", want[r][0])
				}
				for c := range want[r] {
					if g[c] != want[r][c] {
						t.Errorf("row %d column %d = %q, want %q", r, c, g[c], want[r][c])
					}
				}
			}
		})
	}
}

// Размер пачки считается от числа параметров, а не строк: оптимум держится
// около 60 плейсхолдеров на запрос независимо от ширины таблицы. Тест
// закрепляет именно это правило — включая вырождение в построчную вставку на
// таблице шире порога, где многострочный INSERT уже проигрывает.
func TestInsertBatchRows(t *testing.T) {
	for _, tc := range []struct {
		fields, want int
	}{
		{1, 60},
		{6, 10},
		{20, 3},
		{30, 2},
		{60, 1},
		{61, 1},  // шире порога — обратно к одной строке на запрос
		{500, 1},
		{0, 1},   // вырожденный вход не должен давать деление на ноль
		{-1, 1},
	} {
		if got := insertBatchRows(tc.fields); got != tc.want {
			t.Errorf("insertBatchRows(%d) = %d, want %d", tc.fields, got, tc.want)
		}
	}

	// Ни при какой ширине нельзя превысить лимит SQLite в 999 переменных.
	for n := 1; n <= 1200; n++ {
		if params := insertBatchRows(n) * n; params > 999 && insertBatchRows(n) > 1 {
			t.Fatalf("insertBatchRows(%d) yields %d parameters, over SQLite's limit", n, params)
		}
	}
}
