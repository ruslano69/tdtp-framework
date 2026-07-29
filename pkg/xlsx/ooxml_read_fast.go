package xlsx

// ooxml_read_fast.go — byte-level scanner for <sheetData>, the same hybrid
// strategy pkg/core/packet/parser_fast.go uses for the <Data> section.
//
// The shape of the problem is identical. A worksheet is a few hundred bytes of
// structure and then an arbitrary number of cells, all in one narrow syntax
// this package also writes. Handing every one of those cells to a general XML
// decoder pays for namespace bookkeeping, token allocation and interface
// dispatch on data whose form is known in advance.
//
// The safety property is the one that makes this acceptable, and it is copied
// deliberately: the scanner never guesses. Anything it does not recognise —
// a comment, CDATA, an attribute it has not seen, a nested element inside a
// cell, an entity it cannot decode — returns ok=false, and the caller runs the
// encoding/xml path instead. There is no partial result and no "close enough".
// A format this fiddly earns a fast path only if being wrong is impossible.

import (
	"bytes"
	"strconv"
	"unicode/utf8"

	"github.com/ruslano69/tdtp-framework/pkg/core/xmlchar"
)

var (
	bSheetDataOpen  = []byte("<sheetData")
	bSheetDataClose = []byte("</sheetData>")
	bRowOpen        = []byte("<row")
	bRowClose       = []byte("</row>")
	bCellOpen       = []byte("<c")
	bCellClose      = []byte("</c>")
	bValueOpen      = []byte("<v>")
	bValueClose     = []byte("</v>")
	bInlineOpen     = []byte("<is>")
	bInlineClose    = []byte("</is>")
	bTextOpen       = []byte("<t")
	bTextClose      = []byte("</t>")
)

// maxFastSheetBytes caps what the scanner will hold in memory. Beyond it the
// streaming decoder runs instead — a worksheet has no size limit in the format,
// and trading unbounded memory for speed is not a trade this package makes.
const maxFastSheetBytes = 64 << 20

// parseSheetFast scans a worksheet part that is already in memory.
//
// ok=false means "shape not recognised, use the decoder"; it is never an
// error, and the caller must not treat it as one.
func parseSheetFast(data []byte, shared []string) (rows [][]string, ok bool) {
	body, ok := sheetDataBody(data)
	if !ok {
		return nil, false
	}

	// Cell count is a good capacity estimate: '<' inside a value is always
	// escaped, so every "<c" really is a cell.
	cells := bytes.Count(body, bCellOpen)

	// Non-nil even when empty, matching what the decoder path returns for a
	// sheet with no cells. The two results must be indistinguishable, and
	// reflect.DeepEqual — which the differential test uses — separates a nil
	// slice from an empty one even though no caller can.
	out := make([][]string, 0, cells/8+1)
	var (
		maxCol int
		rowNum int
	)

	i := 0
	for i < len(body) {
		for i < len(body) && isSpaceByte(body[i]) {
			i++
		}
		if i >= len(body) {
			break
		}
		if !bytes.HasPrefix(body[i:], bRowOpen) {
			return nil, false // comment, CDATA, or an element this scanner does not know
		}

		attrs, selfClosing, next, ok := scanTag(body, i)
		if !ok {
			return nil, false
		}
		rowNum++
		if r, found := attrValue(attrs, "r"); found {
			n, err := strconv.Atoi(string(r))
			if err != nil || n < 1 {
				return nil, false
			}
			rowNum = n
		}

		var cellsOfRow []string
		if !selfClosing {
			end := bytes.Index(body[next:], bRowClose)
			if end < 0 {
				return nil, false
			}
			cellsOfRow, ok = scanRowCells(body[next:next+end], shared)
			if !ok {
				return nil, false
			}
			next += end + len(bRowClose)
		}

		for len(out) < rowNum {
			out = append(out, nil)
		}
		out[rowNum-1] = cellsOfRow
		if len(cellsOfRow) > maxCol {
			maxCol = len(cellsOfRow)
		}
		i = next
	}

	// Pad to a rectangle and drop trailing blank rows, matching the decoder
	// path exactly — the two must be indistinguishable to the caller.
	for r := range out {
		for len(out[r]) < maxCol {
			out[r] = append(out[r], "")
		}
	}
	for len(out) > 0 && isBlankRow(out[len(out)-1]) {
		out = out[:len(out)-1]
	}
	return out, true
}

