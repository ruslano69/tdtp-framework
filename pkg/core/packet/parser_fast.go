package packet

import (
	"bytes"
	"encoding/xml"
	"unicode/utf8"

	"github.com/ruslano69/tdtp-framework/pkg/core/xmlchar"
)

// Ручной парсер Data-секции — зеркало writePacketTo (см. xmlwriter.go).
//
// Писатель уже снял reflection с горячего участка: Header/Schema идут через
// xml.Marshal (они ~200 байт), а тысячи <R> пишутся вручную. Здесь та же
// гибридная стратегия, но на чтении.
//
// Как это работает: из пакета вырезается тело Data и собирается «скелет» —
// тот же XML, но с пустой Data-секцией (~500 байт независимо от числа строк).
// Скелет отдаётся xml.Unmarshal, который разбирает корневые атрибуты, Header,
// Query, QueryContext, Schema, атрибуты Data и AlarmDetails — со всей их
// семантикой и без риска разойтись с эталоном. Вручную сканируются только
// строки <R>...</R>, где сосредоточена вся масса данных.
//
// Любое отклонение от ожидаемой формы (CDATA, комментарии, нераспознанная
// сущность, <Data/>, сбой разбора скелета) даёт ok=false — вызывающий
// откатывается на полный xml.Unmarshal. Частичный результат не возвращается.

var bDataName = []byte("<Data")
var bDataCloseTag = []byte("</Data>")

var bSchemaName = []byte("<Schema")
var bSchemaCloseTag = []byte("</Schema>")

// schemaSpanSize возвращает размер элемента <Schema>...</Schema> прямо в
// исходных байтах.
//
// Это ровно та величина, которую validateSchemaSize меряет на записи: writer
// пишет туда результат того же xml.Marshal, так что для наших пакетов числа
// совпадают байт в байт. Но здесь она достаётся двумя bytes.Index вместо
// повторной сериализации — marshal схемы на 10 полей стоит ~13.7мкс против
// ~51мкс на разбор небольшого пакета целиком, то есть проверка съедала бы
// четверть разбора, а на 100 полях (~111мкс) превышала бы его вдвое.
//
// Для чужих пакетов это к тому же честнее: меряется то, что реально пришло,
// а не результат нашей повторной сериализации.
//
// ok=false означает, что секцию найти не удалось — вызывающий пропускает
// проверку и полагается на обычный разбор.
//
// Поиск идёт циклом, как в findDataSection, и по той же причине: элемент с
// префиксом в имени (<SchemaVersion>) не должен прекращать поиск. Раньше
// прекращал, и проверка размера молча выключалась — а пропуск здесь означает
// пропуск MaxSchemaBytesRead, то есть ровно того предела, ради которого
// функция и написана.
func schemaSpanSize(data []byte) (int, bool) {
	off := 0
	for {
		i := bytes.Index(data[off:], bSchemaName)
		if i < 0 {
			return 0, false
		}
		i += off

		next := i + len(bSchemaName)
		if next >= len(data) {
			return 0, false
		}
		if !isDataTagBoundary(data[next]) {
			off = next // <SchemaVersion> или другое имя с префиксом Schema
			continue
		}

		gt := bytes.IndexByte(data[next:], '>')
		if gt < 0 {
			return 0, false
		}
		gt += next

		// <Schema/> — пустая схема, весь элемент и есть её размер.
		if data[gt-1] == '/' {
			return gt + 1 - i, true
		}

		end := bytes.Index(data[gt:], bSchemaCloseTag)
		if end < 0 {
			return 0, false
		}

		return gt + end + len(bSchemaCloseTag) - i, true
	}
}

func isXMLSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

// isDataTagBoundary отсекает <DataPacket: имя элемента должно кончаться здесь.
func isDataTagBoundary(c byte) bool {
	return c == '>' || c == '/' || isXMLSpace(c)
}

// findDataSection находит корневой <Data ...> ... </Data>.
// bodyStart — сразу после '>' открывающего тега, bodyEnd — индекс '<' в "</Data>",
// afterClose — сразу после "</Data>".
func findDataSection(data []byte) (bodyStart, bodyEnd, afterClose int, ok bool) {
	off := 0
	for {
		i := bytes.Index(data[off:], bDataName)
		if i < 0 {
			return 0, 0, 0, false
		}
		i += off
		next := i + len(bDataName)
		if next >= len(data) {
			return 0, 0, 0, false
		}
		if !isDataTagBoundary(data[next]) {
			off = next // это <DataPacket или другое имя с префиксом Data
			continue
		}

		gt := bytes.IndexByte(data[next:], '>')
		if gt < 0 {
			return 0, 0, 0, false
		}
		gt += next
		if data[gt-1] == '/' {
			return 0, 0, 0, false // <Data/> — редко, отдаём в fallback
		}

		bodyStart = gt + 1
		j := bytes.Index(data[bodyStart:], bDataCloseTag)
		if j < 0 {
			return 0, 0, 0, false
		}
		bodyEnd = bodyStart + j
		afterClose = bodyEnd + len(bDataCloseTag)
		return bodyStart, bodyEnd, afterClose, true
	}
}

// hasAnotherDataTag ищет второй корневой <Data ...> в хвосте. Для нашей схемы
// это малформед; если он есть — xml.Unmarshal скелета выбрал бы другую секцию,
// поэтому безопаснее уйти в fallback.
func hasAnotherDataTag(tail []byte) bool {
	off := 0
	for {
		i := bytes.Index(tail[off:], bDataName)
		if i < 0 {
			return false
		}
		i += off
		next := i + len(bDataName)
		if next < len(tail) && isDataTagBoundary(tail[next]) {
			return true
		}
		off = next
	}
}

