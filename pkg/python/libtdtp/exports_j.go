package main

/*
#include <stdlib.h>
*/
import "C"
import (
	"encoding/json"
	"fmt"
	"unsafe"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/core/tdtql"
	"github.com/ruslano69/tdtp-framework/pkg/diff"
)

// ---------------------------------------------------------------------------
// Internal types — canonical JSON shape shared by all J_* functions
// ---------------------------------------------------------------------------

// jPacket is the canonical JSON representation for J_* I/O.
type jPacket struct {
	Schema packet.Schema `json:"schema"`
	Header jHeader       `json:"header"`
	Data   [][]string    `json:"data"`
	Error  string        `json:"error,omitempty"`
}

// jHeader is a JSON-friendly mirror of packet.Header (time.Time → string).
type jHeader struct {
	Type          string `json:"type"`
	TableName     string `json:"table_name"`
	MessageID     string `json:"message_id"`
	InReplyTo     string `json:"in_reply_to,omitempty"`
	PartNumber    int    `json:"part_number,omitempty"`
	TotalParts    int    `json:"total_parts,omitempty"`
	RecordsInPart int    `json:"records_in_part,omitempty"`
	Timestamp     string `json:"timestamp"`
	Sender        string `json:"sender,omitempty"`
	Recipient     string `json:"recipient,omitempty"`
}

// jQueryContext carries stateless pagination metadata returned by J_FilterRowsPage.
type jQueryContext struct {
	TotalRecords    int  `json:"total_records"`
	MatchedRecords  int  `json:"matched_records"`
	ReturnedRecords int  `json:"returned_records"`
	MoreAvailable   bool `json:"more_available"`
	NextOffset      int  `json:"next_offset,omitempty"`
	Limit           int  `json:"limit"`
	Offset          int  `json:"offset"`
}

// jFilterResult is the response shape of J_FilterRowsPage.
// Extends jPacket with optional QueryContext for pagination metadata.
type jFilterResult struct {
	Schema       packet.Schema  `json:"schema"`
	Header       jHeader        `json:"header"`
	Data         [][]string     `json:"data"`
	QueryContext *jQueryContext `json:"query_context,omitempty"`
	Error        string         `json:"error,omitempty"`
}

// jDiffResult mirrors diff.DiffResult for JSON output.
type jDiffResult struct {
	Added    [][]string     `json:"added"`
	Removed  [][]string     `json:"removed"`
	Modified []jModifiedRow `json:"modified"`
	Stats    jDiffStats     `json:"stats"`
	Error    string         `json:"error,omitempty"`
}

type jModifiedRow struct {
	Key     string               `json:"key"`
	OldRow  []string             `json:"old_row"`
	NewRow  []string             `json:"new_row"`
	Changes map[int]jFieldChange `json:"changes"`
}

type jFieldChange struct {
	FieldName string `json:"field_name"`
	OldValue  string `json:"old_value"`
	NewValue  string `json:"new_value"`
}

