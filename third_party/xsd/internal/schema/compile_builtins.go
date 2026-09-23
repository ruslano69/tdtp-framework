package schema

import (
	"github.com/jacoelho/xsd/internal/value"
	"github.com/jacoelho/xsd/internal/vocab"
	"github.com/jacoelho/xsd/xsderrors"
)

func (c *compiler) addBuiltins() error {
	// The value package owns the fixed type IDs and definitions. Schema keeps
	// only declaration records needed for QName lookup, derivation policy, and
	// publication; it never allocates or completes a value builtin.
	c.rt.Builtin = canonicalBuiltinIDs(NoComplexType)
	if err := c.addBuiltinSimpleTypes(); err != nil {
		return err
	}
	if err := c.addBuiltinXMLAttributes(); err != nil {
		return err
	}
	return c.addBuiltinAnyType()
}

func (c *compiler) addBuiltinSimpleTypes() error {
	builder := c.ensureValueBuilder()
	for raw := range value.BuiltinTypeCount {
		if err := c.addBuiltinSimpleType(builder, raw); err != nil {
			return err
		}
	}
	return nil
}

func (c *compiler) addBuiltinSimpleType(builder *value.Builder, id SimpleTypeID) error {
	decl, ok := builtinSimpleDeclaration(id)
	if !ok {
		return xsderrors.InternalInvariant("canonical builtin type has no schema declaration")
	}
	q, err := c.rt.internQName(decl.namespace, decl.local)
	if err != nil {
		return err
	}
	view, ok := builder.TypeView(id)
	if !ok {
		return xsderrors.InternalInvariant("canonical builtin type has no value metadata: " + decl.local)
	}
	st := SimpleType{
		ValueSpec: value.TypeSpec{
			Variety:           view.Variety,
			Primitive:         view.Primitive,
			Whitespace:        view.Whitespace,
			WhitespacePresent: true,
			Builtin:           view.Builtin,
			Identity:          view.Identity,
			Base:              view.Base,
			ListItem:          view.ListItem,
			Union:             append([]value.TypeID(nil), view.Union...),
		},
		Name:  q,
		Scope: decl.scope,
	}
	got, err := c.addSimpleType(st)
	if err != nil {
		return err
	}
	if got != id {
		return xsderrors.InternalInvariant("schema and canonical builtin type IDs diverged")
	}
	// addSimpleType starts every declaration as non-global so anonymous
	// declarations cannot accidentally enter the global index. Fixed XSD
	// builtins are the exception; xml:lang/xml:space stay internal.
	if decl.scope == DeclarationScopeGlobal {
		c.rt.SimpleTypes[id].Scope = DeclarationScopeGlobal
		c.rt.GlobalTypes[q] = SimpleRef(id)
		c.simpleDone[q] = id
	}
	return nil
}

func (c *compiler) addBuiltinXMLAttributes() error {
	for i := range BuiltinAttributeCount() {
		attr, ok := BuiltinAttributeSeedAt(i)
		if !ok {
			return xsderrors.InternalInvariant("builtin attribute seed index is invalid")
		}
		typ, ok := attr.TypeID(c.rt.builtinIDs())
		if !ok {
			return xsderrors.InternalInvariant("builtin attribute references missing type: " + attr.Local)
		}
		if err := c.addBuiltinAttribute(attr.Namespace, attr.Local, typ); err != nil {
			return err
		}
	}
	return nil
}

func (c *compiler) addBuiltinAnyType() error {
	anyWildcard, err := c.appendWildcard(BuiltinAnyTypeWildcard())
	if err != nil {
		return err
	}
	attrSet, err := c.addAttributeUseSet(BuiltinAnyTypeAttributeUseSet(anyWildcard))
	if err != nil {
		return err
	}
	modelID, err := c.addModel(BuiltinAnyTypeContentModel())
	if err != nil {
		return err
	}
	q, err := c.rt.internQName(vocab.XSDNamespaceURI, BuiltinAnyTypeLocalName())
	if err != nil {
		return err
	}
	complexID, err := c.registerBuiltinAnyType(q, BuiltinAnyTypeComplexType(q, modelID, attrSet))
	if err != nil {
		return err
	}
	c.complexDone[q] = complexID
	return nil
}

func (c *compiler) addBuiltinAttribute(ns, local string, typ SimpleTypeID) error {
	q, err := c.rt.internQName(ns, local)
	if err != nil {
		return err
	}
	id, err := c.registerGlobalAttribute(q, AttributeDecl{Name: q, Type: typ})
	if err != nil {
		return err
	}
	c.attributeDone[q] = id
	return nil
}

func (c *compiler) missingSimpleType() (SimpleTypeID, error) {
	if c.missingSimple != NoSimpleType {
		return c.missingSimple, nil
	}
	q, err := c.rt.internQName(vocab.EmptyNamespaceURI, MissingSimpleTypeLocalName())
	if err != nil {
		return NoSimpleType, err
	}
	typ := MissingSimpleType(q, c.rt.builtinIDs().AnySimpleType)
	id, err := c.addSimpleType(typ)
	if err != nil {
		return NoSimpleType, err
	}
	if err := c.completeSimpleType(id, typ); err != nil {
		return NoSimpleType, err
	}
	c.missingSimple = id
	return id, nil
}
