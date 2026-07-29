// Package xlsx provides functionality for the TDTP framework.
package xlsx

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/core/schema"
	"github.com/ruslano69/tdtp-framework/pkg/processors"
)

// excel1900Epoch is the Excel date epoch (Jan 1, 1900 = serial 1).
var excel1900Epoch = time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)

// pre1900Cutoff: Excel cannot represent dates before Jan 1, 1900 as serials.
var pre1900Cutoff = time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)

// maxExcelInt is 10^15 - 1: the largest integer Excel can represent exactly
// as IEEE-754 float64 (15 significant digits). Larger values must be strings.
const maxExcelInt int64 = 999_999_999_999_999

// ToXLSX - convert TDTP packet to XLSX file
//
// Creates an Excel file from TDTP data packet with formatted headers and data.
// Headers show field names with types (e.g., "customer_name (TEXT)").
// Primary keys are marked with *.
//
// Handles the following Excel traps automatically:
//   - BIGINT precision: int64 values with >15 significant digits → string cell
//   - NaN / ±Inf: written as blank cells (canonical NULL in Excel)
//   - Pre-1900 dates: written as ISO text strings (Excel serial cannot represent them)
//   - Formula injection: string cells use SetCellStr so leading =, +, -, @ are safe
//   - TDTP [NULL] marker in text fields → blank cell
//
// Example:
//
//	err := xlsx.ToXLSX(packet, "output.xlsx", "Orders")
func ToXLSX(pkt *packet.DataPacket, filePath, sheetName string) error {
	// Check if data is compressed and decompress if needed
	if pkt.Data.Compression != "" {
		if len(pkt.Data.Rows) != 1 {
			return fmt.Errorf("compressed data should have exactly 1 row, got %d", len(pkt.Data.Rows))
		}

		// Decompress data
		decompressedRows, err := processors.DecompressDataForTdtp(pkt.Data.Rows[0].Value)
		if err != nil {
			return fmt.Errorf("failed to decompress data: %w", err)
		}

		// Replace compressed row with decompressed rows
		pkt.Data.Rows = make([]packet.Row, len(decompressedRows))
		for i, row := range decompressedRows {
			pkt.Data.Rows[i] = packet.Row{Value: row}
		}
		pkt.Data.Compression = "" // Mark as decompressed
	}

	// Set default sheet name
	if sheetName == "" {
		sheetName = pkt.Header.TableName
		if sheetName == "" {
			sheetName = "Sheet1"
		}
	}

	sheet := newSheet(sheetName)

	// Write headers. The header row is the schema: "name (TYPE)", "*" for a
	// key. FromXLSX reads the types back out of it.
	for col, field := range pkt.Schema.Fields {
		header := fmt.Sprintf("%s (%s)", field.Name, field.Type)
		if field.Key {
			header += " *"
		}
		sheet.setString(col+1, 1, header, styleHeader)
	}

	// Pre-build schema.FieldDef slice for the core converter (reuse across rows)
	pktParser := packet.NewParser()
	conv := schema.NewConverter()
	fieldDefs := make([]schema.FieldDef, len(pkt.Schema.Fields))
	for i, fld := range pkt.Schema.Fields {
		fieldDefs[i] = schema.FieldDef{
			Name:      fld.Name,
			Type:      schema.DataType(fld.Type),
			Length:    fld.Length,
			Precision: fld.Precision,
			Scale:     fld.Scale,
			Timezone:  fld.Timezone,
			Key:       fld.Key,
			Nullable:  true,
		}
	}

	// Parse and write data rows using core framework primitives
	for rowIdx, row := range pkt.Data.Rows {
		// GetRowValues handles escape sequences (\| inside field values)
		values := pktParser.GetRowValues(row)
		for col, fld := range pkt.Schema.Fields {
			if col >= len(values) {
				continue
			}
			tv, err := conv.ParseValue(values[col], fieldDefs[col])
			if err != nil || tv.IsNull {
				// Leave cell blank — do not call SetCellValue
				continue
			}

			fieldType := schema.DataType(fld.Type)
			cellVal, forceStr := typedValueToExcel(tv, fieldType)
			if cellVal == nil {
				// NaN / Inf / [NULL] marker → blank cell
				continue
			}

			writeCell(sheet, col+1, rowIdx+2, cellVal, forceStr, fieldType)
		}
	}

	// Auto-fit columns
	for col := range pkt.Schema.Fields {
		sheet.setColWidth(col+1, 15)
	}

	return writeXLSX(filePath, sheet)
}

