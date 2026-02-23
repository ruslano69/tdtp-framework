package main

/*
#include <stdlib.h>
#include <string.h>

// ---------------------------------------------------------------------------
// C structs — Direct API boundary
// ---------------------------------------------------------------------------

// D_Field mirrors packet.Field.
typedef struct {
    char name[256];
    char type_name[64];   // "type" is a Go keyword; matches packet.Field.Type
    int  length;
    int  precision;
    int  scale;
    int  is_key;          // 0 / 1
    int  is_readonly;     // 0 / 1
} D_Field;

// D_Schema mirrors packet.Schema.
typedef struct {
    D_Field* fields;      // C.malloc-allocated array, count = field_count
    int      field_count;
} D_Schema;

// D_Row mirrors packet.Row after parsing ([]string).
typedef struct {
    char** values;        // C.malloc-allocated array of C strings
    int    value_count;
} D_Row;

// D_Packet is the primary result/argument struct for Direct functions.
// All pointer fields are C.malloc-allocated; free with D_FreePacket.
typedef struct {
    D_Row*   rows;
    int      row_count;
    D_Schema schema;
    char     msg_type[32];      // "reference" | "request" | "response" | "alarm"
    char     table_name[256];
    char     message_id[64];
    long long timestamp_unix;   // UTC Unix timestamp (seconds)
    char     compression[16];   // "" | "zstd"
    char     error[1024];       // non-empty on failure
} D_Packet;

// D_FilterSpec describes a single filter condition (one row in a WHERE clause).
typedef struct {
    char field[256];
    char op[32];          // eq|ne|gt|gte|lt|lte|in|not_in|between|like|not_like|is_null|is_not_null
    char value[1024];
    char value2[1024];    // used only for BETWEEN
} D_FilterSpec;

// D_MaskConfig specifies which fields to mask.
typedef struct {
    char** fields;        // C.malloc-allocated array of field name strings
    int    field_count;
    char   mask_char[4];  // default "*"
    int    visible_chars; // chars to leave unmasked at the end
} D_MaskConfig;
*/
import "C"
import (
	"unsafe"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

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
	// TODO: free pkt.schema.fields array
	// TODO: for each row in pkt.rows: free each row.values[i], then free row.values
	// TODO: free pkt.rows array
	_ = pkt
}

// D_FreeMaskConfig releases C.malloc memory owned by a D_MaskConfig.
//
//export D_FreeMaskConfig
func D_FreeMaskConfig(cfg *C.D_MaskConfig) {
	if cfg == nil {
		return
	}
	// TODO: free each cfg.fields[i], then free cfg.fields
	_ = cfg
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// dSetError writes an error string into pkt.error (truncated to 1023 chars).
func dSetError(pkt *C.D_Packet, msg string) {
	// TODO: C.strncpy(pkt.error, C.CString(msg), 1023)
	_ = unsafe.Pointer(pkt)
	_ = msg
}

// dFillSchema populates pkt.schema from a packet.Schema using C.malloc.
func dFillSchema(pkt *C.D_Packet, schema packet.Schema) {
	// TODO: C.malloc array of D_Field, fill each field, assign to pkt.schema
	_ = pkt
	_ = schema
}

// dFillRows populates pkt.rows from [][]string using C.malloc.
func dFillRows(pkt *C.D_Packet, rows [][]string) {
	// TODO: C.malloc array of D_Row
	// TODO: for each row: C.malloc values array, C.CString each value
	// TODO: assign to pkt.rows, set pkt.row_count
	_ = pkt
	_ = rows
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
	// TODO: packet.NewParser().ParseFile(C.GoString(path))
	// TODO: decompress if pkt.Data.Compression == "zstd"
	// TODO: dFillSchema(out, pkt.Schema)
	// TODO: dFillRows(out, rows)
	// TODO: fill out.msg_type, out.table_name, out.message_id, out.timestamp_unix
	_ = path
	dSetError(out, "TODO: not implemented")
	return 1
}

// D_WriteFile generates a TDTP file from pkt and writes it to path.
// Returns 0 on success, 1 on error (check out.error).
//
//export D_WriteFile
func D_WriteFile(pkt *C.D_Packet, path *C.char) C.int {
	// TODO: reconstruct packet.DataPacket from pkt fields
	// TODO: packet.NewGenerator().WriteFile(dp, C.GoString(path))
	_, _ = pkt, path
	return 1
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
	// TODO: reconstruct [][]string rows + packet.Schema from pkt
	// TODO: convert C filters array → packet.Filters (AND group)
	// TODO: tdtql.NewFilterEngine().ApplyFilters(...)
	// TODO: apply limit if > 0
	// TODO: dFillSchema(out, ...) + dFillRows(out, ...)
	_, _, _, _ = pkt, filters, count, limit
	dSetError(out, "TODO: not implemented")
	return 1
}

// ---------------------------------------------------------------------------
// Processors
// ---------------------------------------------------------------------------

// D_ApplyMask masks the fields listed in cfg.fields inside pkt, writing result to out.
// Returns 0 on success, 1 on error.
// Caller must release out with D_FreePacket.
//
//export D_ApplyMask
func D_ApplyMask(pkt *C.D_Packet, cfg *C.D_MaskConfig, out *C.D_Packet) C.int {
	// TODO: reconstruct rows + schema from pkt
	// TODO: build processors.FieldMasker from cfg
	// TODO: masker.Process(ctx, rows, schema)
	// TODO: dFillSchema(out, ...) + dFillRows(out, ...)
	_, _ = pkt, cfg
	dSetError(out, "TODO: not implemented")
	return 1
}

// D_ApplyCompress compresses pkt data with zstd, writing result to out.
// Returns 0 on success, 1 on error.
// Caller must release out with D_FreePacket.
//
//export D_ApplyCompress
func D_ApplyCompress(pkt *C.D_Packet, level C.int, out *C.D_Packet) C.int {
	// TODO: reconstruct rows + schema from pkt
	// TODO: processors.NewCompressionProcessor(int(level)).Process(ctx, rows, schema)
	// TODO: dFillRows(out, compressed) + set out.compression = "zstd"
	_, _ = pkt, level
	dSetError(out, "TODO: not implemented")
	return 1
}

// D_ApplyDecompress decompresses a zstd-compressed pkt, writing result to out.
// Returns 0 on success, 1 on error.
// Caller must release out with D_FreePacket.
//
//export D_ApplyDecompress
func D_ApplyDecompress(pkt *C.D_Packet, out *C.D_Packet) C.int {
	// TODO: processors.DecompressDataForTdtp(pkt.rows[0].values[0])
	// TODO: dFillRows(out, decompressed rows)
	_ = pkt
	dSetError(out, "TODO: not implemented")
	return 1
}
