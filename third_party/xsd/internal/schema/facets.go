package schema

import (
	"errors"
	"strconv"

	"github.com/jacoelho/xsd/internal/lex"

	"github.com/jacoelho/xsd/xsderrors"
)

const maxUint32Value = uint64(^uint32(0))
const facetTotalDigits = "totalDigits"

// ParseSizeFacetValue parses length/minLength/maxLength/totalDigits/
// fractionDigits facet values.
func ParseSizeFacetValue(name, value string) (uint32, error) {
	value = lex.CollapseXMLWhitespace(value)
	size, err := parseSizeFacetInteger(value)
	if err != nil {
		return 0, xsderrors.SchemaCompile(xsderrors.CodeSchemaFacet, "invalid "+name+" facet "+value)
	}
	if name == facetTotalDigits && size == 0 {
		return 0, xsderrors.SchemaCompile(xsderrors.CodeSchemaFacet, "totalDigits must be positive")
	}
	if size > maxUint32Value {
		return 0, xsderrors.SchemaCompile(xsderrors.CodeSchemaLimit, name+" facet exceeds uint32 limit")
	}
	return uint32(size), nil
}

// FacetValueError maps runtime simple-value rejection for a facet literal to a
// compile-time schema diagnostic while preserving unsupported diagnostics.
func FacetValueError(lexical string, err error) error {
	if err == nil {
		return nil
	}
	if xsderrors.IsUnsupported(err) {
		return err
	}
	return xsderrors.SchemaCompile(xsderrors.CodeSchemaFacet, "invalid facet value "+lexical)
}

// DeclarationValueConstraintError maps runtime simple-value rejection for a
// declaration value constraint to a compile-time schema diagnostic while
// preserving unsupported diagnostics.
func DeclarationValueConstraintError(label, owner string, err error) error {
	if err == nil {
		return nil
	}
	if xsderrors.IsUnsupported(err) {
		return err
	}
	return xsderrors.SchemaCompile(xsderrors.CodeSchemaFacet, "invalid "+label+" value for "+owner)
}

// ElementValueConstraintTypeError maps runtime element value-constraint owner
// typing failures to compile-time schema diagnostics.
func ElementValueConstraintTypeError(err error) error {
	if err == nil {
		return nil
	}
	var diagnostic *xsderrors.Error
	if errors.As(err, &diagnostic) && diagnostic != nil {
		return err
	}
	return xsderrors.SchemaCompile(xsderrors.CodeSchemaInvalidAttribute, err.Error())
}

// ElementValueConstraintRuntimeError maps runtime element value-constraint
// validation failures to compile-time schema diagnostics.
func ElementValueConstraintRuntimeError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrBareNotationValueConstraint) {
		return xsderrors.SchemaCompile(xsderrors.CodeSchemaFacet, err.Error())
	}
	return xsderrors.SchemaCompile(xsderrors.CodeSchemaInvalidAttribute, err.Error())
}

func parseSizeFacetInteger(value string) (uint64, error) {
	start, negative, err := parseSizeFacetSign(value)
	if err != nil {
		return 0, err
	}
	if start == len(value) {
		return 0, strconv.ErrSyntax
	}
	digitStart := skipSchemaLeadingZeros(value, start, len(value))
	if negative && digitStart != len(value) {
		return 0, strconv.ErrSyntax
	}
	if digitStart == len(value) {
		return 0, nil
	}
	return parseSizeFacetDigits(value, digitStart)
}

func parseSizeFacetSign(value string) (int, bool, error) {
	if value == "" {
		return 0, false, strconv.ErrSyntax
	}
	if value[0] == '+' {
		return 1, false, nil
	}
	if value[0] == '-' {
		return 1, true, nil
	}
	return 0, false, nil
}

func parseSizeFacetDigits(value string, start int) (uint64, error) {
	var n uint64
	for i := start; i < len(value); i++ {
		b := value[i]
		if b < '0' || b > '9' {
			return 0, strconv.ErrSyntax
		}
		if n <= maxUint32Value {
			n = n*10 + uint64(b-'0')
		}
	}
	return n, nil
}

func skipSchemaLeadingZeros(s string, start, end int) int {
	for start < end && s[start] == '0' {
		start++
	}
	return start
}
