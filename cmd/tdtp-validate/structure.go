package main

// structure.go — structural pass of tdtp-validate.
//
// The pass has two layers:
//
//  1. docs/tdtp.xsd itself, executed by a real XSD 1.0 engine
//     (github.com/jacoelho/xsd, pure Go, no cgo). The schema file is
//     embedded from ../../docs/tdtp.xsd, so there is exactly one source
//     of truth — nothing is transcribed by hand. Element presence, order,
//     cardinality, attributes, enumerations, patterns, unions and mixed
//     content all come from the file.
//  2. A small handwritten overlay for the rules that are stricter than
//     the XSD: non-blank names, no structured children under v1.5
//     ciphertext, and a non-degenerate QueryContext. The XSD deliberately
//     allows empty strings (xs:string); an empty column or marker name is
//     meaningless on the wire, so the overlay rejects it.
//
// A generic xnode tree is still built first: well-formedness errors keep
// stable local wording, and the overlay walks the tree.

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/jacoelho/xsd"
	"github.com/jacoelho/xsd/xsderrors"
)

//go:generate go run ./genxsd/gen.go

// tdtpXSD is docs/tdtp.xsd verbatim, synced by genxsd (see spec_xsd_gen.go).
// genfresh_test.go fails if the copy drifts from the file.
var tdtpXSD = []byte(tdtpXSDText)

var (
	xsdEngine     *xsd.Engine
	xsdEngineErr  error
	xsdEngineOnce sync.Once
)

// specEngine compiles the embedded schema once and shares it: Engine is
// immutable and goroutine-safe, so batch mode validates files against the
// same compiled program.
func specEngine() (*xsd.Engine, error) {
	xsdEngineOnce.Do(func() {
		xsdEngine, xsdEngineErr = xsd.Compile(xsd.Bytes("tdtp.xsd", tdtpXSD))
	})
	return xsdEngine, xsdEngineErr
}

// checkSpec runs the embedded XSD against raw bytes and maps engine
// diagnostics to "structure: <path> line:col: message".
func checkSpec(data []byte) []string {
	eng, err := specEngine()
	if err != nil {
		return []string{"structure: internal: cannot compile docs/tdtp.xsd: " + err.Error()}
	}
	if err := eng.Validate(bytes.NewReader(data)); err != nil {
		return splitXsdErrors(err)
	}
	return nil
}

func splitXsdErrors(err error) []string {
	var group xsderrors.Errors
	if errors.As(err, &group) {
		out := make([]string, 0, group.Len())
		for i := range group.Len() {
			out = append(out, "structure: "+xsdDiag(group.At(i)))
		}
		return out
	}
	return []string{"structure: " + xsdDiag(err)}
}

func xsdDiag(err error) string {
	var d *xsderrors.Error
	if errors.As(err, &d) {
		loc := ""
		if d.Path() != "" {
			loc = d.Path() + " "
		}
		if d.Line() > 0 {
			loc += fmt.Sprintf("%d:%d: ", d.Line(), d.Column())
		}
		return loc + d.Message()
	}
	return err.Error()
}

// xnode is one XML element with order-preserved attributes and children.
type xnode struct {
	Name     string
	Space    string // namespace URL, if any — TDTP allows none
	Attrs    []xattr
	Children []*xnode
	Text     string // concatenated chardata
}

type xattr struct {
	Name  string
	Space string
	Value string
}

// buildTree parses well-formedness. Anything the Decoder itself rejects
// (bad tags, mismatched nesting, more than one root) comes back as error.
func buildTree(data []byte) (*xnode, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = true
	var root *xnode
	var stack []*xnode
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			n := &xnode{Name: t.Name.Local, Space: t.Name.Space}
			seen := map[string]bool{}
			for _, a := range t.Attr {
				key := a.Name.Space + "\x00" + a.Name.Local
				if seen[key] {
					return nil, fmt.Errorf("duplicate attribute %q on <%s>", a.Name.Local, t.Name.Local)
				}
				seen[key] = true
				n.Attrs = append(n.Attrs, xattr{Name: a.Name.Local, Space: a.Name.Space, Value: a.Value})
			}
			if len(stack) == 0 {
				if root != nil {
					return nil, fmt.Errorf("multiple root elements")
				}
				root = n
			} else {
				parent := stack[len(stack)-1]
				parent.Children = append(parent.Children, n)
			}
			stack = append(stack, n)
		case xml.EndElement:
			if len(stack) == 0 {
				return nil, fmt.Errorf("stray closing tag </%s>", t.Name.Local)
			}
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].Text += string(t)
			} else if strings.TrimSpace(string(t)) != "" {
				return nil, fmt.Errorf("non-whitespace content outside root element")
			}
		case xml.Comment, xml.ProcInst, xml.Directive:
			// Ignored: comments, <?xml ...?>, <!DOCTYPE ...> carry no TDTP meaning.
		}
	}
	if root == nil {
		return nil, fmt.Errorf("empty document: no root element")
	}
	if len(stack) != 0 {
		return nil, fmt.Errorf("unclosed element <%s>", stack[len(stack)-1].Name)
	}
	return root, nil
}

