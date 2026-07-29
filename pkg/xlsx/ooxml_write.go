package xlsx

// ooxml_write.go — the minimal XLSX writer that replaced excelize.
//
// An .xlsx file is a ZIP of XML parts. Writing the handful this package needs
// is a few hundred lines of archive/zip and text; excelize cost 11 MB of
// binary for the same 14 calls, 9 MB of which was not its own code but the
// dead-code eliminator giving up: its formula engine reaches
// reflect.Value.MethodByName, and the Go linker then keeps every method of
// every reachable type. Measured: 37.6 MB with excelize 2.11, 26.5 MB without
// any xlsx library at all.
//
// Values are written as inline strings rather than a shared-string table.
// Sharing pays off when the same text repeats often; a database export is
// mostly distinct values, and the table is a second pass over all the data
// plus an index to keep consistent. Excel reads inline strings natively — it
// just rewrites them into a shared table when it saves the file itself, which
// is why the reader has to understand both.

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"
)

// Style indices into the cellXfs list written by styleSheetXML. Excel resolves
// a cell's s="N" attribute against that list by position, so these constants
// and that XML have to move together.
const (
	styleDefault  = 0
	styleHeader   = 1
	styleInteger  = 2
	styleFloat    = 3
	styleDate     = 4
	styleDatetime = 5
	styleText     = 6
)

// cell is one written cell. Exactly one of the value fields is used, chosen by
// the writer; kind decides how it is serialised.
type cell struct {
	ref   string // "A1"
	kind  cellKind
	num   string // already-formatted number for kindNumber
	text  string // raw text for kindInline
	style int
}

type cellKind int

const (
	kindNumber cellKind = iota
	kindInline
)

// sheetBuilder accumulates cells for one worksheet.
type sheetBuilder struct {
	name   string
	rows   map[int][]cell // 1-based row number → cells
	maxRow int
	widths map[string]float64 // column letter → width
}

func newSheet(name string) *sheetBuilder {
	return &sheetBuilder{name: name, rows: map[int][]cell{}, widths: map[string]float64{}}
}

// setNumber stores a numeric cell. The caller has already decided the format;
// Excel keeps numbers as text in the XML and parses them back as float64.
func (s *sheetBuilder) setNumber(col, row int, v string, style int) {
	s.add(row, cell{ref: cellRef(col, row), kind: kindNumber, num: v, style: style})
}

// setString stores a text cell. Text is always inline and always typed as a
// string, which is what keeps a leading =, +, - or @ from being read as a
// formula — the injection trap the tests cover.
func (s *sheetBuilder) setString(col, row int, v string, style int) {
	s.add(row, cell{ref: cellRef(col, row), kind: kindInline, text: v, style: style})
}

func (s *sheetBuilder) add(row int, c cell) {
	s.rows[row] = append(s.rows[row], c)
	if row > s.maxRow {
		s.maxRow = row
	}
}

func (s *sheetBuilder) setColWidth(col int, w float64) {
	s.widths[columnName(col)] = w
}

// cellRef builds an A1-style reference from 1-based coordinates.
func cellRef(col, row int) string {
	return columnName(col) + strconv.Itoa(row)
}

// excelSerial converts a time to an Excel 1900-epoch serial number.
//
// The exact inverse of excelSerialToTime, phantom leap day included: Excel
// believes 1900 was a leap year (inherited from Lotus 1-2-3), so every date
// from Mar 1 1900 onward is shifted one day beyond the naive count. Getting
// this wrong by one is the classic off-by-one that only shows up in dates
// nobody tests.
func excelSerial(t time.Time) float64 {
	t = t.UTC()
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)

	days := int(day.Sub(excel1900Epoch).Hours()/24) + 1 // serial 1 == 1900-01-01
	if !day.Before(time.Date(1900, 3, 1, 0, 0, 0, 0, time.UTC)) {
		days++ // step over the phantom Feb 29
	}

	secs := t.Hour()*3600 + t.Minute()*60 + t.Second()
	return float64(days) + float64(secs)/86400
}

