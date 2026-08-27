package etl

import (
	"context"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// NULL в датовой колонке ронял ВЕСЬ запрос.
//
// ExecuteSQL привязывал DATE/DATETIME/TIMESTAMP к *string «иначе modernc
// парсит в time.Time». Разбор это не отменяло — драйвер решает по объявленному
// типу колонки, а не по типу приёмника, — зато на пустой ячейке
// database/sql выдавал "converting NULL to string is unsupported", и пайплайн
// с NULL-датой в результате трансформации падал целиком.
//
// Проверяется на всех трёх написаниях: тип берётся из DDL, и DATE, DATETIME и
// TIMESTAMP идут по разным веткам разметки.
func TestWorkspace_NullDateDoesNotBreakRead(t *testing.T) {
	ctx := context.Background()
	ws := newTestWorkspace(t, ctx)

	fields := []packet.Field{
		{Name: "ID", Type: "INTEGER"},
		{Name: "D", Type: "DATE"},
		{Name: "DT", Type: "DATETIME"},
		{Name: "TS", Type: "TIMESTAMP"},
		{Name: "Note", Type: "TEXT"},
	}
	if err := ws.CreateTable(ctx, "t", fields); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := ws.db.ExecContext(ctx,
		`INSERT INTO "t" VALUES (1, NULL, NULL, NULL, 'all dates null')`); err != nil {
		t.Fatalf("insert: %v", err)
	}

	res, err := ws.ExecuteSQL(ctx, `SELECT * FROM "t"`, "r")
	if err != nil {
		t.Fatalf("NULL in a date column must not fail the read: %v", err)
	}
	rows := res.GetRows()
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	for i, name := range []string{"D", "DT", "TS"} {
		if got := rows[0][i+1]; got != "" {
			t.Errorf("%s = %q, want the empty string (workspace's NULL)", name, got)
		}
	}
	if rows[0][4] != "all dates null" {
		t.Errorf("the non-date column came back as %q", rows[0][4])
	}
}

// Непустые даты обязаны отдаваться теми же байтами, что и раньше: "2006-01-02"
// для DATE, "2006-01-02 15:04:05" для остальных. Иначе починка NULL молча
// сменила бы формат всем существующим пайплайнам.
func TestWorkspace_DateFormatsUnchanged(t *testing.T) {
	ctx := context.Background()
	ws := newTestWorkspace(t, ctx)

	fields := []packet.Field{
		{Name: "ID", Type: "INTEGER"},
		{Name: "D", Type: "DATE"},
		{Name: "DT", Type: "DATETIME"},
		{Name: "TS", Type: "TIMESTAMP"},
	}
	if err := ws.CreateTable(ctx, "t", fields); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := ws.db.ExecContext(ctx,
		`INSERT INTO "t" VALUES (1, '1990-04-13', '2025-10-12 16:35:38', '2025-07-11 06:35:26')`); err != nil {
		t.Fatalf("insert: %v", err)
	}

	res, err := ws.ExecuteSQL(ctx, `SELECT * FROM "t"`, "r")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	got := res.GetRows()[0]
	want := []string{"1", "1990-04-13", "2025-10-12 16:35:38", "2025-07-11 06:35:26"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("column %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// Мешаная строка: заполненные и пустые даты рядом. Ловит ошибку разметки, при
// которой индексы колонок разъезжаются и значение садится не в свою ячейку.
func TestWorkspace_MixedNullAndFilledDates(t *testing.T) {
	ctx := context.Background()
	ws := newTestWorkspace(t, ctx)

	fields := []packet.Field{
		{Name: "ID", Type: "INTEGER"},
		{Name: "D", Type: "DATE"},
		{Name: "Note", Type: "TEXT"},
		{Name: "DT", Type: "DATETIME"},
	}
	if err := ws.CreateTable(ctx, "t", fields); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := ws.db.ExecContext(ctx, `INSERT INTO "t" VALUES
		(1, '2001-12-31', 'both',      '2001-12-31 23:59:59'),
		(2, NULL,         'no date',   '2002-01-01 00:00:00'),
		(3, '2003-03-03', 'no time',   NULL),
		(4, NULL,         'neither',   NULL)`); err != nil {
		t.Fatalf("insert: %v", err)
	}

	res, err := ws.ExecuteSQL(ctx, `SELECT * FROM "t" ORDER BY ID`, "r")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	want := [][]string{
		{"1", "2001-12-31", "both", "2001-12-31 23:59:59"},
		{"2", "", "no date", "2002-01-01 00:00:00"},
		{"3", "2003-03-03", "no time", ""},
		{"4", "", "neither", ""},
	}
	got := res.GetRows()
	if len(got) != len(want) {
		t.Fatalf("rows = %d, want %d", len(got), len(want))
	}
	for r := range want {
		for c := range want[r] {
			if got[r][c] != want[r][c] {
				t.Errorf("row %d column %d = %q, want %q", r, c, got[r][c], want[r][c])
			}
		}
	}
}

// Потоковое чтение — вторая копия того же цикла, и NULL ломал его ровно так же.
func TestWorkspace_NullDateInStream(t *testing.T) {
	ctx := context.Background()
	ws := newTestWorkspace(t, ctx)

	fields := []packet.Field{
		{Name: "ID", Type: "INTEGER"},
		{Name: "DT", Type: "DATETIME"},
	}
	if err := ws.CreateTable(ctx, "t", fields); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := ws.db.ExecContext(ctx,
		`INSERT INTO "t" VALUES (1, NULL), (2, '2025-01-02 03:04:05')`); err != nil {
		t.Fatalf("insert: %v", err)
	}

	res, err := ws.ExecuteSQLStream(ctx, `SELECT * FROM "t" ORDER BY ID`, "r")
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	var rows [][]string
	for r := range res.RowsChan {
		rows = append(rows, r)
	}
	if err := <-res.ErrorChan; err != nil {
		t.Fatalf("NULL in a date column must not fail the stream: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if rows[0][1] != "" {
		t.Errorf("NULL came back as %q, want the empty string", rows[0][1])
	}
	if rows[1][1] != "2025-01-02 03:04:05" {
		t.Errorf("filled value came back as %q", rows[1][1])
	}
}

func newTestWorkspace(t *testing.T, ctx context.Context) *Workspace {
	t.Helper()
	ws, err := NewWorkspace(ctx)
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}
	t.Cleanup(func() { _ = ws.Close(ctx) })
	return ws
}

// LEFT JOIN без совпадения — та форма, в которой баг встречается на практике,
// и единственная, которой удалось его воспроизвести сквозь пайплайн.
//
// Условие срабатывания узкое: колонка должна СОХРАНИТЬ объявленный тип до
// результата трансформации. Прямая ссылка на колонку реальной таблицы
// (d.d_date) его сохраняет, а выражение — NULLIF(...), COALESCE(...) — нет: у
// вычисленной колонки DatabaseTypeName пустой, разметка дат её не трогает, и
// NULL проходит без приключений. Поэтому наивная проверка «сделаем NULL в
// дате» отказ не даёт, и баг дожил до сюда.
//
// На боевом пайплайне это выглядело так:
//
//	Error: pipeline execution failed: failed to execute transformation:
//	failed to scan row: sql: Scan error on column index 2, name "d_date":
//	converting NULL to string is unsupported
func TestWorkspace_LeftJoinUnmatchedDate(t *testing.T) {
	ctx := context.Background()
	ws := newTestWorkspace(t, ctx)

	if err := ws.CreateTable(ctx, "dates", []packet.Field{
		{Name: "ID", Type: "INTEGER"},
		{Name: "D", Type: "DATE"},
	}); err != nil {
		t.Fatalf("create dates: %v", err)
	}
	if err := ws.CreateTable(ctx, "ids", []packet.Field{
		{Name: "ID", Type: "INTEGER"},
	}); err != nil {
		t.Fatalf("create ids: %v", err)
	}
	if _, err := ws.db.ExecContext(ctx,
		`INSERT INTO "dates" VALUES (1, '2020-01-01'), (2, '2020-02-02')`); err != nil {
		t.Fatalf("insert dates: %v", err)
	}
	if _, err := ws.db.ExecContext(ctx,
		`INSERT INTO "ids" VALUES (1), (2), (3), (4)`); err != nil {
		t.Fatalf("insert ids: %v", err)
	}

	res, err := ws.ExecuteSQL(ctx,
		`SELECT i."ID", d."D" FROM "ids" i LEFT JOIN "dates" d ON d."ID" = i."ID" ORDER BY i."ID"`,
		"joined")
	if err != nil {
		t.Fatalf("an unmatched LEFT JOIN row must not fail the transform: %v", err)
	}

	rows := res.GetRows()
	want := [][]string{
		{"1", "2020-01-01"},
		{"2", "2020-02-02"},
		{"3", ""},
		{"4", ""},
	}
	if len(rows) != len(want) {
		t.Fatalf("rows = %d, want %d", len(rows), len(want))
	}
	for r := range want {
		for c := range want[r] {
			if rows[r][c] != want[r][c] {
				t.Errorf("row %d column %d = %q, want %q", r, c, rows[r][c], want[r][c])
			}
		}
	}
}
