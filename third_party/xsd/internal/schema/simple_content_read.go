package schema

// simpleContentTypeReadShape is the runtime-read projection needed to choose
// the simple type used for element text validation.
type simpleContentTypeReadShape struct {
	Type    SimpleTypeID
	Present bool
}

// simpleContentTypeRead exposes simple-content type facts without exposing the
// raw complex-type table.
type simpleContentTypeRead struct {
	typ     SimpleTypeID
	present bool
}

// newSimpleContentTypeRead returns the immutable simple-content type
// projection for one complex type.
func newSimpleContentTypeRead(shape simpleContentTypeReadShape) simpleContentTypeRead {
	typ := shape.Type
	if !shape.Present {
		typ = NoSimpleType
	}
	return simpleContentTypeRead{
		typ:     typ,
		present: shape.Present,
	}
}

// TypeID returns the text type used for simple-content validation.
func (r simpleContentTypeRead) TypeID() SimpleTypeID {
	return r.typ
}

// HasSimpleContent reports whether the complex type has simple content.
func (r simpleContentTypeRead) HasSimpleContent() bool {
	return r.present
}
