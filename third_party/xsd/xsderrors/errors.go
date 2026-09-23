// Package xsderrors defines structured XSD diagnostics.
package xsderrors

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

const nilErrorString = "<nil>"

// ErrSchemaNotFound reports that a resolver could not resolve a schema. A
// resolver miss must return this error without joining another cause; a joined
// error is a fatal resolver failure.
var ErrSchemaNotFound = errors.New("schema not found")

// Category identifies the operation class that produced an error.
type Category string

// Error categories.
const (
	CategorySchemaParse   Category = "schema_parse"
	CategorySchemaCompile Category = "schema_compile"
	CategoryUnsupported   Category = "unsupported"
	CategoryValidation    Category = "validation"
	CategoryFormat        Category = "format"
	CategoryInternal      Category = "internal"
)

// Code is a stable machine-readable error code.
type Code string

// Error codes.
const (
	CodeSchemaRead             Code = "schema.read"
	CodeSchemaXML              Code = "schema.xml"
	CodeSchemaRoot             Code = "schema.root"
	CodeSchemaDuplicate        Code = "schema.duplicate"
	CodeSchemaReference        Code = "schema.reference"
	CodeSchemaFacet            Code = "schema.facet"
	CodeSchemaOccurrence       Code = "schema.occurrence"
	CodeSchemaContentModel     Code = "schema.content_model"
	CodeSchemaNoSources        Code = "schema.no_sources"
	CodeSchemaInvalidAttribute Code = "schema.invalid_attribute"
	CodeSchemaIdentity         Code = "schema.identity"
	CodeSchemaLimit            Code = "schema.limit"
	CodeUnsupportedDTD         Code = "unsupported.dtd"
	CodeUnsupportedExternal    Code = "unsupported.external_entity"
	CodeUnsupportedEntity      Code = "unsupported.entity"
	CodeUnsupportedNonUTF8     Code = "unsupported.non_utf8"
	CodeUnsupportedRedefine    Code = "unsupported.xs_redefine"
	CodeUnsupportedRegex       Code = "unsupported.regex"
	CodeUnsupportedSchemaHint  Code = "unsupported.xsi_schema_location"
	CodeUnsupportedXML11       Code = "unsupported.xml_1_1"
	CodeUnsupportedXSD11       Code = "unsupported.xsd_1_1"
	CodeValidationXML          Code = "validation.xml"
	CodeValidationRoot         Code = "validation.root"
	CodeValidationElement      Code = "validation.element"
	CodeValidationAttribute    Code = "validation.attribute"
	CodeValidationText         Code = "validation.text"
	CodeValidationType         Code = "validation.type"
	CodeValidationFacet        Code = "validation.facet"
	CodeValidationContent      Code = "validation.content"
	CodeValidationNil          Code = "validation.nil"
	CodeValidationIdentity     Code = "validation.identity"
	CodeValidationOption       Code = "validation.option"
	CodeValidationSession      Code = "validation.session"
	CodeValidationLimit        Code = "validation.limit"
	CodeFormatXML              Code = "format.xml"
	CodeFormatOption           Code = "format.option"
	CodeFormatLimit            Code = "format.limit"
	CodeInternalInvariant      Code = "internal.invariant"
)

// Error is an immutable structured diagnostic returned by compile, validation,
// and formatting operations.
type Error struct {
	cause    error
	category Category
	code     Code
	path     string
	message  string
	line     int
	column   int
}

// Errors is an immutable aggregate returned for multiple recoverable errors.
//
//nolint:errname // Errors is the established public aggregate name.
type Errors struct {
	children []error
}

// Category returns the operation class that produced the diagnostic.
func (e *Error) Category() Category {
	if e == nil {
		return ""
	}
	return e.category
}

// Code returns the stable machine-readable diagnostic code.
func (e *Error) Code() Code {
	if e == nil {
		return ""
	}
	return e.code
}

// Path returns the source or instance path, when known.
func (e *Error) Path() string {
	if e == nil {
		return ""
	}
	return e.path
}

// Message returns the diagnostic message without its wrapped cause.
func (e *Error) Message() string {
	if e == nil {
		return ""
	}
	return e.message
}

// Line returns the one-based source line, or zero when unknown.
func (e *Error) Line() int {
	if e == nil {
		return 0
	}
	return e.line
}

// Column returns the one-based source column, or zero when unknown.
func (e *Error) Column() int {
	if e == nil {
		return 0
	}
	return e.column
}

