package value

// TypeView is the immutable value-program metadata needed by schema and
// validation callers. Slices in a returned view are copies owned by the
// caller; the program retains the authoritative records.
type TypeView struct {
	Union              []TypeID
	Facets             FacetView
	ID                 TypeID
	Base               TypeID
	ListItem           TypeID
	Variety            Variety
	Primitive          PrimitiveKind
	Whitespace         WhitespaceMode
	Builtin            BuiltinKind
	Identity           IdentityKind
	NeedsQNameResolver bool
}

// FacetView is the effective facet program for one type.
type FacetView struct {
	Lower          []BoundView
	Upper          []BoundView
	Enumeration    [][]LiteralView
	Patterns       [][]*Pattern
	Length         CardinalityFacet
	MinLength      CardinalityFacet
	MaxLength      CardinalityFacet
	TotalDigits    CardinalityFacet
	FractionDigits CardinalityFacet
	Present        FacetMask
	Fixed          FacetMask
}

// BoundView is one effective ordered bound.
type BoundView struct {
	Literal   LiteralView
	Exclusive bool
}

// LiteralView is the immutable value-space projection retained for a facet
// literal. The value engine owns parsing and equality; consumers do not need
// the primitive payload to reproduce the compiled facet.
type LiteralView struct {
	Canonical string
	Identity  string
	Type      TypeID
	Selected  TypeID
	IsList    bool
}

// TypeView returns effective metadata for a sealed program type.
func (p *Program) TypeView(id TypeID) (TypeView, bool) {
	if p == nil || !p.sealed {
		return TypeView{}, false
	}
	return p.viewType(id)
}

// NeedsQNameResolver reports whether validating id can reach a QName or
// NOTATION value. It is the scalar read used on the per-attribute hot path;
// callers that need facet or union records should use TypeView instead.
func (p *Program) NeedsQNameResolver(id TypeID) (needs, valid bool) {
	if p == nil || !p.sealed {
		return false, false
	}
	t, ok := p.typeDef(id)
	if !ok {
		return false, false
	}
	return t.needsQName, true
}

// IdentityKind reports the statically fixed document-identity kind of id.
// Composite members may also produce per-value ID or IDREF projections;
// callers must inspect the validated Value when recording document identity.
func (p *Program) IdentityKind(id TypeID) (kind IdentityKind, valid bool) {
	if p == nil || !p.sealed {
		return IdentityNone, false
	}
	t, ok := p.typeDef(id)
	if !ok {
		return IdentityNone, false
	}
	return t.identity, true
}

// IsUnconstrainedString reports whether id has the exact shape that can retain
// its borrowed lexical bytes through string validation: atomic xs:string
// semantics, preserve whitespace, no identity projection, and no effective
// facets. It returns false for an invalid program or type ID.
func (p *Program) IsUnconstrainedString(id TypeID) (unconstrained, valid bool) {
	if p == nil || !p.sealed {
		return false, false
	}
	t, ok := p.typeDef(id)
	if !ok {
		return false, false
	}
	return t.variety == Atomic &&
		t.primitive == PrimitiveString &&
		t.whitespace == WhitespacePreserve &&
		t.identity == IdentityNone &&
		t.facets.present == 0, true
}

// TypeView returns effective metadata for a completed incremental type.
func (b *Builder) TypeView(id TypeID) (TypeView, bool) {
	if b == nil || b.sealed || b.program == nil {
		return TypeView{}, false
	}
	if id >= BuiltinTypeCount {
		i := id - BuiltinTypeCount
		if uint64(i) >= uint64(len(b.program.complete)) || !b.program.complete[i] {
			return TypeView{}, false
		}
	}
	return b.program.viewType(id)
}

// NeedsQNameResolver reports whether a completed type can reach QName or
// NOTATION validation without constructing a copied metadata view.
func (b *Builder) NeedsQNameResolver(id TypeID) (needs, valid bool) {
	if b == nil || b.sealed || b.program == nil {
		return false, false
	}
	if id >= BuiltinTypeCount {
		i := id - BuiltinTypeCount
		if uint64(i) >= uint64(len(b.program.complete)) || !b.program.complete[i] {
			return false, false
		}
	}
	t, ok := b.program.typeDef(id)
	if !ok {
		return false, false
	}
	return t.needsQName, true
}