// writeCell places one already-converted value into the sheet.
//
// forceStr is the formula-injection guard: a text cell is written as a typed
// string, so Excel never re-reads a leading =, +, - or @ as a formula. It is
// also how the values that must not become numbers — big integers past 15
// significant digits, pre-1900 dates — keep every digit they had.
func writeCell(sheet *sheetBuilder, col, row int, v any, forceStr bool, fieldType schema.DataType) {
	if forceStr {
		sheet.setString(col, row, fmt.Sprint(v), styleText)
		return
	}
	switch n := v.(type) {
	case int64:
		sheet.setNumber(col, row, strconv.FormatInt(n, 10), styleInteger)
	case float64:
		sheet.setNumber(col, row, formatFloat(n), styleFloat)
	case time.Time:
		style := styleDatetime
		if fieldType == schema.TypeDate {
			style = styleDate
		}
		sheet.setNumber(col, row, formatFloat(excelSerial(n)), style)
	default:
		// Nothing else reaches here: typedValueToExcel returns only the three
		// numeric kinds above with forceStr=false. Storing it as text keeps a
		// future addition visible instead of silently dropped.
		sheet.setString(col, row, fmt.Sprint(v), styleText)
	}
}

// FromXLSX - convert XLSX file to TDTP packet
//
// Reads an Excel file and converts it to TDTP data packet.
// Expects headers in format "field_name (TYPE)" or "field_name (TYPE) *" for keys.
//
// Handles the following import traps automatically:
//   - Error cells (#N/A, #DIV/0!, #NUM!, #VALUE!, etc.) → canonical NULL ("")
//   - Date cells: reads raw Excel serial number and reconstructs via 1900-epoch math
//     (accounts for the Excel 1900 leap-year bug: phantom serial 60 = Feb 29, 1900)
//   - All string values are trimmed (leading/trailing whitespace removed)
//   - Empty cells → NULL ("") for non-TEXT types, empty string for TEXT
//
// Example:
//
//	packet, err := xlsx.FromXLSX("input.xlsx", "Orders")
func FromXLSX(filePath, sheetName string) (*packet.DataPacket, error) {
	// Raw values throughout: a date cell yields its Excel serial ("44927.5"),
	// a numeric cell its decimal text, an error cell "#N/A". Number formats
	// are never applied, so convertFromExcel below stays the only place that
	// interprets any of it.
	rows, err := readSheet(filePath, sheetName)
	if err != nil {
		return nil, err
	}
	if sheetName == "" {
		sheetName = "Sheet1"
	}
	if len(rows) < 1 {
		return nil, fmt.Errorf("file has no rows (not even a header)")
	}

	// Parse header to create schema
	headerRow := rows[0]
	fields := make([]packet.Field, 0, len(headerRow))
	for _, header := range headerRow {
		name, fieldType, isKey := parseHeader(header)
		fields = append(fields, packet.Field{
			Name: name,
			Type: string(fieldType),
			Key:  isKey,
		})
	}

	// Create packet
	pkt := packet.NewDataPacket(packet.TypeReference, sheetName)
	pkt.Header.RecordsInPart = len(rows) - 1
	pkt.Header.PartNumber = 1
	pkt.Header.TotalParts = 1
	pkt.Schema = packet.Schema{Fields: fields}
	pkt.Data = packet.Data{Rows: make([]packet.Row, 0, len(rows)-1)}

	// Parse data rows
	for rowIdx := 1; rowIdx < len(rows); rowIdx++ {
		dataRow := rows[rowIdx]
		values := make([]string, len(fields))

		for col, field := range fields {
			if col >= len(dataRow) {
				values[col] = ""
				continue
			}
			// Trim all strings (import trap: whitespace from Excel formatting)
			raw := strings.TrimSpace(dataRow[col])

			// Error cells (#N/A, #DIV/0!, etc.) → canonical NULL
			if isExcelError(raw) {
				values[col] = ""
				continue
			}

			values[col] = convertFromExcel(raw, schema.DataType(field.Type))
		}

		// Join values with pipe delimiter
		rowStr := strings.Join(values, "|")
		pkt.Data.Rows = append(pkt.Data.Rows, packet.Row{Value: rowStr})
	}

	return pkt, nil
}