// ─── overlay: stricter than the XSD ───────────────────────────────────────

// blankForbidAttrs must be non-blank wherever present. The XSD allows ""
// (xs:string); an empty column, token or marker name is meaningless, so
// the overlay rejects it. Deliberately absent: value/value2 (matching an
// empty string is legitimate) and anything the engine's facets already
// pin down (enumerations, patterns, hashes).
var blankForbidAttrs = map[string]bool{
	"language": true, "version": true, "field": true, "operator": true,
	"name": true, "short": true, "full": true, "marker": true,
}

type overlay struct {
	errs []string
}

func (o *overlay) errf(path, format string, args ...any) {
	o.errs = append(o.errs, "structure: "+path+": "+fmt.Sprintf(format, args...))
}

// checkOverlay walks the tree enforcing the above-XSD rules.
func (o *overlay) checkOverlay(root *xnode) {
	if root.Name != "DataPacket" || root.Space != "" {
		return // the engine already reported the wrong root
	}
	o.walk("DataPacket", root)
}

func (o *overlay) walk(path string, n *xnode) {
	for _, a := range n.Attrs {
		if a.Space == "" && blankForbidAttrs[a.Name] && strings.TrimSpace(a.Value) == "" {
			o.errf(path, "attribute %q must not be blank", a.Name)
		}
	}
	for i, k := range n.Children {
		kp := path + " / " + k.Name
		switch {
		case k.Name == "Schema" && k.Space == "":
			if _, hasEnc := plainAttr(k, "encryption"); hasEnc && len(k.Children) > 0 {
				o.errf(kp, "encrypted Schema must not carry <Field>/<Dictionary> children")
			}
		case k.Name == "QueryContext" && k.Space == "":
			if _, hasEnc := plainAttr(k, "encryption"); hasEnc {
				if len(k.Children) > 0 {
					o.errf(kp, "encrypted <QueryContext> must not carry structured children")
				}
			} else if len(k.Children) == 0 {
				o.errf(kp, "unencrypted <QueryContext> must hold <OriginalQuery> and <ExecutionResults>")
			}
		case k.Name == "Field" && k.Space == "":
			// Query's <Field> holds the column name as text; Schema's
			// <Field> must stay empty (the engine enforces the latter).
			parent := lastSegment(path)
			if parent == "Fields" && strings.TrimSpace(k.Text) == "" {
				o.errf(kp+fmt.Sprintf("[%d]", i+1), "must hold the column name as text")
			}
		case (k.Name == "Severity" || k.Name == "Code" || k.Name == "Message") && k.Space == "":
			if strings.TrimSpace(k.Text) == "" {
				o.errf(kp, "must not be empty")
			}
		}
		o.walk(kp, k)
	}
}

// plainAttr returns an un-namespaced attribute value.
func plainAttr(n *xnode, name string) (string, bool) {
	for _, a := range n.Attrs {
		if a.Name == name && a.Space == "" {
			return a.Value, true
		}
	}
	return "", false
}

func lastSegment(path string) string {
	if i := strings.LastIndex(path, " / "); i >= 0 {
		return path[i+3:]
	}
	return path
}

// checkStructure runs the whole structural pass over raw XML: well-formedness,
// the embedded XSD, then the stricter-than-XSD overlay.
func checkStructure(data []byte) []string {
	root, err := buildTree(data)
	if err != nil {
		return []string{"structure: document: " + err.Error()}
	}
	errs := checkSpec(data)
	o := &overlay{}
	o.checkOverlay(root)
	return append(errs, o.errs...)
}
