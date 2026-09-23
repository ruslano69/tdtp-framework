package schema

// ElementTextContent summarizes whether character data is allowed in one
// element frame.
type ElementTextContent struct {
	mixed       bool
	fixed       bool
	constrained bool
}

// AllowsMixedContent reports whether non-whitespace character data is allowed
// alongside child elements.
func (c ElementTextContent) AllowsMixedContent() bool {
	return c.mixed
}

// HasFixedElementValue reports whether a fixed element constraint requires
// retaining mixed character data for later comparison.
func (c ElementTextContent) HasFixedElementValue() bool {
	return c.fixed
}

// HasValueConstraint reports whether the element has a default or fixed value.
func (c ElementTextContent) HasValueConstraint() bool {
	return c.constrained
}
