package schema

import "github.com/jacoelho/xsd/xsderrors"

func invalidAttributeError(err error) error {
	if err == nil {
		return nil
	}
	return xsderrors.SchemaCompile(xsderrors.CodeSchemaInvalidAttribute, err.Error())
}
