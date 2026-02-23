package main

/*
#include <stdlib.h>
#include <string.h>
#include "tdtp_structs.h"
*/
import "C"
import (
	"unsafe"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/core/tdtql"
)

// ---------------------------------------------------------------------------
// Internal string helpers
// ---------------------------------------------------------------------------

// dWriteStr copies src into a *C.char buffer of capacity n (n includes NUL).
func dWriteStr(dst *C.char, src string, n C.size_t) {
	cs := C.CString(src)
	C.strncpy(dst, cs, n-1)
	C.free(unsafe.Pointer(cs))
}

// dReadStr reads a NUL-terminated string from a *C.char.
func dReadStr(src *C.char) string {
	return C.GoString(src)
}

// dSetError writes an error message into pkt.error.
func dSetError(pkt *C.D_Packet, msg string) {
	dWriteStr((*C.char)(unsafe.Pointer(&pkt.error[0])), msg, 1024)
}

// ---------------------------------------------------------------------------
// Memory management
// ---------------------------------------------------------------------------

// D_FreePacket releases all C.malloc memory owned by a D_Packet.
// Must be called after every successful D_* call that returns / fills a D_Packet.
//
//export D_FreePacket
func D_FreePacket(pkt *C.D_Packet) {
	if pkt == nil {
		return
	}
	// Free each row's values array and its strings.
	if pkt.rows != nil {
		rowSlice := unsafe.Slice(pkt.rows, int(pkt.row_count))
		for _, row := range rowSlice {
			if row.values != nil {
				valueSlice := unsafe.Slice(row.values, int(row.value_count))
				for _, v := range valueSlice {
					C.free(unsafe.Pointer(v))
				}
				C.free(unsafe.Pointer(row.values))
			}
		}
		C.free(unsafe.Pointer(pkt.rows))
		pkt.rows = nil
	}
	// Free schema fields array.
	if pkt.schema.fields != nil {
		C.free(unsafe.Pointer(pkt.schema.fields))
		pkt.schema.fields = nil
	}
}

// D_FreeMaskConfig is a no-op: D_MaskConfig.fields is owned by Python ctypes.
// Exists for API symmetry; Python must keep cfg alive until D_ApplyMask returns.
//
//export D_FreeMaskConfig
func D_FreeMaskConfig(_ *C.D_MaskConfig) {}

// ---------------------------------------------------------------------------
// Internal fill helpers
// ---------------------------------------------------------------------------

// dFillSchema populates pkt.schema from a packet.Schema using C.malloc.
func dFillSchema(pkt *C.D_Packet, schema packet.Schema) {
	n := len(schema.Fields)
	if n == 0 {
		pkt.schema.fields = nil
		pkt.schema.field_count = 0
		return
	}
	// Use calloc to zero-initialize: guarantees bool fields default to 0.
	fieldsPtr := (*C.D_Field)(C.calloc(C.size_t(n), C.size_t(unsafe.Sizeof(C.D_Field{}))))
	fields := unsafe.Slice(fieldsPtr, n)
	for i, f := range schema.Fields {
		dWriteStr((*C.char)(unsafe.Pointer(&fields[i].name[0])), f.Name, 256)
		dWriteStr((*C.char)(unsafe.Pointer(&fields[i].type_name[0])), f.Type, 64)
		fields[i].length = C.int(f.Length)
		fields[i].precision = C.int(f.Precision)
		fields[i].scale = C.int(f.Scale)
		if f.Key {
			fields[i].is_key = 1
		}
		if f.ReadOnly {
			fields[i].is_readonly = 1
		}
	}
	pkt.schema.fields = fieldsPtr
	pkt.schema.field_count = C.int(n)
}

// dFillRows populates pkt.rows from [][]string using C.malloc.
func dFillRows(pkt *C.D_Packet, rows [][]string) {
	n := len(rows)
	if n == 0 {
		pkt.rows = nil
		pkt.row_count = 0
		return
	}
	rowsPtr := (*C.D_Row)(C.malloc(C.size_t(n) * C.size_t(unsafe.Sizeof(C.D_Row{}))))
	rowSlice := unsafe.Slice(rowsPtr, n)
	for i, row := range rows {
		m := len(row)
		valuesPtr := (**C.char)(C.malloc(C.size_t(m) * C.size_t(unsafe.Sizeof((*C.char)(nil)))))
		valueSlice := unsafe.Slice(valuesPtr, m)
		for j, v := range row {
			valueSlice[j] = C.CString(v)
		}
		rowSlice[i].values = valuesPtr
		rowSlice[i].value_count = C.int(m)
	}
	pkt.rows = rowsPtr
	pkt.row_count = C.int(n)
}

