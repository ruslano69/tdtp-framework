package validate

import (
	"encoding/xml"

	"github.com/jacoelho/xsd/internal/lex"
	xsdSchema "github.com/jacoelho/xsd/internal/schema"
	xsdValue "github.com/jacoelho/xsd/internal/value"
	"github.com/jacoelho/xsd/internal/vocab"
	"github.com/jacoelho/xsd/xsderrors"
)

// xsiAttributeIdentityKey returns the identity-field key for an xsi attribute.
type xsiIdentityKey struct {
	key     string
	name    xsdSchema.QName
	present bool
}

func xsiAttributeIdentityKey(rt *xsdSchema.Schema, name xml.Name, lexical string, resolve xsdValue.QNameResolver, workLimit uint64, ctx StartContext) (xsiIdentityKey, error) {
	rn := ResolveRuntimeName(rt, name)
	if !rn.Known {
		return xsiIdentityKey{}, nil
	}
	key, err := xsiAttributeIdentity(rt, name.Local, lexical, resolve, workLimit, ctx)
	if err != nil {
		return xsiIdentityKey{}, err
	}
	return xsiIdentityKey{name: rn.Name, key: key, present: true}, nil
}

func xsiAttributeIdentity(rt *xsdSchema.Schema, local, lexical string, resolve xsdValue.QNameResolver, workLimit uint64, ctx StartContext) (string, error) {
	switch local {
	case vocab.XSIAttrNil:
		return xsiNilIdentity(rt, lexical, workLimit, ctx)
	case vocab.XSIAttrType:
		return xsiTypeIdentity(rt, lexical, resolve, workLimit, ctx)
	case vocab.XSIAttrNoNamespaceSchemaLocation:
		return xsiURIIdentity(rt, lexical, "invalid xsi:noNamespaceSchemaLocation URI "+lexical, workLimit, ctx)
	case vocab.XSIAttrSchemaLocation:
		return xsiSchemaLocationIdentity(rt, lexical, workLimit, ctx)
	default:
		return xsdValue.PrimitiveIdentityKey(xsdValue.PrimitiveString, lex.CollapseXMLWhitespace(lexical)), nil
	}
}

func xsiNilIdentity(rt *xsdSchema.Schema, lexical string, workLimit uint64, ctx StartContext) (string, error) {
	v, err := validateXSIValue(rt, vocab.XSDValueBoolean, lexical, xsdValue.Resolver{}, workLimit, ctx)
	if err != nil {
		return "", validationXSIValueError(ctx, "invalid xsi:nil value", err)
	}
	return v.IdentityKey(), nil
}

func xsiTypeIdentity(rt *xsdSchema.Schema, lexical string, resolve xsdValue.QNameResolver, workLimit uint64, ctx StartContext) (string, error) {
	resolver := xsdValue.Resolver{}
	if resolve != nil {
		resolver.QName = resolve
	}
	v, err := validateXSIValue(rt, vocab.XSDValueQName, lexical, resolver, workLimit, ctx)
	if err != nil {
		return "", validationXSIValueError(ctx, "invalid xsi:type", err)
	}
	return v.IdentityKey(), nil
}

func validateXSIValue(rt *xsdSchema.Schema, local, lexical string, resolver xsdValue.Resolver, workLimit uint64, _ StartContext) (xsdValue.Value, error) {
	id, err := xsiBuiltinType(rt, local)
	if err != nil {
		return xsdValue.Value{}, err
	}
	return rt.ValueProgram().Validate(id, lexical, resolver, xsdValue.NeedIdentity, workLimit, nil)
}

func validationXSIValueError(ctx StartContext, message string, err error) error {
	if invariantErr := simpleValueMetadataInvariant(err); invariantErr != nil {
		return invariantErr
	}
	if limitErr := simpleValueLimitError(ctx, err); limitErr != nil {
		return limitErr
	}
	if xsderrors.IsUnsupported(err) {
		return err
	}
	return validation(ctx, xsderrors.CodeValidationAttribute, message+": "+err.Error())
}

func xsiURIIdentity(rt *xsdSchema.Schema, lexical, message string, workLimit uint64, ctx StartContext) (string, error) {
	anyURI, err := xsiAnyURIType(rt)
	if err != nil {
		return "", err
	}
	result, err := rt.ValueProgram().Validate(anyURI, lexical, xsdValue.Resolver{}, xsdValue.NeedIdentity, workLimit, nil)
	if err != nil {
		if invariantErr := simpleValueMetadataInvariant(err); invariantErr != nil {
			return "", invariantErr
		}
		if limitErr := simpleValueLimitError(ctx, err); limitErr != nil {
			return "", limitErr
		}
		if xsderrors.IsUnsupported(err) {
			return "", err
		}
		return "", validation(ctx, xsderrors.CodeValidationAttribute, message)
	}
	return result.IdentityKey(), nil
}

func xsiSchemaLocationIdentity(rt *xsdSchema.Schema, lexical string, workLimit uint64, ctx StartContext) (string, error) {
	anyURI, err := xsiAnyURIType(rt)
	if err != nil {
		return "", err
	}
	items := make([]string, 0, 4)
	for field := range lex.XMLFieldsSeq(lexical) {
		key, err := xsiSchemaLocationItemIdentity(rt, anyURI, field, workLimit, ctx)
		if err != nil {
			return "", err
		}
		items = append(items, key)
	}
	return xsdValue.ListIdentityKey(items), nil
}

func xsiSchemaLocationItemIdentity(rt *xsdSchema.Schema, anyURI xsdSchema.SimpleTypeID, field string, workLimit uint64, ctx StartContext) (string, error) {
	item, err := rt.ValueProgram().Validate(anyURI, field, xsdValue.Resolver{}, xsdValue.NeedIdentity, workLimit, nil)
	if err != nil {
		if invariantErr := simpleValueMetadataInvariant(err); invariantErr != nil {
			return "", invariantErr
		}
		if limitErr := simpleValueLimitError(ctx, err); limitErr != nil {
			return "", limitErr
		}
		if xsderrors.IsUnsupported(err) {
			return "", err
		}
		return "", validation(ctx, xsderrors.CodeValidationAttribute, "invalid xsi:schemaLocation URI "+field)
	}
	if item.IdentityKey() == "" {
		return "", xsderrors.InternalInvariant("xsi:schemaLocation anyURI identity is missing")
	}
	return item.IdentityKey(), nil
}

func xsiAnyURIType(rt *xsdSchema.Schema) (xsdSchema.SimpleTypeID, error) {
	return xsiBuiltinType(rt, vocab.XSDValueAnyURI)
}

func xsiBuiltinType(rt *xsdSchema.Schema, local string) (xsdSchema.SimpleTypeID, error) {
	name, ok := rt.LookupQName(vocab.XSDNamespaceURI, local)
	if !ok {
		return xsdSchema.NoSimpleType, xsderrors.InternalInvariant("xs:" + local + " name is missing")
	}
	typ, ok := rt.Type(name)
	if !ok {
		return xsdSchema.NoSimpleType, xsderrors.InternalInvariant("xs:" + local + " type is missing")
	}
	id, ok := typ.Simple()
	if !ok {
		return xsdSchema.NoSimpleType, xsderrors.InternalInvariant("xs:" + local + " is not a simple type")
	}
	return id, nil
}
