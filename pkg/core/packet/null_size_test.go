package packet

import (
	"context"
	"fmt"
	"testing"
)

// Пустая ячейка приходит из адаптера одним байтом NUL, а записывается как
// "[NULL]" — шесть. Считать надо то, что будет записано.
func TestWrittenValueLen_NullCountsAsItsMarker(t *testing.T) {
	if got, want := writtenValueLen(nullSentinel), len(SpecNullMarker); got != want {
		t.Errorf("NUL measured as %d, want %d (the length of %q)", got, want, SpecNullMarker)
	}
	if got := writtenValueLen("ordinary"); got != len("ordinary") {
		t.Errorf("an ordinary value measured as %d, want %d", got, len("ordinary"))
	}
	if got := writtenValueLen(""); got != 0 {
		t.Errorf("the empty string measured as %d, want 0", got)
	}

	// Прочие маркеры значение сокращают либо не меняют длины, и оцениваются по
	// сырому виду НАМЕРЕННО: перебор даёт часть меньше заказанной, недоучёт —
	// больше, и ошибаться безопасно только во вторую сторону.
	for _, raw := range []string{"Infinity", "-Infinity", "NaN", "0001-01-01"} {
		if got := writtenValueLen(raw); got != len(raw) {
			t.Errorf("%q measured as %d, want its raw length %d", raw, got, len(raw))
		}
	}
}

// Потоковое и обычное дробление обязаны совпасть на строках с NULL.
//
// Обычный путь зовёт DetectAndApply один раз по всем строкам и меряет уже
// готовые "[NULL]"; поток меряет строку на входе и проставляет маркеры позже,
// в createPart. Пока оценка считала сырой NUL за один байт, потоковые части
// выходили крупнее заказанных, и промах рос вместе с долей пустых ячеек: на
// одной колонке из десяти с 10% пустых — 0.3%, на восьми полностью пустых
// датовых — 43%, то есть 2.78 МБ там, где просили 2.
//
// Данные при этом не терялись: и тогда, и сейчас обе дороги отдают одни строки
// в одном порядке. Ломалось обещание --packet-size, а он существует ровно для
// случая, когда размер ограничен снаружи.
func TestPartitioning_StreamAgreesWithBufferedOnNulls(t *testing.T) {
	// Бюджет задан маленьким намеренно: расхождение видно только на границах
	// частей, а с умолчательными ~1.9 МБ четыре тысячи узких строк умещаются в
	// одну часть, и сравнивать нечего. Первая версия этого теста именно так и
	// прошла на сломанном коде.
	const (
		rows   = 4000
		budget = 100_000
	)

	for _, tc := range []struct {
		name      string
		nullCols  int
		totalCols int
	}{
		{"no nulls", 0, 9},
		{"one column in ten", 1, 9},
		{"every date column", 8, 9},
	} {
		t.Run(tc.name, func(t *testing.T) {
			schema := Schema{Fields: []Field{{Name: "ID", Type: "INTEGER"}}}
			for i := 0; i < tc.totalCols-1; i++ {
				schema.Fields = append(schema.Fields,
					Field{Name: fmt.Sprintf("D%d", i), Type: "DATETIME"})
			}

			data := make([][]string, rows)
			for r := range data {
				rec := make([]string, tc.totalCols)
				rec[0] = fmt.Sprintf("%d", r)
				for c := 1; c < tc.totalCols; c++ {
					if c <= tc.nullCols {
						rec[c] = nullSentinel
					} else {
						rec[c] = "2025-01-02 03:04:05"
					}
				}
				data[r] = rec
			}

			// Обычный путь.
			g := NewGenerator()
			g.SetMaxMessageSize(budget)
			buffered, err := g.GenerateReference("t", schema, data)
			if err != nil {
				t.Fatalf("buffered: %v", err)
			}
			bufShape := make([]int, len(buffered))
			for i, p := range buffered {
				bufShape[i] = p.Header.RecordsInPart
			}

			// Потоковый.
			ch := make(chan []string, 16)
			go func() {
				defer close(ch)
				for _, r := range data {
					ch <- append([]string(nil), r...)
				}
			}()
			sg := NewStreamingGenerator()
			sg.SetPartSize(budget)
			partsChan, summaryChan := sg.GeneratePartsStream(
				context.Background(), ch, schema, "t", TypeReference)

			var strShape []int
			for part := range partsChan {
				if part.Error != nil {
					t.Fatalf("streaming part %d: %v", part.PartNum, part.Error)
				}
				strShape = append(strShape, part.RowsCount)
			}
			if sum := <-summaryChan; sum != nil && sum.TotalRows != rows {
				t.Errorf("streamed %d rows, want %d", sum.TotalRows, rows)
			}

			if len(strShape) != len(bufShape) {
				t.Fatalf("parts: streamed %d %v, buffered %d %v",
					len(strShape), strShape, len(bufShape), bufShape)
			}
			for i := range bufShape {
				if strShape[i] != bufShape[i] {
					t.Errorf("part %d: streamed %d rows, buffered %d — the two "+
						"partitioners disagree, so --packet-size means different "+
						"things on the two paths\nstreamed %v\nbuffered %v",
						i+1, strShape[i], bufShape[i], strShape, bufShape)
					break
				}
			}
		})
	}
}
