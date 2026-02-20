package commands

import (
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// HTMLOptions holds options for TDTP → HTML conversion
type HTMLOptions struct {
	InputFile   string
	OutputFile  string // if empty, auto-generated next to input file
	OpenBrowser bool   // open result in default browser
	Limit       int    // max rows to render (0 = all)
	RowStart    int    // first row to render, 1-indexed (0 = from beginning)
	RowEnd      int    // last row to render, 1-indexed inclusive (0 = to end)
}

// ConvertTDTPToHTML converts a TDTP XML file to a beautiful standalone HTML page.
// Export is significantly faster than CSV because TDTP carries schema metadata,
// so the viewer renders correct column types without guessing.
func ConvertTDTPToHTML(opts HTMLOptions) error {
	// Read input file
	data, err := os.ReadFile(opts.InputFile)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	// Parse TDTP packet
	parser := packet.NewParser()
	pkt, err := parser.ParseBytes(data)
	if err != nil {
		return fmt.Errorf("failed to parse TDTP packet: %w", err)
	}

	// Decompress if needed
	if pkt.Data.Compression != "" {
		fmt.Printf("  Decompressing (%s)...\n", pkt.Data.Compression)
		if err := decompressPacketData(pkt); err != nil {
			return fmt.Errorf("decompression failed: %w", err)
		}
	}

	// Determine output file
	outputFile := opts.OutputFile
	if outputFile == "" {
		base := strings.TrimSuffix(opts.InputFile, filepath.Ext(opts.InputFile))
		outputFile = base + ".html"
	}

	// Generate HTML
	htmlContent, renderedRows := renderHTML(opts.InputFile, pkt, opts)

	// Write output
	if err := os.WriteFile(outputFile, []byte(htmlContent), 0o644); err != nil {
		return fmt.Errorf("failed to write HTML file: %w", err)
	}

	totalRows := len(pkt.Data.Rows)
	fmt.Printf("HTML viewer: %s\n", outputFile)
	fmt.Printf("  Table:  %s\n", pkt.Header.TableName)
	fmt.Printf("  Type:   %s\n", pkt.Header.Type)
	fmt.Printf("  Fields: %d\n", len(pkt.Schema.Fields))
	if renderedRows < totalRows {
		fmt.Printf("  Rows:   %d / %d\n", renderedRows, totalRows)
	} else {
		fmt.Printf("  Rows:   %d\n", totalRows)
	}

	if opts.OpenBrowser {
		openInBrowser(outputFile)
	}

	return nil
}

// renderHTML generates a complete standalone HTML page for the TDTP packet.
// Returns the HTML string and the number of rows actually rendered.
func renderHTML(inputFile string, pkt *packet.DataPacket, opts HTMLOptions) (string, int) {
	var b strings.Builder

	// Parse all rows
	p := packet.NewParser()
	allRows := make([][]string, 0, len(pkt.Data.Rows))
	for _, row := range pkt.Data.Rows {
		allRows = append(allRows, p.GetRowValues(row))
	}
	totalRows := len(allRows)

	// Apply --row range (1-indexed, inclusive)
	startIdx := 0
	endIdx := totalRows
	if opts.RowStart > 0 {
		startIdx = opts.RowStart - 1
		if startIdx > totalRows {
			startIdx = totalRows
		}
	}
	if opts.RowEnd > 0 {
		endIdx = opts.RowEnd
		if endIdx > totalRows {
			endIdx = totalRows
		}
	}

	// Validate range (prevent panic on reverse range like --row 100-50)
	if endIdx < startIdx {
		endIdx = startIdx
	}

	// Apply --limit
	if opts.Limit > 0 {
		// Positive: first N rows from range
		if (endIdx - startIdx) > opts.Limit {
			endIdx = startIdx + opts.Limit
		}
	} else if opts.Limit < 0 {
		// Negative: last N rows from range (like tail -n)
		rowCount := endIdx - startIdx
		wantedRows := -opts.Limit
		if rowCount > wantedRows {
			startIdx = endIdx - wantedRows
		}
	}

	parsedRows := allRows[startIdx:endIdx]
	displayOffset := startIdx // used for actual row numbers in the table

	b.WriteString(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>TDTP Viewer - `)
	b.WriteString(html.EscapeString(pkt.Header.TableName))
	b.WriteString(`</title>
<style>
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body {
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
    background: #0f1117;
    color: #e2e8f0;
    min-height: 100vh;
    padding: 24px;
  }
  .container { max-width: 1600px; margin: 0 auto; }
  .badge {
    display: inline-flex; align-items: center; gap: 6px;
    padding: 4px 10px; border-radius: 20px; font-size: 12px; font-weight: 600;
  }
  .badge-reference { background: #1e3a5f; color: #60a5fa; }
  .badge-response  { background: #1a3a2a; color: #34d399; }
  .badge-request   { background: #3a2a1a; color: #fb923c; }
  .badge-alarm     { background: #3a1a1a; color: #f87171; }
  .badge-key       { background: #2d1b69; color: #a78bfa; }
  .badge-type      { background: #1e293b; color: #94a3b8; font-family: monospace; }
  /* Header card */
  .header-card {
    background: linear-gradient(135deg, #1e293b 0%, #0f172a 100%);
    border: 1px solid #334155;
    border-radius: 12px;
    padding: 24px 28px;
    margin-bottom: 20px;
  }
  .header-top { display: flex; align-items: center; gap: 16px; flex-wrap: wrap; margin-bottom: 16px; }
  .table-name { font-size: 26px; font-weight: 700; color: #f1f5f9; }
  .meta-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
    gap: 12px;
  }
  .meta-item { display: flex; flex-direction: column; gap: 2px; }
  .meta-label { font-size: 11px; font-weight: 600; color: #64748b; text-transform: uppercase; letter-spacing: 0.05em; }
  .meta-value { font-size: 13px; color: #cbd5e1; font-family: monospace; word-break: break-all; }
  /* Schema card */
  .card {
    background: #1e293b;
    border: 1px solid #334155;
    border-radius: 12px;
    margin-bottom: 20px;
    overflow: hidden;
  }
  .card-header {
    padding: 14px 20px;
    border-bottom: 1px solid #334155;
    font-size: 14px; font-weight: 600; color: #94a3b8;
    display: flex; align-items: center; gap: 10px;
    background: #0f172a;
  }
  .pill {
    background: #334155; color: #94a3b8;
    padding: 2px 8px; border-radius: 10px; font-size: 11px; font-weight: 600;
  }
  /* Schema table */
  .schema-table { width: 100%; border-collapse: collapse; }
  .schema-table th {
    padding: 10px 16px; text-align: left;
    font-size: 11px; font-weight: 600; color: #475569; text-transform: uppercase; letter-spacing: 0.05em;
    border-bottom: 1px solid #334155; background: #0f172a;
  }
  .schema-table td {
    padding: 9px 16px; font-size: 13px;
    border-bottom: 1px solid #1e293b;
  }
  .schema-table tr:last-child td { border-bottom: none; }
  .schema-table tr:hover td { background: #263244; }
  .field-name { font-family: monospace; font-weight: 600; color: #e2e8f0; }
  /* Data table */
  .data-wrapper { overflow-x: auto; }
  .data-table { width: 100%; border-collapse: collapse; font-size: 13px; }
  .data-table th {
    padding: 10px 14px; text-align: left;
    font-size: 11px; font-weight: 600; color: #475569; text-transform: uppercase; letter-spacing: 0.04em;
    border-bottom: 2px solid #334155; background: #0f172a;
    white-space: nowrap; position: sticky; top: 0; z-index: 10;
  }
  .data-table th.key-col { color: #a78bfa; }
  .data-table td {
    padding: 8px 14px;
    border-bottom: 1px solid #1e293b;
    font-family: monospace; color: #cbd5e1;
    max-width: 320px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  .data-table tr:hover td { background: #1e2d42; cursor: default; }
  .data-table tr:nth-child(even) td { background: #18222f; }
  .data-table tr:nth-child(even):hover td { background: #1e2d42; }
  .null-val { color: #475569; font-style: italic; }
  .num-val  { color: #60a5fa; text-align: right; }
  .bool-true  { color: #34d399; }
  .bool-false { color: #f87171; }
  .row-num { color: #475569; text-align: right; user-select: none; font-size: 11px; }
  /* Stats bar */
  .stats-bar {
    display: flex; gap: 24px; flex-wrap: wrap;
    padding: 12px 20px; background: #0f172a;
    border-top: 1px solid #334155;
    font-size: 12px; color: #64748b;
  }
  .stats-bar span { display: flex; align-items: center; gap: 6px; }
  .stats-bar strong { color: #94a3b8; }
  /* Footer */
  .footer {
    text-align: center; padding: 20px;
    font-size: 11px; color: #334155;
  }
  .footer a { color: #475569; text-decoration: none; }
  @media (max-width: 600px) {
    body { padding: 12px; }
    .table-name { font-size: 20px; }
  }
</style>
</head>
<body>
<div class="container">
`)

	// --- Header card ---
	msgType := string(pkt.Header.Type)
	badgeClass := "badge-reference"
	switch pkt.Header.Type {
	case packet.TypeResponse:
		badgeClass = "badge-response"
	case packet.TypeRequest:
		badgeClass = "badge-request"
	case packet.TypeAlarm:
		badgeClass = "badge-alarm"
	}

	b.WriteString(`<div class="header-card">`)
	b.WriteString(`<div class="header-top">`)
	b.WriteString(`<span class="table-name">`)
	b.WriteString(html.EscapeString(pkt.Header.TableName))
	b.WriteString(`</span>`)
	b.WriteString(`<span class="badge ` + badgeClass + `">` + html.EscapeString(strings.ToUpper(msgType)) + `</span>`)
	if pkt.Header.PartNumber > 0 && pkt.Header.TotalParts > 1 {
		b.WriteString(fmt.Sprintf(`<span class="badge badge-type">part %d / %d</span>`,
			pkt.Header.PartNumber, pkt.Header.TotalParts))
	}
	b.WriteString(`</div>`) // header-top

	b.WriteString(`<div class="meta-grid">`)
	writeMetaItem(&b, "Message ID", pkt.Header.MessageID)
	writeMetaItem(&b, "Timestamp", pkt.Header.Timestamp.Format(time.RFC3339))
	if pkt.Header.Sender != "" {
		writeMetaItem(&b, "Sender", pkt.Header.Sender)
	}
	if pkt.Header.Recipient != "" {
		writeMetaItem(&b, "Recipient", pkt.Header.Recipient)
	}
	if pkt.Header.InReplyTo != "" {
		writeMetaItem(&b, "In Reply To", pkt.Header.InReplyTo)
	}
	writeMetaItem(&b, "Protocol", pkt.Protocol+" v"+pkt.Version)
	writeMetaItem(&b, "Source File", filepath.Base(inputFile))
	if pkt.Data.Compression != "" {
		writeMetaItem(&b, "Compression", pkt.Data.Compression)
	}
	b.WriteString(`</div>`) // meta-grid
	b.WriteString(`</div>`) // header-card

	// --- Schema card ---
	b.WriteString(`<div class="card">`)
	b.WriteString(fmt.Sprintf(`<div class="card-header">Schema <span class="pill">%d fields</span></div>`, len(pkt.Schema.Fields)))
	b.WriteString(`<table class="schema-table">`)
	b.WriteString(`<thead><tr>`)
	b.WriteString(`<th>#</th><th>Field Name</th><th>Type</th><th>Attributes</th>`)
	b.WriteString(`</tr></thead><tbody>`)

	for i, field := range pkt.Schema.Fields {
		b.WriteString(`<tr>`)
		b.WriteString(fmt.Sprintf(`<td class="row-num">%d</td>`, i+1))
		b.WriteString(`<td class="field-name">`)
		b.WriteString(html.EscapeString(field.Name))
		b.WriteString(`</td>`)

		// Type display
		typeStr := strings.ToUpper(field.Type)
		if field.Length > 0 && field.Length != -1 {
			typeStr += fmt.Sprintf("(%d)", field.Length)
		} else if field.Precision > 0 {
			if field.Scale > 0 {
				typeStr += fmt.Sprintf("(%d,%d)", field.Precision, field.Scale)
			} else {
				typeStr += fmt.Sprintf("(%d)", field.Precision)
			}
		}
		if field.Subtype != "" {
			typeStr += " / " + field.Subtype
		}
		b.WriteString(`<td><span class="badge badge-type">` + html.EscapeString(typeStr) + `</span></td>`)

		// Attributes
		b.WriteString(`<td>`)
		if field.Key {
			b.WriteString(`<span class="badge badge-key">PK</span> `)
		}
		if field.ReadOnly {
			b.WriteString(`<span class="badge badge-type">readonly</span> `)
		}
		if field.Timezone != "" {
			b.WriteString(`<span class="badge badge-type">tz:` + html.EscapeString(field.Timezone) + `</span>`)
		}
		b.WriteString(`</td>`)
		b.WriteString(`</tr>`)
	}

	b.WriteString(`</tbody></table></div>`) // schema card

	// --- Data card ---
	b.WriteString(`<div class="card">`)
	if len(parsedRows) < totalRows {
		b.WriteString(fmt.Sprintf(`<div class="card-header">Data <span class="pill">%d–%d of %d rows</span></div>`,
			displayOffset+1, displayOffset+len(parsedRows), totalRows))
	} else {
		b.WriteString(fmt.Sprintf(`<div class="card-header">Data <span class="pill">%d rows</span></div>`, len(parsedRows)))
	}
	b.WriteString(`<div class="data-wrapper"><table class="data-table"><thead><tr>`)

	// Row number header
	b.WriteString(`<th class="row-num">#</th>`)

	// Column headers
	for _, field := range pkt.Schema.Fields {
		cls := ""
		if field.Key {
			cls = ` class="key-col"`
		}
		label := html.EscapeString(field.Name)
		typeLabel := strings.ToLower(field.Type)
		b.WriteString(fmt.Sprintf(`<th%s>%s<br><small>%s</small></th>`, cls, label, html.EscapeString(typeLabel)))
	}
	b.WriteString(`</tr></thead><tbody>`)

	// Data rows
	for rowIdx, vals := range parsedRows {
		b.WriteString(`<tr>`)
		b.WriteString(fmt.Sprintf(`<td class="row-num">%d</td>`, rowIdx+displayOffset+1))

		for colIdx, val := range vals {
			if colIdx >= len(pkt.Schema.Fields) {
				break
			}
			field := pkt.Schema.Fields[colIdx]

			if val == "" {
				b.WriteString(`<td><span class="null-val">NULL</span></td>`)
				continue
			}

			// Render by type
			switch strings.ToLower(field.Type) {
			case "integer", "decimal", "real":
				b.WriteString(`<td class="num-val">` + html.EscapeString(val) + `</td>`)
			case "boolean":
				cls := "bool-false"
				if val == "1" || strings.EqualFold(val, "true") {
					cls = "bool-true"
				}
				b.WriteString(`<td><span class="` + cls + `">` + html.EscapeString(val) + `</span></td>`)
			case "blob":
				b.WriteString(`<td><span class="null-val">&lt;binary&gt;</span></td>`)
			default:
				b.WriteString(`<td>` + html.EscapeString(val) + `</td>`)
			}
		}
		b.WriteString(`</tr>`)
	}

	b.WriteString(`</tbody></table></div>`) // data-wrapper

	// Stats bar
	keyCount := 0
	for _, f := range pkt.Schema.Fields {
		if f.Key {
			keyCount++
		}
	}
	if len(parsedRows) < totalRows {
		b.WriteString(fmt.Sprintf(`<div class="stats-bar">
  <span>showing rows <strong>%d–%d</strong> of <strong>%d</strong></span>
  <span><strong>%d</strong> columns</span>
  <span><strong>%d</strong> primary key(s)</span>
</div>`, displayOffset+1, displayOffset+len(parsedRows), totalRows, len(pkt.Schema.Fields), keyCount))
	} else {
		b.WriteString(fmt.Sprintf(`<div class="stats-bar">
  <span><strong>%d</strong> rows</span>
  <span><strong>%d</strong> columns</span>
  <span><strong>%d</strong> primary key(s)</span>
</div>`, len(parsedRows), len(pkt.Schema.Fields), keyCount))
	}

	b.WriteString(`</div>`) // data card

	// Footer
	b.WriteString(`<div class="footer">Generated by <a href="https://github.com/ruslano69/tdtp-framework">TDTP Framework</a> &mdash; Table Data Transfer Protocol</div>`)

	b.WriteString(`</div></body></html>`)
	return b.String(), len(parsedRows)
}

// writeMetaItem writes a metadata label+value pair
func writeMetaItem(b *strings.Builder, label, value string) {
	b.WriteString(`<div class="meta-item">`)
	b.WriteString(`<span class="meta-label">` + html.EscapeString(label) + `</span>`)
	b.WriteString(`<span class="meta-value">` + html.EscapeString(value) + `</span>`)
	b.WriteString(`</div>`)
}

// openInBrowser attempts to open a file in the default system browser
func openInBrowser(filePath string) {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		absPath = filePath
	}
	url := "file://" + absPath

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}

	if err := cmd.Start(); err != nil {
		fmt.Printf("  (could not open browser: %v)\n", err)
		fmt.Printf("  Open manually: %s\n", url)
	}
}
