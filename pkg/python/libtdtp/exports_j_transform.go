package main

/*
#include <stdlib.h>
*/
import "C"
import (
	"encoding/json"
	"fmt"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/core/schema"
	"github.com/ruslano69/tdtp-framework/pkg/core/tdtql"
	"github.com/ruslano69/tdtp-framework/pkg/merge"
)

// jOrderField is one sort key in the J_Sort request.
type jOrderField struct {
	Field     string `json:"field"`
	Direction string `json:"direction"` // "asc" (default) | "desc"
}

// J_Sort orders data rows by one or more fields.
// orderByJSON: [{"field":"Balance","direction":"desc"},{"field":"Name"}]
// Returns the same schema/header shape as J_read with sorted data.
// Caller must free result with J_FreeString.
//
//export J_Sort
func J_Sort(dataJSON *C.char, orderByJSON *C.char) *C.char {
	jp, err := unmarshalJPacket(dataJSON)
	if err != nil {
		return jErr(err.Error())
	}

	var fields []jOrderField
	if s := C.GoString(orderByJSON); s != "" && s != "null" {
		if err := json.Unmarshal([]byte(s), &fields); err != nil {
			return jErr(fmt.Sprintf("invalid options JSON: %v", err))
		}
	}
	if len(fields) == 0 {
		return jErr("invalid options JSON: order-by list is empty")
	}

	orderBy := &packet.OrderBy{}
	if len(fields) == 1 {
		orderBy.Field = fields[0].Field
		orderBy.Direction = normalizeDirection(fields[0].Direction)
	} else {
		orderBy.Fields = make([]packet.OrderField, len(fields))
		for i, f := range fields {
			orderBy.Fields[i] = packet.OrderField{
				Name:      f.Field,
				Direction: normalizeDirection(f.Direction),
			}
		}
	}

	sorter := tdtql.NewSorter()
	sorted, err := sorter.Sort(jp.Data, orderBy, jp.Schema, schema.NewConverter())
	if err != nil {
		return jErr(fmt.Sprintf("filter error: %v", err))
	}

	result := jp
	result.Data = sorted
	return jOK(result)
}

// normalizeDirection returns the uppercase form the core Sorter expects ("ASC"/"DESC").
func normalizeDirection(d string) string {
	if d == "desc" || d == "DESC" {
		return "DESC"
	}
	return "ASC"
}

// jMergeOptions configures J_Merge.
type jMergeOptions struct {
	Strategy  string   `json:"strategy"` // union|intersection|left|right|append
	KeyFields []string `json:"key_fields"`
}

// jMergeResult is the J_Merge response: merged packet plus merge statistics.
type jMergeResult struct {
	Schema packet.Schema `json:"schema"`
	Header jHeader       `json:"header"`
	Data   [][]string    `json:"data"`
	Stats  jMergeStats   `json:"stats"`
	Error  string        `json:"error,omitempty"`
}

type jMergeStats struct {
	TotalPackets   int `json:"total_packets"`
	TotalRowsIn    int `json:"total_rows_in"`
	TotalRowsOut   int `json:"total_rows_out"`
	Duplicates     int `json:"duplicates"`
	ConflictsCount int `json:"conflicts"`
}

// J_Merge combines multiple TDTP datasets into one.
// packetsJSON: [ <jPacket>, <jPacket>, ... ]  (array of datasets)
// optionsJSON: {"strategy":"union","key_fields":["ID"]}
//
//	strategy: "union" (default, dedup by key) | "intersection" | "left" |
//	          "right" | "append" (no dedup)
//
// Returns merged {schema, header, data, stats}.
// Caller must free result with J_FreeString.
//
//export J_Merge
func J_Merge(packetsJSON *C.char, optionsJSON *C.char) *C.char {
	var jps []jPacket
	if err := json.Unmarshal([]byte(C.GoString(packetsJSON)), &jps); err != nil {
		return jErr(fmt.Sprintf("invalid data JSON: %v", err))
	}
	if len(jps) == 0 {
		return jErr("invalid data JSON: no packets to merge")
	}

	var opts jMergeOptions
	if s := C.GoString(optionsJSON); s != "" && s != "null" {
		if err := json.Unmarshal([]byte(s), &opts); err != nil {
			return jErr(fmt.Sprintf("invalid options JSON: %v", err))
		}
	}

	strategy, err := parseMergeStrategy(opts.Strategy)
	if err != nil {
		return jErr(fmt.Sprintf("invalid options JSON: %v", err))
	}

	pkts := make([]*packet.DataPacket, len(jps))
	for i := range jps {
		pkts[i] = jPacketToDataPacket(jps[i])
	}

	merger := merge.NewMerger(merge.MergeOptions{
		Strategy:  strategy,
		KeyFields: opts.KeyFields,
	})
	res, err := merger.Merge(pkts...)
	if err != nil {
		return jErr(fmt.Sprintf("merge error: %v", err))
	}

	out := packetToJPacket(res.Packet, res.Packet.GetRows())
	return jOK(jMergeResult{
		Schema: out.Schema,
		Header: out.Header,
		Data:   out.Data,
		Stats: jMergeStats{
			TotalPackets:   res.Stats.TotalPackets,
			TotalRowsIn:    res.Stats.TotalRowsIn,
			TotalRowsOut:   res.Stats.TotalRowsOut,
			Duplicates:     res.Stats.Duplicates,
			ConflictsCount: res.Stats.ConflictsCount,
		},
	})
}

func parseMergeStrategy(s string) (merge.MergeStrategy, error) {
	switch s {
	case "", "union":
		return merge.StrategyUnion, nil
	case "intersection":
		return merge.StrategyIntersection, nil
	case "left", "left_priority":
		return merge.StrategyLeftPriority, nil
	case "right", "right_priority":
		return merge.StrategyRightPriority, nil
	case "append":
		return merge.StrategyAppend, nil
	default:
		return merge.StrategyUnion, fmt.Errorf("unknown merge strategy %q", s)
	}
}
