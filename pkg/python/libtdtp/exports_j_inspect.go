package main

/*
#include <stdlib.h>
*/
import "C"
import (
	"fmt"
	"os"
	"unsafe"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// jInspectResult is the structured metadata returned by J_Inspect.
// Mirrors `tdtpcli --inspect` but as JSON instead of printed YAML, so agents
// can read packet metadata in-process without spawning a subprocess.
type jInspectResult struct {
	Table       string         `json:"table"`
	Type        string         `json:"type"`
	Protocol    string         `json:"protocol"`
	Version     string         `json:"version"`
	Timestamp   string         `json:"timestamp"`
	MessageID   string         `json:"message_id"`
	FieldsCount int            `json:"fields_count"`
	Schema      packet.Schema  `json:"schema"`
	TotalRows   int            `json:"total_rows"`
	PartNumber  int            `json:"part_number"`
	TotalParts  int            `json:"total_parts"`
	Compression string         `json:"compression"`
	Checksum    string         `json:"checksum"`
	Compact     bool           `json:"compact"`
	Pipeline    *jPipelineInfo `json:"pipeline,omitempty"`
	Error       string         `json:"error,omitempty"`
}

// jPipelineInfo carries embedded v1.4 PipelineContext metadata, if present.
type jPipelineInfo struct {
	Name      string            `json:"name"`
	Version   string            `json:"version,omitempty"`
	Variables map[string]string `json:"variables,omitempty"`
}

// J_Inspect returns structured metadata for a TDTP file without decompressing
// the data section — fast, and works in the base build (no compress tag needed).
// Caller must free result with J_FreeString.
//
//export J_Inspect
func J_Inspect(path *C.char) *C.char {
	raw, err := os.ReadFile(C.GoString(path))
	if err != nil {
		return jErr(fmt.Sprintf("parse error: %v", err))
	}
	// ParseBytes (unlike ParseFile) does not auto-expand compact rows, so the
	// on-disk Data.Compact flag is preserved and no decompression/expansion runs.
	parser := packet.NewParser()
	pkt, err := parser.ParseBytes(raw)
	if err != nil {
		return jErr(fmt.Sprintf("parse error: %v", err))
	}
	return jOK(buildInspectResult(pkt))
}

// J_InspectBytes is the in-memory counterpart of J_Inspect.
// Caller must free result with J_FreeString.
//
//export J_InspectBytes
func J_InspectBytes(data *C.char, length C.int) *C.char {
	raw := C.GoBytes(unsafe.Pointer(data), length)
	parser := packet.NewParser()
	pkt, err := parser.ParseBytes(raw)
	if err != nil {
		return jErr(fmt.Sprintf("parse error: %v", err))
	}
	return jOK(buildInspectResult(pkt))
}

// buildInspectResult gathers metadata from a parsed packet. No decompression.
func buildInspectResult(pkt *packet.DataPacket) jInspectResult {
	// Row count: prefer header RecordsInPart (no decompression needed),
	// fall back to counting actual rows for uncompressed files.
	rowCount := pkt.Header.RecordsInPart
	if rowCount == 0 {
		rowCount = len(pkt.Data.Rows)
	}

	compression := pkt.Data.Compression
	if compression == "" {
		compression = "none"
	}
	checksum := pkt.Data.Checksum
	if checksum == "" {
		checksum = "none"
	}

	res := jInspectResult{
		Table:       pkt.Header.TableName,
		Type:        string(pkt.Header.Type),
		Protocol:    pkt.Protocol,
		Version:     pkt.Version,
		Timestamp:   pkt.Header.Timestamp.UTC().Format("2006-01-02T15:04:05Z"),
		MessageID:   pkt.Header.MessageID,
		FieldsCount: len(pkt.Schema.Fields),
		Schema:      pkt.Schema,
		TotalRows:   rowCount,
		PartNumber:  pkt.Header.PartNumber,
		TotalParts:  pkt.Header.TotalParts,
		Compression: compression,
		Checksum:    checksum,
		Compact:     pkt.Data.Compact,
	}

	if pkt.PipelineContext != nil {
		pc := pkt.PipelineContext
		info := &jPipelineInfo{
			Name:    pc.Pipeline.Name,
			Version: pc.Pipeline.Version,
		}
		if len(pc.Variables) > 0 {
			info.Variables = make(map[string]string, len(pc.Variables))
			for _, v := range pc.Variables {
				info.Variables[v.Name] = v.Value
			}
		}
		res.Pipeline = info
	}

	return res
}
