package main

/*
#include <stdlib.h>
*/
import "C"
import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"unsafe"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/mercury"
	"github.com/ruslano69/tdtp-framework/pkg/pipeline"
)

// jVerifyMercuryResult is the J_VerifyMercury response.
type jVerifyMercuryResult struct {
	OK              bool   `json:"ok"`               // false only for a definitive verification verdict (tampered/not-registered/local mismatch)
	Version         string `json:"version"`          // effective version ("1.4", or "1.3.1" if the packet predates integrity hashes)
	MercuryVerified bool   `json:"mercury_verified"` // true only if xzMercury confirmed the fingerprint over the network
	Degraded        bool   `json:"degraded,omitempty"`
	DegradedReason  string `json:"degraded_reason,omitempty"`
	Sender          string `json:"sender,omitempty"` // producer identity from the Mercury hash record
	PacketXXH3      string `json:"packet_xxh3,omitempty"`
	Detail          string `json:"detail,omitempty"`
	Error           string `json:"error,omitempty"`
}

// J_VerifyMercury runs the full TDTP v1.4 consumer pre-flight against a live
// xzMercury hash registry: GET /api/hashes/{uuid}/{part}?xxh3=... to confirm
// the packet's fingerprint was registered by an authenticated producer, then
// recomputes the local xxh3 hashes (same check as J_Verify).
//
// This is the C ABI equivalent of `tdtpcli --test --mercury-url <url>` /
// applyV14SecurityGate. J_Verify (see exports_j_verify.go) deliberately never
// touches the network — it only recomputes local xxh3 hashes. Any consumer
// that needs proof the packet came from an authenticated producer (not just
// that it wasn't corrupted in transit) must call J_VerifyMercury instead.
//
// mercuryURL must be non-empty; callers that only want local integrity
// checking should call J_Verify, which has no network dependency at all.
//
// Result shapes:
//   - Pre-v1.4 packet              → {ok:true, mercury_verified:false, detail:"..."}
//   - Mercury confirms fingerprint → {ok:true, mercury_verified:true, sender:"..."}
//   - Mercury unreachable          → {ok:true, mercury_verified:false, degraded:true, degraded_reason:"..."}
//     (local xxh3 is still checked in this case; caller decides whether a
//     degraded — Mercury-less — result is acceptable for its use case)
//   - Hash not registered / tampered / local xxh3 mismatch → {ok:false, detail:"..."}
//   - File/parse/decompress failure or empty mercuryURL    → {error:"..."}
//
// Caller must free result with J_FreeString.
//
//export J_VerifyMercury
func J_VerifyMercury(path *C.char, mercuryURL *C.char) *C.char {
	url := C.GoString(mercuryURL)
	if url == "" {
		return jErr("mercury URL must not be empty (use J_Verify for local-only integrity check)")
	}

	raw, err := os.ReadFile(C.GoString(path))
	if err != nil {
		return jErr(fmt.Sprintf("parse error: %v", err))
	}
	parser := packet.NewParser()
	pkt, err := parser.ParseBytes(raw)
	if err != nil {
		return jErr(fmt.Sprintf("parse error: %v", err))
	}

	// Materialize rows the same way J_Verify does: decompress or expand
	// compact carry-forward first, so the local xxh3 recomputation inside
	// VerifyAndPrepare hashes the same plain-text rows the producer stamped.
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

	if packet.NeedsRowCountCheck(pkt.Version) {
		return jOK(jVerifyMercuryResult{
			OK:      true,
			Version: pkt.Version,
			Detail:  "pre-1.4 packet — no integrity hashes, network check not applicable",
		})
	}

	verifier := mercury.NewClient(url, 5000)
	result, err := pipeline.VerifyAndPrepare(context.Background(), pkt, verifier, pipeline.FallbackDegrade)
	if err != nil {
		// Definitive verification verdict (tampered / not registered / local
		// xxh3 mismatch) — not an infra failure, so report via ok:false rather
		// than the error field (mirrors J_Verify's ok:false-on-mismatch shape).
		return jOK(jVerifyMercuryResult{
			OK:      false,
			Version: pkt.Version,
			Detail:  err.Error(),
		})
	}

	res := jVerifyMercuryResult{
		OK:         true,
		Version:    result.Version,
		PacketXXH3: pkt.XXH3,
	}
	switch {
	case result.Degraded:
		res.Degraded = true
		res.DegradedReason = result.DegradedReason
	case result.MercuryRecord != nil:
		res.MercuryVerified = true
		res.Sender = result.MercuryRecord.Sender
	}
	return jOK(res)
}
