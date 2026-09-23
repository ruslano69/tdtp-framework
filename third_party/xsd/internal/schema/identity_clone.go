package schema

import "slices"

// NewDeclaredIdentityConstraint constructs a declared but not yet compiled
// identity constraint placeholder.
func NewDeclaredIdentityConstraint(name QName) IdentityConstraint {
	return IdentityConstraint{Name: name, Refer: NoIdentityConstraint}
}

// NewIdentityConstraint constructs identity metadata and owns the derived field
// lookup tables used at validation time.
func NewIdentityConstraint(kind IdentityKind, name QName, refer IdentityConstraintID, selector []IdentityPath, fields []IdentityField) IdentityConstraint {
	if kind != IdentityKeyRef {
		refer = NoIdentityConstraint
	}
	selector = CloneIdentityPaths(selector)
	fields = CloneIdentityFields(fields)
	elementFields, attrFields, attrWildcardFields := BuildIdentityFieldLookup(fields)
	return IdentityConstraint{
		Selector:                selector,
		Fields:                  fields,
		ElementFields:           elementFields,
		AttributeFields:         attrFields,
		AttributeWildcardFields: attrWildcardFields,
		Name:                    name,
		Refer:                   refer,
		Kind:                    kind,
	}
}

// CloneIdentityPaths deep-clones parsed identity selector path metadata.
func CloneIdentityPaths(in []IdentityPath) []IdentityPath {
	out := slices.Clone(in)
	for i := range out {
		out[i].Steps = slices.Clone(in[i].Steps)
	}
	return out
}

// CloneIdentityFields deep-clones parsed identity field metadata.
func CloneIdentityFields(in []IdentityField) []IdentityField {
	out := slices.Clone(in)
	for i := range out {
		out[i].Paths = cloneIdentityFieldPaths(in[i].Paths)
	}
	return out
}

func cloneIdentityFieldPaths(in []IdentityFieldPath) []IdentityFieldPath {
	out := slices.Clone(in)
	for i := range out {
		out[i] = cloneIdentityFieldPath(in[i])
	}
	return out
}

func cloneIdentityFieldPath(in IdentityFieldPath) IdentityFieldPath {
	in.Steps = slices.Clone(in.Steps)
	return in
}