// parseHeader - parse header string "field_name (TYPE)" or "field_name (TYPE) *"
func parseHeader(header string) (name string, fieldType schema.DataType, isKey bool) {
	name = header
	fieldType = schema.TypeText
	isKey = false

	// Check for key marker
	if strings.HasSuffix(header, " *") {
		isKey = true
		header = strings.TrimSuffix(header, " *")
	}

	// Find type in parentheses
	if idx := strings.LastIndex(header, "("); idx > 0 {
		if endIdx := strings.LastIndex(header, ")"); endIdx > idx {
			name = strings.TrimSpace(header[:idx])
			typeStr := strings.TrimSpace(header[idx+1 : endIdx])
			fieldType = schema.DataType(typeStr)
		}
	}

	return name, fieldType, isKey
}

// typedValueToExcel converts a TypedValue to an Excel-safe Go value.
//
// Returns (value, forceStr):
//   - value is the Go value to write; nil means "leave cell blank"
//   - forceStr=true means caller must use SetCellStr (text cell, no numeric style)
//
// Traps handled:
//  1. BIGINT precision: int64 > 15 significant digits → string (preserves all digits)
//  2. NaN / ±Inf: → nil (blank cell; Excel has no native representation)
//  3. Pre-1900 date: → ISO string (Excel serial cannot represent dates before 1900-01-01)
//  4. [NULL] text marker: → nil (blank cell)
//  5. String cells always use forceStr so formula injection (=, +, -, @) is prevented
func typedValueToExcel(tv *schema.TypedValue, fieldType schema.DataType) (any, bool) {
	switch {
	case tv.IntValue != nil:
		val := *tv.IntValue
		// Trap 1: BIGINT precision loss.
		// Excel stores all numbers as IEEE-754 float64 (max 15 significant digits).
		// Values outside ±10^15 must be written as strings to preserve all digits.
		if val > maxExcelInt || val < -maxExcelInt {
			return strconv.FormatInt(val, 10), true
		}
		return val, false

	case tv.FloatValue != nil:
		val := *tv.FloatValue
		// Trap 2: Excel has no NaN or ±Infinity numeric type.
		// Write as blank cell (canonical Excel NULL) rather than a misleading string.
		if math.IsNaN(val) || math.IsInf(val, 0) {
			return nil, false
		}
		return val, false

	case tv.BoolValue != nil:
		if *tv.BoolValue {
			return "TRUE", true
		}
		return "FALSE", true

	case tv.TimeValue != nil:
		t := *tv.TimeValue
		// Trap 3: Excel serial date numbers start at Jan 1, 1900.
		// Dates before 1900-01-01 cannot be represented as serials → write as ISO text.
		if t.Before(pre1900Cutoff) {
			if fieldType == schema.TypeDate {
				return t.Format("2006-01-02"), true
			}
			return t.Format("2006-01-02T15:04:05"), true
		}
		// For dates >= 1900-01-01 the writer converts to an Excel serial,
		// including the 1900 leap-year bug (phantom serial 60 = Feb 29, 1900).
		return t, false

	case tv.StringValue != nil:
		s := *tv.StringValue
		// Trap 4: [NULL] SpecialValues marker → blank cell.
		if s == packet.SpecNullMarker {
			return nil, false
		}
		// Trap 5: all string cells use forceStr (→ SetCellStr) so that values
		// starting with =, +, -, @ are stored as text, not interpreted as formulas.
		return s, true

	default:
		if tv.RawValue == "" {
			return nil, false
		}
		return tv.RawValue, true
	}
}

