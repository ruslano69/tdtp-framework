package schema

import (
	"github.com/jacoelho/xsd/internal/lex"
	"github.com/jacoelho/xsd/internal/vocab"
	"github.com/jacoelho/xsd/xsderrors"
)

// TopLevelSchemaChild describes one child of xs:schema enough for top-level
// component syntax validation.
type TopLevelSchemaChild struct {
	Local        string
	HasName      bool
	HasRef       bool
	HasForm      bool
	HasUse       bool
	HasMinOccurs bool
	HasMaxOccurs bool
}

// TopLevelSchemaChildError identifies a top-level schema child syntax issue.
type TopLevelSchemaChildError struct {
	Code    xsderrors.Code
	Message string
}

func (e *TopLevelSchemaChildError) Error() string {
	if e == nil {
		return nilErrorString
	}
	return e.Message
}

// NotationChild describes one child of xs:notation without exposing raw XML
// nodes to internal compile syntax validation.
type NotationChild struct {
	Local string
	XSD   bool
}

// NotationDeclaration is the normalized syntax input for xs:notation.
type NotationDeclaration struct {
	Text     string
	Children []NotationChild
	Name     LexicalAttribute
	Public   LexicalAttribute
	System   LexicalAttribute
}

// NotationSyntaxError identifies the notation node or child that failed
// declaration syntax validation. Index is -1 for the notation node itself.
type NotationSyntaxError struct {
	Code    xsderrors.Code
	Message string
	Index   int
}

func (e *NotationSyntaxError) Error() string {
	if e == nil {
		return nilErrorString
	}
	return e.Message
}

// SchemaID describes one normalized schema id value.
type SchemaID struct {
	Value string
}

// SchemaIDError identifies the id that failed schema id validation.
type SchemaIDError struct {
	Code    xsderrors.Code
	Message string
	Index   int
}

func (e *SchemaIDError) Error() string {
	if e == nil {
		return nilErrorString
	}
	return e.Message
}

// SchemaNodeNames describes one schema node enough for compile-owned name
// syntax validation.
type SchemaNodeNames struct {
	Local       string
	ParentLocal string
	ID          string
	Name        string
	XSD         bool
	ParentXSD   bool
	HasID       bool
	HasName     bool
}

type schemaAnnotationAction struct {
	SkipChildren bool
}

// SchemaAnnotationSyntaxError identifies the annotation node or child that
// failed syntax validation. Index is -1 for the node itself.
type SchemaAnnotationSyntaxError struct {
	Code    xsderrors.Code
	Message string
	Index   int
}

func (e *SchemaAnnotationSyntaxError) Error() string {
	if e == nil {
		return nilErrorString
	}
	return e.Message
}

// ValidateSchemaIDs validates uniqueness of schema id values.
func ValidateSchemaIDs(ids []SchemaID) error {
	seen := make(map[string]bool, len(ids))
	for i, id := range ids {
		if seen[id.Value] {
			return schemaIDError(i, xsderrors.CodeSchemaInvalidAttribute, "duplicate schema id "+id.Value)
		}
		seen[id.Value] = true
	}
	return nil
}

func schemaIDError(index int, code xsderrors.Code, msg string) error {
	return &SchemaIDError{Index: index, Code: code, Message: msg}
}

// ValidateSchemaTargetNamespace validates the raw xs:schema targetNamespace
// attribute value.
func ValidateSchemaTargetNamespace(target LexicalAttribute) error {
	if target.Present && target.Value == "" {
		return xsderrors.SchemaCompile(xsderrors.CodeSchemaInvalidAttribute, "schema targetNamespace cannot be empty")
	}
	return nil
}

// ValidateSchemaNodeNames validates schema node id/name syntax and name
// placement rules.
func ValidateSchemaNodeNames(node SchemaNodeNames) error {
	if !node.XSD {
		return nil
	}
	if node.HasID && !lex.IsNCName(node.ID) {
		return xsderrors.SchemaCompile(xsderrors.CodeSchemaInvalidAttribute, "schema id must be NCName")
	}
	if node.HasName && !lex.IsNCName(node.Name) {
		return xsderrors.SchemaCompile(xsderrors.CodeSchemaInvalidAttribute, "schema component name must be NCName")
	}
	if node.Local == attributeGroup && node.HasName && (!node.ParentXSD || node.ParentLocal != vocab.XSDElemSchema) {
		return xsderrors.SchemaCompile(xsderrors.CodeSchemaInvalidAttribute, "attributeGroup use cannot have name")
	}
	return nil
}

func schemaAnnotationSyntaxError(index int, code xsderrors.Code, msg string) error {
	return &SchemaAnnotationSyntaxError{Index: index, Code: code, Message: msg}
}

