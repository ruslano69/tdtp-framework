package main

/*
#include <stdlib.h>
#include "tdtp_structs.h"
*/
import "C"
import (
	"math"
	"strconv"
	"unsafe"
)

// Columnar extraction for the Arrow bridge.
//
// These functions take an already-parsed D_Packet (rows live in C memory as
// char**) and produce one column as a contiguous typed C buffer, parsing the
// string cells in Go in a single pass. Python wraps the buffer with numpy /
// pyarrow, eliminating the per-row Python loop of the row-major path.
//
// Memory: every returned buffer is C.malloc'd and must be released with
// D_FreeBuffer. For D_ColumnUTF8 both the data buffer (return value) and the
// offsets buffer (out-param) must be freed.

// cellValue returns the string value at (rowIdx, colIdx) in pkt's flat
// row_data/row_offsets buffer (see tdtp_structs.h D_Packet doc), or "" if
// colIdx is out of range.
func cellValue(pkt *C.D_Packet, rowIdx, colIdx int) string {
	cols := int(pkt.col_count)
	if colIdx < 0 || colIdx >= cols || pkt.row_data == nil {
		return ""
	}
	k := rowIdx*cols + colIdx
	offs := unsafe.Slice(pkt.row_offsets, int(pkt.row_count)*cols+1)
	start, end := offs[k], offs[k+1]
	if start == end {
		return ""
	}
	buf := unsafe.Slice((*byte)(unsafe.Pointer(pkt.row_data)), end)
	return string(buf[start:end])
}

// D_ColumnFloat64 extracts column colIdx as a contiguous float64 buffer.
// Empty / unparseable cells become NaN (Arrow/pandas treat NaN as null).
// Length is pkt.row_count. Free with D_FreeBuffer.
//
//export D_ColumnFloat64
func D_ColumnFloat64(pkt *C.D_Packet, colIdx C.int) *C.double {
	n := int(pkt.row_count)
	if n == 0 {
		return nil
	}
	buf := (*C.double)(C.malloc(C.size_t(n) * C.size_t(unsafe.Sizeof(C.double(0)))))
	out := unsafe.Slice(buf, n)
	ci := int(colIdx)
	for i := 0; i < n; i++ {
		s := cellValue(pkt, i, ci)
		if s == "" {
			out[i] = C.double(math.NaN())
			continue
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			out[i] = C.double(math.NaN())
		} else {
			out[i] = C.double(f)
		}
	}
	return buf
}

// D_ColumnInt64 extracts column colIdx as a contiguous int64 buffer.
// Empty / unparseable cells become 0 (int64 has no NaN; use Float64 if you
// need to distinguish nulls). Length is pkt.row_count. Free with D_FreeBuffer.
//
//export D_ColumnInt64
func D_ColumnInt64(pkt *C.D_Packet, colIdx C.int) *C.longlong {
	n := int(pkt.row_count)
	if n == 0 {
		return nil
	}
	buf := (*C.longlong)(C.malloc(C.size_t(n) * C.size_t(unsafe.Sizeof(C.longlong(0)))))
	out := unsafe.Slice(buf, n)
	ci := int(colIdx)
	for i := 0; i < n; i++ {
		s := cellValue(pkt, i, ci)
		if s == "" {
			out[i] = 0
			continue
		}
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			out[i] = 0
		} else {
			out[i] = C.longlong(v)
		}
	}
	return buf
}

// D_ColumnUTF8 extracts column colIdx in Arrow's variable-length string layout:
// a single concatenated UTF-8 data buffer (return value) plus an int32 offsets
// array of length row_count+1 (out-param outOffsets). *outNBytes receives the
// total byte length of the data buffer.
// Both the data buffer and the offsets buffer must be freed with D_FreeBuffer.
//
//export D_ColumnUTF8
func D_ColumnUTF8(pkt *C.D_Packet, colIdx C.int, outOffsets **C.int, outNBytes *C.int) *C.char {
	n := int(pkt.row_count)
	ci := int(colIdx)

	// First pass: gather values and compute offsets.
	strs := make([]string, n)
	offs := make([]int32, n+1)
	total := 0
	for i := 0; i < n; i++ {
		s := cellValue(pkt, i, ci)
		strs[i] = s
		offs[i] = int32(total)
		total += len(s)
	}
	offs[n] = int32(total)

	// Data buffer (at least 1 byte so malloc never returns nil for empty cols).
	dataSize := total
	if dataSize == 0 {
		dataSize = 1
	}
	data := C.malloc(C.size_t(dataSize))
	dslice := unsafe.Slice((*byte)(data), total)
	pos := 0
	for _, s := range strs {
		copy(dslice[pos:pos+len(s)], s)
		pos += len(s)
	}

	// Offsets buffer (n+1 int32).
	offBytes := C.size_t(n+1) * C.size_t(unsafe.Sizeof(C.int(0)))
	offBuf := C.malloc(offBytes)
	oslice := unsafe.Slice((*int32)(offBuf), n+1)
	copy(oslice, offs)

	*outOffsets = (*C.int)(offBuf)
	*outNBytes = C.int(total)
	return (*C.char)(data)
}

// D_FreeBuffer releases a buffer returned by any D_Column* function.
//
//export D_FreeBuffer
func D_FreeBuffer(ptr unsafe.Pointer) {
	if ptr != nil {
		C.free(ptr)
	}
}
