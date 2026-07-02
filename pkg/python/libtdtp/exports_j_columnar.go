package main

/*
#include <stdlib.h>
*/
import "C"
import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// jColumnarInput is the JSON shape accepted by J_WriteColumnar.
// columns is column-major: columns[col_idx][row_idx].
type jColumnarInput struct {
	Schema  packet.Schema `json:"schema"`
	Header  jHeader       `json:"header"`
	Columns [][]string    `json:"columns"` // [n_cols][n_rows]
}

// J_WriteColumnar builds a TDTP file from column-major string data.
//
// columnarJSON must serialize to jColumnarInput:
//
//	{
//	  "schema": {"fields": [{"name":"ID","type":"INTEGER"}, ...]},
//	  "header": {"type":"reference","table_name":"t","message_id":"m","timestamp":"..."},
//	  "columns": [["1","2","3"], ["Alice","Bob","Carol"], ...]
//	}
//
// Compared with J_WriteFile, this avoids the Python-side row transposition:
// the caller sends one array per column (numpy tolist()), Go transposes in one
// allocation.
//
// Returns {"ok":true} or {"error":"..."}.
// Caller must free with J_FreeString.
//
//export J_WriteColumnar
func J_WriteColumnar(columnarJSON *C.char, outPath *C.char) *C.char {
	var ci jColumnarInput
	if err := json.Unmarshal([]byte(C.GoString(columnarJSON)), &ci); err != nil {
		return jErr(fmt.Sprintf("parse error: invalid columnar JSON: %v", err))
	}

	nCols := len(ci.Columns)
	nFields := len(ci.Schema.Fields)
	if nFields == 0 {
		return jErr("invalid input: schema has no fields")
	}
	if nCols != nFields {
		return jErr(fmt.Sprintf("invalid input: %d schema fields but %d columns", nFields, nCols))
	}

	var nRows int
	if nCols > 0 {
		nRows = len(ci.Columns[0])
		for i := 1; i < nCols; i++ {
			if len(ci.Columns[i]) != nRows {
				return jErr(fmt.Sprintf(
					"invalid input: column 0 has %d rows but column %d has %d rows",
					nRows, i, len(ci.Columns[i]),
				))
			}
		}
	}

	// Transpose column-major → row-major in one allocation.
	rows := make([][]string, nRows)
	for r := 0; r < nRows; r++ {
		row := make([]string, nCols)
		for c := 0; c < nCols; c++ {
			row[c] = ci.Columns[c][r]
		}
		rows[r] = row
	}

	// Build DataPacket and write.
	pkt := packet.NewDataPacket(packet.MessageType(ci.Header.Type), ci.Header.TableName)
	pkt.Header.MessageID = ci.Header.MessageID
	pkt.Schema = ci.Schema
	pkt.Data = packet.RowsToData(rows)

	if ts := ci.Header.Timestamp; ts != "" {
		if t, err := time.Parse("2006-01-02T15:04:05Z", ts); err == nil {
			pkt.Header.Timestamp = t
		}
	}

	gen := packet.NewGenerator()
	if err := gen.WriteToFile(pkt, C.GoString(outPath)); err != nil {
		return jErr(fmt.Sprintf("write error: %v", err))
	}
	return jOK(map[string]bool{"ok": true})
}
