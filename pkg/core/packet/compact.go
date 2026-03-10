package packet

import "strings"

// ExpandCompactRows разворачивает compact-формат данных в полный формат.
//
// В compact-формате (Data.Compact == true) fixed поля пишутся только в первой
// строке группы. В последующих строках группы на позициях fixed полей стоят пустые
// строки. При смене значения fixed поля (новая группа) значение снова пишется явно.
//
// Алгоритм carry-forward:
//   - Для каждого fixed поля хранится последнее явно записанное значение.
//   - Если в текущей строке позиция fixed поля пустая — подставляется carry-значение.
//   - Если непустая — обновляет carry и используется как есть.
//
// После вызова Data.Compact устанавливается в false (данные уже развёрнуты).
// Если Data.Compact == false — функция ничего не делает.
func ExpandCompactRows(packet *DataPacket) error {
	if !packet.Data.Compact || len(packet.Data.Rows) == 0 {
		return nil
	}

	nFields := len(packet.Schema.Fields)

	// Позиции fixed полей
	fixedPos := make([]bool, nFields)
	hasFixed := false
	for i, f := range packet.Schema.Fields {
		fixedPos[i] = f.Fixed
		if f.Fixed {
			hasFixed = true
		}
	}

	if !hasFixed {
		packet.Data.Compact = false
		return nil
	}

	parser := NewParser()

	// carry-forward значения fixed полей
	carry := make([]string, nFields)

	newRows := make([]Row, len(packet.Data.Rows))
	for rowIdx, row := range packet.Data.Rows {
		values := parser.GetRowValues(row)

		// Дополняем до nFields если строка короче (например, только variable поля присутствуют)
		for len(values) < nFields {
			values = append(values, "")
		}

		// Для fixed полей: если значение пустое — подставляем carry; если нет — обновляем carry
		for i := 0; i < nFields && i < len(values); i++ {
			if fixedPos[i] {
				if values[i] != "" {
					carry[i] = values[i]
				} else {
					values[i] = carry[i]
				}
			}
		}

		// Сериализуем значения обратно в Row (с экранированием)
		escaped := make([]string, len(values))
		for i, v := range values {
			escaped[i] = escapeValue(v)
		}
		newRows[rowIdx] = Row{Value: strings.Join(escaped, "|")}
	}

	packet.Data.Rows = newRows
	packet.Data.Compact = false
	return nil
}

// RowsToCompactData преобразует [][]string в Data с compact-форматом.
//
// Для полей с Fixed=true значение пишется только при первом появлении или при
// смене значения (новая группа). В остальных строках группы на этой позиции
// записывается пустая строка (пропуск).
//
// Если в схеме нет ни одного fixed поля — возвращает обычный Data (compact=false).
func RowsToCompactData(rows [][]string, schema Schema) Data {
	if len(rows) == 0 {
		return Data{Compact: true}
	}

	nFields := len(schema.Fields)

	// Позиции fixed полей
	fixedPos := make([]bool, nFields)
	hasFixed := false
	for i, f := range schema.Fields {
		if i >= nFields {
			break
		}
		fixedPos[i] = f.Fixed
		if f.Fixed {
			hasFixed = true
		}
	}

	if !hasFixed {
		return RowsToData(rows)
	}

	data := Data{
		Compact: true,
		Rows:    make([]Row, len(rows)),
	}

	// lastFixed хранит последнее записанное значение fixed поля (для детектирования смены)
	lastFixed := make([]string, nFields)
	firstRow := true

	for rowIdx, row := range rows {
		parts := make([]string, nFields)

		for i := 0; i < nFields; i++ {
			var val string
			if i < len(row) {
				val = row[i]
			}

			if i < len(fixedPos) && fixedPos[i] {
				if firstRow || val != lastFixed[i] {
					// Первая строка или смена значения — пишем явно
					parts[i] = escapeValue(val)
					lastFixed[i] = val
				} else {
					// Значение не изменилось — пропуск
					parts[i] = ""
				}
			} else {
				parts[i] = escapeValue(val)
			}
		}

		data.Rows[rowIdx] = Row{Value: strings.Join(parts, "|")}
		firstRow = false
	}

	return data
}
