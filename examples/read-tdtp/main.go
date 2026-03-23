package main

import (
	"context"
	"fmt"
	"os"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/processors"
)

func main() {
	path := "tdtp.xml"
	if len(os.Args) > 1 {
		path = os.Args[1]
	}

	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()

	parser := packet.NewParser()
	pkt, err := parser.ParseWithDecompression(f, func(_ context.Context, compressed string, algo string) ([]string, error) {
		return processors.DecompressDataForTdtpWithAlgo(compressed, algo)
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	cols := make([]string, len(pkt.Schema.Fields))
	for i, f := range pkt.Schema.Fields {
		cols[i] = f.Name
	}

	fmt.Printf("Table: %s | rows: %d\n\n", pkt.Header.TableName, len(pkt.Data.Rows))

	for _, row := range pkt.GetRows() {
		for i, val := range row {
			if i < len(cols) {
				fmt.Printf("  %-15s %s\n", cols[i]+":", val)
			}
		}
		fmt.Println()
	}
}
