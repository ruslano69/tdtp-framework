package schema

import "github.com/jacoelho/xsd/internal/value"

// SchemaBuild is compiler-owned mutable schema state. PublishSchema is the
// only supported transition to an immutable validation Schema.
type schemaBuild struct {
	GlobalAttributes map[QName]AttributeID
	GlobalElements   map[QName]ElementID
	Notations        map[QName]bool
	GlobalIdentities map[QName]IdentityConstraintID
	GlobalTypes      map[QName]TypeID
	// typeWork charges derivation traversals to the compiler's dependency
	// budget while this mutable build is being compiled or published. A nil
	// budget is reserved for isolated unit-test fixtures.
	typeWork *workBudget
	// valueBuilder owns canonical simple-type construction until publication.
	// It is private compiler state and never escapes as a mutable object.
	valueBuilder          *value.Builder
	Substitutions         SubstitutionTable
	Models                []ContentModel
	AttributeUseSets      []AttributeUseSet
	Wildcards             []Wildcard
	CompiledModels        []CompiledModel
	SimpleTypes           []SimpleType
	simpleTypeUnavailable []bool
	Attributes            []AttributeDecl
	Elements              []ElementDecl
	Identities            []IdentityConstraint
	ComplexTypes          []ComplexType
	Names                 NameTable
	Builtin               BuiltinIDs
}

// schemaProgram is the immutable validation representation produced by one
// successful publication. It contains only data consumed after compilation;
// compiler records and provenance stay in SchemaBuild until sealing succeeds.
type schemaProgram struct {
	GlobalElements   map[QName]ElementID
	GlobalTypes      map[QName]TypeID
	Notations        map[ExpandedName]bool
	TypeDerivations  typeDerivationRead
	GlobalAttributes map[QName]AttributeID
	Value            *value.Program
	IdentityDispatch IdentityDispatchRead
	Elements         elementReadTable
	Substitutions    SubstitutionTable
	Names            nameReadView
	// SimpleTypeUnavailable preserves the compiler's optional-import recovery
	// fact. The value program owns all other simple-type metadata.
	SimpleTypeUnavailable []bool
	AttributeUseSets      []AttributeUseSetRead
	CompiledModels        []compiledModelRead
	Attributes            []AttributeDeclRead
	Wildcards             []WildcardView
	ComplexTypes          []complexTypeRead
}

type complexTypeRead struct {
	attributeUseSet AttributeUseSetID
	contentModel    ContentModelID
	textType        SimpleTypeID
	block           DerivationMask
	flags           complexTypeReadFlags
}

type complexTypeReadFlags uint8

const (
	complexTypeReadSimple complexTypeReadFlags = 1 << iota
	complexTypeReadMixed
	complexTypeReadAbstract
)

func newComplexTypeReads(types []ComplexType) []complexTypeRead {
	reads := make([]complexTypeRead, len(types))
	for i := range types {
		reads[i] = newComplexTypeRead(types[i])
	}
	return reads
}

func newComplexTypeRead(ct ComplexType) complexTypeRead {
	var flags complexTypeReadFlags
	if ct.SimpleContent() {
		flags |= complexTypeReadSimple
	}
	if ct.Mixed() {
		flags |= complexTypeReadMixed
	}
	if ct.Abstract {
		flags |= complexTypeReadAbstract
	}
	return complexTypeRead{
		attributeUseSet: ct.Attrs,
		contentModel:    ct.Content,
		textType:        ct.TextType,
		block:           ct.Block,
		flags:           flags,
	}
}

func (r complexTypeRead) typeInfo() TypeInfo {
	return newTypeInfo(typeInfoShape{
		Block:    r.block,
		Abstract: r.flags&complexTypeReadAbstract != 0,
	})
}

func (r complexTypeRead) simpleContent() simpleContentTypeRead {
	return newSimpleContentTypeRead(simpleContentTypeReadShape{
		Type:    r.textType,
		Present: r.flags&complexTypeReadSimple != 0,
	})
}

func (r complexTypeRead) textContent(fixed, constrained bool) ElementTextContent {
	return ElementTextContent{
		mixed:       r.flags&complexTypeReadMixed != 0,
		fixed:       fixed,
		constrained: constrained,
	}
}

// Schema is sealed validation-ready schema state.
type Schema struct {
	program schemaProgram
}

// TypeName returns a compiler-owned type name.
func (rt *schemaBuild) TypeName(t TypeID) QName {
	name, ok := TypeNameByID(rt.SimpleTypes, rt.ComplexTypes, t)
	if !ok {
		panic("invalid runtime type ID")
	}
	return name
}

