package schema

import "github.com/jacoelho/xsd/xsderrors"

func (c *compiler) compileWildcardParticle(n *schemaNode, ctx *schemaContext) (Particle, error) {
	if err := checkAnyParticleChildren(n); err != nil {
		return Particle{}, err
	}
	id, err := c.compileWildcard(n, ctx)
	if err != nil {
		return Particle{}, err
	}
	occurs, err := parseOccurs(n, c.limits)
	if err != nil {
		return Particle{}, err
	}
	return WildcardParticle(id, occurs), nil
}

func (c *compiler) compileAttributeWildcard(n *schemaNode, ctx *schemaContext) (WildcardID, error) {
	if err := checkAnyAttributeChildren(n); err != nil {
		return NoWildcard, err
	}
	return c.compileWildcard(n, ctx)
}

func (c *compiler) compileWildcard(n *schemaNode, ctx *schemaContext) (WildcardID, error) {
	source := n.semantic.wildcard()
	if source == nil {
		return NoWildcard, withSchemaCompileLocation(n, xsderrors.InternalInvariant("wildcard node has no typed wildcard source"))
	}
	ns, hasNS := source.Namespace.Value, source.Namespace.Present
	process, hasProcess := source.ProcessContents.Value, source.ProcessContents.Present
	w, err := ParseWildcard(&c.rt, WildcardAttrs{
		Namespace:          ns,
		ProcessContents:    process,
		TargetNamespace:    ctx.targetNS,
		HasNamespace:       hasNS,
		HasProcessContents: hasProcess,
	})
	if err != nil {
		return NoWildcard, withSchemaCompileLocation(n, err)
	}
	return c.appendWildcard(w)
}

// Wildcard returns compiler-owned wildcard metadata for internal compile
// helpers.
func (c *compiler) Wildcard(id WildcardID) (Wildcard, bool) {
	return c.rt.Wildcard(id)
}

// AddWildcard stores wildcard metadata produced by internal compile helpers.
func (c *compiler) AddWildcard(w Wildcard) (WildcardID, error) {
	return c.appendWildcard(w)
}