// dGetRows extracts [][]string from a D_Packet (Go-side read).
func dGetRows(pkt *C.D_Packet) [][]string {
	n := int(pkt.row_count)
	if n == 0 || pkt.rows == nil {
		return nil
	}
	rowSlice := unsafe.Slice(pkt.rows, n)
	rows := make([][]string, n)
	for i, row := range rowSlice {
		m := int(row.value_count)
		vals := make([]string, m)
		if m > 0 && row.values != nil {
			valueSlice := unsafe.Slice(row.values, m)
			for j, v := range valueSlice {
				vals[j] = C.GoString(v)
			}
		}
		rows[i] = vals
	}
	return rows
}

// dGetSchema extracts packet.Schema from a D_Packet.
func dGetSchema(pkt *C.D_Packet) packet.Schema {
	n := int(pkt.schema.field_count)
	if n == 0 || pkt.schema.fields == nil {
		return packet.Schema{}
	}
	fieldSlice := unsafe.Slice(pkt.schema.fields, n)
	fields := make([]packet.Field, n)
	for i, f := range fieldSlice {
		fields[i] = packet.Field{
			Name:      dReadStr((*C.char)(unsafe.Pointer(&f.name[0]))),
			Type:      dReadStr((*C.char)(unsafe.Pointer(&f.type_name[0]))),
			Length:    int(f.length),
			Precision: int(f.precision),
			Scale:     int(f.scale),
			Key:       f.is_key != 0,
			ReadOnly:  f.is_readonly != 0,
		}
	}
	return packet.Schema{Fields: fields}
}

// dFillHeader copies header metadata into an output D_Packet.
func dFillHeader(out *C.D_Packet, pkt *packet.DataPacket) {
	dWriteStr((*C.char)(unsafe.Pointer(&out.msg_type[0])), string(pkt.Header.Type), 32)
	dWriteStr((*C.char)(unsafe.Pointer(&out.table_name[0])), pkt.Header.TableName, 256)
	dWriteStr((*C.char)(unsafe.Pointer(&out.message_id[0])), pkt.Header.MessageID, 64)
	out.timestamp_unix = C.longlong(pkt.Header.Timestamp.Unix())
}

// ---------------------------------------------------------------------------
// I/O
// ---------------------------------------------------------------------------

// D_ReadFile parses a TDTP file and fills out via pointer.
// Returns 0 on success, 1 on error (check out.error for message).
// Caller must release with D_FreePacket(&out) when done.
//
//export D_ReadFile
func D_ReadFile(path *C.char, out *C.D_Packet) C.int {
	pkt, err := packet.NewParser().ParseFile(C.GoString(path))
	if err != nil {
		dSetError(out, "parse error: "+err.Error())
		return 1
	}

	if pkt.Data.Compression != "" {
		return dDecompressRows(pkt, out)
	}

	rows := pkt.GetRows()
	dFillSchema(out, pkt.Schema)
	dFillRows(out, rows)
	dFillHeader(out, pkt)
	return 0
}

// D_WriteFile generates a TDTP file from pkt and writes it to path.
// Returns 0 on success, 1 on error (check out.error).
//
//export D_WriteFile
func D_WriteFile(pkt *C.D_Packet, path *C.char) C.int {
	msgType := dReadStr((*C.char)(unsafe.Pointer(&pkt.msg_type[0])))
	tableName := dReadStr((*C.char)(unsafe.Pointer(&pkt.table_name[0])))

	dp := packet.NewDataPacket(packet.MessageType(msgType), tableName)
	dp.Header.MessageID = dReadStr((*C.char)(unsafe.Pointer(&pkt.message_id[0])))
	dp.Schema = dGetSchema(pkt)
	dp.SetRows(dGetRows(pkt))

	gen := packet.NewGenerator()
	if err := gen.WriteToFile(dp, C.GoString(path)); err != nil {
		dSetError(pkt, "write error: "+err.Error())
		return 1
	}
	return 0
}

// ---------------------------------------------------------------------------
// TDTQL filtering
// ---------------------------------------------------------------------------

