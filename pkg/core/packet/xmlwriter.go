package packet

import (
	"bufio"
	"bytes"
	"encoding/xml"
	"io"
	"os"
	"strings"
)

// Фиксированные байтовые константы для горячего пути записи строк.
// Хардкодим как []byte чтобы избежать повторной конвертации string→[]byte в цикле.
var (
	bTagROpen   = []byte("<R>")
	bTagRClose  = []byte("</R>")
	bEscLt      = []byte("&lt;")
	bEscGt      = []byte("&gt;")
	bEscAmp     = []byte("&amp;")
	bEscQuot    = []byte("&quot;")
)

// writePacketTo сериализует DataPacket в XML без encoding/xml reflection для Data-секции.
//
// Header и Schema маленькие (~200 bytes) — сериализуются через xml.Marshal.
// Data (тысячи строк) — пишется вручную: нет reflection, нет промежуточных аллокаций.
// Это устраняет главный bottleneck (xml.MarshalIndent 229ms на 100k строк).
func writePacketTo(w *bufio.Writer, packet *DataPacket) error {
	// XML declaration
	w.WriteString(xml.Header)

	// Корневой тег с атрибутами
	w.WriteString(`<DataPacket`)
	writeXMLAttr(w, "protocol", packet.Protocol)
	writeXMLAttr(w, "version", packet.Version)
	w.WriteByte('>')

	// Header — маленький, xml.Marshal дешёв
	if err := marshalInto(w, packet.Header, "Header"); err != nil {
		return err
	}

	// Query (omitempty)
	if packet.Query != nil {
		if err := marshalInto(w, packet.Query, "Query"); err != nil {
			return err
		}
	}

	// QueryContext (omitempty)
	if packet.QueryContext != nil {
		if err := marshalInto(w, packet.QueryContext, "QueryContext"); err != nil {
			return err
		}
	}

	// Schema — маленькая, xml.Marshal дешёв
	if err := marshalInto(w, packet.Schema, "Schema"); err != nil {
		return err
	}

	// Data — ручной writer, без reflection ─────────────────────────────────
	w.WriteString(`<Data`)
	if packet.Data.Compression != "" {
		writeXMLAttr(w, "compression", packet.Data.Compression)
	}
	if packet.Data.Checksum != "" {
		writeXMLAttr(w, "checksum", packet.Data.Checksum)
	}
	if packet.Data.Compact {
		w.WriteString(` compact="true"`)
	}
	if packet.Data.Tail {
		w.WriteString(` tail="true"`)
	}
	if packet.Data.Carry != "" {
		writeXMLAttr(w, "carry", packet.Data.Carry)
	}
	w.WriteByte('>')

	if len(packet.rawRows) > 0 {
		// Fast path: rawRows установлены GenerateReference.
		// Пишем значения напрямую — ни RowsToData, ни strings.Join не нужны.
		// TDTP-экранирование (|→\|, \→\\) + XML-экранирование (<>&) — один проход.
		for _, row := range packet.rawRows {
			w.Write(bTagROpen)
			for j, val := range row {
				if j > 0 {
					w.WriteByte('|')
				}
				writeRawValue(w, val)
			}
			w.Write(bTagRClose)
		}
	} else {
		// Broker-путь или компрессия: Data.Rows уже содержат pipe-joined строки.
		for i := range packet.Data.Rows {
			w.Write(bTagROpen)
			writeXMLChardata(w, packet.Data.Rows[i].Value)
			w.Write(bTagRClose)
		}
	}

	w.WriteString(`</Data>`)
	// ──────────────────────────────────────────────────────────────────────

	// AlarmDetails (omitempty)
	if packet.AlarmDetails != nil {
		if err := marshalInto(w, packet.AlarmDetails, "AlarmDetails"); err != nil {
			return err
		}
	}

	w.WriteString(`</DataPacket>`)
	return w.Flush()
}