// formatFloat writes a number the way Excel stores them: plain decimal, no
// exponent, no trailing zeros. strconv's 'f' with -1 precision gives the
// shortest form that round-trips back to the same float64.
func formatFloat(v float64) string {
	if v == math.Trunc(v) && math.Abs(v) < 1e15 {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// ─── XML parts ───────────────────────────────────────────────────────────────

const contentTypesXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>
<Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>
<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>
</Types>`

const rootRelsXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>
</Relationships>`

const workbookRelsXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>
<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>
</Relationships>`

// styleSheetXML defines the cellXfs the styleXxx constants index into.
//
// numFmtId values 1, 2, 14, 22 and 49 are OOXML built-ins (integer, two
// decimals, short date, date-time, text), so no numFmts block is needed. The
// two fills at index 0 and 1 are mandatory in that order — the spec reserves
// them, and Excel silently misreads every later fill if they are missing.
const styleSheetXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
<fonts count="2">
<font><sz val="11"/><name val="Calibri"/></font>
<font><b/><sz val="11"/><color rgb="FFFFFFFF"/><name val="Calibri"/></font>
</fonts>
<fills count="3">
<fill><patternFill patternType="none"/></fill>
<fill><patternFill patternType="gray125"/></fill>
<fill><patternFill patternType="solid"><fgColor rgb="FF4472C4"/><bgColor indexed="64"/></patternFill></fill>
</fills>
<borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders>
<cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>
<cellXfs count="7">
<xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/>
<xf numFmtId="0" fontId="1" fillId="2" borderId="0" xfId="0" applyFont="1" applyFill="1" applyAlignment="1"><alignment horizontal="center" vertical="center"/></xf>
<xf numFmtId="1" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>
<xf numFmtId="2" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>
<xf numFmtId="14" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>
<xf numFmtId="22" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>
<xf numFmtId="49" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>
</cellXfs>
<cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles>
</styleSheet>`

func workbookXML(sheetName string) string {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n")
	sb.WriteString(`<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" `)
	sb.WriteString(`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">`)
	sb.WriteString(`<sheets><sheet name="`)
	xmlEscapeAttr(&sb, sanitizeSheetName(sheetName))
	sb.WriteString(`" sheetId="1" r:id="rId1"/></sheets></workbook>`)
	return sb.String()
}

// sanitizeSheetName enforces Excel's own rules so the file opens: at most 31
// characters, none of : \ / ? * [ ], and never empty.
func sanitizeSheetName(name string) string {
	if name == "" {
		return "Sheet1"
	}
	name = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`:\/?*[]`, r) {
			return '_'
		}
		return r
	}, name)
	if runes := []rune(name); len(runes) > 31 {
		name = string(runes[:31])
	}
	return name
}

func (s *sheetBuilder) xml() string {
	var sb strings.Builder
	sb.Grow(64 * len(s.rows))
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n")
	sb.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`)

	if len(s.widths) > 0 {
		sb.WriteString(`<cols>`)
		for col := 1; ; col++ {
			w, ok := s.widths[columnName(col)]
			if !ok {
				break
			}
			fmt.Fprintf(&sb, `<col min="%d" max="%d" width="%g" customWidth="1"/>`, col, col, w)
		}
		sb.WriteString(`</cols>`)
	}

	sb.WriteString(`<sheetData>`)
	for row := 1; row <= s.maxRow; row++ {
		cells := s.rows[row]
		if len(cells) == 0 {
			continue // a row with no cells is simply absent; Excel treats it as blank
		}
		fmt.Fprintf(&sb, `<row r="%d">`, row)
		for _, c := range cells {
			sb.WriteString(`<c r="` + c.ref + `"`)
			if c.style != styleDefault {
				fmt.Fprintf(&sb, ` s="%d"`, c.style)
			}
			switch c.kind {
			case kindNumber:
				sb.WriteString(`><v>` + c.num + `</v></c>`)
			case kindInline:
				sb.WriteString(` t="inlineStr"><is><t`)
				// xml:space="preserve" or Excel eats leading/trailing spaces,
				// which the reader then cannot distinguish from a trimmed value.
				if needsSpacePreserve(c.text) {
					sb.WriteString(` xml:space="preserve"`)
				}
				sb.WriteString(`>`)
				xmlEscapeText(&sb, c.text)
				sb.WriteString(`</t></is></c>`)
			}
		}
		sb.WriteString(`</row>`)
	}
	sb.WriteString(`</sheetData></worksheet>`)
	return sb.String()
}

func needsSpacePreserve(s string) bool {
	return s != strings.TrimSpace(s)
}

// xmlEscapeText escapes character data. encoding/xml's EscapeText is used
// rather than a hand-rolled replacer so that control characters illegal in XML
// 1.0 are handled the same way the rest of this framework handles them.
func xmlEscapeText(sb *strings.Builder, s string) {
	_ = xml.EscapeText(sb, []byte(s))
}

func xmlEscapeAttr(sb *strings.Builder, s string) {
	xmlEscapeText(sb, s)
}

// writeXLSX packages the parts into the .xlsx zip.
func writeXLSX(path string, sheet *sheetBuilder) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	zw := zip.NewWriter(f)
	parts := []struct{ name, body string }{
		{"[Content_Types].xml", contentTypesXML},
		{"_rels/.rels", rootRelsXML},
		{"xl/workbook.xml", workbookXML(sheet.name)},
		{"xl/_rels/workbook.xml.rels", workbookRelsXML},
		{"xl/styles.xml", styleSheetXML},
		{"xl/worksheets/sheet1.xml", sheet.xml()},
	}
	for _, p := range parts {
		w, err := zw.Create(p.name)
		if err != nil {
			return fmt.Errorf("zip entry %s: %w", p.name, err)
		}
		if _, err := w.Write([]byte(p.body)); err != nil {
			return fmt.Errorf("write %s: %w", p.name, err)
		}
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("finalise zip: %w", err)
	}
	return nil
}
