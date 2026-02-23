// Package main provides C-compatible shared library for Python (and other language) bindings.
//
// Build:
//
//	go build -buildmode=c-shared -o libtdtp.so ./pkg/python/libtdtp/
//
// Two API families are exported:
//
//	J_*  — JSON boundary: args/results serialized as JSON strings (*C.char).
//	        Universal, easy to use from any language, small serialization overhead.
//
//	D_*  — Direct boundary: args/results passed as C structs via pointer.
//	        Maximum performance, no serialization, but requires explicit D_Free* calls.
package main

import "C" //nolint:typecheck

func main() {}
