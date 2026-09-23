package schema

import (
	"slices"

	valuepkg "github.com/jacoelho/xsd/internal/value"
	"github.com/jacoelho/xsd/internal/vocab"
	"github.com/jacoelho/xsd/internal/xsdregex"
	"github.com/jacoelho/xsd/xsderrors"
)

type facetChildMode uint8

const (
	facetChildModeInvalid facetChildMode = iota
	facetChildModeDerivation
	facetChildModeExplicitList
)

func (c *compiler) compileFacets(parent *schemaNode, st *SimpleType, base, literalType SimpleTypeID) error {
	return withSchemaCompileLocation(parent, c.compileFacetChildren(typedXSDChildren(parent), st, base, literalType, facetChildModeDerivation))
}

func (c *compiler) compileFacetList(children []*schemaNode, st *SimpleType, base, literalType SimpleTypeID) error {
	return c.compileFacetChildren(children, st, base, literalType, facetChildModeExplicitList)
}

// compileFacetChildren admits source facet syntax and records one local facet
// step in ValueSpec. Effective inheritance, typed literal evaluation, bound
// ordering, and fixed-facet derivation are owned by value.Builder.Complete.
func (c *compiler) compileFacetChildren(children []*schemaNode, st *SimpleType, base, literalType SimpleTypeID, mode facetChildMode) error {
	if err := validateFacetChildMode(mode); err != nil {
		return err
	}
	var state compiledFacetState
	for _, child := range children {
		if !isFacetCompilationChild(child, mode) {
			continue
		}
		if err := c.compileFacetChild(child, st, base, literalType, &state); err != nil {
			return err
		}
	}
	if !state.sawFacet {
		return nil
	}
	state.apply(st)
	if err := ValidateOrderedFacetStep(state.orderedStep); err != nil {
		return xsderrors.SchemaCompile(xsderrors.CodeSchemaFacet, err.Error())
	}
	return nil
}

// validateUnavailableFacetChildren performs source admission for a type whose
// base is intentionally unavailable. Such a type is never validated, but its
// local metadata still needs to remain structurally safe until publication.
func (c *compiler) validateUnavailableFacetChildren(
	children []*schemaNode,
	st *SimpleType,
	base SimpleTypeID,
	mode facetChildMode,
) error {
	if err := validateFacetChildMode(mode); err != nil {
		return err
	}
	var state compiledFacetState
	for _, child := range children {
		if !isFacetCompilationChild(child, mode) {
			continue
		}
		if err := c.validateUnavailableFacetChild(child, st, base, &state); err != nil {
			return err
		}
	}
	if err := ValidateOrderedFacetStep(state.orderedStep); err != nil {
		return xsderrors.SchemaCompile(xsderrors.CodeSchemaFacet, err.Error())
	}
	state.apply(st)
	return nil
}

func (c *compiler) validateUnavailableFacetChild(child *schemaNode, st *SimpleType, base SimpleTypeID, state *compiledFacetState) error {
	compile, err := validateFacetChildSource(child, st, &state.stepSingleFacets)
	if err != nil || !compile {
		return err
	}
	facet, err := facetAttrs(child)
	if err != nil {
		return err
	}
	switch child.local {
	case vocab.XSDFacetLength, vocab.XSDFacetMinLength, vocab.XSDFacetMaxLength,
		vocab.XSDFacetTotalDigits, vocab.XSDFacetFractionDigits:
		return compileSizeFacet(st, child, facet.value, facet.fixed)
	case vocab.XSDFacetMinInclusive:
		state.orderedStep.MinInclusive = true
	case vocab.XSDFacetMaxInclusive:
		state.orderedStep.MaxInclusive = true
	case vocab.XSDFacetMinExclusive:
		state.orderedStep.MinExclusive = true
	case vocab.XSDFacetMaxExclusive:
		state.orderedStep.MaxExclusive = true
	case vocab.XSDFacetPattern:
		pattern, err := xsdregex.Compile(facet.value, xsdregex.CompileOptions{})
		if err != nil {
			return withSchemaCompileLocation(child, schemaRegexError(err))
		}
		state.valuePatterns = append(state.valuePatterns, pattern)
		state.sawFacet = true
	case vocab.XSDFacetWhiteSpace:
		return c.compileWhitespaceFacet(st, base, child, facet.value, facet.fixed)
	}
	return nil
}

