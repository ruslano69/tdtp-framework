package main

/*
#include <stdlib.h>
*/
import "C"
import (
	"encoding/json"
	"unsafe"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/processors"
)

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// jOK serializes v to JSON and returns a *C.char the caller must J_FreeString.
func jOK(v any) *C.char {
	b, _ := json.Marshal(v)
	return C.CString(string(b))
}

// jErr returns a JSON error envelope: {"error": "..."}.
func jErr(msg string) *C.char {
	b, _ := json.Marshal(map[string]string{"error": msg})
	return C.CString(string(b))
}

// ---------------------------------------------------------------------------
// Memory management
// ---------------------------------------------------------------------------

// J_FreeString releases a *C.char returned by any J_* function.
// Must be called after every successful J_* call to prevent memory leaks.
//
//export J_FreeString
func J_FreeString(s *C.char) {
	C.free(unsafe.Pointer(s))
}

// ---------------------------------------------------------------------------
// Version
// ---------------------------------------------------------------------------

// J_GetVersion returns the library version as a plain string.
// Caller must free with J_FreeString.
//
//export J_GetVersion
func J_GetVersion() *C.char {
	return C.CString("1.6.0")
}

// ---------------------------------------------------------------------------
// I/O
// ---------------------------------------------------------------------------

// J_ReadFile parses a TDTP file and returns its contents as JSON:
//
//	{"schema":{"fields":[...]},"header":{...},"data":[["v1","v2"],...]}
//
// On error returns {"error":"..."}.
// Caller must free result with J_FreeString.
//
//export J_ReadFile
func J_ReadFile(path *C.char) *C.char {
	// TODO: call packet.NewParser().ParseFile(C.GoString(path))
	// TODO: decompress if pkt.Data.Compression == "zstd"
	// TODO: build jReadResult{Schema, Header, Data} and return jOK(result)
	_ = packet.NewParser()
	return jErr("TODO: not implemented")
}

// J_WriteFile generates a TDTP file from JSON data and writes it to path.
// dataJSON must match the shape returned by J_ReadFile.
// Returns {"ok":true} or {"error":"..."}.
// Caller must free result with J_FreeString.
//
//export J_WriteFile
func J_WriteFile(dataJSON *C.char, path *C.char) *C.char {
	// TODO: json.Unmarshal dataJSON → jReadResult
	// TODO: build DataPacket from result
	// TODO: call packet.NewGenerator().WriteFile(pkt, C.GoString(path))
	_, _ = dataJSON, path
	return jErr("TODO: not implemented")
}

// ---------------------------------------------------------------------------
// TDTQL filtering
// ---------------------------------------------------------------------------

// J_FilterRows applies a TDTQL WHERE clause to data and returns filtered rows.
//
// dataJSON — output of J_ReadFile.
// whereClause — TDTQL expression, e.g. "Balance > 1000 AND City = 'Omsk'".
// limit — max rows to return (0 = unlimited).
//
// Returns same shape as J_ReadFile (schema preserved, only data filtered).
// Caller must free result with J_FreeString.
//
//export J_FilterRows
func J_FilterRows(dataJSON *C.char, whereClause *C.char, limit C.int) *C.char {
	// TODO: json.Unmarshal dataJSON → jReadResult
	// TODO: tdtql.NewParser().Parse(C.GoString(whereClause)) → AST
	// TODO: tdtql.NewFilterEngine().ApplyFilters(filters, rows, schema, converter)
	// TODO: apply limit if > 0
	// TODO: return jOK(filtered result)
	_, _, _ = dataJSON, whereClause, limit
	return jErr("TODO: not implemented")
}

// ---------------------------------------------------------------------------
// Processors
// ---------------------------------------------------------------------------

// J_ApplyProcessor runs a single named processor over data.
//
// dataJSON   — output of J_ReadFile.
// procType   — one of: "field_masker", "field_normalizer", "field_validator",
//
//	"checksum", "compress", "decompress".
//
// configJSON — processor-specific config, e.g.:
//
//	field_masker:     {"fields":["email","phone"],"mask_char":"*","visible_chars":4}
//	field_normalizer: {"rules":[{"field":"name","trim":true,"upper":false}]}
//	field_validator:  {"rules":[{"field":"age","min":0,"max":150}]}
//	compress:         {"algorithm":"zstd","level":3}
//
// Returns same shape as J_ReadFile.
// Caller must free result with J_FreeString.
//
//export J_ApplyProcessor
func J_ApplyProcessor(dataJSON *C.char, procType *C.char, configJSON *C.char) *C.char {
	// TODO: json.Unmarshal dataJSON → jReadResult
	// TODO: processors.NewFactory().Create(C.GoString(procType), config)
	// TODO: proc.Process(ctx, rows, schema)
	// TODO: return jOK(result)
	_ = processors.NewFactory()
	_, _, _ = dataJSON, procType, configJSON
	return jErr("TODO: not implemented")
}

// J_ApplyChain runs an ordered chain of processors over data.
//
// dataJSON  — output of J_ReadFile.
// chainJSON — array of processor configs:
//
//	[{"type":"field_masker","params":{...}},{"type":"compress","params":{...}}]
//
// Returns same shape as J_ReadFile.
// Caller must free result with J_FreeString.
//
//export J_ApplyChain
func J_ApplyChain(dataJSON *C.char, chainJSON *C.char) *C.char {
	// TODO: json.Unmarshal chainJSON → []processors.Config
	// TODO: processors.NewChain(configs...).Process(ctx, rows, schema)
	// TODO: return jOK(result)
	_, _ = dataJSON, chainJSON
	return jErr("TODO: not implemented")
}

// ---------------------------------------------------------------------------
// Diff
// ---------------------------------------------------------------------------

// J_Diff computes the difference between two TDTP datasets (add/remove/modify).
//
// oldJSON, newJSON — outputs of J_ReadFile.
// Returns diff result JSON: {"added":[...],"removed":[...],"modified":[...],"stats":{...}}.
// Caller must free result with J_FreeString.
//
//export J_Diff
func J_Diff(oldJSON *C.char, newJSON *C.char) *C.char {
	// TODO: json.Unmarshal both → jReadResult
	// TODO: build DataPacket from each
	// TODO: diff.NewDiffer().Diff(old, new, options)
	// TODO: return jOK(diffResult)
	_, _ = oldJSON, newJSON
	return jErr("TODO: not implemented")
}

// ---------------------------------------------------------------------------
// Internal types (shared between J_* functions)
// ---------------------------------------------------------------------------

// jReadResult is the canonical JSON shape for J_ReadFile / J_WriteFile / J_FilterRows.
type jReadResult struct {
	Schema packet.Schema  `json:"schema"`
	Header packet.Header  `json:"header"`
	Data   [][]string     `json:"data"`
	Error  string         `json:"error,omitempty"`
}
