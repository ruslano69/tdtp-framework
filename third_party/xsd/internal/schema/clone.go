package schema

import "slices"

// CloneWildcard deep-clones wildcard metadata.
func CloneWildcard(in Wildcard) Wildcard {
	in.Namespaces = slices.Clone(in.Namespaces)
	return in
}

// CloneContentModel deep-clones content-model metadata.
func CloneContentModel(in ContentModel) ContentModel {
	in.Particles = slices.Clone(in.Particles)
	in.ChoiceLimits = slices.Clone(in.ChoiceLimits)
	return in
}

// CloneSimpleTypeDerivation deep-clones simple-type derivation projection
// metadata.
func CloneSimpleTypeDerivation(in SimpleTypeDerivation) SimpleTypeDerivation {
	in.Union = slices.Clone(in.Union)
	return in
}

// CloneValueConstraintSimpleType deep-clones value-constraint simple-type
// projection metadata.
func CloneValueConstraintSimpleType(in ValueConstraintSimpleType) ValueConstraintSimpleType {
	in.Union = slices.Clone(in.Union)
	return in
}

// CloneValueConstraintIdentity deep-clones value-constraint identity
// projection metadata.
func CloneValueConstraintIdentity(in ValueConstraintIdentity) ValueConstraintIdentity {
	in.ResolvedNames = slices.Clone(in.ResolvedNames)
	return in
}
