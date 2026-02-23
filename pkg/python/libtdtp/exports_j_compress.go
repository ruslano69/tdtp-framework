//go:build compress

package main

/*
#include <stdlib.h>
*/
import "C"
import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/processors"
)

// jDecompressRows decompresses a zstd packet and returns a jPacket JSON.
// Active when built with: go build -tags compress -buildmode=c-shared
func jDecompressRows(pkt *packet.DataPacket) *C.char {
	if len(pkt.Data.Rows) == 0 {
		return jOK(packetToJPacket(pkt, [][]string{}))
	}

	parser := packet.NewParser()
	lines, err := processors.DecompressDataForTdtp(pkt.Data.Rows[0].Value)
	if err != nil {
		return jErr(fmt.Sprintf("decompress error: %v", err))
	}

	var rows [][]string
	for _, line := range lines {
		if line == "" {
			continue
		}
		rows = append(rows, parser.GetRowValues(packet.Row{Value: line}))
	}

	return jOK(packetToJPacket(pkt, rows))
}

// jApplyProcessor runs a single named processor.
func jApplyProcessor(dataJSON *C.char, procType *C.char, configJSON *C.char) *C.char {
	jp, err := unmarshalJPacket(dataJSON)
	if err != nil {
		return jErr(err.Error())
	}

	var params map[string]any
	if err := json.Unmarshal([]byte(C.GoString(configJSON)), &params); err != nil {
		return jErr(fmt.Sprintf("invalid config JSON: %v", err))
	}

	pType := C.GoString(procType)

	switch pType {
	case "compress":
		return jRunCompress(jp, params)
	case "decompress":
		return jRunDecompress(jp)
	}

	proc, err := processors.CreateProcessor(processors.Config{
		Type:   pType,
		Params: params,
	})
	if err != nil {
		return jErr(fmt.Sprintf("processor error: %v", err))
	}

	processed, err := proc.Process(context.Background(), jp.Data, jp.Schema)
	if err != nil {
		return jErr(fmt.Sprintf("process error: %v", err))
	}

	result := jp
	result.Data = processed
	return jOK(result)
}

// jApplyChain runs an ordered chain of processors.
func jApplyChain(dataJSON *C.char, chainJSON *C.char) *C.char {
	jp, err := unmarshalJPacket(dataJSON)
	if err != nil {
		return jErr(err.Error())
	}

	var configs []processors.Config
	if err := json.Unmarshal([]byte(C.GoString(chainJSON)), &configs); err != nil {
		return jErr(fmt.Sprintf("invalid chain JSON: %v", err))
	}

	chain, err := processors.CreateChainFromConfigs(configs)
	if err != nil {
		return jErr(fmt.Sprintf("chain build error: %v", err))
	}

	processed, err := chain.Process(context.Background(), jp.Data, jp.Schema)
	if err != nil {
		return jErr(fmt.Sprintf("chain process error: %v", err))
	}

	result := jp
	result.Data = processed
	return jOK(result)
}

// jRunCompress compresses rows with zstd, returns single-blob jPacket.
func jRunCompress(jp jPacket, params map[string]any) *C.char {
	level := 3
	if v, ok := params["level"]; ok {
		if fv, ok := v.(float64); ok {
			level = int(fv)
		}
	}

	compressor, err := processors.NewCompressionProcessor(level)
	if err != nil {
		return jErr(fmt.Sprintf("compressor init error: %v", err))
	}
	defer compressor.Close()

	rowBytes := make([]byte, 0, 4096)
	for i, row := range jp.Data {
		for j, v := range row {
			if j > 0 {
				rowBytes = append(rowBytes, '|')
			}
			rowBytes = append(rowBytes, []byte(v)...)
		}
		if i < len(jp.Data)-1 {
			rowBytes = append(rowBytes, '\n')
		}
	}

	compressed, err := compressor.ProcessBlock(context.Background(), rowBytes)
	if err != nil {
		return jErr(fmt.Sprintf("compression error: %v", err))
	}

	result := jp
	result.Data = [][]string{{string(compressed)}}
	return jOK(result)
}

// jRunDecompress decompresses a single-blob jPacket back to rows.
func jRunDecompress(jp jPacket) *C.char {
	if len(jp.Data) == 0 || len(jp.Data[0]) == 0 {
		return jErr("no compressed data found")
	}

	parser := packet.NewParser()
	lines, err := processors.DecompressDataForTdtp(jp.Data[0][0])
	if err != nil {
		return jErr(fmt.Sprintf("decompress error: %v", err))
	}

	var rows [][]string
	for _, line := range lines {
		if line == "" {
			continue
		}
		rows = append(rows, parser.GetRowValues(packet.Row{Value: line}))
	}

	result := jp
	result.Data = rows
	return jOK(result)
}