func validateFacetChildMode(mode facetChildMode) error {
	switch mode {
	case facetChildModeDerivation, facetChildModeExplicitList:
		return nil
	case facetChildModeInvalid:
		return xsderrors.InternalInvariant("invalid facet child mode")
	default:
		return xsderrors.InternalInvariant("unknown facet child mode")
	}
}

func isFacetCompilationChild(child *schemaNode, mode facetChildMode) bool {
	if child.kind == schemaKindForeign || child.local == vocab.XSDElemAnnotation || child.local == vocab.XSDElemSimpleType {
		return false
	}
	return mode == facetChildModeExplicitList || IsFacetLocal(child.local)
}

type compiledFacetState struct {
	valueEnumeration []valuepkg.LiteralSpec
	valuePatterns    []*valuepkg.Pattern
	orderedStep      OrderedFacetStep
	stepSingleFacets FacetMask
	sawEnumeration   bool
	sawFacet         bool
}

func (s *compiledFacetState) beginStep() {
	s.sawFacet = true
}

func (s *compiledFacetState) apply(st *SimpleType) {
	if s.sawEnumeration {
		st.ValueSpec.Facets.Enumeration = slices.Clone(s.valueEnumeration)
		st.ValueSpec.Facets.Present |= valuepkg.FacetEnumeration
	}
	if len(s.valuePatterns) != 0 {
		// Patterns declared by one restriction step form one OR group. The
		// value builder intersects groups inherited through the base chain.
		st.ValueSpec.Facets.Patterns = append(st.ValueSpec.Facets.Patterns, slices.Clone(s.valuePatterns))
		st.ValueSpec.Facets.Present |= valuepkg.FacetPattern
	}
}

func (c *compiler) compileFacetChild(child *schemaNode, st *SimpleType, base, literalType SimpleTypeID, state *compiledFacetState) error {
	compile, err := validateFacetChildSource(child, st, &state.stepSingleFacets)
	if err != nil || !compile {
		return err
	}
	state.beginStep()
	facet, err := facetAttrs(child)
	if err != nil {
		return err
	}
	return c.compileFacetValue(child, st, base, literalType, state, facet)
}

func validateFacetChildSource(child *schemaNode, st *SimpleType, single *FacetMask) (bool, error) {
	source := child.semantic.facet()
	if source == nil {
		return false, withSchemaCompileLocation(child, xsderrors.InternalInvariant("facet node has no typed facet source"))
	}
	compile, err := ValidateFacetSource(FacetSource{
		Local: child.local, InXSDNamespace: child.kind != schemaKindForeign, HasValue: source.Value.Present,
		Variety: st.ValueSpec.Variety, Primitive: st.ValueSpec.Primitive,
	})
	if err != nil {
		return false, withSchemaCompileLocation(child, err)
	}
	if !compile {
		return false, nil
	}
	mask, _ := facetMaskForLocal(child.local)
	if mask != FacetPattern && mask != FacetEnumeration {
		if *single&mask != 0 {
			return false, withSchemaCompileLocation(child, xsderrors.SchemaCompile(xsderrors.CodeSchemaFacet, "duplicate "+child.local+" facet"))
		}
		*single |= mask
	}
	return true, nil
}

