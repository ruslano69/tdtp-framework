// Package main provides C-compatible API for Python bindings
package main

import "C"
import (
	"encoding/json"
	"fmt"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/processors"
)

// TDTPResult - результат чтения TDTP файла
type TDTPResult struct {
	Schema packet.Schema `json:"schema"`
	Data   [][]string    `json:"data"`
	Header packet.Header `json:"header"`
	Error  string        `json:"error,omitempty"`
}

//export ReadTDTP
func ReadTDTP(path *C.char) *C.char {
	goPath := C.GoString(path)

	// Используем существующий парсер
	parser := packet.NewParser()
	pkt, err := parser.ParseFile(goPath)
	if err != nil {
		result := TDTPResult{Error: fmt.Sprintf("failed to parse TDTP: %v", err)}
		jsonData, _ := json.Marshal(result)
		return C.CString(string(jsonData))
	}

	// Обработка данных
	var rows [][]string

	// Проверка на компрессию
	if pkt.Data.Compression == "zstd" {
		// Декомпрессия через существующий код
		if len(pkt.Data.Rows) > 0 {
			decompressedRows, err := processors.DecompressDataForTdtp(pkt.Data.Rows[0].Value)
			if err != nil {
				result := TDTPResult{Error: fmt.Sprintf("failed to decompress: %v", err)}
				jsonData, _ := json.Marshal(result)
				return C.CString(string(jsonData))
			}

			// Парсим строки (разделитель |)
			for _, rowStr := range decompressedRows {
				if rowStr == "" {
					continue
				}
				row := parser.GetRowValues(packet.Row{Value: rowStr})
				rows = append(rows, row)
			}
		}
	} else {
		// Без компрессии - используем встроенный метод
		rows = pkt.GetRows()
	}

	// Формируем результат
	result := TDTPResult{
		Schema: pkt.Schema,
		Data:   rows,
		Header: pkt.Header,
	}

	jsonData, _ := json.Marshal(result)
	return C.CString(string(jsonData))
}

//export GetVersion
func GetVersion() *C.char {
	return C.CString("1.6.0")
}

// Требуется для CGO
func main() {}
