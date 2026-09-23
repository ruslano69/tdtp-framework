package schema

type elementReadFlags uint8

const (
	elementReadAbstract elementReadFlags = 1 << iota
	elementReadNillable
)

type elementReadMeta struct {
	typ           TypeID
	identityStart int
	identityCount int
	constraint    int
	block         DerivationMask
	flags         elementReadFlags
}

type elementConstraintRead struct {
	value ValueConstraintRead
	fixed bool
}

// elementReadTable is the sole published owner of element declaration facts.
// Names stay columnar because content-model matching reads them without the
// colder start, identity, and value-constraint metadata.
type elementReadTable struct {
	names       []QName
	meta        []elementReadMeta
	identities  []IdentityConstraintID
	constraints []elementConstraintRead
}

func newElementReadTable(decls []ElementDecl, complexTypes []ComplexType) elementReadTable {
	identityCount, constraintCount := elementReadStorageCounts(decls)
	table := elementReadTable{
		names:       make([]QName, len(decls)),
		meta:        make([]elementReadMeta, len(decls)),
		identities:  make([]IdentityConstraintID, 0, identityCount),
		constraints: make([]elementConstraintRead, 0, constraintCount),
	}
	for i := range decls {
		table.addDeclaration(i, decls[i], complexTypes)
	}
	return table
}

func elementReadStorageCounts(decls []ElementDecl) (identityCount, constraintCount int) {
	for i := range decls {
		identityCount += len(decls[i].Identity)
		if decls[i].Fixed != nil || decls[i].Default != nil {
			constraintCount++
		}
	}
	return identityCount, constraintCount
}

func (t *elementReadTable) addDeclaration(index int, decl ElementDecl, complexTypes []ComplexType) {
	t.names[index] = decl.Name
	meta := elementReadMeta{
		typ:           decl.Type,
		block:         effectiveElementBlock(decl, complexTypes),
		identityStart: len(t.identities),
		identityCount: len(decl.Identity),
		constraint:    -1,
		flags:         elementFlags(decl),
	}
	t.identities = append(t.identities, decl.Identity...)
	meta.constraint = t.addValueConstraint(decl)
	t.meta[index] = meta
}

func elementFlags(decl ElementDecl) elementReadFlags {
	var flags elementReadFlags
	if decl.Abstract {
		flags |= elementReadAbstract
	}
	if decl.Nillable {
		flags |= elementReadNillable
	}
	return flags
}

func (t *elementReadTable) addValueConstraint(decl ElementDecl) int {
	if decl.Fixed != nil {
		value, _ := newValueConstraintReadFromConstraint(decl.Fixed)
		t.constraints = append(t.constraints, elementConstraintRead{value: value, fixed: true})
		return len(t.constraints) - 1
	}
	if decl.Default != nil {
		value, _ := newValueConstraintReadFromConstraint(decl.Default)
		t.constraints = append(t.constraints, elementConstraintRead{value: value})
		return len(t.constraints) - 1
	}
	return -1
}

func (t *elementReadTable) name(id ElementID) (QName, bool) {
	if !ValidElementID(id, len(t.meta)) || len(t.names) != len(t.meta) {
		return QName{}, false
	}
	return t.names[id], true
}

func (t *elementReadTable) start(id ElementID) (ElementStartInfo, bool) {
	if !ValidElementID(id, len(t.meta)) || len(t.names) != len(t.meta) {
		return ElementStartInfo{}, false
	}
	meta := t.meta[id]
	constraint, valid := t.constraintAt(meta.constraint)
	if !valid {
		return ElementStartInfo{}, false
	}
	return ElementStartInfo{
		Type:     meta.typ,
		Block:    meta.block,
		Abstract: meta.flags&elementReadAbstract != 0,
		Nillable: meta.flags&elementReadNillable != 0,
		Fixed:    constraint != nil && constraint.fixed,
		Default:  constraint != nil && !constraint.fixed,
	}, true
}

func (t *elementReadTable) identityConstraints(id ElementID) (IdentityConstraintIDs, bool) {
	if !ValidElementID(id, len(t.meta)) {
		return IdentityConstraintIDs{}, false
	}
	meta := t.meta[id]
	end := meta.identityStart + meta.identityCount
	if meta.identityStart < 0 || meta.identityCount < 0 || end < meta.identityStart || end > len(t.identities) {
		return IdentityConstraintIDs{}, false
	}
	return borrowedIdentityConstraintIDs(t.identities[meta.identityStart:end]), true
}

func (t *elementReadTable) valueConstraints(id ElementID) (constraints ElementValueConstraints, present, valid bool) {
	if id == NoElement {
		return ElementValueConstraints{}, false, true
	}
	if !ValidElementID(id, len(t.meta)) {
		return ElementValueConstraints{}, false, false
	}
	meta := t.meta[id]
	constraint, ok := t.constraintAt(meta.constraint)
	if !ok {
		return ElementValueConstraints{}, false, false
	}
	if constraint == nil {
		return newElementValueConstraints(meta.typ, ValueConstraintRead{}, false, ValueConstraintRead{}, false), true, true
	}
	if constraint.fixed {
		return newElementValueConstraints(meta.typ, constraint.value, true, ValueConstraintRead{}, false), true, true
	}
	return newElementValueConstraints(meta.typ, ValueConstraintRead{}, false, constraint.value, true), true, true
}

func (t *elementReadTable) constraintAt(index int) (*elementConstraintRead, bool) {
	if index < 0 {
		return nil, true
	}
	if index >= len(t.constraints) {
		return nil, false
	}
	return &t.constraints[index], true
}

func (t *elementReadTable) constraintFlags(id ElementID) (fixed, present, valid bool) {
	if id == NoElement {
		return false, false, true
	}
	if !ValidElementID(id, len(t.meta)) {
		return false, false, false
	}
	constraint, valid := t.constraintAt(t.meta[id].constraint)
	if constraint == nil {
		return false, false, valid
	}
	return constraint.fixed, true, true
}

func effectiveElementBlock(decl ElementDecl, complexTypes []ComplexType) DerivationMask {
	block := decl.Block
	if id, ok := decl.Type.Complex(); ok && ValidComplexTypeID(id, len(complexTypes)) {
		block |= complexTypes[id].Block
	}
	return block
}