func (p *Program) viewType(id TypeID) (TypeView, bool) {
	t, ok := p.typeDef(id)
	if !ok {
		return TypeView{}, false
	}
	view := TypeView{
		ID:         id,
		Variety:    t.variety,
		Primitive:  t.primitive,
		Whitespace: t.whitespace,
		Builtin:    t.builtin,
		Identity:   t.identity,
		Base:       t.base,
		ListItem:   t.listItem,
		Union:      append([]TypeID(nil), t.union...),
		Facets:     facetView(t.facets),
	}
	view.NeedsQNameResolver = t.needsQName
	return view, true
}

func facetView(in facetProgram) FacetView {
	out := FacetView{
		Present:        in.present,
		Fixed:          in.fixed,
		Length:         in.length,
		MinLength:      in.minLength,
		MaxLength:      in.maxLength,
		TotalDigits:    in.totalDigits,
		FractionDigits: in.fractionDigits,
		Patterns:       clonePatterns(in.patterns),
	}
	for _, bound := range in.lower {
		out.Lower = append(out.Lower, BoundView{Exclusive: bound.exclusive, Literal: literalView(bound.value)})
	}
	for _, bound := range in.upper {
		out.Upper = append(out.Upper, BoundView{Exclusive: bound.exclusive, Literal: literalView(bound.value)})
	}
	for _, group := range in.enumGroups {
		literals := make([]LiteralView, 0, len(group))
		for _, literal := range group {
			literals = append(literals, literalView(literal))
		}
		out.Enumeration = append(out.Enumeration, literals)
	}
	return out
}

func literalView(v parsedValue) LiteralView {
	return LiteralView{
		Type:      v.typeID,
		Selected:  v.selected,
		Canonical: v.canonical,
		Identity:  v.identity,
		IsList:    v.isList,
	}
}

func (p *Program) typeNeedsQName(id TypeID, active map[TypeID]struct{}, depth int) bool {
	t, ok := p.typeDef(id)
	if !ok {
		return false
	}
	if depth >= int(p.maxDepth) {
		return false
	}
	if t.needsQNameKnown {
		return t.needsQName
	}
	if t.primitive == PrimitiveQName || t.primitive == PrimitiveNotation {
		return true
	}
	if needs, known := p.typeNeedsQNameKnownChildren(*t, depth); known {
		return needs
	}
	if active == nil {
		active = make(map[TypeID]struct{})
	}
	if _, seen := active[id]; seen {
		return false
	}
	active[id] = struct{}{}
	defer delete(active, id)
	return p.typeNeedsQNameChildren(*t, active, depth)
}

func (p *Program) typeNeedsQNameChildren(t typeDef, active map[TypeID]struct{}, depth int) bool {
	if t.variety == List {
		return p.typeNeedsQName(t.listItem, active, depth+1)
	}
	if t.variety == Union {
		for _, member := range t.union {
			if p.typeNeedsQName(member, active, depth+1) {
				return true
			}
		}
	}
	return false
}

func (p *Program) typeNeedsQNameKnownChildren(t typeDef, depth int) (needs bool, known bool) {
	if depth+1 >= int(p.maxDepth) && (t.variety == List || t.variety == Union) {
		return false, true
	}
	switch t.variety {
	case Atomic:
		return false, true
	case List:
		return p.knownQNameChild(t.listItem)
	case Union:
		return p.knownQNameUnion(t.union)
	default:
		return false, false
	}
}

func (p *Program) knownQNameChild(id TypeID) (needs bool, known bool) {
	child, ok := p.typeDef(id)
	if !ok || !child.needsQNameKnown {
		return false, false
	}
	return child.needsQName, true
}

func (p *Program) knownQNameUnion(union []TypeID) (needs bool, known bool) {
	for _, member := range union {
		childNeeds, childKnown := p.knownQNameChild(member)
		if !childKnown {
			return false, false
		}
		if childNeeds {
			return true, true
		}
	}
	return false, true
}