// Cause returns the wrapped cause, when present.
func (e *Error) Cause() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func (e *Error) Error() string {
	if e == nil {
		return nilErrorString
	}
	var b strings.Builder
	if e.code != "" {
		b.WriteString(string(e.code))
	} else {
		b.WriteString(string(e.category))
	}
	if e.line > 0 {
		fmt.Fprintf(&b, " at %d:%d", e.line, e.column)
	}
	if e.path != "" {
		b.WriteString(" ")
		b.WriteString(e.path)
	}
	if e.message != "" {
		b.WriteString(": ")
		b.WriteString(e.message)
	}
	if e.cause != nil {
		b.WriteString(": ")
		b.WriteString(e.cause.Error())
	}
	return b.String()
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func (e Errors) Error() string {
	switch len(e.children) {
	case 0:
		return nilErrorString
	case 1:
		return e.children[0].Error()
	default:
		return fmt.Sprintf("%d validation errors: %s", len(e.children), e.children[0])
	}
}

// Unwrap returns an owned copy so callers cannot mutate the aggregate.
func (e Errors) Unwrap() []error {
	return slices.Clone(e.children)
}

// Len returns the number of diagnostics in the aggregate.
func (e Errors) Len() int {
	return len(e.children)
}

// At returns diagnostic index, or nil when index is out of range.
func (e Errors) At(index int) error {
	if index < 0 || index >= len(e.children) {
		return nil
	}
	return e.children[index]
}

// NewErrors returns nil, the sole non-nil error, or an immutable aggregate.
func NewErrors(children ...error) error {
	owned := make([]error, 0, len(children))
	for _, child := range children {
		if isNilDiagnostic(child) {
			continue
		}
		owned = append(owned, child)
	}
	switch len(owned) {
	case 0:
		return nil
	case 1:
		return owned[0]
	default:
		return Errors{children: owned}
	}
}

// Flatten returns an owned top-level diagnostic list for presentation.
func Flatten(err error) []error {
	flat, _ := appendFlattened(nil, err)
	return flat
}

// appendFlattened reports whether err contains an aggregate that replaced its
// wrapper in the presentation projection.
func appendFlattened(dst []error, err error) ([]error, bool) {
	if isNilDiagnostic(err) {
		return dst, false
	}
	switch direct := err.(type) { //nolint:errorlint // Projection policy depends on direct public shapes.
	case *Error:
		return append(dst, direct), false
	case Errors:
		return appendFlattenedChildren(dst, direct.children), true
	case *Errors:
		return appendFlattenedChildren(dst, direct.children), true
	case interface{ Unwrap() []error }:
		return appendFlattenedChildren(dst, direct.Unwrap()), true
	case interface{ Unwrap() error }:
		return appendFlattenedWrapper(dst, err, direct.Unwrap())
	default:
		return append(dst, err), false
	}
}

func appendFlattenedChildren(dst, children []error) []error {
	for _, child := range children {
		dst, _ = appendFlattened(dst, child)
	}
	return dst
}

func appendFlattenedWrapper(dst []error, wrapper, child error) ([]error, bool) {
	mark := len(dst)
	dst, expanded := appendFlattened(dst, child)
	if expanded {
		return dst, true
	}
	return append(dst[:mark], wrapper), false
}

func isNilDiagnostic(err error) bool {
	if err == nil {
		return true
	}
	if diagnostic, ok := err.(*Error); ok { //nolint:errorlint // Typed nil is a direct public value case.
		return diagnostic == nil
	}
	if aggregate, ok := err.(Errors); ok { //nolint:errorlint // The zero aggregate represents no errors.
		return len(aggregate.children) == 0
	}
	if aggregate, ok := err.(*Errors); ok { //nolint:errorlint // Typed nil and zero aggregates are absent diagnostics.
		return aggregate == nil || len(aggregate.children) == 0
	}
	return false
}

// IsUnsupported reports whether err represents an unsupported feature.
func IsUnsupported(err error) bool {
	if isNilDiagnostic(err) {
		return false
	}
	switch x := any(err).(type) {
	case *Error:
		return isUnsupportedDiagnostic(x)
	case Errors:
		return slices.ContainsFunc(x.children, IsUnsupported)
	case *Errors:
		return slices.ContainsFunc(x.children, IsUnsupported)
	}
	if x, ok := asDiagnostic(err); ok && isUnsupportedDiagnostic(x) {
		return true
	}
	return isUnsupportedWrapper(err)
}

func asDiagnostic(err error) (*Error, bool) {
	if isNilDiagnostic(err) {
		return nil, false
	}
	if as, ok := err.(interface{ As(target any) bool }); ok {
		var diagnostic *Error
		if as.As(&diagnostic) {
			return diagnostic, true
		}
	}
	return nil, false
}

func isUnsupportedDiagnostic(err *Error) bool {
	return err != nil && (err.category == CategoryUnsupported || IsUnsupported(err.cause))
}

func isUnsupportedWrapper(err error) bool {
	if x, ok := err.(interface{ Unwrap() []error }); ok {
		return slices.ContainsFunc(x.Unwrap(), IsUnsupported)
	}
	if x, ok := err.(interface{ Unwrap() error }); ok {
		return IsUnsupported(x.Unwrap())
	}
	return false
}

// ValidCategoryCode reports whether code belongs to category in the public
// diagnostic catalog.
func ValidCategoryCode(category Category, code Code) bool {
	switch category {
	case CategorySchemaParse:
		return validSchemaParseCode(code)
	case CategorySchemaCompile:
		return validSchemaCompileCode(code)
	case CategoryUnsupported:
		return validUnsupportedCode(code)
	case CategoryValidation:
		return validValidationCode(code)
	case CategoryFormat:
		return validFormatCode(code)
	case CategoryInternal:
		return code == CodeInternalInvariant
	}
	return false
}

func validSchemaParseCode(code Code) bool {
	return code == CodeSchemaRead || code == CodeSchemaXML || code == CodeSchemaRoot || code == CodeSchemaLimit
}

func validSchemaCompileCode(code Code) bool {
	return code == CodeSchemaRead || code == CodeSchemaRoot || code == CodeSchemaDuplicate || code == CodeSchemaReference ||
		code == CodeSchemaFacet || code == CodeSchemaOccurrence || code == CodeSchemaContentModel || code == CodeSchemaNoSources ||
		code == CodeSchemaInvalidAttribute || code == CodeSchemaIdentity || code == CodeSchemaLimit
}

func validUnsupportedCode(code Code) bool {
	return code == CodeUnsupportedDTD || code == CodeUnsupportedExternal || code == CodeUnsupportedEntity ||
		code == CodeUnsupportedNonUTF8 || code == CodeUnsupportedRedefine || code == CodeUnsupportedRegex ||
		code == CodeUnsupportedSchemaHint || code == CodeUnsupportedXML11 || code == CodeUnsupportedXSD11
}

func validValidationCode(code Code) bool {
	return code == CodeValidationXML || code == CodeValidationRoot || code == CodeValidationElement || code == CodeValidationAttribute ||
		code == CodeValidationText || code == CodeValidationType || code == CodeValidationFacet || code == CodeValidationContent ||
		code == CodeValidationNil || code == CodeValidationIdentity || code == CodeValidationOption || code == CodeValidationSession ||
		code == CodeValidationLimit
}

func validFormatCode(code Code) bool {
	return code == CodeFormatXML || code == CodeFormatOption || code == CodeFormatLimit
}

func newDiagnostic(category Category, code Code, msg string, cause error) error {
	if !ValidCategoryCode(category, code) {
		return &Error{
			category: CategoryInternal,
			code:     CodeInternalInvariant,
			message:  fmt.Sprintf("diagnostic code %q does not belong to category %q", code, category),
		}
	}
	if isNilDiagnostic(cause) {
		cause = nil
	}
	return &Error{cause: cause, category: category, code: code, message: msg}
}

// SchemaParse returns a schema parsing diagnostic.
func SchemaParse(code Code, msg string, cause error) error {
	return newDiagnostic(CategorySchemaParse, code, msg, cause)
}

// SchemaCompile returns a schema compilation diagnostic.
func SchemaCompile(code Code, msg string) error {
	return newDiagnostic(CategorySchemaCompile, code, msg, nil)
}

// WithLocation attaches missing source location fields to a direct diagnostic.
// It never unwraps or replaces an aggregate or wrapper.
func WithLocation(path string, line, column int, err error) error {
	if isNilDiagnostic(err) {
		return nil
	}
	x, ok := directDiagnostic(err)
	if !ok {
		return err
	}
	if (path == "" || x.path != "") && (line <= 0 || x.line > 0) {
		return err
	}
	y := *x
	if y.path == "" {
		y.path = path
	}
	if y.line == 0 && line > 0 {
		y.line, y.column = line, column
	}
	return &y
}

func directDiagnostic(err error) (*Error, bool) {
	x, ok := err.(*Error) //nolint:errorlint // Decoration must preserve wrapper and aggregate structure.
	return x, ok && x != nil
}

// Unsupported returns an unsupported-feature diagnostic.
func Unsupported(code Code, msg string, cause error) error {
	return newDiagnostic(CategoryUnsupported, code, msg, cause)
}

// Validation returns a document validation diagnostic.
func Validation(code Code, msg string, cause error) error {
	return newDiagnostic(CategoryValidation, code, msg, cause)
}

// Format returns an XML formatting diagnostic.
func Format(code Code, cause error) error {
	return newDiagnostic(CategoryFormat, code, "", cause)
}

// InternalInvariant returns an internal invariant diagnostic.
func InternalInvariant(msg string) error {
	return newDiagnostic(CategoryInternal, CodeInternalInvariant, msg, nil)
}
