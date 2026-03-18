// Package packet provides functionality for the TDTP framework.
package packet

import "strings"

// ExpandCompactRows разворачивает compact-формат данных в полный формат.
//
// В compact-формате (Data.Compact == true) fixed поля пишутся только в первой
// строке группы. В последующих строках группы на позициях fixed полей стоят пустые
// строки. При смене значения fixed поля (новая группа) значение снова пишется явно.
//
// Алгоритм carry-forward:
//   - Если Data.Carry непустой — парсится как начальное carry-состояние чанка,
//     что позволяет декодировать произвольный чанк независимо от предыдущих.
//   - Для каждого fixed поля хранится последнее явно записанное значение.
//   - Если в текущей строке позиция fixed поля пустая — подставляется carry-значение.
//   - Если непустая — обновляет carry и используется как есть.
//
// Если Data.Tail == true — последняя строка должна содержать все fixed поля явно
// (является carry-снэпшотом). Нарушение этого инварианта возвращает ошибку.
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

	// Если задан carry-атрибут чанка — инициализируем carry из него.
	// Это позволяет декодировать чанк независимо, без предыдущих пакетов.
	if packet.Data.Carry != "" {
		initValues := parser.GetRowValues(Row{Value: packet.Data.Carry})
		for i := 0; i < nFields && i < len(initValues); i++ {
			if fixedPos[i] && initValues[i] != "" {
				carry[i] = initValues[i]
			}
		}
	}

	lastIdx := len(packet.Data.Rows) - 1

	// Валидация tail-строки: все fixed поля должны быть явно заполнены
	if packet.Data.Tail {
		tailValues := parser.GetRowValues(packet.Data.Rows[lastIdx])
		for i := 0; i < nFields; i++ {
			if fixedPos[i] {
				val := ""
				if i < len(tailValues) {
					val = tailValues[i]
				}
				if val == "" {
					return &CompactTailError{FieldIndex: i, FieldName: packet.Schema.Fields[i].Name}
				}
			}
		}
	}

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
	packet.Data.Tail = false
	packet.Data.Carry = ""
	return nil
}

// CompactTailError возникает когда tail-строка содержит пустое fixed поле.
type CompactTailError struct {
	FieldIndex int
	FieldName  string
}

func (e *CompactTailError) Error() string {
	return "compact tail row: fixed field " + e.FieldName + " is empty (tail row must repeat all fixed fields explicitly)"
}

// RowsToCompactData преобразует [][]string в Data с compact-форматом.
//
// Для полей с Fixed=true значение пишется только при первом появлении или при
// смене значения (новая группа). В остальных строках группы на этой позиции
// записывается пустая строка (пропуск).
//
// Если tail=true — последняя строка записывается с явными значениями всех fixed
// полей (carry-снэпшот). Это позволяет:
//   - читать данные с конца пакета / потока без полного прохода с начала;
//   - валидировать консистентность carry-forward при декодировании;
//   - передавать carry-состояние следующему чанку через атрибут carry="...".
//
// Если в схеме нет ни одного fixed поля — возвращает обычный Data (compact=false).
func RowsToCompactData(rows [][]string, schema Schema, tail bool) Data {
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
	lastIdx := len(rows) - 1

	for rowIdx, row := range rows {
		parts := make([]string, nFields)

		// Tail-строка: все fixed поля пишутся явно, независимо от carry.
		// Это делает последнюю строку самодостаточной carry-снэпшотом.
		isTailRow := tail && rowIdx == lastIdx

		for i := 0; i < nFields; i++ {
			var val string
			if i < len(row) {
				val = row[i]
			}

			if i < len(fixedPos) && fixedPos[i] {
				if firstRow || val != lastFixed[i] || isTailRow {
					// Первая строка, смена значения, или tail-строка — пишем явно
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

	if tail {
		data.Tail = true
	}

	return data
}

// AutoFixedFields возвращает имена полей схемы, у которых имя начинается с "_".
// Используется для auto-detect fixed полей при compact-экспорте.
func AutoFixedFields(schema Schema) []string {
	var names []string
	for _, f := range schema.Fields {
		if strings.HasPrefix(f.Name, "_") {
			names = append(names, f.Name)
		}
	}
	return names
}

// ResolveFixedFields возвращает финальный список fixed полей.
// Если explicit непустой — возвращает его. Иначе auto-detect по _ prefix.
func ResolveFixedFields(schema Schema, explicit []string) []string {
	if len(explicit) > 0 {
		return explicit
	}
	return AutoFixedFields(schema)
}

// ApplyCompact применяет compact-формат к пакету:
// помечает поля fixedFieldNames как fixed в схеме, стрипает _ prefix из имён,
// перекодирует строки в compact-формат, устанавливает версию 1.3.1.
func ApplyCompact(pkt *DataPacket, fixedFieldNames []string, tail bool) error {
	fixedSet := make(map[string]bool, len(fixedFieldNames))
	for _, f := range fixedFieldNames {
		fixedSet[f] = true
	}

	for i := range pkt.Schema.Fields {
		name := pkt.Schema.Fields[i].Name
		stripped := strings.TrimPrefix(name, "_")
		if fixedSet[name] || fixedSet[stripped] {
			pkt.Schema.Fields[i].Fixed = true
			if strings.HasPrefix(name, "_") {
				pkt.Schema.Fields[i].Name = stripped
			}
		}
	}

	parser := NewParser()
	rows := make([][]string, len(pkt.Data.Rows))
	for i, row := range pkt.Data.Rows {
		rows[i] = parser.GetRowValues(row)
	}

	pkt.Data = RowsToCompactData(rows, pkt.Schema, tail)
	pkt.Version = "1.3.1"
	return nil
}
