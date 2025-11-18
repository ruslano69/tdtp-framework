package xlsx

import (
	//context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ruslano69/tdtp-framework-main/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework-main/pkg/core/schema"
	"github.com/ruslano69/tdtp-framework-main/pkg/processors"
	"github.com/xuri/excelize/v2"
)

// ToXLSX - convert TDTP packet to XLSX file
//
// Creates an Excel file from TDTP data packet with formatted headers and data.
// Headers show field names with types (e.g., "customer_name (TEXT)").
// Primary keys are marked with *.
//
// Example:
//
//	err := xlsx.ToXLSX(packet, "output.xlsx", "Orders")
func ToXLSX(pkt *packet.DataPacket, filePath string, sheetName string) error {
	// Create new Excel file
	f := excelize.NewFile()
	defer f.Close()

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

	// Create/rename sheet
	index, err := f.NewSheet(sheetName)
	if err != nil {
		return fmt.Errorf("failed to create sheet: %w", err)
	}
	f.SetActiveSheet(index)
	if sheetName != "Sheet1" {
		f.DeleteSheet("Sheet1")
	}

	// Create header style
	headerStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true, Size: 11, Color: "#FFFFFF"},
		Fill: excelize.Fill{Type: "pattern", Color: []string{"#4472C4"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})

	// Write headers
	for col, field := range pkt.Schema.Fields {
		cell := columnName(col+1) + "1"
		header := fmt.Sprintf("%s (%s)", field.Name, field.Type)
		if field.Key {
			header += " *"
		}
		f.SetCellValue(sheetName, cell, header)
		f.SetCellStyle(sheetName, cell, cell, headerStyle)
	}

	// Parse and write data rows
	for rowIdx, row := range pkt.Data.Rows {
		values := parseRow(row.Value)
		for col, field := range pkt.Schema.Fields {
			if col >= len(values) {
				continue
			}
			cell := columnName(col+1) + strconv.Itoa(rowIdx+2)
			value := convertToExcel(values[col], schema.DataType(field.Type))
			f.SetCellValue(sheetName, cell, value)
			applyCellFormat(f, sheetName, cell, schema.DataType(field.Type))
		}
	}

	// Auto-fit columns
	for col := range pkt.Schema.Fields {
		colName := columnName(col + 1)
		f.SetColWidth(sheetName, colName, colName, 15)
	}

	// Save file
	return f.SaveAs(filePath)
}

// FromXLSX - convert XLSX file to TDTP packet
//
// Reads an Excel file and converts it to TDTP data packet.
// Expects headers in format "field_name (TYPE)" or "field_name (TYPE) *" for keys.
//
// Example:
//
//	packet, err := xlsx.FromXLSX("input.xlsx", "Orders")
func FromXLSX(filePath string, sheetName string) (*packet.DataPacket, error) {
	f, err := excelize.OpenFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()

	// Get sheet name
	if sheetName == "" {
		sheetName = f.GetSheetName(0)
	}

	// Read rows
	rows, err := f.GetRows(sheetName)
	if err != nil {
		return nil, fmt.Errorf("failed to read rows: %w", err)
	}
	if len(rows) < 2 {
		return nil, fmt.Errorf("file must have header and at least one data row")
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
	pkt := &packet.DataPacket{
		Protocol: "TDTP",
		Version:  "1.0",
		Header: packet.Header{
			Type:          packet.TypeReference,
			TableName:     sheetName,
			Timestamp:     time.Now().UTC(),
			RecordsInPart: len(rows) - 1,
		},
		Schema: packet.Schema{Fields: fields},
		Data:   packet.Data{Rows: make([]packet.Row, 0, len(rows)-1)},
	}

	// Parse data rows
	for rowIdx := 1; rowIdx < len(rows); rowIdx++ {
		dataRow := rows[rowIdx]
		values := make([]string, len(fields))

		for col, field := range fields {
			if col >= len(dataRow) {
				values[col] = ""
				continue
			}
			values[col] = convertFromExcel(dataRow[col], schema.DataType(field.Type))
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

// parseRow - parse TDTP row value (pipe-delimited)
func parseRow(value string) []string {
	if value == "" {
		return []string{}
	}
	return strings.Split(value, "|")
}

// convertToExcel - convert TDTP value to Excel format
func convertToExcel(value string, fieldType schema.DataType) interface{} {
	if value == "" {
		return ""
	}

	switch fieldType {
	case schema.TypeInteger, schema.TypeInt:
		if i, err := strconv.ParseInt(value, 10, 64); err == nil {
			return i
		}
	case schema.TypeReal, schema.TypeFloat, schema.TypeDouble, schema.TypeDecimal:
		if f, err := strconv.ParseFloat(value, 64); err == nil {
			return f
		}
	case schema.TypeBoolean, schema.TypeBool:
		if value == "1" || value == "true" || value == "TRUE" {
			return "TRUE"
		}
		return "FALSE"
	}

	return value
}

// convertFromExcel - convert Excel value to TDTP format
func convertFromExcel(value string, fieldType schema.DataType) string {
	if value == "" {
		return ""
	}

	switch fieldType {
	case schema.TypeBoolean, schema.TypeBool:
		if value == "TRUE" || value == "true" {
			return "1"
		}
		return "0"
	}

	return value
}

// applyCellFormat - apply Excel format based on type
func applyCellFormat(f *excelize.File, sheet, cell string, fieldType schema.DataType) {
	switch fieldType {
	case schema.TypeInteger, schema.TypeInt:
		f.SetCellStyle(sheet, cell, cell, 1)
	case schema.TypeReal, schema.TypeFloat, schema.TypeDouble, schema.TypeDecimal:
		f.SetCellStyle(sheet, cell, cell, 2)
	case schema.TypeDate:
		f.SetCellStyle(sheet, cell, cell, 14)
	case schema.TypeDatetime, schema.TypeTimestamp:
		f.SetCellStyle(sheet, cell, cell, 22)
	default:
		f.SetCellStyle(sheet, cell, cell, 49)
	}
}

// columnName - convert column index to Excel column name (1 → A, 27 → AA)
func columnName(col int) string {
	name := ""
	for col > 0 {
		col--
		name = string(rune('A'+col%26)) + name
		col /= 26
	}
	return name
}