package validate

import (
	"encoding/xml"
	"maps"
	"strings"

	"github.com/jacoelho/xsd/internal/lex"
	"github.com/jacoelho/xsd/internal/uriref"
	"github.com/jacoelho/xsd/internal/vocab"
	"github.com/jacoelho/xsd/xsderrors"
)

// SchemaLocationHints records instance schema-location hints seen during
// validation. Only namespaces are retained because loading external schemas is
// intentionally unsupported at validation time.
type SchemaLocationHints struct {
	namespaces     map[string]struct{}
	namespaceBytes int64
}

type schemaLocationHintLimits struct {
	Namespaces     int
	NamespaceBytes int64
}

type schemaLocationHintDelta struct {
	namespaces     map[string]struct{}
	namespaceBytes int64
}

func cloneSchemaLocationHints(h SchemaLocationHints) SchemaLocationHints {
	return SchemaLocationHints{
		namespaces:     maps.Clone(h.namespaces),
		namespaceBytes: h.namespaceBytes,
	}
}

// RecordAttribute records one xsi:schemaLocation or
// xsi:noNamespaceSchemaLocation attribute value.
func (h *SchemaLocationHints) RecordAttribute(name xml.Name, value string, limits schemaLocationHintLimits, ctx StartContext) error {
	var delta schemaLocationHintDelta
	if err := h.stageAttribute(name, value, limits, ctx, &delta); err != nil {
		return err
	}
	h.commitNamespaceHints(delta.namespaces, delta.namespaceBytes)
	return nil
}

func (h *SchemaLocationHints) stageAttribute(name xml.Name, value string, limits schemaLocationHintLimits, ctx StartContext, delta *schemaLocationHintDelta) error {
	switch name.Local {
	case vocab.XSIAttrSchemaLocation:
		return h.stageNamespaceSchemaLocation(value, limits, ctx, delta)
	case vocab.XSIAttrNoNamespaceSchemaLocation:
		return h.stageNoNamespaceSchemaLocation(value, limits, ctx, delta)
	default:
		return nil
	}
}

// Has reports whether a schema-location hint was seen for ns.
func (h *SchemaLocationHints) Has(ns string) bool {
	if h == nil {
		return false
	}
	_, ok := h.namespaces[ns]
	return ok
}

// Reset clears retained hints, keeping the namespace map only when bounded.
func (h *SchemaLocationHints) Reset(maxRetainedNamespaces int) {
	if h == nil {
		return
	}
	h.namespaceBytes = 0
	if len(h.namespaces) > maxRetainedNamespaces {
		h.namespaces = nil
		return
	}
	clear(h.namespaces)
}

func (h *SchemaLocationHints) stageNamespaceSchemaLocation(value string, limits schemaLocationHintLimits, ctx StartContext, delta *schemaLocationHintDelta) error {
	count, err := validateSchemaLocationFields(value, ctx)
	if err != nil {
		return err
	}
	if count%2 != 0 {
		return validation(ctx, xsderrors.CodeValidationAttribute, "xsi:schemaLocation must contain namespace/location pairs")
	}
	return h.stageNamespaceHints(value, delta, limits, ctx)
}

func validateSchemaLocationFields(value string, ctx StartContext) (int, error) {
	count := 0
	for field := range lex.XMLFieldsSeq(value) {
		if _, err := uriref.Check(field); err != nil {
			return 0, validation(ctx, xsderrors.CodeValidationAttribute, "invalid xsi:schemaLocation URI "+field)
		}
		count++
	}
	return count, nil
}

func (h *SchemaLocationHints) stageNamespaceHints(value string, delta *schemaLocationHintDelta, limits schemaLocationHintLimits, ctx StartContext) error {
	index := 0
	for field := range lex.XMLFieldsSeq(value) {
		if index%2 == 0 {
			if err := h.stageNamespaceHint(field, delta, limits, ctx); err != nil {
				return err
			}
		}
		index++
	}
	return nil
}

func (h *SchemaLocationHints) stageNamespaceHint(ns string, delta *schemaLocationHintDelta, limits schemaLocationHintLimits, ctx StartContext) error {
	if _, exists := h.namespaces[ns]; exists {
		return nil
	}
	if _, exists := delta.namespaces[ns]; exists {
		return nil
	}
	if len(h.namespaces)+len(delta.namespaces) >= limits.Namespaces {
		return validation(ctx, xsderrors.CodeValidationLimit, "schema-location namespace limit exceeded")
	}
	fieldBytes := int64(len(ns))
	remaining := limits.NamespaceBytes - h.namespaceBytes
	if remaining < delta.namespaceBytes || fieldBytes > remaining-delta.namespaceBytes {
		return validation(ctx, xsderrors.CodeValidationLimit, "schema-location namespace byte limit exceeded")
	}
	if delta.namespaces == nil {
		delta.namespaces = make(map[string]struct{})
	}
	delta.namespaces[ns] = struct{}{}
	delta.namespaceBytes += fieldBytes
	return nil
}

func (h *SchemaLocationHints) commitNamespaceHints(pending map[string]struct{}, pendingBytes int64) {
	if len(pending) != 0 && h.namespaces == nil {
		h.namespaces = make(map[string]struct{}, len(pending))
	}
	for ns := range pending {
		h.namespaces[strings.Clone(ns)] = struct{}{}
	}
	h.namespaceBytes += pendingBytes
}

func (h *SchemaLocationHints) stageNoNamespaceSchemaLocation(value string, limits schemaLocationHintLimits, ctx StartContext, delta *schemaLocationHintDelta) error {
	value = lex.TrimXMLWhitespaceString(value)
	if _, err := uriref.Check(value); err != nil {
		return validation(ctx, xsderrors.CodeValidationAttribute, "invalid xsi:noNamespaceSchemaLocation URI "+value)
	}
	return h.stageNamespaceHint("", delta, limits, ctx)
}

// IsSchemaLocationHintName reports whether name is an XSI schema-location hint.
func IsSchemaLocationHintName(name xml.Name) bool {
	return name.Space == vocab.XSINamespaceURI &&
		(name.Local == vocab.XSIAttrSchemaLocation || name.Local == vocab.XSIAttrNoNamespaceSchemaLocation)
}