// ValidateNotationDeclaration validates xs:notation declaration syntax.
func ValidateNotationDeclaration(declaration NotationDeclaration) error {
	if lex.TrimXMLWhitespaceString(declaration.Text) != "" {
		return notationSyntaxError(-1, xsderrors.CodeSchemaContentModel, "notation can contain only annotation")
	}
	for i, child := range declaration.Children {
		if !child.XSD || child.Local != annotationChild {
			return notationSyntaxError(i, xsderrors.CodeSchemaContentModel, "notation can contain only annotation")
		}
	}
	if !declaration.Name.Present {
		return notationSyntaxError(-1, xsderrors.CodeSchemaInvalidAttribute, "notation missing name")
	}
	if !declaration.Public.Present && !declaration.System.Present {
		return notationSyntaxError(-1, xsderrors.CodeSchemaInvalidAttribute, "notation requires public or system")
	}
	return nil
}

func notationSyntaxError(index int, code xsderrors.Code, msg string) error {
	return &NotationSyntaxError{Index: index, Code: code, Message: msg}
}

// ValidateTopLevelSchemaChild validates syntax owned by xs:schema's direct
// child component declaration.
func ValidateTopLevelSchemaChild(child TopLevelSchemaChild) error {
	switch child.Local {
	case annotationChild, includeChild, importChild, redefineChild, notationChild:
		return nil
	case simpleTypeChild, complexTypeChild:
		return requireTopLevelName(child)
	case attributeGroup:
		if err := rejectTopLevelReference(child, attributeGroup); err != nil {
			return err
		}
		return requireTopLevelName(child)
	case groupChild:
		return validateTopLevelGroup(child)
	case attributeChild:
		return validateTopLevelAttribute(child)
	case elementChild:
		return validateTopLevelElement(child)
	case uniqueChild, keyChild, keyrefChild, selectorChild, fieldChild:
		return topLevelSchemaChildError(xsderrors.CodeSchemaContentModel, "identity constraint must be inside element")
	default:
		return topLevelSchemaChildError(xsderrors.CodeSchemaContentModel, "invalid top-level schema child "+child.Local)
	}
}

func rejectTopLevelReference(child TopLevelSchemaChild, local string) error {
	if child.HasRef {
		return topLevelSchemaChildError(xsderrors.CodeSchemaInvalidAttribute, "top-level "+local+" cannot have ref")
	}
	return nil
}

func validateTopLevelGroup(child TopLevelSchemaChild) error {
	if err := rejectTopLevelReference(child, groupChild); err != nil {
		return err
	}
	if child.HasMinOccurs {
		return topLevelSchemaChildError(xsderrors.CodeSchemaInvalidAttribute, "top-level "+groupChild+" cannot have minOccurs")
	}
	if child.HasMaxOccurs {
		return topLevelSchemaChildError(xsderrors.CodeSchemaInvalidAttribute, "top-level "+groupChild+" cannot have maxOccurs")
	}
	return requireTopLevelName(child)
}

func validateTopLevelAttribute(child TopLevelSchemaChild) error {
	if err := rejectTopLevelReference(child, attributeChild); err != nil {
		return err
	}
	if child.HasForm {
		return topLevelSchemaChildError(xsderrors.CodeSchemaInvalidAttribute, "top-level "+attributeChild+" cannot have form")
	}
	if child.HasUse {
		return topLevelSchemaChildError(xsderrors.CodeSchemaInvalidAttribute, "top-level "+attributeChild+" cannot have use")
	}
	return requireTopLevelName(child)
}

func validateTopLevelElement(child TopLevelSchemaChild) error {
	if err := rejectTopLevelReference(child, elementChild); err != nil {
		return err
	}
	if child.HasForm {
		return topLevelSchemaChildError(xsderrors.CodeSchemaInvalidAttribute, "top-level "+elementChild+" cannot have form")
	}
	if child.HasMinOccurs {
		return topLevelSchemaChildError(xsderrors.CodeSchemaInvalidAttribute, "top-level "+elementChild+" cannot have minOccurs")
	}
	if child.HasMaxOccurs {
		return topLevelSchemaChildError(xsderrors.CodeSchemaInvalidAttribute, "top-level "+elementChild+" cannot have maxOccurs")
	}
	return requireTopLevelName(child)
}

func requireTopLevelName(child TopLevelSchemaChild) error {
	if !child.HasName {
		return topLevelSchemaChildError(xsderrors.CodeSchemaReference, "top-level "+child.Local+" missing name")
	}
	return nil
}

func topLevelSchemaChildError(code xsderrors.Code, msg string) error {
	return &TopLevelSchemaChildError{Code: code, Message: msg}
}