func (c *compiler) compileFacetValue(child *schemaNode, st *SimpleType, base, literalType SimpleTypeID, state *compiledFacetState, facet facetInput) error {
	switch child.local {
	case vocab.XSDFacetLength, vocab.XSDFacetMinLength, vocab.XSDFacetMaxLength, vocab.XSDFacetTotalDigits, vocab.XSDFacetFractionDigits:
		return compileSizeFacet(st, child, facet.value, facet.fixed)
	case vocab.XSDFacetMinInclusive, vocab.XSDFacetMaxInclusive, vocab.XSDFacetMinExclusive, vocab.XSDFacetMaxExclusive:
		return c.compileBoundFacet(st, base, child, facet.value, facet.fixed, &state.orderedStep)
	case vocab.XSDFacetEnumeration:
		return c.compileEnumerationFacet(child, literalType, facet.value, state)
	case vocab.XSDFacetPattern:
		return compilePatternFacet(child, facet.value, state)
	case vocab.XSDFacetWhiteSpace:
		return c.compileWhitespaceFacet(st, base, child, facet.value, facet.fixed)
	}
	return nil
}

func (c *compiler) compileEnumerationFacet(child *schemaNode, literalType SimpleTypeID, lexical string, state *compiledFacetState) error {
	literal, err := c.compileLiteral(literalType, lexical, schemaQNameResolver(child))
	if err != nil {
		return withSchemaCompileLocation(child, err)
	}
	state.valueEnumeration = append(state.valueEnumeration, literal)
	state.sawEnumeration = true
	return nil
}

func compilePatternFacet(child *schemaNode, patternText string, state *compiledFacetState) error {
	pattern, err := xsdregex.Compile(patternText, xsdregex.CompileOptions{})
	if err != nil {
		return withSchemaCompileLocation(child, schemaRegexError(err))
	}
	state.valuePatterns = append(state.valuePatterns, pattern)
	return nil
}

type facetInput struct {
	value string
	fixed facetFixedness
}

type facetFixedness uint8

const (
	facetFixednessInvalid facetFixedness = iota
	facetVariable
	facetFixed
)

func validateFacetFixedness(fixedness facetFixedness) error {
	switch fixedness {
	case facetVariable, facetFixed:
		return nil
	case facetFixednessInvalid:
		return xsderrors.InternalInvariant("facet fixedness is invalid")
	default:
		return xsderrors.InternalInvariant("facet fixedness is unknown")
	}
}

func facetAttrs(n *schemaNode) (facetInput, error) {
	source := n.semantic.facet()
	if source == nil {
		return facetInput{}, withSchemaCompileLocation(n, xsderrors.InternalInvariant("facet node has no typed facet source"))
	}
	value := source.Value.Value
	fixed, err := ParseBooleanAttr(BooleanAttr{Name: vocab.XSDAttrFixed, Value: source.Fixed.Value, HasValue: source.Fixed.Present})
	if err != nil {
		return facetInput{}, err
	}
	fixedness := facetVariable
	if fixed {
		fixedness = facetFixed
	}
	return facetInput{value: value, fixed: fixedness}, nil
}

func compileSizeFacet(st *SimpleType, node *schemaNode, facetValue string, fixedness facetFixedness) error {
	if err := validateFacetFixedness(fixedness); err != nil {
		return err
	}
	name := node.local
	size, err := ParseSizeFacetValue(name, facetValue)
	if err != nil {
		return withSchemaCompileLocation(node, err)
	}
	var flag FacetMask
	switch name {
	case vocab.XSDFacetLength:
		flag = FacetLength
	case vocab.XSDFacetMinLength:
		flag = FacetMinLength
	case vocab.XSDFacetMaxLength:
		flag = FacetMaxLength
	case vocab.XSDFacetTotalDigits:
		flag = FacetTotalDigits
	case vocab.XSDFacetFractionDigits:
		flag = FacetFractionDigits
	}
	setValueCardinalityFacet(&st.ValueSpec.Facets, flag, size)
	if fixedness == facetFixed {
		st.ValueSpec.Facets.Fixed |= flag
	}
	return nil
}

