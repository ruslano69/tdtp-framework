package etl

// Почастная загрузка TDTP-файлов в workspace.
//
// Раньше все части склеивались в один пакет, который потом целиком разбивался
// на колонки и вставлялся в SQLite: в памяти одновременно лежали строки пакета,
// их разбитая копия и растущая таблица. Теперь часть читается, вставляется и
// освобождается; на 5 млн строк это снизило пик памяти процесса с 5,0 до
// 1,9 ГБ при том же времени загрузки.

import (
	"context"
	"fmt"
	"slices"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// loadByPart создаёт таблицу источника и наполняет её часть за частью.
// Возвращает пакет без строк: схему первой части и общее число строк.
func (p *Processor) loadByPart(ctx context.Context, source SourceConfig) (*packet.DataPacket, error) {
	var (
		first  *packet.DataPacket
		fields []packet.Field // схема первой части с именами до sanitize
		total  int
		// insertErr отличает ошибку записи в workspace от ошибки чтения файла.
		insertErr error
	)

	err := forEachTDTPPart(source, func(path string, pkt *packet.DataPacket) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		rows := pkt.Header.RecordsInPart

		if first == nil {
			fields = slices.Clone(pkt.Schema.Fields)
			p.sanitizeSource(source.Name, pkt)
			if err := p.workspace.CreateTable(ctx, source.Name, pkt.Schema.Fields); err != nil {
				insertErr = fmt.Errorf("failed to create table '%s': %w", source.Name, err)
				return insertErr
			}
		} else if err := checkPartSchema(fields, pkt); err != nil {
			return fmt.Errorf("file '%s': %w", path, err)
		}

		if err := p.workspace.LoadData(ctx, source.Name, pkt); err != nil {
			insertErr = fmt.Errorf("failed to load data into '%s' from '%s': %w", source.Name, path, err)
			return insertErr
		}
		total += rows

		if first == nil {
			pkt.Data = packet.Data{} // строки уже в SQLite
			first = pkt
		}
		return nil
	})
	if insertErr != nil {
		return nil, fmt.Errorf("failed to populate workspace: %w", insertErr)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to load sources: source '%s': %w", source.Name, err)
	}

	first.Header.TableName = source.Name
	first.Header.RecordsInPart = total
	return first, nil
}

// checkPartSchema проверяет, что строки части можно читать по схеме первой.
//
// Строки вставляются по позициям, а результат описывается схемой первой части,
// поэтому колонки, их имена и типы обязаны совпадать.
//
// Маркеры особых значений могут законно различаться: в compact-формате они
// выбираются для каждой части. Расхождение — ошибка, только когда в строках
// этой части есть значение, равное одному из двух маркеров: по схеме первой
// части оно прочиталось бы не тем, что означает.
func checkPartSchema(first []packet.Field, pkt *packet.DataPacket) error {
	fields := pkt.Schema.Fields
	if len(fields) != len(first) {
		return fmt.Errorf("schema differs from the first part: %d fields, the first has %d", len(fields), len(first))
	}

	var rows [][]string // разбиваются, только если есть расхождение в маркерах
	for i, f := range fields {
		want := first[i]
		if f.Name != want.Name || f.Type != want.Type || f.Subtype != want.Subtype {
			return fmt.Errorf("schema differs from the first part: field %d is %s %s %q, the first has %s %s %q",
				i+1, f.Name, f.Type, f.Subtype, want.Name, want.Type, want.Subtype)
		}

		expected, actual := specialMarkers(want), specialMarkers(f)
		for kind := range expected {
			if expected[kind] == actual[kind] {
				continue
			}
			if rows == nil {
				rows = pkt.GetRows()
			}
			for _, row := range rows {
				if i >= len(row) {
					break // короткую строку отклонит LoadData
				}
				if v := row[i]; v != "" && (v == expected[kind] || v == actual[kind]) {
					return fmt.Errorf("field %q: special value markers differ from the first part "+
						"(%q here, %q there) and the part contains %q", f.Name, actual[kind], expected[kind], v)
				}
			}
		}
	}
	return nil
}

// specialMarkers — действующие маркеры поля; пустая строка — маркера нет.
// У NULL он есть всегда: без объявления читатели понимают прежний "[NULL]".
func specialMarkers(f packet.Field) [5]string {
	markers := [5]string{packet.NullMarkerOf(f)}
	if sv := f.SpecialValues; sv != nil {
		for i, m := range []*packet.MarkerValue{sv.Infinity, sv.NegInfinity, sv.NaN, sv.NoDate} {
			if m != nil {
				markers[i+1] = m.Marker
			}
		}
	}
	return markers
}
