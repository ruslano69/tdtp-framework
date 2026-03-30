// bench_raw: minimal SQLite → custom XML export, no framework overhead.
// Build: go build -o /tmp/bench_raw ./cmd/bench_raw/
// Run:   /tmp/bench_raw /path/to/db.sqlite [output.xml]
package main

import (
	"bufio"
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const (
	dbDriver   = "sqlite"
	tableName  = "users"
	batchSize  = 7143            // ~same per-packet as framework (100k/14 packets)
	bufferSize = 4 * 1024 * 1024 // 4MB write buffer
)

// xmlEscape replaces XML special chars without using encoding/xml (avoids reflection).
func xmlEscape(s string) string {
	if !strings.ContainsAny(s, `<>&"'`) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '&':
			b.WriteString("&amp;")
		case '"':
			b.WriteString("&quot;")
		case '\'':
			b.WriteString("&#39;")
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

func run(dbPath, outPath string) error {
	t0 := time.Now()

	// ── Open DB ──────────────────────────────────────────────────────────────
	db, err := sql.Open(dbDriver, dbPath)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer func() { _ = db.Close() }()

	// ── Count rows ───────────────────────────────────────────────────────────
	var total int64
	if err := db.QueryRow("SELECT COUNT(*) FROM " + tableName).Scan(&total); err != nil {
		return fmt.Errorf("count: %w", err)
	}

	// ── Read schema ──────────────────────────────────────────────────────────
	rows, err := db.Query("SELECT * FROM " + tableName + " LIMIT 0")
	if err != nil {
		return fmt.Errorf("schema: %w", err)
	}
	cols, _ := rows.Columns()
	if err := rows.Close(); err != nil {
		return fmt.Errorf("schema close: %w", err)
	}

	tOpen := time.Since(t0)

	// ── Open output ──────────────────────────────────────────────────────────
	out, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("create: %w", err)
	}
	defer func() { _ = out.Close() }()
	w := bufio.NewWriterSize(out, bufferSize)

	// ── Fetch data ───────────────────────────────────────────────────────────
	tScan := time.Now()

	dataRows, err := db.Query("SELECT * FROM " + tableName)
	if err != nil {
		return fmt.Errorf("query: %w", err)
	}
	defer func() { _ = dataRows.Close() }()

	scanBuf := make([]any, len(cols))
	scanPtrs := make([]any, len(cols))
	for i := range scanBuf {
		scanPtrs[i] = &scanBuf[i]
	}

	// ── Write XML ────────────────────────────────────────────────────────────
	w.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	w.WriteString(`<DataPacket sender="bench" recipient="bench" table="` + tableName + `" total="`)
	w.WriteString(strconv.FormatInt(total, 10))
	w.WriteString(`">` + "\n")

	// Schema block
	w.WriteString("  <Schema>\n")
	for _, col := range cols {
		w.WriteString(`    <Field name="`)
		w.WriteString(col)
		w.WriteString(`"/>` + "\n")
	}
	w.WriteString("  </Schema>\n")
	w.WriteString("  <Data>\n")

	var rowCount int64
	var packetCount int

	for dataRows.Next() {
		if err := dataRows.Scan(scanPtrs...); err != nil {
			return fmt.Errorf("scan: %w", err)
		}

		// Batch separator (for multi-packet simulation)
		if rowCount > 0 && rowCount%int64(batchSize) == 0 {
			packetCount++
		}

		w.WriteString("    <Row>")
		for _, val := range scanBuf {
			w.WriteString("<V>")
			if val != nil {
				switch v := val.(type) {
				case int64:
					w.WriteString(strconv.FormatInt(v, 10))
				case float64:
					w.WriteString(strconv.FormatFloat(v, 'g', -1, 64))
				case string:
					w.WriteString(xmlEscape(v))
				case []byte:
					w.WriteString(xmlEscape(string(v)))
				case bool:
					if v {
						w.WriteByte('1')
					} else {
						w.WriteByte('0')
					}
				case time.Time:
					w.WriteString(v.UTC().Format(time.RFC3339))
				default:
					fmt.Fprintf(w, "%v", v)
				}
			}
			w.WriteString("</V>")
		}
		w.WriteString("</Row>\n")
		rowCount++
	}
	if err := dataRows.Err(); err != nil {
		return fmt.Errorf("rows: %w", err)
	}

	w.WriteString("  </Data>\n")
	w.WriteString("</DataPacket>\n")
	if err := w.Flush(); err != nil {
		return fmt.Errorf("flush: %w", err)
	}

	tTotal := time.Since(t0)
	tWrite := time.Since(tScan)

	fi, _ := out.Stat()
	sizeMB := float64(fi.Size()) / 1024 / 1024
	fields := rowCount * int64(len(cols))
	fieldsPerSec := float64(fields) / tTotal.Seconds() / 1e6

	fmt.Printf("rows:        %d\n", rowCount)
	fmt.Printf("columns:     %d\n", len(cols))
	fmt.Printf("fields:      %d (%.1fM)\n", fields, float64(fields)/1e6)
	fmt.Printf("output:      %.1f MB\n", sizeMB)
	fmt.Printf("─────────────────────────\n")
	fmt.Printf("open+schema: %v\n", tOpen)
	fmt.Printf("scan+write:  %v\n", tWrite)
	fmt.Printf("total:       %v\n", tTotal)
	fmt.Printf("─────────────────────────\n")
	fmt.Printf("throughput:  %.2fM fields/sec\n", fieldsPerSec)
	fmt.Printf("output:      %s\n", outPath)
	_ = packetCount
	return nil
}

func main() {
	dbPath := "/home/user/tdtp-framework/benchmark_100k.db"
	outPath := "/tmp/bench_raw_out.xml"
	if len(os.Args) > 1 {
		dbPath = os.Args[1]
	}
	if len(os.Args) > 2 {
		outPath = os.Args[2]
	}
	if err := run(dbPath, outPath); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
