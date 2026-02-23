//go:build !compress

package main

/*
#include <stdlib.h>
*/
import "C"
import (
	"encoding/json"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// jDecompressRows is the stub used when the "compress" build tag is absent.
// Returns a clear error so callers know to rebuild with -tags compress.
func jDecompressRows(_ *packet.DataPacket) *C.char {
	b, _ := json.Marshal(map[string]string{
		"error": "compressed TDTP files require libtdtp built with '-tags compress' " +
			"(needs github.com/klauspost/compress in module cache)",
	})
	return C.CString(string(b))
}

// jApplyProcessor stub — only non-compress processors are unavailable here
// because the processors package imports klauspost/compress even for non-compress types.
// Rebuild with -tags compress to enable all processors.
func jApplyProcessor(dataJSON *C.char, procType *C.char, configJSON *C.char) *C.char {
	_ = dataJSON
	_ = procType
	_ = configJSON
	b, _ := json.Marshal(map[string]string{
		"error": "processors require libtdtp built with '-tags compress'",
	})
	return C.CString(string(b))
}

// jApplyChain stub.
func jApplyChain(dataJSON *C.char, chainJSON *C.char) *C.char {
	_ = dataJSON
	_ = chainJSON
	b, _ := json.Marshal(map[string]string{
		"error": "processors require libtdtp built with '-tags compress'",
	})
	return C.CString(string(b))
}
