package format

import (
	"errors"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/jacoelho/xsd/internal/lex"
	"github.com/jacoelho/xsd/internal/xmlstream"
)

func sourceNameSpan(input string, start, end int) (nameStart, nameEnd int) {
	i := start
	if i < end && input[i] == '<' {
		i++
	}
	if i < end && input[i] == '/' {
		i++
	}
	nameStart = i
	for i < end && !lex.IsNameTerminator(input[i]) {
		i++
	}
	nameEnd = i
	return nameStart, nameEnd
}

//nolint:gocognit // This scanner owns the validated start-tag source grammar.
func writeSourceStart(w io.Writer, input string, end, nameStart, nameEnd int) error {
	if nameStart >= nameEnd || nameEnd > len(input) {
		return errors.New("invalid start element source span")
	}
	if _, err := io.WriteString(w, "<"); err != nil {
		return err
	}
	if _, err := io.WriteString(w, input[nameStart:nameEnd]); err != nil {
		return err
	}
	i := nameEnd
	for {
		for i < end && lex.IsXMLWhitespaceByte(input[i]) {
			i++
		}
		if i >= end {
			return errors.New("invalid start element source span")
		}
		switch input[i] {
		case '>':
			_, err := io.WriteString(w, ">")
			return err
		case '/':
			i++
			for i < end && lex.IsXMLWhitespaceByte(input[i]) {
				i++
			}
			if i >= end || input[i] != '>' {
				return errors.New("invalid empty start element source span")
			}
			_, err := io.WriteString(w, ">")
			return err
		}

		attrStart := i
		for i < end && !lex.IsNameTerminator(input[i]) {
			i++
		}
		attrEnd := i
		if attrStart == attrEnd {
			return errors.New("invalid attribute source span")
		}
		for i < end && lex.IsXMLWhitespaceByte(input[i]) {
			i++
		}
		if i >= end || input[i] != '=' {
			return errors.New("invalid attribute source span")
		}
		i++
		for i < end && lex.IsXMLWhitespaceByte(input[i]) {
			i++
		}
		if i >= end || (input[i] != '\'' && input[i] != '"') {
			return errors.New("invalid attribute source span")
		}
		quote := input[i]
		valueStart := i + 1
		quoteOffset := strings.IndexByte(input[valueStart:end], quote)
		if quoteOffset < 0 {
			return errors.New("invalid attribute source span")
		}
		valueEnd := valueStart + quoteOffset
		if _, err := io.WriteString(w, " "); err != nil {
			return err
		}
		if _, err := io.WriteString(w, input[attrStart:attrEnd]); err != nil {
			return err
		}
		if _, err := io.WriteString(w, "=\""); err != nil {
			return err
		}
		if err := writeSourceAttributeValue(w, input[valueStart:valueEnd]); err != nil {
			return err
		}
		if _, err := io.WriteString(w, "\""); err != nil {
			return err
		}
		i = valueEnd + 1
	}
}

func writeSourceEnd(w io.Writer, input string, start, end int) error {
	nameStart, nameEnd := sourceNameSpan(input, start, end)
	if nameStart >= nameEnd || nameEnd > len(input) {
		return errors.New("invalid end element source span")
	}
	if _, err := io.WriteString(w, "</"); err != nil {
		return err
	}
	if _, err := io.WriteString(w, input[nameStart:nameEnd]); err != nil {
		return err
	}
	_, err := io.WriteString(w, ">")
	return err
}

// Already-canonical references stay in their source span. Only normalization
// boundaries flush the span, so escaped input does not require a write per rune.
//
//nolint:gocognit // Entity, quote, and XML-whitespace normalization share one scan.
func writeSourceAttributeValue(w io.Writer, value string) error {
	start := 0
	for scan := 0; scan < len(value); {
		i := strings.IndexAny(value[scan:], "&\r\n\t\"")
		if i < 0 {
			break
		}
		i += scan
		end := i + 1
		var decoded rune
		switch value[i] {
		case '&':
			semi := strings.IndexByte(value[end:], ';')
			if semi < 0 {
				return errors.New("invalid entity source span")
			}
			end += semi + 1
			var err error
			decoded, err = xmlstream.DecodeEntityReferenceString(value[i+1 : end-1])
			if err != nil {
				return err
			}
			if decoded < utf8.RuneSelf {
				escape := xmlAttributeEscape(byte(decoded)) //nolint:gosec // decoded is below utf8.RuneSelf.
				if escape != "" && value[i:end] == escape {
					scan = end
					continue
				}
			}
		case '"':
			decoded = '"'
		default:
			decoded = ' '
			if value[i] == '\r' && end < len(value) && value[end] == '\n' {
				end++
			}
		}
		if err := writeString(w, value[start:i]); err != nil {
			return err
		}
		if err := writeXMLAttributeRune(w, decoded); err != nil {
			return err
		}
		start = end
		scan = end
	}
	return writeString(w, value[start:])
}

//nolint:gocognit // CDATA and reference origins require distinct source spans.
func writeSourceText(w io.Writer, input string, event formatEvent) error {
	raw := input[event.start:event.end]
	if event.textKind == xmlstream.CharacterDataCDATA {
		first := strings.HasPrefix(raw, "<![CDATA[")
		last := strings.HasSuffix(raw, "]]>")
		if first {
			if _, err := io.WriteString(w, "<![CDATA["); err != nil {
				return err
			}
			raw = raw[len("<![CDATA["):]
		}
		if last {
			raw = raw[:len(raw)-len("]]>")]
		}
		if err := writeNormalizedSource(w, raw); err != nil {
			return err
		}
		if last {
			_, err := io.WriteString(w, "]]>")
			return err
		}
		return nil
	}
	start := 0
	for i := 0; i < len(raw); i++ {
		if raw[i] != '&' {
			continue
		}
		if err := writeEscapedTextString(w, raw[start:i]); err != nil {
			return err
		}
		semi := strings.IndexByte(raw[i+1:], ';')
		if semi < 0 {
			return errors.New("invalid entity source span")
		}
		semi += i + 1
		decoded, err := xmlstream.DecodeEntityReferenceString(raw[i+1 : semi])
		if err != nil {
			return err
		}
		if err := writeEscapedTextRune(w, decoded); err != nil {
			return err
		}
		i = semi
		start = i + 1
	}
	return writeEscapedTextString(w, raw[start:])
}

