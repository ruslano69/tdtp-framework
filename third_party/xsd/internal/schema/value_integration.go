package schema

import (
	"errors"

	"github.com/jacoelho/xsd/internal/value"
	"github.com/jacoelho/xsd/xsderrors"
)

// newSchemaValueBuilder creates the one mutable value owner used while a
// schema is being compiled. The schema graph limits are the admission bounds
// for value types and construction storage; the value package still owns the
// detailed type and facet checks.
func newSchemaValueBuilder(limits Limits) *value.Builder {
	// Schema component admission bounds the number of value records. The
	// value builder's fixed builtin prefix is not part of that source-node
	// budget; XML attribute declarations reference fixed value IDs directly.
	maxTypes := uint64(value.BuiltinTypeCount)
	maxTypes = addBounded(maxTypes, nonNegativeLimit(limits.MaxSchemaInstantiatedNodes))
	maxTypes = min(maxTypes, uint64(^uint32(0)))

	// A value dependency chain is bounded by the compiler's component-depth
	// contract, independently of XML nesting depth. One value can revisit a
	// bounded type graph through list items and union members, so evaluation
	// work is the bounded token payload multiplied by the admitted type graph.
	maxDepth := maxComponentDependencyDepth
	maxStorage := valueStorageBound(limits)
	maxWork := valueEvalWorkBound(limits)
	return value.NewBuilder(value.BuilderOptions{
		MaxDepth:            uint16(maxDepth),
		MaxTypes:            uint32(maxTypes),
		MaxStorageBytes:     maxStorage,
		MaxConstructionWork: maxWork,
	})
}

func valueEvalWorkBound(limits Limits) uint64 {
	input := uint64(max(0, limits.MaxSchemaTokenBytes)) + 1
	types := addBounded(uint64(value.BuiltinTypeCount), nonNegativeLimit(limits.MaxSchemaInstantiatedNodes))
	return multiplyBounded(input, types)
}

func addBounded(a, b uint64) uint64 {
	if a > ^uint64(0)-b {
		return ^uint64(0)
	}
	return a + b
}

func valueStorageBound(limits Limits) uint64 {
	// typeSpecStorage charges a fixed record estimate plus copied lexical and
	// pattern bytes. Source bytes cover the latter; a fixed per admitted node
	// term covers the record and slice metadata. Checked arithmetic keeps the
	// bound itself from becoming an accidental overflow.
	const perNode = uint64(1024)
	nodes := nonNegativeLimit(limits.MaxSchemaInstantiatedNodes)
	sourceBytes := uint64(max(0, limits.MaxSchemaTotalBytes))
	bound := addBounded(multiplyBounded(sourceBytes, 2), multiplyBounded(nodes, perNode))
	if bound < uint64(value.BuiltinTypeCount)+1 {
		return uint64(value.BuiltinTypeCount) + 1
	}
	return bound
}

func nonNegativeLimit(limit int) uint64 {
	if limit < 0 {
		return 0
	}
	// NormalizeOptions rejects negative limits before this helper is reached.
	return uint64(limit)
}

func multiplyBounded(a, b uint64) uint64 {
	if a != 0 && b > ^uint64(0)/a {
		return ^uint64(0)
	}
	return a * b
}

func (c *compiler) ensureValueBuilder() *value.Builder {
	if c.rt.valueBuilder == nil {
		c.rt.valueBuilder = newSchemaValueBuilder(c.limits)
	}
	return c.rt.valueBuilder
}

// valueTypeSpec returns the canonical value metadata for one schema type. The
// schema fields are declaration facts; ValueSpec is the sole value-semantic
// source and is populated at construction time.
func valueTypeSpec(st SimpleType) value.TypeSpec {
	// Builder.Complete copies each retained slice while normalizing and compiling
	// the record. Returning the source view here avoids a redundant copy between
	// schema indexing and that ownership boundary.
	return st.ValueSpec
}

func valueBuilderError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, value.ErrLimit) {
		return xsderrors.SchemaCompile(xsderrors.CodeSchemaLimit, err.Error())
	}
	if errors.Is(err, value.ErrFacet) {
		return xsderrors.SchemaCompile(xsderrors.CodeSchemaFacet, err.Error())
	}
	if errors.Is(err, value.ErrMetadata) {
		return xsderrors.InternalInvariant(err.Error())
	}
	return err
}

func (c *compiler) completeValueType(id SimpleTypeID, st SimpleType) error {
	if id < value.BuiltinTypeCount {
		return nil
	}
	builder := c.ensureValueBuilder()
	spec := valueTypeSpec(st)
	if st.Missing {
		// A missing schema component remains unavailable to callers, but its
		// reserved ID still needs a complete value record so later IDs stay
		// canonical and the builder can seal atomically.
		spec = value.TypeSpec{
			Variety:           value.Atomic,
			Primitive:         value.PrimitiveString,
			Whitespace:        value.WhitespaceCollapse,
			WhitespacePresent: true,
			Base:              value.BuiltinType(value.PrimitiveString),
			ListItem:          value.NoType,
		}
	}
	return valueBuilderError(builder.Complete(id, spec))
}

func (c *compiler) validateValue(id SimpleTypeID, lexical string, resolve value.QNameResolver, needs value.Needs) (value.Value, []ResolvedValueName, error) {
	recorder := valueConstraintResolver{resolve: resolve}
	resolver := value.Resolver{Notation: func(namespace, local string) bool {
		q, ok := c.rt.lookupQName(namespace, local)
		return ok && c.rt.notationDeclared(q)
	}}
	if resolve != nil {
		resolver.QName = recorder.resolveQName
	}
	validated, err := c.ensureValueBuilder().Validate(id, lexical, resolver, needs, nil)
	return validated, recorder.names, err
}

func (c *compiler) validateValueLiteral(id SimpleTypeID, lexical string, resolve value.QNameResolver) (value.Value, []ResolvedValueName, error) {
	return c.validateValue(id, lexical, resolve, value.NeedCanonical|value.NeedIdentity)
}

// ValueProgram returns the immutable value program used by validation.
func (rt *Schema) ValueProgram() *value.Program {
	if rt == nil {
		return nil
	}
	return rt.program.Value
}

// NotationDeclared reports whether one expanded name is a schema notation.
func (rt *Schema) NotationDeclared(namespace, local string) bool {
	if rt == nil {
		return false
	}
	return rt.program.Notations[ExpandedName{Namespace: namespace, Local: local}]
}