// convertFromExcel converts a raw Excel cell value to TDTP string format.
// The raw value comes from GetRows with RawCellValue:true.
func convertFromExcel(value string, fieldType schema.DataType) string {
	if value == "" {
		return ""
	}

	switch fieldType {
	case schema.TypeBoolean, schema.TypeBool:
		switch strings.ToUpper(value) {
		case "TRUE", "1":
			return "1"
		}
		return "0"

	case schema.TypeDate:
		// Raw value for a date cell is the Excel serial number (e.g. "44927").
		// Reconstruct the actual date via 1900-epoch math with leap-year bug fix.
		if serial, err := strconv.ParseFloat(value, 64); err == nil && serial > 0 {
			t := excelSerialToTime(serial)
			return t.Format("2006-01-02")
		}
		// Fall through: already a date string (e.g. from a text cell)
		return value

	case schema.TypeDatetime, schema.TypeTimestamp:
		// Same serial logic; fractional part encodes the time-of-day.
		if serial, err := strconv.ParseFloat(value, 64); err == nil && serial > 0 {
			t := excelSerialToTime(serial)
			return t.UTC().Format(time.RFC3339)
		}
		return value
	}

	return value
}

// isExcelError returns true for well-known Excel error cell values.
// These must map to canonical NULL on import.
func isExcelError(s string) bool {
	switch s {
	case "#N/A", "#DIV/0!", "#NUM!", "#VALUE!", "#REF!", "#NAME?", "#NULL!", "#GETTING_DATA":
		return true
	}
	return false
}

// excelSerialToTime converts an Excel date serial number to time.Time.
//
// Excel date arithmetic:
//   - Serial 1  = Jan 1,  1900
//   - Serial 59 = Feb 28, 1900
//   - Serial 60 = Feb 29, 1900  ← phantom (Excel 1900 leap-year bug from Lotus 1-2-3)
//   - Serial 61 = Mar 1,  1900
//   - Fractional part = fraction of the day (0.5 = 12:00 noon)
//
// This function accounts for the phantom leap day so that all dates ≥ Mar 1, 1900
// are reconstructed correctly.
func excelSerialToTime(serial float64) time.Time {
	days := int(serial)
	frac := serial - float64(days)

	var base time.Time
	switch {
	case days >= 61:
		// After the phantom Feb 29: subtract 2 (1 for 0-based epoch + 1 for phantom day).
		base = excel1900Epoch.AddDate(0, 0, days-2)
	case days == 60:
		// Phantom date — map to Feb 28, 1900 (the real last day of Feb 1900).
		base = time.Date(1900, 2, 28, 0, 0, 0, 0, time.UTC)
	default:
		// Serial 1–59: subtract 1 for 0-based epoch offset.
		base = excel1900Epoch.AddDate(0, 0, days-1)
	}

	// Reconstruct time-of-day from the fractional part of the serial.
	// Use millisecond resolution to avoid floating-point rounding drift.
	totalMs := int(math.Round(frac * 86400 * 1000))
	h := totalMs / 3_600_000
	m := (totalMs % 3_600_000) / 60_000
	s := (totalMs % 60_000) / 1_000

	return time.Date(base.Year(), base.Month(), base.Day(), h, m, s, 0, time.UTC)
}

// columnName converts a 1-based column index to an Excel column letter (1→A, 27→AA).
func columnName(col int) string {
	name := ""
	for col > 0 {
		col--
		name = string(rune('A'+col%26)) + name
		col /= 26
	}
	return name
}
