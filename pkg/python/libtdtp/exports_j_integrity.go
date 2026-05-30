package main

/*
#include <stdlib.h>
*/
import "C"
import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// jPartCheck is the per-part integrity report inside J_Test.
type jPartCheck struct {
	File        string `json:"file"`
	Rows        int    `json:"rows"`
	Compression string `json:"compression"`
	Checksum    string `json:"checksum"`  // "ok" | "none" | "invalid"
	RowCount    string `json:"row_count"` // "ok" | "mismatch: header=N actual=M"
	OK          bool   `json:"ok"`
}

// jTestResult is the J_Test response: an overall verdict plus per-part detail.
type jTestResult struct {
	OK         bool         `json:"ok"`
	TotalParts int          `json:"total_parts"`
	TotalRows  int          `json:"total_rows"`
	Parts      []jPartCheck `json:"parts"`
	Errors     []string     `json:"errors"`
	Error      string       `json:"error,omitempty"`
}

// J_Test performs a dry-run integrity check on a TDTP file or multi-part batch,
// without writing to any database. Mirrors `tdtpcli --test`:
//   - resolves and verifies all _part_N_of_M siblings are present
//   - parses each part (structural validity)
//   - validates the XXH3 checksum of compressed parts
//   - performs a decompression dry-run for compressed parts
//   - compares header RecordsInPart against the actual row count
//
// Returns {ok, total_parts, total_rows, parts:[...], errors:[...]}.
// Caller must free result with J_FreeString.
//
//export J_Test
func J_Test(path *C.char) *C.char {
	files, err := resolvePartSet(C.GoString(path))
	if err != nil {
		return jErr(err.Error())
	}

	res := jTestResult{
		OK:         true,
		TotalParts: len(files),
		Parts:      make([]jPartCheck, 0, len(files)),
		Errors:     []string{},
	}

	for _, f := range files {
		check := testOnePart(f)
		if !check.OK {
			res.OK = false
		}
		res.TotalRows += check.Rows
		res.Parts = append(res.Parts, check)
	}

	// Collect per-part failures into the top-level errors list for convenience.
	for _, p := range res.Parts {
		if !p.OK {
			detail := p.RowCount
			if p.Checksum == "invalid" {
				detail = "checksum invalid"
			}
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %s", p.File, detail))
		}
	}

	return jOK(res)
}

// testOnePart validates a single part file and returns its integrity report.
func testOnePart(path string) jPartCheck {
	base := filepath.Base(path)
	check := jPartCheck{File: base, Checksum: "none", RowCount: "ok", OK: true}

	// Read original header flags (compression/checksum) without expansion.
	raw, err := os.ReadFile(path)
	if err != nil {
		check.OK = false
		check.RowCount = fmt.Sprintf("read error: %v", err)
		return check
	}
	parser := packet.NewParser()
	meta, err := parser.ParseBytes(raw)
	if err != nil {
		check.OK = false
		check.RowCount = fmt.Sprintf("parse error: %v", err)
		return check
	}
	check.Compression = meta.Data.Compression
	if check.Compression == "" {
		check.Compression = "none"
	}
	hadChecksum := meta.Data.Checksum != ""
	headerRows := meta.Header.RecordsInPart

	// Full materialization: decompresses + validates checksum (via jDecompressRows)
	// and expands compact rows. A failure here means the part is corrupt.
	jp, err := readPacketToJPacket(path)
	if err != nil {
		check.OK = false
		if hadChecksum {
			check.Checksum = "invalid"
		}
		check.RowCount = fmt.Sprintf("%v", err)
		return check
	}
	if hadChecksum {
		check.Checksum = "ok"
	}

	check.Rows = len(jp.Data)
	if headerRows != 0 && headerRows != check.Rows {
		check.OK = false
		check.RowCount = fmt.Sprintf("mismatch: header=%d actual=%d", headerRows, check.Rows)
	}
	return check
}
