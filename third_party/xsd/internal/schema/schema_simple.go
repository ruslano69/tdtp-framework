package schema

// SimpleTypeIdentityRuntime exposes the value-owned document-identity
// projection at schema validation boundaries.
type SimpleTypeIdentityRuntime interface {
	SimpleTypeIdentity(id SimpleTypeID) (SimpleIdentityKind, bool)
}

// SimpleTypeIdentity returns compiler-owned identity metadata.
func (rt *schemaBuild) SimpleTypeIdentity(id SimpleTypeID) (SimpleIdentityKind, bool) {
	st, ok := SimpleTypeByID(rt.SimpleTypes, id)
	if !ok {
		return SimpleIdentityNone, false
	}
	if rt.valueBuilder != nil {
		if view, complete := rt.valueBuilder.TypeView(id); complete {
			return view.Identity, true
		}
	}
	return st.ValueSpec.Identity, true
}