func (c *compiler) compileBoundFacet(st *SimpleType, base SimpleTypeID, child *schemaNode, lexical string, fixedness facetFixedness, step *OrderedFacetStep) error {
	if err := validateFacetFixedness(fixedness); err != nil {
		return err
	}
	literal, err := c.compileLiteral(base, lexical, schemaQNameResolver(child))
	if err != nil {
		return err
	}
	var flag FacetMask
	switch child.local {
	case vocab.XSDFacetMinInclusive:
		flag = FacetMinInclusive
		step.MinInclusive = true
	case vocab.XSDFacetMaxInclusive:
		flag = FacetMaxInclusive
		step.MaxInclusive = true
	case vocab.XSDFacetMinExclusive:
		flag = FacetMinExclusive
		step.MinExclusive = true
	case vocab.XSDFacetMaxExclusive:
		flag = FacetMaxExclusive
		step.MaxExclusive = true
	}
	setValueBoundFacet(&st.ValueSpec.Facets, flag, literal, fixedness)
	return nil
}

func (c *compiler) compileWhitespaceFacet(st *SimpleType, base SimpleTypeID, n *schemaNode, facetValue string, fixedness facetFixedness) error {
	if err := validateFacetFixedness(fixedness); err != nil {
		return err
	}
	mode, err := ParseWhitespaceFacetValue(facetValue, c.rt.simpleTypeWhitespace(base))
	if err != nil {
		return withSchemaCompileLocation(n, err)
	}
	st.ValueSpec.Whitespace = mode
	st.ValueSpec.WhitespacePresent = true
	if fixedness == facetFixed {
		st.ValueSpec.Facets.Fixed |= FacetWhiteSpace
	}
	return nil
}

func setValueCardinalityFacet(f *valuepkg.FacetSpec, flag FacetMask, size uint32) {
	f.Present |= flag
	cardinality := valuepkg.CardinalityFacet{Value: size, Present: true}
	switch flag {
	case FacetLength:
		f.Length = cardinality
	case FacetMinLength:
		f.MinLength = cardinality
	case FacetMaxLength:
		f.MaxLength = cardinality
	case FacetTotalDigits:
		f.TotalDigits = cardinality
	case FacetFractionDigits:
		f.FractionDigits = cardinality
	}
}

func setValueBoundFacet(f *valuepkg.FacetSpec, flag FacetMask, literal valuepkg.LiteralSpec, fixedness facetFixedness) {
	bound := valuepkg.BoundFacet{LiteralSpec: literal, Present: true}
	switch flag {
	case FacetMinInclusive:
		f.MinInclusive = bound
	case FacetMaxInclusive:
		f.MaxInclusive = bound
	case FacetMinExclusive:
		f.MinExclusive = bound
	case FacetMaxExclusive:
		f.MaxExclusive = bound
	}
	f.Present |= flag
	if fixedness == facetFixed {
		f.Fixed |= flag
	}
}

func (c *compiler) compileLiteral(base SimpleTypeID, lexical string, resolve valuepkg.QNameResolver) (valuepkg.LiteralSpec, error) {
	_, names, err := c.validateValueLiteral(base, lexical, resolve)
	if err != nil {
		return valuepkg.LiteralSpec{}, FacetValueError(lexical, err)
	}
	literal := valuepkg.LiteralSpec{
		Lexical: lexical,
		Type:    base,
		Resolver: valuepkg.Resolver{Notation: func(namespace, local string) bool {
			q, ok := c.rt.lookupQName(namespace, local)
			return ok && c.rt.notationDeclared(q)
		}},
	}
	if len(names) != 0 {
		replay, replayErr := NewValueConstraintNameReplay(names)
		if replayErr != nil {
			return valuepkg.LiteralSpec{}, FacetValueError(lexical, replayErr)
		}
		literal.Resolver.QName = replay.ResolveQName
	}
	return literal, nil
}

func schemaRegexError(err error) error {
	if err == nil {
		return nil
	}
	return xsderrors.SchemaCompile(xsderrors.CodeSchemaFacet, "invalid pattern: "+err.Error())
}
