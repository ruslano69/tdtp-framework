//go:build compress

package main

/*
#include <stdlib.h>
#include <string.h>
#include "tdtp_structs.h"
*/
import "C"
import (
	"strings"
	"unsafe"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/processors"
)

// dDecompressRows decompresses a zstd D_Packet and fills out with plain rows.
// Active when built with: go build -tags compress -buildmode=c-shared
func dDecompressRows(pkt *packet.DataPacket, out *C.D_Packet) C.int {
	if len(pkt.Data.Rows) == 0 {
		dFillSchema(out, pkt.Schema)
		dFillRows(out, nil)
		dFillHeader(out, pkt)
		return 0
	}

	parser := packet.NewParser()
	lines, err := processors.DecompressDataForTdtpWithAlgo(pkt.Data.Rows[0].Value, pkt.Data.Compression)
	if err != nil {
		dSetError(out, "decompress error: "+err.Error())
		return 1
	}

	// Rebuild pkt.Data.Rows from decompressed lines so ExpandCompactRows can work.
	decompRows := make([]packet.Row, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		decompRows = append(decompRows, packet.Row{Value: line})
	}
	pkt.Data.Rows = decompRows
	pkt.Data.Compression = ""

	// Expand compact carry-forward encoding so callers always receive fully-populated rows.
	if pkt.Data.Compact {
		if err := packet.ExpandCompactRows(pkt); err != nil {
			dSetError(out, "compact expand error: "+err.Error())
			return 1
		}
	}

	rows := make([][]string, 0, len(pkt.Data.Rows))
	for _, row := range pkt.Data.Rows {
		rows = append(rows, parser.GetRowValues(row))
	}

	dFillSchema(out, pkt.Schema)
	dFillRows(out, rows)
	dFillHeader(out, pkt)
	return 0
}

// D_ApplyCompress compresses pkt data, writing result to out.
// The algorithm is read from pkt.compression[16]; if empty, "zstd" is used.
// Returns 0 on success, 1 on error.
// Caller must release out with D_FreePacket.
//
//export D_ApplyCompress
func D_ApplyCompress(pkt *C.D_Packet, level C.int, out *C.D_Packet) C.int {
	rows := dGetRows(pkt)

	algo := dReadStr((*C.char)(unsafe.Pointer(&pkt.compression[0])))
	if algo == "" {
		algo = "zstd"
	}

	rowStrings := make([]string, len(rows))
	for i, row := range rows {
		rowStrings[i] = strings.Join(row, "|")
	}

	compressed, _, err := processors.CompressDataForTdtpAlgo(rowStrings, algo, int(level))
	if err != nil {
		dSetError(out, "compression error: "+err.Error())
		return 1
	}

	// Output: single row with the compressed blob.
	dFillSchema(out, dGetSchema(pkt))
	dFillRows(out, [][]string{{compressed}})
	dWriteStr((*C.char)(unsafe.Pointer(&out.msg_type[0])), dReadStr((*C.char)(unsafe.Pointer(&pkt.msg_type[0]))), 32)
	dWriteStr((*C.char)(unsafe.Pointer(&out.table_name[0])), dReadStr((*C.char)(unsafe.Pointer(&pkt.table_name[0]))), 256)
	dWriteStr((*C.char)(unsafe.Pointer(&out.message_id[0])), dReadStr((*C.char)(unsafe.Pointer(&pkt.message_id[0]))), 64)
	out.timestamp_unix = pkt.timestamp_unix
	dWriteStr((*C.char)(unsafe.Pointer(&out.compression[0])), algo, 16)
	return 0
}

// D_ApplyDecompress decompresses a single-blob packet, writing plain rows to out.
// Returns 0 on success, 1 on error.
// Caller must release out with D_FreePacket.
//
//export D_ApplyDecompress
func D_ApplyDecompress(pkt *C.D_Packet, out *C.D_Packet) C.int {
	rows := dGetRows(pkt)
	if len(rows) == 0 || len(rows[0]) == 0 {
		dSetError(out, "no compressed data found")
		return 1
	}

	algo := dReadStr((*C.char)(unsafe.Pointer(&pkt.compression[0])))
	parser := packet.NewParser()
	lines, err := processors.DecompressDataForTdtpWithAlgo(rows[0][0], algo)
	if err != nil {
		dSetError(out, "decompress error: "+err.Error())
		return 1
	}

	plainRows := make([][]string, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		plainRows = append(plainRows, parser.GetRowValues(packet.Row{Value: line}))
	}

	dFillSchema(out, dGetSchema(pkt))
	dFillRows(out, plainRows)
	dWriteStr((*C.char)(unsafe.Pointer(&out.msg_type[0])), dReadStr((*C.char)(unsafe.Pointer(&pkt.msg_type[0]))), 32)
	dWriteStr((*C.char)(unsafe.Pointer(&out.table_name[0])), dReadStr((*C.char)(unsafe.Pointer(&pkt.table_name[0]))), 256)
	dWriteStr((*C.char)(unsafe.Pointer(&out.message_id[0])), dReadStr((*C.char)(unsafe.Pointer(&pkt.message_id[0]))), 64)
	out.timestamp_unix = pkt.timestamp_unix
	return 0
}
