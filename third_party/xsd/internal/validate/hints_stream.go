package validate

import (
	"github.com/jacoelho/xsd/internal/xmlstream"
	"github.com/jacoelho/xsd/xsderrors"
)

// RecordAttributes records any XSI schema-location hints in attrs.
func (h *SchemaLocationHints) RecordAttributes(attrs []xmlstream.Attr, reader *xmlstream.Reader, limits schemaLocationHintLimits, ctx StartContext) error {
	var delta schemaLocationHintDelta
	for i := range attrs {
		attr := &attrs[i]
		if !IsSchemaLocationHintName(attr.Name) {
			continue
		}
		value, err := schemaLocationHintValue(attr, reader)
		if err != nil {
			return err
		}
		if err := h.stageAttribute(attr.Name, value, limits, ctx, &delta); err != nil {
			return err
		}
	}
	if len(delta.namespaces) == 0 {
		return nil
	}
	working := cloneSchemaLocationHints(*h)
	working.commitNamespaceHints(delta.namespaces, delta.namespaceBytes)
	*h = working
	return nil
}

func schemaLocationHintValue(attr *xmlstream.Attr, reader *xmlstream.Reader) (string, error) {
	if !attr.HasBorrowedValue() {
		return attr.Value, nil
	}
	if reader == nil {
		return "", xsderrors.InternalInvariant("borrowed XML attribute requires a reader")
	}
	value, ok := reader.MaterializeValue(attr)
	if !ok {
		return "", xsderrors.InternalInvariant("XML attribute value is unavailable")
	}
	return value, nil
}