type jDiffStats struct {
	TotalInA  int `json:"total_in_a"`
	TotalInB  int `json:"total_in_b"`
	Added     int `json:"added"`
	Removed   int `json:"removed"`
	Modified  int `json:"modified"`
	Unchanged int `json:"unchanged"`
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func jOK(v any) *C.char {
	b, _ := json.Marshal(v)
	return C.CString(string(b))
}

func jErr(msg string) *C.char {
	b, _ := json.Marshal(map[string]string{"error": msg})
	return C.CString(string(b))
}

func packetToJPacket(pkt *packet.DataPacket, rows [][]string) jPacket {
	return jPacket{
		Schema: pkt.Schema,
		Header: jHeader{
			Type:          string(pkt.Header.Type),
			TableName:     pkt.Header.TableName,
			MessageID:     pkt.Header.MessageID,
			InReplyTo:     pkt.Header.InReplyTo,
			PartNumber:    pkt.Header.PartNumber,
			TotalParts:    pkt.Header.TotalParts,
			RecordsInPart: pkt.Header.RecordsInPart,
			Timestamp:     pkt.Header.Timestamp.UTC().Format("2006-01-02T15:04:05Z"),
			Sender:        pkt.Header.Sender,
			Recipient:     pkt.Header.Recipient,
		},
		Data: rows,
	}
}

func jPacketToDataPacket(jp jPacket) *packet.DataPacket {
	pkt := packet.NewDataPacket(packet.MessageType(jp.Header.Type), jp.Header.TableName)
	pkt.Header.MessageID = jp.Header.MessageID
	pkt.Header.InReplyTo = jp.Header.InReplyTo
	pkt.Header.PartNumber = jp.Header.PartNumber
	pkt.Header.TotalParts = jp.Header.TotalParts
	pkt.Header.RecordsInPart = jp.Header.RecordsInPart
	pkt.Header.Sender = jp.Header.Sender
	pkt.Header.Recipient = jp.Header.Recipient
	pkt.Schema = jp.Schema
	pkt.Data = packet.RowsToData(jp.Data)
	return pkt
}

func unmarshalJPacket(raw *C.char) (jPacket, error) {
	var jp jPacket
	if err := json.Unmarshal([]byte(C.GoString(raw)), &jp); err != nil {
		return jPacket{}, fmt.Errorf("invalid data JSON: %w", err)
	}
	return jp, nil
}

// ---------------------------------------------------------------------------
// Memory management
// ---------------------------------------------------------------------------

// J_FreeString releases a *C.char returned by any J_* function.
//
//export J_FreeString
func J_FreeString(s *C.char) {
	C.free(unsafe.Pointer(s))
}

// ---------------------------------------------------------------------------
// Version
// ---------------------------------------------------------------------------

// J_GetVersion returns the library version as a plain C string.
// Caller must free with J_FreeString.
//
//export J_GetVersion
func J_GetVersion() *C.char {
	return C.CString("1.6.0")
}

// ---------------------------------------------------------------------------
// I/O
// ---------------------------------------------------------------------------

// J_ReadFile parses a TDTP file and returns its contents as JSON.
// Compressed files (zstd) are handled via jDecompressRows (exports_j_compress.go).
// Caller must free result with J_FreeString.
//
//export J_ReadFile
func J_ReadFile(path *C.char) *C.char {
	parser := packet.NewParser()
	pkt, err := parser.ParseFile(C.GoString(path))
	if err != nil {
		return jErr(fmt.Sprintf("parse error: %v", err))
	}

	if pkt.Data.Compression != "" {
		// Decompression delegated to exports_j_compress.go
		return jDecompressRows(pkt)
	}

	return jOK(packetToJPacket(pkt, pkt.GetRows()))
}

// J_WriteFile generates a TDTP file from JSON data and writes it to path.
// Returns {"ok":true} or {"error":"..."}.
// Caller must free result with J_FreeString.
//
//export J_WriteFile
func J_WriteFile(dataJSON *C.char, path *C.char) *C.char {
	jp, err := unmarshalJPacket(dataJSON)
	if err != nil {
		return jErr(err.Error())
	}

	pkt := jPacketToDataPacket(jp)
	gen := packet.NewGenerator()
	if err := gen.WriteToFile(pkt, C.GoString(path)); err != nil {
		return jErr(fmt.Sprintf("write error: %v", err))
	}

	return jOK(map[string]bool{"ok": true})
}

// ---------------------------------------------------------------------------
// TDTQL filtering
// ---------------------------------------------------------------------------

// J_FilterRows applies a TDTQL WHERE clause to data and returns filtered rows.
// whereClause — TDTQL expression, e.g. "Balance > 1000 AND City = 'Omsk'".
// limit — max rows (0 = unlimited).
// Uses executor.Execute with a full Query so pagination is handled natively in the core.
// Caller must free result with J_FreeString.
//
//export J_FilterRows
func J_FilterRows(dataJSON *C.char, whereClause *C.char, limit C.int) *C.char {
	jp, err := unmarshalJPacket(dataJSON)
	if err != nil {
		return jErr(err.Error())
	}

	translator := tdtql.NewTranslator()
	filters, err := translator.TranslateWhere(C.GoString(whereClause))
	if err != nil {
		return jErr(fmt.Sprintf("invalid WHERE clause: %v", err))
	}

	query := packet.NewQuery()
	query.Filters = filters
	query.Limit = int(limit)

	executor := tdtql.NewExecutor()
	execResult, err := executor.Execute(query, jp.Data, jp.Schema)
	if err != nil {
		return jErr(fmt.Sprintf("filter error: %v", err))
	}

	result := jp
	result.Data = execResult.FilteredRows
	return jOK(result)
}

// J_FilterRowsPage applies a TDTQL WHERE clause with full pagination support.
// whereClause — TDTQL expression, e.g. "Balance > 1000 AND City = 'Omsk'".
// limit  — max rows per page (0 = unlimited).
// offset — number of matched rows to skip before returning results.
// Returns the same schema/header/data shape as J_FilterRows plus a "query_context"
// object with stateless pagination metadata so the caller knows whether more pages exist.
// Caller must free result with J_FreeString.
//
//export J_FilterRowsPage
func J_FilterRowsPage(dataJSON *C.char, whereClause *C.char, limit C.int, offset C.int) *C.char {
	jp, err := unmarshalJPacket(dataJSON)
	if err != nil {
		return jErr(err.Error())
	}

	translator := tdtql.NewTranslator()
	filters, err := translator.TranslateWhere(C.GoString(whereClause))
	if err != nil {
		return jErr(fmt.Sprintf("invalid WHERE clause: %v", err))
	}

	query := packet.NewQuery()
	query.Filters = filters
	query.Limit = int(limit)
	query.Offset = int(offset)

	executor := tdtql.NewExecutor()
	execResult, err := executor.Execute(query, jp.Data, jp.Schema)
	if err != nil {
		return jErr(fmt.Sprintf("filter error: %v", err))
	}

	qc := &jQueryContext{
		TotalRecords:    execResult.TotalRows,
		MatchedRecords:  execResult.MatchedRows,
		ReturnedRecords: execResult.ReturnedRows,
		MoreAvailable:   execResult.MoreAvailable,
		NextOffset:      execResult.NextOffset,
		Limit:           int(limit),
		Offset:          int(offset),
	}

	return jOK(jFilterResult{
		Schema:       jp.Schema,
		Header:       jp.Header,
		Data:         execResult.FilteredRows,
		QueryContext: qc,
	})
}

// ---------------------------------------------------------------------------
// Processors — delegated to exports_j_processors.go
// ---------------------------------------------------------------------------

// J_ApplyProcessor runs a single named processor over data.
// procType: "field_masker" | "field_normalizer" | "field_validator" |
//
//	"compress" | "decompress"
//
// configJSON: processor-specific JSON config object.
// Caller must free result with J_FreeString.
//
//export J_ApplyProcessor
func J_ApplyProcessor(dataJSON *C.char, procType *C.char, configJSON *C.char) *C.char {
	return jApplyProcessor(dataJSON, procType, configJSON)
}

// J_ApplyChain runs an ordered chain of processors.
// chainJSON: [{"type":"field_masker","params":{...}}, ...]
// Caller must free result with J_FreeString.
//
//export J_ApplyChain
func J_ApplyChain(dataJSON *C.char, chainJSON *C.char) *C.char {
	return jApplyChain(dataJSON, chainJSON)
}

// ---------------------------------------------------------------------------
// Diff
// ---------------------------------------------------------------------------

// J_Diff computes the difference between two TDTP datasets.
// Returns {"added":[...],"removed":[...],"modified":[...],"stats":{...}}.
// Caller must free result with J_FreeString.
//
//export J_Diff
func J_Diff(oldJSON *C.char, newJSON *C.char) *C.char {
	jpOld, err := unmarshalJPacket(oldJSON)
	if err != nil {
		return jErr(fmt.Sprintf("old data error: %v", err))
	}
	jpNew, err := unmarshalJPacket(newJSON)
	if err != nil {
		return jErr(fmt.Sprintf("new data error: %v", err))
	}

	pktOld := jPacketToDataPacket(jpOld)
	pktNew := jPacketToDataPacket(jpNew)

	differ := diff.NewDiffer(diff.DiffOptions{})
	result, err := differ.Compare(pktOld, pktNew)
	if err != nil {
		return jErr(fmt.Sprintf("diff error: %v", err))
	}

	modified := make([]jModifiedRow, 0, len(result.Modified))
	for _, m := range result.Modified {
		changes := make(map[int]jFieldChange, len(m.Changes))
		for idx, ch := range m.Changes {
			changes[idx] = jFieldChange{
				FieldName: ch.FieldName,
				OldValue:  ch.OldValue,
				NewValue:  ch.NewValue,
			}
		}
		modified = append(modified, jModifiedRow{
			Key:     m.Key,
			OldRow:  m.OldRow,
			NewRow:  m.NewRow,
			Changes: changes,
		})
	}

	added := result.Added
	if added == nil {
		added = [][]string{}
	}
	removed := result.Removed
	if removed == nil {
		removed = [][]string{}
	}
	if modified == nil {
		modified = []jModifiedRow{}
	}

	return jOK(jDiffResult{
		Added:    added,
		Removed:  removed,
		Modified: modified,
		Stats: jDiffStats{
			TotalInA:  result.Stats.TotalInA,
			TotalInB:  result.Stats.TotalInB,
			Added:     result.Stats.AddedCount,
			Removed:   result.Stats.RemovedCount,
			Modified:  result.Stats.ModifiedCount,
			Unchanged: result.Stats.UnchangedCount,
		},
	})
}