// sheetDataBody returns the bytes between <sheetData ...> and </sheetData>.
func sheetDataBody(data []byte) ([]byte, bool) {
	start := bytes.Index(data, bSheetDataOpen)
	if start < 0 {
		return nil, false
	}
	_, selfClosing, next, ok := scanTag(data, start)
	if !ok {
		return nil, false
	}
	if selfClosing {
		return nil, true // <sheetData/> — a legitimately empty sheet
	}
	end := bytes.Index(data[next:], bSheetDataClose)
	if end < 0 {
		return nil, false
	}
	return data[next : next+end], true
}

// scanRowCells reads the cells of one row.
func scanRowCells(body []byte, shared []string) ([]string, bool) {
	var out []string
	i := 0
	for i < len(body) {
		for i < len(body) && isSpaceByte(body[i]) {
			i++
		}
		if i >= len(body) {
			break
		}
		if !bytes.HasPrefix(body[i:], bCellOpen) {
			return nil, false
		}

		attrs, selfClosing, next, ok := scanTag(body, i)
		if !ok {
			return nil, false
		}

		col := len(out) + 1
		if r, found := attrValue(attrs, "r"); found {
			c, _ := parseCellRef(string(r))
			if c < 1 {
				return nil, false
			}
			col = c
		}
		typ := ""
		if t, found := attrValue(attrs, "t"); found {
			typ = string(t)
		}

		value := ""
		if !selfClosing {
			end := bytes.Index(body[next:], bCellClose)
			if end < 0 {
				return nil, false
			}
			value, ok = scanCellBody(body[next:next+end], typ, shared)
			if !ok {
				return nil, false
			}
			next += end + len(bCellClose)
		}

		for len(out) < col {
			out = append(out, "")
		}
		out[col-1] = value
		i = next
	}
	return out, true
}

// scanCellBody extracts one cell's value, which lives in <v> for every type
// except inlineStr, where it is in <is><t>.
func scanCellBody(body []byte, typ string, shared []string) (string, bool) {
	if len(bytes.TrimSpace(body)) == 0 {
		return "", true
	}

	var raw []byte
	switch {
	case bytes.HasPrefix(bytes.TrimLeft(body, " \t\r\n"), bInlineOpen):
		inner, ok := between(body, bInlineOpen, bInlineClose)
		if !ok {
			return "", false
		}
		// One <t> only. Several means formatting runs, which the decoder
		// path joins; rather than duplicate that here, decline.
		if bytes.Count(inner, bTextOpen) != 1 {
			return "", false
		}
		t, _, next, ok := scanTag(inner, bytes.Index(inner, bTextOpen))
		_ = t
		if !ok {
			return "", false
		}
		end := bytes.Index(inner[next:], bTextClose)
		if end < 0 {
			return "", false
		}
		raw = inner[next : next+end]

	default:
		inner, ok := between(body, bValueOpen, bValueClose)
		if !ok {
			return "", false
		}
		raw = inner
	}

	// A raw '<' means a nested element, comment or CDATA — shapes only the
	// full decoder reproduces faithfully.
	if bytes.IndexByte(raw, '<') >= 0 {
		return "", false
	}
	text, ok := decodeXMLText(raw)
	if !ok {
		return "", false
	}

	switch typ {
	case "s":
		idx, err := strconv.Atoi(text)
		if err != nil || idx < 0 || idx >= len(shared) {
			return "", false
		}
		return shared[idx], true
	case "b":
		if text == "1" {
			return "TRUE", true
		}
		return "FALSE", true
	default:
		return text, true
	}
}