func writeSourceComment(w io.Writer, input string, start, end int) error {
	raw := input[start:end]
	if !strings.HasPrefix(raw, "<!--") || !strings.HasSuffix(raw, "-->") {
		return errors.New("invalid comment source span")
	}
	if _, err := io.WriteString(w, "<!--"); err != nil {
		return err
	}
	if err := writeNormalizedSource(w, raw[len("<!--"):len(raw)-len("-->")]); err != nil {
		return err
	}
	_, err := io.WriteString(w, "-->")
	return err
}

//nolint:gocognit // PI separators and content use XML line-ending rules.
func writeSourcePI(w io.Writer, input string, start, end int) error {
	raw := input[start:end]
	if !strings.HasPrefix(raw, "<?") || !strings.HasSuffix(raw, "?>") {
		return errors.New("invalid processing instruction source span")
	}
	limit := len(raw) - len("?>")
	i := 2
	targetStart := i
	for i < limit && !lex.IsXMLWhitespaceByte(raw[i]) && raw[i] != '?' {
		i++
	}
	if targetStart == i {
		return errors.New("invalid processing instruction source span")
	}
	if _, err := io.WriteString(w, "<?"); err != nil {
		return err
	}
	if _, err := io.WriteString(w, raw[targetStart:i]); err != nil {
		return err
	}
	if i < limit {
		if raw[i] == '\r' && i+1 < limit && raw[i+1] == '\n' {
			i++
		}
		i++
		if _, err := io.WriteString(w, " "); err != nil {
			return err
		}
		if err := writeNormalizedSource(w, raw[i:limit]); err != nil {
			return err
		}
	}
	_, err := io.WriteString(w, "?>")
	return err
}

func writeNormalizedSource(w io.Writer, source string) error {
	start := 0
	for i := 0; i < len(source); i++ {
		if source[i] != '\r' {
			continue
		}
		if err := writeString(w, source[start:i]); err != nil {
			return err
		}
		if _, err := io.WriteString(w, "\n"); err != nil {
			return err
		}
		if i+1 < len(source) && source[i+1] == '\n' {
			i++
		}
		start = i + 1
	}
	return writeString(w, source[start:])
}

//nolint:gocognit // This is the canonical XML text escape table.
func writeEscapedTextString(w io.Writer, source string) error {
	start := 0
	for i := 0; i < len(source); i++ {
		var escape string
		end := i + 1
		switch source[i] {
		case '&':
			escape = xmlEscapeAmp
		case '<':
			escape = xmlEscapeLT
		case '>':
			escape = "&gt;"
		case '\'':
			escape = "&#39;"
		case '"':
			escape = "&#34;"
		case '\r':
			escape = xmlEscapeLF
			if i+1 < len(source) && source[i+1] == '\n' {
				end++
			}
		case '\n':
			escape = xmlEscapeLF
		case '\t':
			escape = "&#x9;"
		default:
			continue
		}
		if err := writeString(w, source[start:i]); err != nil {
			return err
		}
		if _, err := io.WriteString(w, escape); err != nil {
			return err
		}
		start = end
		i = end - 1
	}
	return writeString(w, source[start:])
}

func writeEscapedTextRune(w io.Writer, value rune) error {
	switch value {
	case '&':
		return writeString(w, xmlEscapeAmp)
	case '<':
		return writeString(w, xmlEscapeLT)
	case '>':
		return writeString(w, "&gt;")
	case '\'':
		return writeString(w, "&#39;")
	case '"':
		return writeString(w, "&#34;")
	case '\r':
		return writeString(w, "&#xD;")
	case '\n':
		return writeString(w, xmlEscapeLF)
	case '\t':
		return writeString(w, "&#x9;")
	}
	if value < utf8.RuneSelf {
		return writeByte(w, byte(value)) //nolint:gosec // value is below utf8.RuneSelf.
	}
	var buf [utf8.UTFMax]byte
	n := utf8.EncodeRune(buf[:], value)
	for _, b := range buf[:n] {
		if err := writeByte(w, b); err != nil {
			return err
		}
	}
	return nil
}

func writeXMLAttributeRune(w io.Writer, value rune) error {
	if value < utf8.RuneSelf {
		if escape := xmlAttributeEscape(byte(value)); escape != "" { //nolint:gosec // value is below utf8.RuneSelf.
			return writeString(w, escape)
		}
		return writeByte(w, byte(value)) //nolint:gosec // value is below utf8.RuneSelf.
	}
	var buf [utf8.UTFMax]byte
	n := utf8.EncodeRune(buf[:], value)
	for _, b := range buf[:n] {
		if err := writeByte(w, b); err != nil {
			return err
		}
	}
	return nil
}

func writeString(w io.Writer, value string) error {
	_, err := io.WriteString(w, value)
	return err
}

func writeByte(w io.Writer, value byte) error {
	if sw, ok := w.(io.ByteWriter); ok {
		return sw.WriteByte(value)
	}
	_, err := w.Write([]byte{value})
	return err
}
