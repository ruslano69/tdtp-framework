package schema

// AttributeDecl returns the validation read projection for an attribute.
func (rt *Schema) AttributeDecl(id AttributeID) (AttributeDeclRead, bool) {
	return attributeDeclReadByID(rt.program.Attributes, id)
}

// ElementIdentityConstraints returns an immutable view of identity constraints
// attached to an element.
func (rt *Schema) ElementIdentityConstraints(id ElementID) (IdentityConstraintIDs, bool) {
	return rt.program.Elements.identityConstraints(id)
}

// HasIdentityConstraints reports whether the schema has identity constraints.
func (rt *Schema) HasIdentityConstraints() bool {
	return rt.program.IdentityDispatch.ConstraintCount() != 0
}

// IdentityConstraint returns the aggregate validation read for an identity constraint.
func (rt *Schema) IdentityConstraint(id IdentityConstraintID) (IdentityConstraintRead, bool) {
	return rt.program.IdentityDispatch.identityConstraint(id)
}

// IdentityDispatch returns the immutable selector and field dispatch index
// derived during schema publication.
func (rt *Schema) IdentityDispatch() IdentityDispatchRead {
	return rt.program.IdentityDispatch
}

func (rt *Schema) complexAttributeUses(id ComplexTypeID) (AttributeUseSetRead, bool) {
	if !ValidComplexTypeID(id, len(rt.program.ComplexTypes)) {
		return AttributeUseSetRead{}, false
	}
	set := rt.program.ComplexTypes[id].attributeUseSet
	if !ValidAttributeUseSetID(set, len(rt.program.AttributeUseSets)) {
		return AttributeUseSetRead{}, false
	}
	return rt.program.AttributeUseSets[set], true
}

// AttributeUseSetForType returns attribute-use reads for a runtime type.
func (rt *Schema) AttributeUseSetForType(typ TypeID) (set AttributeUseSetRead, present, valid bool) {
	id, ok := typ.Complex()
	if !ok {
		return AttributeUseSetRead{}, false, true
	}
	set, valid = rt.complexAttributeUses(id)
	return set, true, valid
}

// SimpleContentType returns the simple-content type for a runtime type.
func (rt *Schema) SimpleContentType(t TypeID) (simpleID SimpleTypeID, present, valid bool) {
	if id, ok := t.Simple(); ok {
		_, present, valid = rt.simpleTypeAvailability(id)
		return id, present, valid
	}
	id, ok := t.Complex()
	if !ok || !ValidComplexTypeID(id, len(rt.program.ComplexTypes)) {
		return NoSimpleType, false, false
	}
	read := rt.program.ComplexTypes[id].simpleContent()
	if !read.HasSimpleContent() {
		return NoSimpleType, false, true
	}
	textType := read.TypeID()
	_, present, valid = rt.simpleTypeAvailability(textType)
	return textType, present, valid
}

// ElementValueConstraints returns value constraints for an element declaration.
func (rt *Schema) ElementValueConstraints(id ElementID) (constraints ElementValueConstraints, present, valid bool) {
	return rt.program.Elements.valueConstraints(id)
}

// ElementTextContent returns text-content metadata for a runtime type and element.
func (rt *Schema) ElementTextContent(t TypeID, elem ElementID) (ElementTextContent, bool) {
	fixed, constrained, valid := rt.program.Elements.constraintFlags(elem)
	if !valid {
		return ElementTextContent{}, false
	}
	if id, ok := t.Complex(); ok {
		if !ValidComplexTypeID(id, len(rt.program.ComplexTypes)) {
			return ElementTextContent{}, false
		}
		return rt.program.ComplexTypes[id].textContent(fixed, constrained), true
	}
	id, ok := t.Simple()
	if !ok {
		return ElementTextContent{}, false
	}
	if _, _, valid := rt.simpleTypeAvailability(id); !valid {
		return ElementTextContent{}, false
	}
	return ElementTextContent{constrained: constrained}, true
}
