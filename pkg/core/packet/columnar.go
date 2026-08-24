package packet

import "fmt"

// RowsToColumnarData раскладывает строки по колонкам: каждый Row результата —
// одна колонка целиком, значения разделены '|'.
//
// Экранирование берётся то же, что у построчного пути (writeEscaped через
// mask), и это не выбор из удобства. Два писателя в пакете уже дают разные
// байты на значении с переводом строки; третья схема добавила бы третий
// вариант тех же данных. Колоночная раскладка наследует каноническую.
func RowsToColumnarData(rows [][]string, nFields int, mask []bool) Data {
	data := Data{Layout: LayoutColumns, Rows: make([]Row, nFields)}
	if len(rows) == 0 {
		for c := range data.Rows {
			data.Rows[c] = Row{Value: ""}
		}
		return data
	}

	// Ёмкость под колонку: средняя длина значения по первой строке, умноженная
	// на высоту. Промах стоит удвоения, отсутствие оценки — их логарифма.
	avg := 0
	for _, v := range rows[0] {
		avg += len(v) + 1
	}
	if len(rows[0]) > 0 {
		avg /= len(rows[0])
	}

	buf := make([]byte, 0, (avg+1)*len(rows))
	for c := 0; c < nFields; c++ {
		buf = buf[:0]
		for r := range rows {
			if r > 0 {
				buf = append(buf, '|')
			}
			if c < len(rows[r]) {
				if c < len(mask) && !mask[c] {
					buf = append(buf, rows[r][c]...)
				} else {
					buf = append(buf, escapeValue(rows[r][c])...)
				}
			}
		}
		data.Rows[c] = Row{Value: string(buf)}
	}
	return data
}

// ExpandColumnarRows разворачивает колоночную раскладку обратно в построчную.
//
// Устроен как ExpandCompactRows: правит Data.Rows на месте и снимает свой
// флаг, не трогая Compression, Checksum и XXH3 — они описывают уже случившееся
// и после разворота остаются верными.
//
// Зовётся после распаковки: у сжатого пакета строки ещё лежат в блобе.
func ExpandColumnarRows(pkt *DataPacket) error {
	if pkt.Data.Layout != LayoutColumns {
		return nil
	}
	// Сжатый пакет разворачивать нечем — строки ещё в блобе. Не ошибка, а
	// «рано»: DecompressData позовёт эту же функцию сразу после распаковки.
	// Ошибка тут сделала бы функцию непригодной как автоматический шаг, а
	// именно автоматизм и нужен: разворот руками — это десять мест вызова, и
	// одиннадцатое забытое даёт молча неверные строки, без всякого признака.
	if pkt.Data.Compression != "" {
		return nil
	}

	nFields := len(pkt.Schema.Fields)
	if len(pkt.Data.Rows) != nFields {
		return fmt.Errorf("columnar layout: %d column(s) in Data, %d field(s) in Schema",
			len(pkt.Data.Rows), nFields)
	}
	if nFields == 0 {
		pkt.Data.Layout = ""
		return nil
	}

	parser := NewParser()
	cols := make([][]string, nFields)
	for c := range pkt.Data.Rows {
		cols[c] = parser.GetRowValues(pkt.Data.Rows[c])
	}

	// Все колонки обязаны быть одной высоты. Иначе значения разъехались бы по
	// чужим строкам — порча, которую ничто дальше не заметит.
	height := len(cols[0])
	for c := range cols {
		if len(cols[c]) != height {
			return fmt.Errorf("columnar layout: column %q has %d value(s), column %q has %d",
				pkt.Schema.Fields[c].Name, len(cols[c]), pkt.Schema.Fields[0].Name, height)
		}
	}
	// Пустая Data даёт одну пустую строку на колонку — это ноль строк, а не одна.
	if height == 1 && cols[0][0] == "" {
		empty := true
		for c := range cols {
			if cols[c][0] != "" {
				empty = false
				break
			}
		}
		if empty {
			pkt.Data.Rows = nil
			pkt.Data.Layout = ""
			return nil
		}
	}

	newRows := make([]Row, height)
	buf := make([]byte, 0, 128)
	for r := 0; r < height; r++ {
		buf = buf[:0]
		for c := 0; c < nFields; c++ {
			if c > 0 {
				buf = append(buf, '|')
			}
			buf = append(buf, escapeValue(cols[c][r])...)
		}
		newRows[r] = Row{Value: string(buf)}
	}

	pkt.Data.Rows = newRows
	pkt.Data.Layout = ""
	return nil
}

// BuildEscapeMask отдаёт маску экранирования по схеме: false для типов,
// которые экранировать не нужно. Экспортируется для тех, кто раскладывает
// данные по колонкам вне пакета (например, сжатый путь экспорта).
func BuildEscapeMask(schema Schema) []bool {
	return buildEscapeMask(schema)
}
