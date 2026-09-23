package validate

import (
	"encoding/xml"

	"github.com/jacoelho/xsd/internal/lex"
	xsdSchema "github.com/jacoelho/xsd/internal/schema"
	xsdValue "github.com/jacoelho/xsd/internal/value"
	"github.com/jacoelho/xsd/internal/vocab"
	"github.com/jacoelho/xsd/internal/xmlstream"
	"github.com/jacoelho/xsd/xsderrors"
)

// ResolveRuntimeName returns name with its runtime QName when the schema knows it.
func ResolveRuntimeName(rt *xsdSchema.Schema, name xml.Name) xsdSchema.RuntimeName {
	q, ok := rt.LookupQName(name.Space, name.Local)
	if ok {
		return xsdSchema.RuntimeName{Name: q, Known: true, NS: name.Space, Local: name.Local}
	}
	return xsdSchema.RuntimeName{Known: false, NS: name.Space, Local: name.Local}
}

// NamespaceLookup resolves an XML namespace prefix to its URI.
type NamespaceLookup func(string) (string, bool)

// ResolveLexicalQName resolves a lexical QName after XML whitespace collapse.
func ResolveLexicalQName(lexical string, lookup NamespaceLookup) (xsdValue.ExpandedName, bool) {
	v := lex.CollapseXMLWhitespace(lexical)
	parts := lex.SplitQName(v)
	if !parts.Valid {
		return xsdValue.ExpandedName{}, false
	}
	uri, ok := lookup(parts.Prefix)
	if !ok {
		return xsdValue.ExpandedName{}, false
	}
	return xsdValue.ExpandedName{Namespace: uri, Local: parts.Local}, true
}

// HasSchemaLocation reports whether an xsi:schemaLocation hint was seen for a namespace.
type HasSchemaLocation func(string) bool

type pathSource interface {
	PathString() string
	PathStringAtDepth(depth int) string
	retainPathAtDepth(depth int) retainedPath
}

// StartContext identifies a validation location.
type StartContext struct {
	document pathSource
	Path     string
	Line     int
	Column   int
}

// PathString returns the current validation path, materializing it lazily for
// document-owned contexts.
func (ctx StartContext) PathString() string {
	if ctx.Path != "" || ctx.document == nil {
		return ctx.Path
	}
	return ctx.document.PathString()
}

// PathStringAtDepth returns the validation path at depth. Explicit contexts
// already represent their requested location.
func (ctx StartContext) PathStringAtDepth(depth int) string {
	if ctx.Path != "" || ctx.document == nil {
		return ctx.Path
	}
	return ctx.document.PathStringAtDepth(depth)
}

func (ctx StartContext) retainPathAtDepth(depth int) retainedPath {
	if ctx.document == nil {
		panic("retained XML path requires a document context")
	}
	return ctx.document.retainPathAtDepth(depth)
}

type startDeclaration struct {
	block    xsdSchema.DerivationMask
	present  bool
	abstract bool
	nillable bool
	fixed    bool
}

type assessedNilValue struct {
	value     bool
	specified bool
}

type elementEffectiveState struct {
	declaration startDeclaration
	nil         assessedNilValue
	typeID      xsdSchema.TypeID
	typeInfo    xsdSchema.TypeInfo
}

const elementNotNillableMessage = "element is not nillable"

func (state elementEffectiveState) issue() validationIssue {
	if issue := elementEffectiveTypeIssue(state.typeID, state.typeInfo); issue.valid() {
		return issue
	}
	if state.nil.specified && state.declaration.present && !state.declaration.nillable {
		return validationIssue{code: xsderrors.CodeValidationNil, message: elementNotNillableMessage}
	}
	if state.nil.value {
		if !state.declaration.present {
			return validationIssue{code: xsderrors.CodeValidationNil, message: elementNotNillableMessage}
		}
		if state.declaration.fixed {
			return validationIssue{code: xsderrors.CodeValidationNil, message: "nilled element cannot have fixed value"}
		}
	}
	return validationIssue{}
}

func elementEffectiveTypeIssue(typeID xsdSchema.TypeID, info xsdSchema.TypeInfo) validationIssue {
	if typeID.IsComplex() && info.Abstract {
		return validationIssue{code: xsderrors.CodeValidationType, message: "complex type is abstract"}
	}
	return validationIssue{}
}

