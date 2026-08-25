package sqlite

import (
	"context"
	"testing"
)

// Потоковое чтение обязано давать те же значения, что построчное.
//
// Иначе выбор режима менял бы данные — и менял бы xxh3, который по ним
// считается. Оба пути идут через один cellToTDTP именно ради этого.
func TestReadAllRowsStream_MatchesReadAllRows(t *testing.T) {
	a, ctx := openBench(t, datesDB)

	schema, err := a.GetTableSchema(ctx, "Users")
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	want, err := a.ReadAllRows(ctx, "Users", schema)
	if err != nil {
		t.Fatalf("ReadAllRows: %v", err)
	}

	_, rowsChan, errChan, err := a.ReadAllRowsStream(ctx, "Users", schema)
	if err != nil {
		t.Fatalf("ReadAllRowsStream: %v", err)
	}

	i := 0
	for row := range rowsChan {
		if i >= len(want) {
			t.Fatalf("поток отдал больше строк, чем %d", len(want))
		}
		for c := range row {
			if row[c] != want[i][c] {
				t.Fatalf("строка %d колонка %d (%s): поток %q, построчно %q",
					i, c, schema.Fields[c].Name, row[c], want[i][c])
			}
		}
		i++
	}
	// Ошибку читаем ПОСЛЕ закрытия канала: закрытие значит «строк больше нет»,
	// а не «всё прошло хорошо».
	if err := <-errChan; err != nil {
		t.Fatalf("поток завершился ошибкой: %v", err)
	}
	if i != len(want) {
		t.Fatalf("поток отдал %d строк, построчно %d", i, len(want))
	}
}

// Отмена контекста обязана останавливать чтение, а не оставлять горутину с
// открытым курсором.
func TestReadAllRowsStream_StopsOnCancel(t *testing.T) {
	a, ctx := openBench(t, datesDB)
	schema, err := a.GetTableSchema(ctx, "Users")
	if err != nil {
		t.Fatalf("schema: %v", err)
	}

	cctx, cancel := context.WithCancel(ctx)
	_, rowsChan, errChan, err := a.ReadAllRowsStream(cctx, "Users", schema)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}

	<-rowsChan // одну строку взяли
	cancel()

	drained := 0
	for range rowsChan {
		drained++
		if drained > 100000 {
			t.Fatal("канал не закрылся после отмены")
		}
	}
	if err := <-errChan; err == nil {
		t.Error("после отмены ожидалась ошибка контекста")
	}
}

func BenchmarkReadAllRowsStream_Dates(b *testing.B) {
	a, ctx := openBench(b, datesDB)
	schema, _ := a.GetTableSchema(ctx, "Users")
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, rowsChan, errChan, err := a.ReadAllRowsStream(ctx, "Users", schema)
		if err != nil {
			b.Fatal(err)
		}
		n := 0
		for range rowsChan {
			n++
		}
		if err := <-errChan; err != nil {
			b.Fatal(err)
		}
	}
}