// AnyTypeID returns the compiler-owned xs:anyType ID.
func (rt *schemaBuild) AnyTypeID() ComplexTypeID {
	return rt.Builtin.AnyType
}

func (rt *schemaBuild) spendTypeDerivationWork(steps int) error {
	if rt.typeWork == nil {
		return nil
	}
	return rt.typeWork.spend(steps)
}

// ComplexTypeCount returns the number of compiler-owned complex types.
func (rt *schemaBuild) ComplexTypeCount() int {
	return len(rt.ComplexTypes)
}

// SimpleTypeCount returns the number of compiler-owned simple types.
func (rt *schemaBuild) SimpleTypeCount() int {
	return len(rt.SimpleTypes)
}

// SimpleTypeFinal returns compiler-owned simple-type final constraints.
func (rt *schemaBuild) SimpleTypeFinal(id SimpleTypeID) (DerivationMask, bool) {
	st, ok := UsableSimpleType(rt.SimpleTypes, id)
	if !ok {
		return 0, false
	}
	return st.Final, true
}

// SimpleTypeDerivation returns compiler-owned simple-type derivation metadata.
func (rt *schemaBuild) SimpleTypeDerivation(id SimpleTypeID) (SimpleTypeDerivation, bool) {
	st, ok := UsableSimpleType(rt.SimpleTypes, id)
	if !ok {
		return SimpleTypeDerivation{}, false
	}
	return newSimpleTypeDerivationForSimpleType(*st), true
}

// ComplexTypeDerivation returns compiler-owned complex-type derivation metadata.
func (rt *schemaBuild) ComplexTypeDerivation(id ComplexTypeID) (ComplexTypeDerivation, bool) {
	ct, ok := ComplexTypeByID(rt.ComplexTypes, id)
	if !ok {
		return ComplexTypeDerivation{}, false
	}
	return newComplexTypeDerivationForComplexType(*ct), true
}

// ContentModel returns a compiler-owned content model by ID.
func (rt *schemaBuild) ContentModel(id ContentModelID) (ContentModel, bool) {
	return ContentModelByID(rt.Models, id)
}

// ElementName returns a compiler-owned element name by ID.
func (rt *schemaBuild) ElementName(id ElementID) (QName, bool) {
	decl, ok := ElementDeclByID(rt.Elements, id)
	if !ok {
		return QName{}, false
	}
	return decl.Name, true
}

// ElementType returns a compiler-owned element type by ID.
func (rt *schemaBuild) ElementType(id ElementID) (TypeID, bool) {
	return ElementTypeByID(rt.Elements, id)
}

// ElementRestriction returns compiler-owned particle-restriction metadata.
func (rt *schemaBuild) ElementRestriction(id ElementID) (ParticleRestrictionElement, bool) {
	if !ValidElementID(id, len(rt.Elements)) {
		return ParticleRestrictionElement{}, false
	}
	decl := rt.Elements[id]
	return ParticleRestrictionElement{
		Identities: borrowedIdentityConstraintIDs(decl.Identity),
		Type:       decl.Type,
		Block:      decl.Block,
		Fixed:      NewValueConstraintIdentity(decl.Fixed),
		Scope:      decl.Scope,
		Nillable:   decl.Nillable,
	}, true
}

// Wildcard returns a compiler-owned wildcard by ID.
func (rt *schemaBuild) Wildcard(id WildcardID) (Wildcard, bool) {
	return WildcardByID(rt.Wildcards, id)
}

// ForEachSubstitutionMember iterates compiler-owned substitution members.
func (rt *schemaBuild) ForEachSubstitutionMember(id ElementID, fn func(ElementID) bool) {
	rt.Substitutions.ForEachMember(id, fn)
}

// HasSubstitutionMembers reports whether a compiler-owned element has substitution members.
func (rt *schemaBuild) HasSubstitutionMembers(id ElementID) bool {
	return rt.Substitutions.HasMembers(id)
}

// SubstitutionMemberByName returns a compiler-owned substitution member by name.
func (rt *schemaBuild) SubstitutionMemberByName(id ElementID, name QName) (ElementID, bool) {
	return rt.Substitutions.MemberByName(id, name)
}

// ForEachSubstitutionEntry iterates effective substitution entries under id.
func (rt *schemaBuild) ForEachSubstitutionEntry(id ElementID, fn func(QName, ElementID) bool) {
	rt.Substitutions.ForEachEntry(id, fn)
}

// TypeLabel formats a compiler-owned type name for diagnostics.
func (rt *schemaBuild) TypeLabel(t TypeID) string {
	return rt.Names.Format(rt.TypeName(t))
}