// between returns what lies between the first open tag and the matching close.
func between(body, open, close []byte) ([]byte, bool) {
	s := bytes.Index(body, open)
	if s < 0 {
		return nil, false
	}
	s += len(open)
	e := bytes.Index(body[s:], close)
	if e < 0 {
		return nil, false
	}
	return body[s : s+e], true
}

// scanTag walks one start tag from '<' to '>', returning its attribute span,
// whether it closed itself, and the offset just past it.
func scanTag(data []byte, start int) (attrs []byte, selfClosing bool, next int, ok bool) {
	i := start
	for i < len(data) && data[i] != '>' {
		// A quoted attribute value may contain '>'.
		if data[i] == '"' || data[i] == '\'' {
			q := data[i]
			i++
			for i < len(data) && data[i] != q {
				i++
			}
			if i >= len(data) {
				return nil, false, 0, false
			}
		}
		i++
	}
	if i >= len(data) {
		return nil, false, 0, false
	}
	body := data[start:i]
	selfClosing = len(body) > 0 && body[len(body)-1] == '/'
	if selfClosing {
		body = body[:len(body)-1]
	}
	return body, selfClosing, i + 1, true
}

// attrValue finds name="value" inside a start tag's span.
func attrValue(tag []byte, name string) ([]byte, bool) {
	i := 0
	for {
		j := bytes.Index(tag[i:], []byte(name))
		if j < 0 {
			return nil, false
		}
		p := i + j
		// The match has to be a whole attribute name: preceded by space and
		// followed by '='. Without this, "r" matches inside "spans".
		if p == 0 || !isSpaceByte(tag[p-1]) {
			i = p + 1
			continue
		}
		k := p + len(name)
		for k < len(tag) && isSpaceByte(tag[k]) {
			k++
		}
		if k >= len(tag) || tag[k] != '=' {
			i = p + 1
			continue
		}
		k++
		for k < len(tag) && isSpaceByte(tag[k]) {
			k++
		}
		if k >= len(tag) || (tag[k] != '"' && tag[k] != '\'') {
			return nil, false
		}
		q := tag[k]
		k++
		e := bytes.IndexByte(tag[k:], q)
		if e < 0 {
			return nil, false
		}
		return tag[k : k+e], true
	}
}

// decodeXMLText resolves the five predefined entities and numeric character
// references. Anything else — a named entity from a DTD — returns false, since
// resolving those needs the document's declarations.
func decodeXMLText(s []byte) (string, bool) {
	if bytes.IndexByte(s, '&') < 0 {
		return string(s), true
	}
	var sb []byte
	for i := 0; i < len(s); {
		if s[i] != '&' {
			sb = append(sb, s[i])
			i++
			continue
		}
		j := bytes.IndexByte(s[i:], ';')
		if j < 0 {
			return "", false
		}
		ent := s[i+1 : i+j]
		switch string(ent) {
		case "lt":
			sb = append(sb, '<')
		case "gt":
			sb = append(sb, '>')
		case "amp":
			sb = append(sb, '&')
		case "quot":
			sb = append(sb, '"')
		case "apos":
			sb = append(sb, '\'')
		default:
			// Numeric reference. Anything else is a named entity from a DTD,
			// which needs the document's declarations to resolve.
			if len(ent) < 2 || ent[0] != '#' {
				return "", false
			}
			// xmlchar, not a local check: a hand-rolled one here already let
			// through &#1;, &#xFFFE; and surrogates, which encoding/xml
			// rejects outright — the fast path was accepting documents the
			// fallback refuses. Same bug this framework fixed in 1.19.2 for
			// the packet parser, reintroduced by copying instead of sharing.
			r, ok := xmlchar.DecodeRef(ent[1:])
			if !ok {
				return "", false
			}
			sb = utf8.AppendRune(sb, r)
		}
		i += j + 1
	}
	return string(sb), true
}

func isSpaceByte(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n'
}
