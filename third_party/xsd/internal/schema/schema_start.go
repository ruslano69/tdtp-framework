package schema

type simpleTypeAvailability uint8

const (
	simpleTypeAvailabilityInvalid simpleTypeAvailability = iota
	simpleTypeAvailabilityAvailable
	simpleTypeAvailabilityUnavailable
)

// AnyType returns the runtime xs:anyType reference for validation start assessment.
func (rt *Schema) AnyType() TypeID {
	return ComplexRef(rt.program.TypeDerivations.AnyTypeID())
}

// RootElement returns the global element declaration for name.
func (rt *Schema) RootElement(name RuntimeName) (ElementID, ElementStartInfo, bool) {
	if !name.Known {
		return NoElement, ElementStartInfo{}, false
	}
	id, ok := rt.program.GlobalElements[name.Name]
	if !ok {
		return NoElement, ElementStartInfo{}, false
	}
	info, ok := rt.program.Elements.start(id)
	return id, info, ok
}

// Element returns validation start data for an element declaration.
func (rt *Schema) Element(id ElementID) (ElementStartInfo, bool) {
	return rt.program.Elements.start(id)
}

// Type returns the global type for name.
func (rt *Schema) Type(name QName) (TypeID, bool) {
	return rt.GlobalType(name)
}

// GlobalType returns the global type declaration for name.
func (rt *Schema) GlobalType(name QName) (TypeID, bool) {
	return globalTypeByName(rt.program.GlobalTypes, rt.program.TypeDerivations, name)
}

// LookupQName returns the runtime QName for a namespace URI and local name.
func (rt *Schema) LookupQName(ns, local string) (QName, bool) {
	return rt.program.Names.LookupQName(ns, local)
}

// Namespace returns the namespace URI for id.
func (rt *Schema) Namespace(id NamespaceID) string {
	return rt.program.Names.Namespace(id)
}

// TypeInfo returns validation start data for a runtime type.
func (rt *Schema) TypeInfo(id TypeID) (TypeInfo, bool) {
	if simple, ok := id.Simple(); ok {
		availability, _, ok := rt.simpleTypeAvailability(simple)
		if !ok {
			return TypeInfo{}, false
		}
		return newTypeInfo(typeInfoShape{Unavailable: availability == simpleTypeAvailabilityUnavailable}), true
	}
	complexID, ok := id.Complex()
	if !ok || !ValidComplexTypeID(complexID, len(rt.program.ComplexTypes)) {
		return TypeInfo{}, false
	}
	read := rt.program.ComplexTypes[complexID]
	info := read.typeInfo()
	if simple := read.simpleContent(); simple.HasSimpleContent() {
		availability, present, ok := rt.simpleTypeAvailability(simple.TypeID())
		if !ok || !present {
			return TypeInfo{}, false
		}
		info.Unavailable = availability == simpleTypeAvailabilityUnavailable
	}
	return info, true
}

func (rt *Schema) simpleTypeAvailability(id SimpleTypeID) (availability simpleTypeAvailability, present, valid bool) {
	if rt == nil || rt.program.Value == nil || !ValidSimpleTypeID(id, len(rt.program.SimpleTypeUnavailable)) {
		return simpleTypeAvailabilityInvalid, false, false
	}
	if unavailable := rt.program.SimpleTypeUnavailable[id]; unavailable {
		return simpleTypeAvailabilityUnavailable, true, true
	}
	if _, ok := rt.program.Value.NeedsQNameResolver(id); !ok {
		return simpleTypeAvailabilityInvalid, false, false
	}
	return simpleTypeAvailabilityAvailable, true, true
}

// SimpleTypeUnavailable reports whether id contains an intentionally absent
// schema subcomponent. ok is false only for invalid published metadata.
func (rt *Schema) SimpleTypeUnavailable(id SimpleTypeID) (unavailable, ok bool) {
	availability, _, ok := rt.simpleTypeAvailability(id)
	return availability == simpleTypeAvailabilityUnavailable, ok
}

// TypeDerivation reports how derived derives from base.
func (rt *Schema) TypeDerivation(derived, base TypeID) (DerivationMask, bool) {
	return rt.program.TypeDerivations.derivation(derived, base, nil)
}

// TypeDerivationWithScratch reports how derived derives from base while reusing
// document-local union traversal storage.
func (rt *Schema) TypeDerivationWithScratch(derived, base TypeID, scratch *TypeDerivationScratch) (DerivationMask, bool) {
	return rt.program.TypeDerivations.derivation(derived, base, scratch)
}

// ContentModelForType returns the content model used to validate children of a runtime type.
func (rt *Schema) ContentModelForType(t TypeID) ContentModelID {
	id, ok := t.Complex()
	if !ok || !ValidComplexTypeID(id, len(rt.program.ComplexTypes)) {
		return NoContentModel
	}
	return rt.program.ComplexTypes[id].contentModel
}

// GlobalAttribute returns the global attribute declaration for name.
func (rt *Schema) GlobalAttribute(name QName) (id AttributeID, present, valid bool) {
	return GlobalAttributeByName(rt.program.GlobalAttributes, rt.program.Attributes, name)
}

// WildcardView returns a validation-facing wildcard view.
func (rt *Schema) WildcardView(id WildcardID) (WildcardView, bool) {
	return wildcardViewByID(rt.program.Wildcards, id)
}
