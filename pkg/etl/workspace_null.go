package etl

// Что считать NULL на входе в workspace и чем отмечать его на выходе.
//
// Источники обозначают NULL по-разному, а SQLite должен получить настоящий
// NULL — иначе AVG, COUNT, IS NULL и фильтры считают по тексту маркера. Раньше
// NULL считались только "" и слово "NULL": NULL из базы (0x00) и из TDTP-файла
// ("[NULL]") сохранялся непустым текстом, а пустая строка становилась NULL.

import (
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/adapters/base"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/core/schema"
)

// nullStyle — как источник таблицы обозначает NULL.
type nullStyle int

const (
	// nullsTDTP — пакет TDTP: маркер поля (packet.NullMarkerOf), то есть
	// объявленный или прежний "[NULL]" без объявления.
	nullsTDTP nullStyle = iota
	// nullsDatabase — адаптер базы: 0x00. Необъявленный "[NULL]" здесь —
	// обычный текст из ячейки.
	nullsDatabase
	// nullsCSV — своего обозначения нет: пустая ячейка и слово NULL.
	nullsCSV
)

// SetSourceType сообщает workspace, из какого источника наполняется таблица
// (значение sources[].type). Без вызова строки читаются как TDTP.
func (w *Workspace) SetSourceType(tableName, sourceType string) {
	if w.nullStyles == nil {
		w.nullStyles = make(map[string]nullStyle)
	}
	switch sourceType {
	case "", "tdtp", "tdtp-enc", "tdtp-s3":
		w.nullStyles[tableName] = nullsTDTP
	case "csv":
		w.nullStyles[tableName] = nullsCSV
	default:
		w.nullStyles[tableName] = nullsDatabase
	}
}

// columnNull — правило NULL для одной колонки.
type columnNull struct {
	marker string // "" — маркера нет
	// emptyIsNull: "" не может быть значением. Это любая нетекстовая колонка,
	// а у CSV — любая.
	emptyIsNull bool
	wordIsNull  bool // слово "NULL" (только CSV)
}

func nullRules(fields []packet.Field, style nullStyle) []columnNull {
	rules := make([]columnNull, len(fields))
	for i, f := range fields {
		if style == nullsCSV {
			rules[i] = columnNull{emptyIsNull: true, wordIsNull: true}
			continue
		}
		isText := schema.IsTextType(schema.NormalizeType(schema.DataType(strings.ToUpper(f.Type))))
		rule := columnNull{emptyIsNull: !isText}
		if f.SpecialValues != nil && f.SpecialValues.Null != nil {
			rule.marker = f.SpecialValues.Null.Marker
		}
		if rule.marker == "" && style == nullsTDTP {
			rule.marker = packet.SpecNullMarker
		}
		rules[i] = rule
	}
	return rules
}

func (r columnNull) is(v string) bool {
	switch {
	case v == base.NullSentinel:
		return true
	case v == "":
		return r.emptyIsNull
	case r.wordIsNull && v == "NULL":
		return true
	}
	return r.marker != "" && v == r.marker
}

// canonicalResult приводит прочитанное из workspace к обычному виду пакета:
// NULL, отмеченный 0x00, получает объявленный маркер, не совпадающий ни с одним
// текстом колонки. Тогда любой выход — TDTP, XLSX, брокер — и обработчики
// pre_export видят NULL так же, как в любом другом пакете.
func canonicalResult(result *packet.DataPacket, rows [][]string) {
	rows, result.Schema = packet.DetectAndApply(rows, result.Schema)
	result.Data = packet.RowsToData(rows)
	result.Header.RecordsInPart = len(rows)
}
