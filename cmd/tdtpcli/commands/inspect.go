package commands

import (
	"fmt"
	"os"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// InspectFile reads a TDTP XML file and prints a clean YAML summary
// suitable for LLM/agent consumption.
func InspectFile(inputFile string) error {
	data, err := os.ReadFile(inputFile)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
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

// detectSpecialValues scans the first 5 rows for masked/redacted patterns.
func detectSpecialValues(pkt *packet.DataPacket) string {
	if pkt.Data.Compression != "" {
		// Data is compressed — can't scan without decompressing; skip
		return "unknown (compressed)"
	}

	rows := pkt.Data.Rows
	if len(rows) > 5 {
		rows = rows[:5]
	}

	combined := ""
	for _, r := range rows {
		combined += r.Value + "|"
	}

	var found []string
	if strings.Contains(combined, "***") {
		found = append(found, "masked(***)")
	}
	if strings.Contains(combined, "XXX") {
		found = append(found, "masked(XXX)")
	}
	if strings.Contains(combined, "[MASKED]") || strings.Contains(combined, "[REDACTED]") {
		found = append(found, "redacted")
	}
	if strings.Contains(combined, "NULL") || strings.Contains(combined, "||") {
		found = append(found, "nulls")
	}

	if len(found) == 0 {
		return "none"
	}
	return strings.Join(found, ", ")
}
