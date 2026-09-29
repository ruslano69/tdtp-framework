package etl

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/transform"
)

// loadCSVFile preserves every cell as text. SQL owns business validation and
// casting, so an invalid date or number does not disappear during ingestion.
func loadCSVFile(source SourceConfig) (*packet.DataPacket, error) {
	if err := source.Validate(); err != nil {
		return nil, err
	}
	file, err := os.Open(source.DSN)
	if err != nil {
		return nil, fmt.Errorf("open CSV %q: %w", source.DSN, err)
	}
	defer func() { _ = file.Close() }()

	var input io.Reader = file
	switch strings.ToLower(source.CSV.Encoding) {
	case "windows-1251", "cp1251", "1251":
		input = transform.NewReader(file, charmap.Windows1251.NewDecoder())
	}
	reader := csv.NewReader(input)
	reader.FieldsPerRecord = len(source.CSV.Columns)
	if source.CSV.Delimiter != "" {
		reader.Comma = []rune(source.CSV.Delimiter)[0]
	}

	hasHeader := source.CSV.Header == nil || *source.CSV.Header
	if hasHeader {
		header, err := reader.Read()
		if err != nil {
			return nil, fmt.Errorf("read CSV header: %w", err)
		}
		header[0] = strings.TrimPrefix(header[0], "\ufeff")
		for i, name := range source.CSV.Columns {
			if header[i] != name {
				return nil, fmt.Errorf("CSV header column %d: got %q, expected %q", i+1, header[i], name)
			}
		}
	}

	pkt := packet.NewDataPacket(packet.TypeReference, source.Name)
	pkt.Schema.Fields = make([]packet.Field, len(source.CSV.Columns))
	for i, name := range source.CSV.Columns {
		pkt.Schema.Fields[i] = packet.Field{Name: name, Type: "TEXT"}
	}
	for rowNum := 1; ; rowNum++ {
		values, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("CSV record %d: %w", rowNum, err)
		}
		if rowNum == 1 && !hasHeader {
			values[0] = strings.TrimPrefix(values[0], "\ufeff")
		}
		for col, value := range values {
			if !utf8.ValidString(value) {
				return nil, fmt.Errorf("CSV record %d column %q contains invalid UTF-8", rowNum, source.CSV.Columns[col])
			}
		}
		pkt.Data.Rows = append(pkt.Data.Rows, packet.Row{Value: packet.JoinRowEscaped(values)})
	}
	pkt.Header.RecordsInPart = len(pkt.Data.Rows)
	return pkt, nil
}
