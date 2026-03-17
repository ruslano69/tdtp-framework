package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/storage"
)

// InspectFile reads a TDTP XML file (local or s3://) and prints a clean YAML summary
// suitable for LLM/agent consumption. storageCfg may be nil for local files.
func InspectFile(ctx context.Context, inputFile string, storageCfg *storage.Config) error {
	var data []byte
	var err error

	if storage.IsRemote(inputFile) && storageCfg != nil {
		_, uriBucket, key, _ := storage.ParseURI(inputFile)
		cfg := *storageCfg
		if uriBucket != "" {
			cfg.S3.Bucket = uriBucket
		}
		store, openErr := storage.New(cfg)
		if openErr != nil {
			return fmt.Errorf("failed to open storage: %w", openErr)
		}
		defer store.Close()
		rc, getErr := store.Get(ctx, key)
		if getErr != nil {
			return fmt.Errorf("failed to get s3 object %s: %w", key, getErr)
		}
		defer rc.Close()
		data, err = io.ReadAll(rc)
		if err != nil {
			return fmt.Errorf("failed to read s3 object: %w", err)
		}
	} else {
		data, err = os.ReadFile(inputFile)
		if err != nil {
			return fmt.Errorf("failed to read file: %w", err)
		}
	}

	parser := packet.NewParser()
	pkt, err := parser.ParseBytes(data)
	if err != nil {
		return fmt.Errorf("failed to parse TDTP packet: %w", err)
	}

	// Row count: prefer header RecordsInPart (no decompression needed),
	// fall back to counting actual rows for uncompressed files.
	rowCount := pkt.Header.RecordsInPart
	if rowCount == 0 {
		rowCount = len(pkt.Data.Rows)
	}

	// Compression
	compress := pkt.Data.Compression
	if compress == "" {
		compress = "none"
	}

	// Checksum (integrity)
	checksum := pkt.Data.Checksum
	if checksum == "" {
		checksum = "none"
	}

	// Multipart info
	parts := ""
	if pkt.Header.TotalParts > 1 {
		parts = fmt.Sprintf("%d/%d", pkt.Header.PartNumber, pkt.Header.TotalParts)
	} else {
		parts = "1/1"
	}

	// Filter summary from embedded Query
	filter := formatInspectFilter(pkt)

	// Special/masked values detection
	specialValues := detectSpecialValues(pkt)

	// --- YAML output ---
	fmt.Printf("table: %s\n", pkt.Header.TableName)
	fmt.Printf("type: %s\n", pkt.Header.Type)
	fmt.Printf("protocol: %s %s\n", pkt.Protocol, pkt.Version)
	fmt.Printf("timestamp: %s\n", pkt.Header.Timestamp.UTC().Format("2006-01-02T15:04:05Z"))
	fmt.Printf("fields_count: %d\n", len(pkt.Schema.Fields))
	fmt.Println("fields:")
	for _, f := range pkt.Schema.Fields {
		attrs := buildFieldAttrs(f)
		fmt.Printf("  - name: %-24s type: %-12s%s\n", f.Name, f.Type, attrs)
	}
	fmt.Printf("total_rows: %d\n", rowCount)
	fmt.Printf("parts: %s\n", parts)
	fmt.Printf("compress: %s\n", compress)
	fmt.Printf("checksum: %s\n", checksum)
	fmt.Printf("filter: %s\n", filter)
	fmt.Printf("special_values: %s\n", specialValues)

	return nil
}

// buildFieldAttrs returns inline YAML attributes for a field (key, subtype, length, precision/scale, readonly).
func buildFieldAttrs(f packet.Field) string {
	var parts []string
	if f.Key {
		parts = append(parts, "key: true")
	}
	if f.ReadOnly {
		parts = append(parts, "readonly: true")
	}
	if f.Subtype != "" {
		parts = append(parts, "subtype: "+f.Subtype)
	}
	if f.Length > 0 {
		parts = append(parts, fmt.Sprintf("length: %d", f.Length))
	}
	if f.Precision > 0 {
		parts = append(parts, fmt.Sprintf("precision: %d scale: %d", f.Precision, f.Scale))
	}
	if len(parts) == 0 {
		return ""
	}
	return "  # " + strings.Join(parts, ", ")
}

// formatInspectFilter summarises the embedded TDTQL query if present.
func formatInspectFilter(pkt *packet.DataPacket) string {
	// Check QueryContext first (response packets with execution results)
	if pkt.QueryContext != nil {
		res := pkt.QueryContext.ExecutionResults
		q := pkt.QueryContext.OriginalQuery
		parts := []string{}
		if q.Filters != nil {
			parts = append(parts, "where: yes")
		}
		if q.Limit > 0 {
			parts = append(parts, fmt.Sprintf("limit: %d", q.Limit))
		}
		if q.Offset > 0 {
			parts = append(parts, fmt.Sprintf("offset: %d", q.Offset))
		}
		if res.TotalRecordsInTable > 0 {
			parts = append(parts, fmt.Sprintf("total_in_table: %d", res.TotalRecordsInTable))
		}
		if len(parts) > 0 {
			return "{" + strings.Join(parts, ", ") + "}"
		}
	}
	// Check direct Query (request packets)
	if pkt.Query != nil {
		parts := []string{}
		if pkt.Query.Filters != nil {
			parts = append(parts, "where: yes")
		}
		if pkt.Query.Limit > 0 {
			parts = append(parts, fmt.Sprintf("limit: %d", pkt.Query.Limit))
		}
		if pkt.Query.Offset > 0 {
			parts = append(parts, fmt.Sprintf("offset: %d", pkt.Query.Offset))
		}
		if len(parts) > 0 {
			return "{" + strings.Join(parts, ", ") + "}"
		}
	}
	return "none"
}

// detectSpecialValues reads SpecialValues markers from the schema (v1.3.1).
// Schema is in the XML header and always available — even for compressed packets.
func detectSpecialValues(pkt *packet.DataPacket) string {
	var found []string
	seen := make(map[string]bool)

	add := func(s string) {
		if !seen[s] {
			seen[s] = true
			found = append(found, s)
		}
	}

	for _, f := range pkt.Schema.Fields {
		sv := f.SpecialValues
		if sv == nil {
			continue
		}
		if sv.Null != nil {
			add("nulls")
		}
		if sv.Infinity != nil {
			add("+inf")
		}
		if sv.NegInfinity != nil {
			add("-inf")
		}
		if sv.NaN != nil {
			add("nan")
		}
		if sv.NoDate != nil {
			add("no_date")
		}
	}

	if len(found) == 0 {
		return "none"
	}
	return strings.Join(found, ", ")
}
