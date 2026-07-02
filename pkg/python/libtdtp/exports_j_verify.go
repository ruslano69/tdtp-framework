package main

/*
#include <stdlib.h>
*/
import "C"
import (
	"encoding/json"
	"fmt"
	"os"
	"unsafe"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// jVerifyResult is the J_Verify response.
type jVerifyResult struct {
	OK           bool   `json:"ok"`            // true if hashes match OR packet is not stamped
	HasIntegrity bool   `json:"has_integrity"` // whether v1.4 XXH3 hashes are present
	PacketXXH3   string `json:"packet_xxh3,omitempty"`
	Detail       string `json:"detail,omitempty"` // mismatch description when ok=false
	Error        string `json:"error,omitempty"`
}

// J_Verify checks the v1.4 XXH3 integrity hashes of a TDTP file (local, no Mercury).
// An agent pulling an untrusted packet can confirm it was not tampered before use.
//   - If the packet carries no integrity hashes → {ok:true, has_integrity:false}.
//   - If hashes match → {ok:true, has_integrity:true, packet_xxh3:...}.
//   - If any hash mismatches → {ok:false, has_integrity:true, detail:...}.
//
// Caller must free result with J_FreeString.
//
//export J_Verify
func J_Verify(path *C.char) *C.char {
	raw, err := os.ReadFile(C.GoString(path))
	if err != nil {
		return jErr(fmt.Sprintf("parse error: %v", err))
	}
	// ParseBytes preserves the on-disk XXH3 attributes, compression and compact flags.
	parser := packet.NewParser()
	pkt, err := parser.ParseBytes(raw)
	if err != nil {
		return jErr(fmt.Sprintf("parse error: %v", err))
	}

	if !packet.HasIntegrity(pkt) {
		return jOK(jVerifyResult{OK: true, HasIntegrity: false})
	}

	// Materialize rows so VerifyIntegrity can recompute the data hash:
	// decompress compressed blobs, expand compact carry-forward.
	if pkt.Data.Compression != "" {
		cstr := jDecompressRows(pkt) // mutates pkt.Data.Rows in place (compress build)
		var probe jPacket
		_ = json.Unmarshal([]byte(C.GoString(cstr)), &probe)
		C.free(unsafe.Pointer(cstr))
		if probe.Error != "" {
			return jErr(fmt.Sprintf("decompress error: %s", probe.Error))
		}
	} else if pkt.Data.Compact {
		if err := packet.ExpandCompactRows(pkt); err != nil {
			return jErr(fmt.Sprintf("compact expand error: %v", err))
		}
	}

	if err := packet.VerifyIntegrity(pkt); err != nil {
		return jOK(jVerifyResult{
			OK:           false,
			HasIntegrity: true,
			PacketXXH3:   pkt.XXH3,
			Detail:       err.Error(),
		})
	}

	return jOK(jVerifyResult{
		OK:           true,
		HasIntegrity: true,
		PacketXXH3:   pkt.XXH3,
	})
}

// jStampResult is the J_Stamp response.
type jStampResult struct {
	OK         bool   `json:"ok"`
	Path       string `json:"path"`
	PacketXXH3 string `json:"packet_xxh3"`
	SchemaXXH3 string `json:"schema_xxh3"`
	DataXXH3   string `json:"data_xxh3"`
	Error      string `json:"error,omitempty"`
}

// J_Stamp computes v1.4 XXH3 integrity hashes for a dataset and writes a stamped
// TDTP file to path (analog of `tdtpcli --export --integrity`, without Mercury).
// Returns the three fingerprints so the producer can record/transmit them.
// Caller must free result with J_FreeString.
//
//export J_Stamp
func J_Stamp(dataJSON *C.char, path *C.char) *C.char {
	jp, err := unmarshalJPacket(dataJSON)
	if err != nil {
		return jErr(err.Error())
	}

	pkt := jPacketToDataPacket(jp)

	// Without this, a consumer re-reading the file treats it as pre-1.4 (by
	// version string, not by hash presence) and silently skips the entire
	// v1.4 gate — both the Mercury check and the local xxh3 recheck. Mirrors
	// the identical requirement in cmd/tdtpcli/commands/export.go's integrityProc.
	pkt.Version = "1.4"

	res, err := packet.ComputeIntegrity(pkt)
	if err != nil {
		return jErr(fmt.Sprintf("write error: integrity stamp failed: %v", err))
	}

	gen := packet.NewGenerator()
	if err := gen.WriteToFile(pkt, C.GoString(path)); err != nil {
		return jErr(fmt.Sprintf("write error: %v", err))
	}

	return jOK(jStampResult{
		OK:         true,
		Path:       C.GoString(path),
		PacketXXH3: res.PacketXXH3,
		SchemaXXH3: res.SchemaXXH3,
		DataXXH3:   res.DataXXH3,
	})
}