// decodeChardata раскрывает XML-сущности в теле строки и нормализует переводы
// строк — то и другое ровно так, как это делает encoding/xml, на который
// откатывается быстрый путь.
//
// Про нормализацию: XML 1.0 §2.11 предписывает привести сырые CRLF и одиночный
// CR к LF, и encoding/xml так и поступает. Расхождение здесь означало бы, что
// один и тот же файл читается по-разному в зависимости от того, сработал
// быстрый путь или нет — а он срабатывает не всегда. Числовые ссылки под
// нормализацию не подпадают: &#xD; — это способ записать CR так, чтобы он
// пережил разбор (см. escCR в xmlwriter.go), поэтому он раскрывается в CR и
// остаётся CR.
//
// Быстрый путь — нет ни '&', ни CR — возвращает содержимое без разбора.
// Нераспознанная сущность даёт ok=false (уходим в fallback).
func decodeChardata(s []byte) (string, bool) {
	if bytes.IndexByte(s, '&') < 0 && bytes.IndexByte(s, '\r') < 0 {
		return string(s), true
	}

	out := make([]byte, 0, len(s))

	for i := 0; i < len(s); {
		switch s[i] {
		case '\r':
			// CRLF и одиночный CR → LF.
			out = append(out, '\n')
			i++
			if i < len(s) && s[i] == '\n' {
				i++
			}

		case '&':
			semi := bytes.IndexByte(s[i:], ';')
			if semi < 0 {
				return "", false
			}
			ent := s[i+1 : i+semi] // без '&' и ';'
			i += semi + 1

			switch {
			case bytes.Equal(ent, []byte("lt")):
				out = append(out, '<')
			case bytes.Equal(ent, []byte("gt")):
				out = append(out, '>')
			case bytes.Equal(ent, []byte("amp")):
				out = append(out, '&')
			case bytes.Equal(ent, []byte("quot")):
				out = append(out, '"')
			case bytes.Equal(ent, []byte("apos")):
				out = append(out, '\'')
			case len(ent) > 1 && ent[0] == '#':
				// xmlchar — единственная реализация правил XML 1.0 §2.2
				// в проекте; вторая копия в pkg/xlsx однажды разошлась с
				// этой и вернула баг, закрытый в 1.19.2.
				r, ok := xmlchar.DecodeRef(ent[1:])
				if !ok {
					return "", false
				}
				out = utf8.AppendRune(out, r)
			default:
				return "", false
			}

		default:
			j := i
			for j < len(s) && s[j] != '&' && s[j] != '\r' {
				j++
			}
			out = append(out, s[i:j]...)
			i = j
		}
	}

	return string(out), true
}

// scanDataRows разбирает тело Data-секции в []Row.
// Ожидает последовательность <R>chardata</R>, допускает пробелы между ними.
func scanDataRows(body []byte) ([]Row, bool) {
	// Точная оценка: '<' внутри значения всегда экранирован, поэтому
	// количество "<R>" равно количеству строк.
	// Слайс остаётся nil при нуле строк — xml.Unmarshal в этом случае тоже
	// оставляет Rows нулевым, а reflect.DeepEqual различает nil и []Row{}.
	var rows []Row
	if n := bytes.Count(body, bTagROpen); n > 0 {
		rows = make([]Row, 0, n)
	}

	i := 0
	for i < len(body) {
		for i < len(body) && isXMLSpace(body[i]) {
			i++
		}
		if i >= len(body) {
			break
		}
		if !bytes.HasPrefix(body[i:], bTagROpen) {
			return nil, false // <R/>, комментарий, CDATA, чужой элемент
		}
		i += len(bTagROpen)

		j := bytes.Index(body[i:], bTagRClose)
		if j < 0 {
			return nil, false
		}
		cell := body[i : i+j]

		// В корректном пакете '<' внутри значения всегда экранирован (&lt;).
		// Сырой '<' означает CDATA, комментарий или вложенный элемент —
		// их семантику воспроизводит только полноценный декодер.
		if bytes.IndexByte(cell, '<') >= 0 {
			return nil, false
		}

		value, ok := decodeChardata(cell)
		if !ok {
			return nil, false
		}
		rows = append(rows, Row{Value: value})
		i += j + len(bTagRClose)
	}

	return rows, true
}

// tryFastParse — гибридный разбор пакета. ok=false означает «форма не та,
// разбирай обычным путём»; ошибка при этом не возвращается.
func tryFastParse(data []byte) (*DataPacket, bool) {
	bodyStart, bodyEnd, afterClose, ok := findDataSection(data)
	if !ok {
		return nil, false
	}
	if hasAnotherDataTag(data[afterClose:]) {
		return nil, false
	}

	rows, ok := scanDataRows(data[bodyStart:bodyEnd])
	if !ok {
		return nil, false
	}

	// Скелет: пакет без строк. Размер не зависит от объёма данных.
	skeleton := make([]byte, 0, bodyStart+len(bDataCloseTag)+len(data)-afterClose)
	skeleton = append(skeleton, data[:bodyStart]...)
	skeleton = append(skeleton, bDataCloseTag...)
	skeleton = append(skeleton, data[afterClose:]...)

	var packet DataPacket
	if err := xml.Unmarshal(skeleton, &packet); err != nil {
		return nil, false // структура неожиданная — пусть fallback вернёт ошибку
	}

	packet.Data.Rows = rows
	return &packet, true
}
