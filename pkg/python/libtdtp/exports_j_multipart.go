package main

/*
#include <stdlib.h>
*/
import "C"
import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"unsafe"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// partPattern matches multi-part filenames like name_part_3_of_6.tdtp.xml
var partPattern = regexp.MustCompile(`^(.+)_part_(\d+)_of_(\d+)(\..+)$`)

// readPacketToJPacket reads and fully materializes one TDTP file into a jPacket,
// transparently decompressing (reusing jDecompressRows) and expanding compact rows.
// This is the shared read path behind J_ReadFile and J_ReadMultipart.
func readPacketToJPacket(path string) (jPacket, error) {
	parser := packet.NewParser()
	pkt, err := parser.ParseFile(path)
	if err != nil {
		return jPacket{}, fmt.Errorf("parse error: %v", err)
	}

	if pkt.Data.Compression != "" {
		// Reuse the decompression path; it returns the canonical JSON envelope.
		cstr := jDecompressRows(pkt)
		defer C.free(unsafe.Pointer(cstr))
		var jp jPacket
		if err := json.Unmarshal([]byte(C.GoString(cstr)), &jp); err != nil {
			return jPacket{}, fmt.Errorf("parse error: %v", err)
		}
		if jp.Error != "" {
			return jPacket{}, fmt.Errorf("%s", jp.Error)
		}
		return jp, nil
	}

	return packetToJPacket(pkt, pkt.GetRows()), nil
}

// resolvePartSet returns the ordered list of part files for a multi-part batch.
// If path is not a _part_N_of_M file, it returns just [path] (single file).
// Errors if any expected part is missing.
func resolvePartSet(path string) ([]string, error) {
	base := filepath.Base(path)
	m := partPattern.FindStringSubmatch(base)
	if m == nil {
		// Not a part file — treat as a single packet.
		return []string{path}, nil
	}

	stem, ext := m[1], m[4]
	var total int
	fmt.Sscanf(m[3], "%d", &total)
	if total < 1 {
		return nil, fmt.Errorf("parse error: invalid total parts in %q", base)
	}

	dir := filepath.Dir(path)
	files := make([]string, 0, total)
	for i := 1; i <= total; i++ {
		name := fmt.Sprintf("%s_part_%d_of_%d%s", stem, i, total, ext)
		full := filepath.Join(dir, name)
		if _, err := os.Stat(full); err != nil {
			return nil, fmt.Errorf("parse error: missing part %d of %d (%s)", i, total, name)
		}
		files = append(files, full)
	}
	return files, nil
}

// J_ReadMultipart reads a multi-part TDTP batch and concatenates it into one
// dataset. Pass the path to any single part (or a non-part file) — siblings are
// auto-discovered via the _part_N_of_M naming convention. Compressed and compact
// parts are handled transparently.
// Returns the same {schema, header, data} shape as J_read, with header
// part_number/total_parts reset to 1/1 and records_in_part = total rows.
// Caller must free result with J_FreeString.
//
//export J_ReadMultipart
func J_ReadMultipart(path *C.char) *C.char {
	files, err := resolvePartSet(C.GoString(path))
	if err != nil {
		return jErr(err.Error())
	}

	var combined jPacket
	totalRows := 0
	for i, f := range files {
		jp, err := readPacketToJPacket(f)
		if err != nil {
			return jErr(err.Error())
		}
		if i == 0 {
			combined = jp
			combined.Data = make([][]string, 0, len(jp.Data))
		}
		combined.Data = append(combined.Data, jp.Data...)
		totalRows += len(jp.Data)
	}

	// Present the assembled batch as a single logical packet.
	combined.Header.PartNumber = 1
	combined.Header.TotalParts = 1
	combined.Header.RecordsInPart = totalRows
	combined.Compression = ""
	combined.Checksum = ""

	return jOK(combined)
}
