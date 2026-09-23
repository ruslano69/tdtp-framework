package validate

import (
	"encoding/xml"

	xsdSchema "github.com/jacoelho/xsd/internal/schema"
	"github.com/jacoelho/xsd/internal/vocab"
	"github.com/jacoelho/xsd/xsderrors"
)

func attributeValidation(ctx StartContext, msg string) error {
	return validation(ctx, xsderrors.CodeValidationAttribute, msg)
}

func isXSIAttributeName(name xml.Name) bool {
	return name.Space == vocab.XSINamespaceURI &&
		(name.Local == vocab.XSIAttrType ||
			name.Local == vocab.XSIAttrNil ||
			name.Local == vocab.XSIAttrSchemaLocation ||
			name.Local == vocab.XSIAttrNoNamespaceSchemaLocation)
}

// AttributeSeen tracks declared attributes seen on one element.
type AttributeSeen struct {
	list []bool
	mask uint64
}

func newAttributeSeenWithScratch(n int, scratch *[]bool) AttributeSeen {
	if n <= 64 {
		return AttributeSeen{}
	}
	if n > maxRetainedSliceCap {
		return AttributeSeen{list: make([]bool, n)}
	}
	if cap(*scratch) < n {
		*scratch = make([]bool, n)
	} else {
		*scratch = (*scratch)[:n]
		clear(*scratch)
	}
	return AttributeSeen{list: *scratch}
}

func (s *AttributeSeen) mark(slot int) bool {
	if s.list != nil {
		if s.list[slot] {
			return false
		}
		s.list[slot] = true
		return true
	}
	bit := uint64(1) << slot
	if s.mask&bit != 0 {
		return false
	}
	s.mask |= bit
	return true
}

func (s *AttributeSeen) has(slot int) bool {
	if s.list != nil {
		return s.list[slot]
	}
	return s.mask&(uint64(1)<<slot) != 0
}

type attributeWildcardDisposition uint8

const (
	attributeWildcardNoMatch attributeWildcardDisposition = iota
	attributeWildcardSkip
	attributeWildcardDeclared
	attributeWildcardLaxMissing
	attributeWildcardStrictMissing
)

type attributeWildcardMatch struct {
	attribute   xsdSchema.AttributeID
	disposition attributeWildcardDisposition
}

func matchAttributeWildcard(rt *xsdSchema.Schema, wildcard xsdSchema.WildcardID, name xsdSchema.RuntimeName) (attributeWildcardMatch, bool) {
	if wildcard == xsdSchema.NoWildcard {
		return attributeWildcardMatch{}, true
	}
	w, ok := rt.WildcardView(wildcard)
	if !ok {
		return attributeWildcardMatch{}, false
	}
	if !w.AllowsURI(name.NS) {
		return attributeWildcardMatch{}, true
	}
	if w.Process() == xsdSchema.ProcessSkip {
		return attributeWildcardMatch{disposition: attributeWildcardSkip}, true
	}
	if name.Known {
		attribute, found, valid := rt.GlobalAttribute(name.Name)
		if !valid {
			return attributeWildcardMatch{}, false
		}
		if found {
			return attributeWildcardMatch{
				attribute:   attribute,
				disposition: attributeWildcardDeclared,
			}, true
		}
	}
	if w.Process() == xsdSchema.ProcessLax {
		return attributeWildcardMatch{disposition: attributeWildcardLaxMissing}, true
	}
	return attributeWildcardMatch{disposition: attributeWildcardStrictMissing}, true
}