// marshalInto сериализует v через xml.Marshal и пишет результат в w.
// Используется для маленьких секций (Header, Schema) где reflection приемлем.
func marshalInto(w *bufio.Writer, v any, _ string) error {
	b, err := xml.Marshal(v)
	if err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}

// writeXMLAttr пишет атрибут name="value" с экранированием XML спецсимволов.
func writeXMLAttr(w *bufio.Writer, name, value string) {
	w.WriteByte(' ')
	w.WriteString(name)
	w.WriteString(`="`)
	writeXMLAttrValue(w, value)
	w.WriteByte('"')
}

// writeXMLAttrValue пишет строку с экранированием для XML атрибута (<>&"').
func writeXMLAttrValue(w *bufio.Writer, s string) {
	for {
		i := strings.IndexAny(s, `<>&"`)
		if i < 0 {
			w.WriteString(s)
			return
		}
		if i > 0 {
			w.WriteString(s[:i])
		}
		switch s[i] {
		case '<':
			w.Write(bEscLt)
		case '>':
			w.Write(bEscGt)
		case '&':
			w.Write(bEscAmp)
		case '"':
			w.Write(bEscQuot)
		}
		s = s[i+1:]
	}
}

// writeRawValue пишет одно значение ячейки с комбинированным экранированием за один проход:
//   - TDTP pipe-разделитель: | → \|,  \ → \\
//   - XML chardata:          < → &lt;, > → &gt;, & → &amp;
//
// Заменяет связку escapeValue + strings.Join + writeXMLChardata — ноль аллокаций.
func writeRawValue(w *bufio.Writer, s string) {
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '|':
			w.WriteString(s[start:i])
			w.WriteString(`\|`)
			start = i + 1
		case '\\':
			w.WriteString(s[start:i])
			w.WriteString(`\\`)
			start = i + 1
		case '<':
			w.WriteString(s[start:i])
			w.Write(bEscLt)
			start = i + 1
		case '>':
			w.WriteString(s[start:i])
			w.Write(bEscGt)
			start = i + 1
		case '&':
			w.WriteString(s[start:i])
			w.Write(bEscAmp)
			start = i + 1
		}
	}
	w.WriteString(s[start:])
}

// writeXMLChardata пишет строку с экранированием для XML chardata (<>&).
// strings.IndexAny использует SIMD-оптимизированный поиск — быстрее побайтового цикла.
// Для типичных строк без спецсимволов — один Write всей строки.
func writeXMLChardata(w *bufio.Writer, s string) {
	for {
		i := strings.IndexAny(s, "<>&")
		if i < 0 {
			w.WriteString(s)
			return
		}
		if i > 0 {
			w.WriteString(s[:i])
		}
		switch s[i] {
		case '<':
			w.Write(bEscLt)
		case '>':
			w.Write(bEscGt)
		case '&':
			w.Write(bEscAmp)
		}
		s = s[i+1:]
	}
}

// newPacketWriter создаёт bufio.Writer поверх w с буфером 4MB.
func newPacketWriter(w io.Writer) *bufio.Writer {
	return bufio.NewWriterSize(w, 4*1024*1024)
}

// packetToBytes сериализует пакет в []byte через bytes.Buffer (для ToXML).
func packetToBytes(packet *DataPacket) ([]byte, error) {
	// Предварительная оценка размера: ~200 bytes overhead + ~300 bytes per row
	estimated := 512 + len(packet.Data.Rows)*300
	var buf bytes.Buffer
	buf.Grow(estimated)
	bw := bufio.NewWriterSize(&buf, 4*1024*1024)
	if err := writePacketTo(bw, packet); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// WriteToFileFast записывает пакет прямо в файл без промежуточного []byte.
// Используется вместо WriteToFile для экспорта в файлы.
func (g *Generator) WriteToFileFast(packet *DataPacket, filename string) error {
	f, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	return writePacketTo(newPacketWriter(f), packet)
}