// D_FilterRows filters pkt rows by the provided filter specs and writes result to out.
// filters is an array of D_FilterSpec with count elements; they are combined with AND.
// limit — max rows in result (0 = unlimited).
// Returns 0 on success, 1 on error.
// Caller must release out with D_FreePacket.
//
//export D_FilterRows
func D_FilterRows(
	pkt *C.D_Packet,
	filters *C.D_FilterSpec,
	count C.int,
	limit C.int,
	out *C.D_Packet,
) C.int {
	rows := dGetRows(pkt)
	schema := dGetSchema(pkt)

	// Build packet.Filters AND group from the C array.
	var pktFilters *packet.Filters
	n := int(count)
	if n > 0 && filters != nil {
		specs := unsafe.Slice(filters, n)
		filterList := make([]packet.Filter, n)
		for i, s := range specs {
			filterList[i] = packet.Filter{
				Field:    dReadStr((*C.char)(unsafe.Pointer(&s.field[0]))),
				Operator: dReadStr((*C.char)(unsafe.Pointer(&s.op[0]))),
				Value:    dReadStr((*C.char)(unsafe.Pointer(&s.value[0]))),
				Value2:   dReadStr((*C.char)(unsafe.Pointer(&s.value2[0]))),
			}
		}
		pktFilters = &packet.Filters{
			And: &packet.LogicalGroup{Filters: filterList},
		}
	}

	executor := tdtql.NewExecutor()
	filtered, err := executor.ExecuteWhere(pktFilters, rows, schema)
	if err != nil {
		dSetError(out, "filter error: "+err.Error())
		return 1
	}

	if lim := int(limit); lim > 0 && lim < len(filtered) {
		filtered = filtered[:lim]
	}

	dFillSchema(out, schema)
	dFillRows(out, filtered)
	// Copy header metadata from source.
	dWriteStr((*C.char)(unsafe.Pointer(&out.msg_type[0])), dReadStr((*C.char)(unsafe.Pointer(&pkt.msg_type[0]))), 32)
	dWriteStr((*C.char)(unsafe.Pointer(&out.table_name[0])), dReadStr((*C.char)(unsafe.Pointer(&pkt.table_name[0]))), 256)
	dWriteStr((*C.char)(unsafe.Pointer(&out.message_id[0])), dReadStr((*C.char)(unsafe.Pointer(&pkt.message_id[0]))), 64)
	out.timestamp_unix = pkt.timestamp_unix
	return 0
}

// ---------------------------------------------------------------------------
// Processors
// ---------------------------------------------------------------------------

// D_ApplyMask masks the fields listed in cfg.fields inside pkt, writing result to out.
// mask_char is the replacement character; visible_chars trailing chars are left unmasked.
// Returns 0 on success, 1 on error.
// Caller must release out with D_FreePacket.
//
//export D_ApplyMask
func D_ApplyMask(pkt *C.D_Packet, cfg *C.D_MaskConfig, out *C.D_Packet) C.int {
	rows := dGetRows(pkt)
	schema := dGetSchema(pkt)

	// Build field→index map for the schema.
	fieldIdx := make(map[string]int, len(schema.Fields))
	for i, f := range schema.Fields {
		fieldIdx[f.Name] = i
	}

	// Collect target column indices.
	nFields := int(cfg.field_count)
	targetCols := make(map[int]struct{}, nFields)
	if nFields > 0 && cfg.fields != nil {
		nameSlice := unsafe.Slice(cfg.fields, nFields)
		for _, namePtr := range nameSlice {
			name := C.GoString(namePtr)
			if idx, ok := fieldIdx[name]; ok {
				targetCols[idx] = struct{}{}
			}
		}
	}

	maskChar := dReadStr((*C.char)(unsafe.Pointer(&cfg.mask_char[0])))
	if maskChar == "" {
		maskChar = "*"
	}
	visibleChars := int(cfg.visible_chars)
	maskRune := rune(maskChar[0])

	// Apply masking row-by-row.
	masked := make([][]string, len(rows))
	for i, row := range rows {
		newRow := make([]string, len(row))
		copy(newRow, row)
		for col := range targetCols {
			if col < len(newRow) {
				newRow[col] = dMaskValue(newRow[col], maskRune, visibleChars)
			}
		}
		masked[i] = newRow
	}

	dFillSchema(out, schema)
	dFillRows(out, masked)
	dWriteStr((*C.char)(unsafe.Pointer(&out.msg_type[0])), dReadStr((*C.char)(unsafe.Pointer(&pkt.msg_type[0]))), 32)
	dWriteStr((*C.char)(unsafe.Pointer(&out.table_name[0])), dReadStr((*C.char)(unsafe.Pointer(&pkt.table_name[0]))), 256)
	dWriteStr((*C.char)(unsafe.Pointer(&out.message_id[0])), dReadStr((*C.char)(unsafe.Pointer(&pkt.message_id[0]))), 64)
	out.timestamp_unix = pkt.timestamp_unix
	return 0
}

// dMaskValue replaces value characters with maskChar, leaving visibleChars at the end.
func dMaskValue(value string, maskChar rune, visibleChars int) string {
	runes := []rune(value)
	n := len(runes)
	if visibleChars >= n {
		return value
	}
	for i := 0; i < n-visibleChars; i++ {
		runes[i] = maskChar
	}
	return string(runes)
}
