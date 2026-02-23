//go:build !compress

package main

/*
#include <stdlib.h>
#include <string.h>
#include "tdtp_structs.h"
*/
import "C"
import (
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// dDecompressRows stub — requires -tags compress build.
func dDecompressRows(_ *packet.DataPacket, out *C.D_Packet) C.int {
	dSetError(out, "compressed TDTP files require libtdtp built with '-tags compress'")
	return 1
}

// D_ApplyCompress stub.
//
//export D_ApplyCompress
func D_ApplyCompress(_ *C.D_Packet, _ C.int, out *C.D_Packet) C.int {
	dSetError(out, "D_ApplyCompress requires libtdtp built with '-tags compress'")
	return 1
}

// D_ApplyDecompress stub.
//
//export D_ApplyDecompress
func D_ApplyDecompress(_ *C.D_Packet, out *C.D_Packet) C.int {
	dSetError(out, "D_ApplyDecompress requires libtdtp built with '-tags compress'")
	return 1
}