type xsiTypeOverrideInput struct {
	ctx         StartContext
	declaration startDeclaration
	declared    xsdSchema.TypeID
	override    xsdSchema.TypeID
}

func validateXSITypeOverride(
	rt *xsdSchema.Schema,
	scratch *xsdSchema.TypeDerivationScratch,
	input xsiTypeOverrideInput,
) error {
	derivation, derived := rt.TypeDerivationWithScratch(input.override, input.declared, scratch)
	if !derived {
		return validation(input.ctx, xsderrors.CodeValidationType, "xsi:type is not derived from declared type")
	}
	if !input.declaration.present || input.override == input.declared {
		return nil
	}
	if input.declaration.block&xsdSchema.DerivationExtension != 0 && derivation&xsdSchema.DerivationExtension != 0 {
		return validation(input.ctx, xsderrors.CodeValidationType, "xsi:type extension is blocked")
	}
	if input.declaration.block&xsdSchema.DerivationRestriction != 0 && derivation&xsdSchema.DerivationRestriction != 0 {
		return validation(input.ctx, xsderrors.CodeValidationType, "xsi:type restriction is blocked")
	}
	return nil
}

func resolveXSIType(
	rt *xsdSchema.Schema,
	value string,
	resolve xsdValue.QNameResolver,
	hasSchemaLocation HasSchemaLocation,
	ctx StartContext,
) (xsdSchema.TypeID, error) {
	name, ok := resolve(value)
	if !ok {
		return xsdSchema.TypeID{}, validation(ctx, xsderrors.CodeValidationType, "unknown xsi:type "+value)
	}
	ns, local := name.Namespace, name.Local
	q, knownName := rt.LookupQName(ns, local)
	if knownName {
		if typ, ok := rt.Type(q); ok {
			return typ, nil
		}
		ns = rt.Namespace(q.Namespace)
	}
	if hasSchemaLocation != nil && hasSchemaLocation(ns) {
		return xsdSchema.TypeID{}, unsupportedSchemaLocation(ctx, vocab.XSIAttrType, xsdSchema.RuntimeName{
			Name:  q,
			Known: knownName,
			NS:    ns,
			Local: local,
		})
	}
	return xsdSchema.TypeID{}, validation(ctx, xsderrors.CodeValidationType, "unknown xsi:type "+value)
}

func validation(ctx StartContext, code xsderrors.Code, msg string) error {
	return xsderrors.WithLocation(ctx.PathString(), ctx.Line, ctx.Column, xsderrors.Validation(code, msg, nil))
}

func unsupportedSchemaLocation(ctx StartContext, component string, rn xsdSchema.RuntimeName) error {
	return xsderrors.WithLocation(ctx.PathString(), ctx.Line, ctx.Column,
		xsderrors.Unsupported(
			xsderrors.CodeUnsupportedSchemaHint,
			"xsi:schemaLocation loading is not supported for "+component+" "+rn.Label(),
			nil,
		))
}

// IsXSITypeName reports whether name is the xsi:type attribute.
func IsXSITypeName(name xml.Name) bool {
	return name.Space == vocab.XSINamespaceURI && name.Local == vocab.XSIAttrType
}

type xsiStartAttributeFlags struct {
	Type           bool
	Nil            bool
	SchemaLocation bool
}

func xsiStartAttributeFlagsFor(attrs []xmlstream.Attr) xsiStartAttributeFlags {
	var flags xsiStartAttributeFlags
	for i := range attrs {
		if attrs[i].Name.Space != vocab.XSINamespaceURI {
			continue
		}
		switch attrs[i].Name.Local {
		case vocab.XSIAttrType:
			flags.Type = true
		case vocab.XSIAttrNil:
			flags.Nil = true
		case vocab.XSIAttrSchemaLocation, vocab.XSIAttrNoNamespaceSchemaLocation:
			flags.SchemaLocation = true
		}
	}
	return flags
}

func formatXMLName(n xml.Name) string {
	return xsdSchema.FormatExpandedName(n.Space, n.Local)
}

// ParseXSINil parses an xsi:nil attribute value after XML whitespace collapse.
func ParseXSINil(lexical string) (value, valid bool) {
	switch lex.CollapseXMLWhitespace(lexical) {
	case "true", "1":
		return true, true
	case "false", "0":
		return false, true
	default:
		return false, false
	}
}
