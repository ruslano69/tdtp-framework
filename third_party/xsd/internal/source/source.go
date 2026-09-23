// Package source defines internal schema source and resolver primitives.
package source

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/jacoelho/xsd/internal/uriref"
	"github.com/jacoelho/xsd/xsderrors"
)

// Source identifies a schema document passed to compilation.
type Source struct {
	open    func() (io.ReadCloser, error)
	context resolutionContext
	name    string
	data    []byte
	digest  [sha256.Size]byte
	kind    sourceKind
}

type sourceKind uint8

const (
	sourceInvalid sourceKind = iota
	sourceBytes
	sourceOpener
)

type resolutionContext struct {
	resolver          *resolverOwner
	localFileFallback bool
}

// ReferenceBase keeps the spelling presented to a custom resolver separate
// from the base that the built-in identity and file backends can represent.
// Applying xml:base may preserve a valid resolver base while making built-in
// fallback unavailable.
type ReferenceBase struct {
	fallback string
	resolver resolverBase
}

type resolverBaseKind uint8

const (
	resolverBaseUnavailable resolverBaseKind = iota
	resolverBaseURI
	resolverBaseLocal
)

type resolverBase struct {
	value     string
	localPath string
	query     resolverQuery
	kind      resolverBaseKind
}

type resolverQuery struct {
	value   string
	present bool
}

func (b resolverBase) available() bool {
	return b.kind != resolverBaseUnavailable
}

// NewReferenceBase returns the unresolved base for one source context. The
// source name is preserved exactly for custom resolver callbacks until an
// xml:base value is applied.
func NewReferenceBase(name string) ReferenceBase {
	return ReferenceBase{
		resolver: newResolverBase(name),
		fallback: name,
	}
}

func newResolverBase(value string) resolverBase {
	if value == "" {
		return resolverBase{}
	}
	if isLocalName(value) {
		return localResolverBase(value, resolverQuery{})
	}
	return resolverBase{value: value, kind: resolverBaseURI}
}

// ResolverValue returns the effective base to present to a custom resolver.
func (b ReferenceBase) ResolverValue() (string, bool) {
	return b.resolver.value, b.resolver.available()
}

// WithXMLBase applies one xml:base value. Syntactically valid URI forms that
// cannot be represented by the built-in local backend remain available to a
// custom resolver without becoming document identities themselves.
func (b ReferenceBase) WithXMLBase(reference uriref.Reference) (ReferenceBase, error) {
	reference = reference.WithoutFragment()
	if reference.Raw() == "" {
		return b.withoutFragment(), nil
	}
	resolver, err := resolveResolverBase(b, reference)
	if err != nil {
		return ReferenceBase{}, err
	}
	fallback, fallbackErr := resolveFallbackBase(b, reference)
	if fallbackErr != nil {
		return ReferenceBase{}, fallbackErr
	}
	return (ReferenceBase{resolver: resolver, fallback: fallback}).withoutFragment(), nil
}

func (b ReferenceBase) withoutFragment() ReferenceBase {
	if b.resolver.kind == resolverBaseURI {
		b.resolver = uriResolverBaseValue(withoutFragment(b.resolver.value))
	}
	if b.fallback != "" && !isLocalName(b.fallback) {
		b.fallback = withoutFragment(b.fallback)
	}
	return b
}

func resolveFallbackBase(base ReferenceBase, reference uriref.Reference) (string, error) {
	if base.fallback == "" {
		return resolveFallbackWithoutBase(base, reference)
	}
	return resolveFallbackAgainst(base.fallback, reference)
}

func resolveFallbackWithoutBase(base ReferenceBase, reference uriref.Reference) (string, error) {
	parts := reference.Parts()
	if parts.HasScheme {
		return canonicalFallbackReference(reference)
	}
	if base.resolver.kind != resolverBaseLocal || parts.Path == "" {
		return "", nil
	}
	return resolveFallbackAgainst(base.resolver.localPath, reference)
}

func resolveFallbackAgainst(fallbackBase string, reference uriref.Reference) (string, error) {
	if isLocalName(fallbackBase) {
		resolved, err := ResolveReference(fallbackBase, reference.Escaped())
		if IsReferenceUnavailable(err) {
			return "", nil
		}
		return resolved, err
	}
	if reference.Parts().HasScheme {
		return canonicalFallbackReference(reference)
	}
	baseReference, err := uriref.Parse(fallbackBase)
	if err != nil {
		return "", nil //nolint:nilerr // Arbitrary source names are identities, not schema-provided URI syntax.
	}
	resolved, err := uriref.Resolve(baseReference, reference)
	if errors.Is(err, uriref.ErrOpaqueBase) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return canonicalFallbackReference(resolved)
}

func canonicalFallbackReference(reference uriref.Reference) (string, error) {
	resolved, err := ResolveReference("", reference.Escaped())
	if IsReferenceUnavailable(err) {
		return "", nil
	}
	return resolved, err
}

func withoutFragment(uri string) string {
	if withoutFragment, _, present := strings.Cut(uri, "#"); present {
		return withoutFragment
	}
	return uri
}

type resolverOwner struct {
	resolve Resolver
}

var fileResolverOwner = &resolverOwner{}

func (o *resolverOwner) resolveSchema(base, location string) (Source, error) {
	return o.resolve.ResolveSchema(base, location)
}

// Resolver resolves schema include/import locations during compilation.
type Resolver func(base, location string) (Source, error)

// ResolveSchema resolves one schema include/import location.
func (r Resolver) ResolveSchema(base, location string) (Source, error) {
	if r == nil {
		return Source{}, xsderrors.ErrSchemaNotFound
	}
	return r(base, location)
}

// File returns a file schema source and resolves local schemaLocation refs.
func File(file string) Source {
	file = filepath.Clean(file)
	absolute, absoluteErr := filepath.Abs(file)
	if absoluteErr == nil {
		file = absolute
	}
	return Source{
		name: file,
		kind: sourceOpener,
		open: func() (io.ReadCloser, error) {
			if absoluteErr != nil {
				return nil, absoluteErr
			}
			reader, err := os.Open(file)
			if err != nil {
				return nil, err
			}
			return reader, nil
		},
		context: resolutionContext{resolver: fileResolverOwner, localFileFallback: true},
	}
}

// Bytes returns an in-memory schema source.
func Bytes(name string, data []byte) Source {
	if data == nil {
		data = []byte{}
	}
	data = bytes.Clone(data)
	return Source{name: name, data: data, digest: sha256.Sum256(data), kind: sourceBytes}
}

// Opener returns a schema source backed by an opener.
func Opener(name string, open func() (io.ReadCloser, error)) Source {
	return Source{name: name, open: open, kind: sourceOpener}
}

// WithResolver returns s with r used for schema include/import resolution.
func (s Source) WithResolver(r Resolver) Source {
	if r == nil {
		if s.context.localFileFallback {
			// A nil custom resolver removes only the custom callback. Keep the
			// built-in file backend represented by the same owner as File so
			// equivalent source graphs share one resolution context.
			s.context.resolver = fileResolverOwner
		} else {
			s.context.resolver = nil
		}
	} else {
		s.context.resolver = &resolverOwner{resolve: r}
	}
	return s
}

// Name returns the source name.
func (s Source) Name() string {
	return s.name
}

// SameResolutionContext reports whether s and other resolve descendants with
// the same resolver owner and built-in backend capabilities.
func (s Source) SameResolutionContext(other Source) bool {
	return s.context == other.context
}

// Resolution is the result of resolving one schema reference. It contains a
// source when a backend supplied the referenced document, only a target when a
// generic document identity is representable, and neither when the valid
// reference is unavailable to the configured backends.
type Resolution struct {
	target string
	source Source
}

// Source returns the resolved source and whether a backend supplied it.
func (r Resolution) Source() (Source, bool) {
	return r.source, r.source.name != ""
}

// Target returns the singular referenced document identity, when one is
// representable independently of a backend.
func (r Resolution) Target() string {
	return r.target
}

// ResolveFrom resolves location from a base whose custom-resolver spelling and
// built-in fallback capability have been tracked independently.
func (s Source) ResolveFrom(base ReferenceBase, location uriref.Reference) (Resolution, error) {
	resolved, err := s.resolveWithCustomResolver(base, location)
	if err != nil {
		return Resolution{}, err
	}
	if _, ok := resolved.Source(); ok {
		return resolved, nil
	}
	return s.resolveWithBuiltins(base, location)
}

func (s Source) resolveWithCustomResolver(base ReferenceBase, location uriref.Reference) (Resolution, error) {
	if s.context.resolver == nil || s.context.resolver == fileResolverOwner {
		return Resolution{}, nil
	}
	resolverBase, ok := base.ResolverValue()
	if !ok {
		return Resolution{}, nil
	}
	resolved, err := s.context.resolver.resolveSchema(resolverBase, location.Raw())
	if err != nil {
		if errorIsOnly(err, xsderrors.ErrSchemaNotFound) {
			return Resolution{}, nil
		}
		return Resolution{}, err
	}
	if resolved.name == "" {
		return Resolution{}, errors.New("schema resolver returned a source without a name")
	}
	resolved.context.resolver = s.context.resolver
	return Resolution{source: resolved, target: Key(resolved.name)}, nil
}

func (s Source) resolveWithBuiltins(base ReferenceBase, location uriref.Reference) (Resolution, error) {
	if location.HasFragment() {
		return Resolution{}, nil
	}
	resolvedBase, err := base.WithXMLBase(location)
	if err != nil {
		return Resolution{}, referenceResolutionError{err: err}
	}
	if resolvedBase.fallback == "" {
		return Resolution{}, nil
	}
	target := resolvedBase.fallback
	if s.context.localFileFallback {
		if file, ok := localSchemaFile(target); ok {
			resolved := File(file)
			resolved.context.resolver = s.context.resolver
			return Resolution{source: resolved, target: Key(resolved.name)}, nil
		}
	}
	return Resolution{target: Key(target)}, nil
}

type referenceResolutionError struct {
	err error
}

func (e referenceResolutionError) Error() string { return e.err.Error() }

func (e referenceResolutionError) Unwrap() error { return e.err }

// IsReferenceResolutionError reports whether err came from generic URI
// reference identity resolution rather than an attached schema resolver.
func IsReferenceResolutionError(err error) bool {
	var target referenceResolutionError
	return errors.As(err, &target)
}

// ReadStage identifies the source acquisition stage that failed.
type ReadStage uint8

const (
	// ReadStageOpen identifies an opener failure.
	ReadStageOpen ReadStage = iota + 1
	// ReadStageRead identifies a stream read failure.
	ReadStageRead
	// ReadStageClose identifies a stream close failure.
	ReadStageClose
)

// ReadResult reports a bounded source acquisition. Bytes and Digest describe
// the raw source bytes consumed, including a first byte beyond maxBytes when
// the limit is exceeded.
type ReadResult struct {
	Err           error
	Bytes         int64
	Digest        [32]byte
	Stage         ReadStage
	LimitExceeded bool
	// OpenNotFound reports an exclusively not-found opener error after successful cleanup.
	OpenNotFound bool
}

// OpenInput opens s for bounded streaming. A non-nil Input owns the opened
// source until Finish is called. The returned result is non-zero only when
// opening or source admission fails before an Input can be returned.
func (s Source) OpenInput(maxBytes int64) (*Input, ReadResult) {
	if maxBytes < 0 {
		return nil, ReadResult{
			Err:   xsderrors.SchemaCompile(xsderrors.CodeSchemaLimit, "schema reader byte limit cannot be negative"),
			Stage: ReadStageOpen,
		}
	}
	switch s.kind {
	case sourceBytes:
		if int64(len(s.data)) > maxBytes {
			return nil, ReadResult{Err: schemaSourceLimitError(s.name), LimitExceeded: true}
		}
		return newBytesInput(s.name, s.data, s.digest, maxBytes), ReadResult{}
	case sourceOpener:
		if s.open == nil {
			return nil, ReadResult{
				Err:   xsderrors.SchemaCompile(xsderrors.CodeSchemaRead, "schema source opener is nil"),
				Stage: ReadStageOpen,
			}
		}
		return s.openSourceInput(maxBytes)
	case sourceInvalid:
		return nil, ReadResult{
			Err:   xsderrors.SchemaCompile(xsderrors.CodeSchemaRead, "schema source is invalid"),
			Stage: ReadStageOpen,
		}
	default:
	}
	return nil, ReadResult{
		Err:   xsderrors.SchemaCompile(xsderrors.CodeSchemaRead, "schema source is invalid"),
		Stage: ReadStageOpen,
	}
}

func (s Source) openSourceInput(maxBytes int64) (*Input, ReadResult) {
	r, err := s.open()
	if err != nil {
		return nil, openSourceFailure(r, err)
	}
	if isNilReadCloser(r) {
		return nil, ReadResult{
			Err:   xsderrors.SchemaCompile(xsderrors.CodeSchemaRead, "schema opener returned a nil reader"),
			Stage: ReadStageOpen,
		}
	}
	return newInput(s.name, r, maxBytes), ReadResult{}
}

func openSourceFailure(r io.ReadCloser, err error) ReadResult {
	openNotFound := errorIsOnly(err, os.ErrNotExist)
	if !isNilReadCloser(r) {
		if closeErr := r.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
			openNotFound = false
		}
	}
	return ReadResult{Err: err, Stage: ReadStageOpen, OpenNotFound: openNotFound}
}

func isNilReadCloser(r io.ReadCloser) bool {
	if r == nil {
		return true
	}
	v := reflect.ValueOf(r)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	case reflect.Invalid, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128,
		reflect.Array, reflect.Interface, reflect.String, reflect.Struct, reflect.UnsafePointer:
		return false
	default:
	}
	return false
}

const maxConsecutiveEmptySchemaReads = 100

func schemaSourceLimitError(name string) error {
	msg := "schema source exceeds MaxSchemaSourceBytes"
	if name != "" {
		msg = "schema source " + name + " exceeds MaxSchemaSourceBytes"
	}
	return xsderrors.WithLocation(name, 0, 0, xsderrors.SchemaCompile(xsderrors.CodeSchemaLimit, msg))
}

// IsSchemaLimitError reports whether err is a schema source byte-limit diagnostic.
func IsSchemaLimitError(err error) bool {
	var x *xsderrors.Error
	ok := errors.As(err, &x)
	return ok && x.Code() == xsderrors.CodeSchemaLimit
}

// Key canonicalizes a schema source name for loaded-document identity.
func Key(name string) string {
	if isLocalName(name) {
		return canonicalLocalPath(name)
	}
	u, err := url.Parse(name)
	if err != nil {
		return name
	}
	syntax := uriReferenceSyntaxFor(name, u)
	if file, ok := localFileURIPath(u, syntax.fragment); ok {
		return file
	}
	if canonical, ok := canonicalURL(u, syntax); ok {
		return canonical
	}
	return name
}

var errReferenceUnavailable = errors.New("schema reference is unavailable to the local backend")

// IsReferenceUnavailable reports whether a URI reference is syntactically
// valid but cannot be represented by the local source backend.
func IsReferenceUnavailable(err error) bool {
	return errors.Is(err, errReferenceUnavailable)
}

func resolveResolverBase(base ReferenceBase, reference uriref.Reference) (resolverBase, error) {
	if base.resolver.available() {
		return resolveAvailableResolverBase(base.resolver, reference)
	}
	if reference.Parts().HasScheme {
		return uriResolverBase(reference), nil
	}
	return resolverBase{}, nil
}

func resolveAvailableResolverBase(base resolverBase, reference uriref.Reference) (resolverBase, error) {
	if base.kind == resolverBaseLocal {
		return resolveLocalResolverBase(base, reference), nil
	}
	baseReference, err := uriref.Parse(base.value)
	if err != nil {
		if reference.Parts().HasScheme {
			return uriResolverBase(reference), nil
		}
		return resolverBase{}, nil
	}
	resolved, err := uriref.Resolve(baseReference, reference)
	if errors.Is(err, uriref.ErrOpaqueBase) {
		return resolverBase{}, nil
	}
	if err != nil {
		return resolverBase{}, err
	}
	return uriResolverBase(resolved), nil
}

func uriResolverBase(reference uriref.Reference) resolverBase {
	return uriResolverBaseValue(reference.Raw())
}

func uriResolverBaseValue(value string) resolverBase {
	if value == "" {
		return resolverBase{}
	}
	return resolverBase{value: value, kind: resolverBaseURI}
}

func resolveLocalResolverBase(base resolverBase, reference uriref.Reference) resolverBase {
	parts := reference.Parts()
	if parts.HasScheme || parts.HasAuthority {
		return uriResolverBase(reference)
	}
	path := parts.Path
	if path == "" {
		query := base.query
		if parts.HasQuery {
			query = resolverQuery{value: parts.Query, present: true}
		}
		return localResolverBase(base.localPath, query)
	}
	if filepath.IsAbs(filepath.FromSlash(path)) || os.IsPathSeparator(path[0]) {
		path = filepath.FromSlash(path)
	} else {
		dir := filepath.Dir(base.localPath)
		if localPathFormOf(base.localPath) == localPathDirectory {
			dir = base.localPath
		}
		path = filepath.Join(dir, filepath.FromSlash(path))
	}
	path = canonicalLocalReference(path, localPathFormOf(parts.Path))
	return localResolverBase(path, resolverQuery{value: parts.Query, present: parts.HasQuery})
}

func localResolverBase(path string, query resolverQuery) resolverBase {
	value := path
	if query.present {
		value += "?" + query.value
	}
	if value == "" {
		return resolverBase{}
	}
	return resolverBase{
		value: value, localPath: path, query: query, kind: resolverBaseLocal,
	}
}

// ResolveReference resolves one URI reference against base and returns its
// singular document identity.
func ResolveReference(base, reference string) (string, error) {
	baseLocal := isLocalName(base)
	if reference == "" {
		if baseLocal {
			return canonicalLocalReference(base, localPathFormOf(base)), nil
		}
		return resolveEmptyURIReference(base)
	}
	if strings.IndexByte(reference, '#') >= 0 {
		return "", errors.New("schema reference fragments are not supported")
	}
	if baseLocal && filepath.VolumeName(reference) != "" && filepath.IsAbs(reference) {
		return canonicalLocalReference(reference, localPathFormOf(reference)), nil
	}
	ref, err := url.Parse(reference)
	if err != nil {
		return "", err
	}
	refAuthority := parseURIAuthoritySyntax(reference, ref.Scheme)
	if ref.Scheme != "" {
		return resolveAbsoluteReference(ref, refAuthority)
	}
	if baseLocal {
		return resolveLocalReference(base, ref, refAuthority)
	}
	return resolveURIReference(base, ref, refAuthority)
}

func resolveEmptyURIReference(base string) (string, error) {
	baseURL, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	syntax := uriReferenceSyntaxFor(base, baseURL)
	if file, ok := localFileURIPath(baseURL, syntax.fragment); ok {
		return file, nil
	}
	canonical, ok := canonicalURL(baseURL, syntax)
	if !ok {
		return "", errors.New("schema base has an invalid URI path")
	}
	return canonical, nil
}

func resolveAbsoluteReference(ref *url.URL, authority uriAuthoritySyntax) (string, error) {
	if strings.EqualFold(ref.Scheme, "file") {
		ref.Scheme = "file"
		if !hasEncodedPathSeparator(ref.EscapedPath()) {
			if file, ok := localFileURIPath(ref, uriFragmentAbsent); ok {
				return canonicalLocalReference(file, localPathFormOf(ref.Path)), nil
			}
		}
	}
	canonical, ok := canonicalURL(ref, uriReferenceSyntax{authority: authority, fragment: uriFragmentAbsent})
	if !ok {
		return "", errors.New("schema reference has an invalid URI path")
	}
	return canonical, nil
}

func resolveLocalReference(base string, ref *url.URL, authority uriAuthoritySyntax) (string, error) {
	switch authority.kind {
	case uriAuthorityAbsent:
	case uriAuthorityNonEmpty:
		return "", errReferenceUnavailable
	case uriAuthorityEmpty:
		if authority.escapedPath == "" {
			return "", errReferenceUnavailable
		}
	case uriAuthorityInvalid:
		return "", errors.New("schema reference has invalid authority syntax")
	default:
		err := errors.New("schema reference has invalid authority syntax")
		return "", err
	}
	if hasEncodedPathSeparator(ref.EscapedPath()) {
		return "", errReferenceUnavailable
	}
	if ref.Host != "" || ref.RawQuery != "" || ref.ForceQuery {
		return "", errReferenceUnavailable
	}
	refPath, err := url.PathUnescape(ref.EscapedPath())
	if err != nil {
		return "", errors.New("local schema reference has an invalid escaped path")
	}
	if strings.IndexByte(refPath, 0) >= 0 {
		return "", errReferenceUnavailable
	}
	return resolveLocalReferencePath(base, refPath), nil
}

func resolveLocalReferencePath(base, refPath string) string {
	resolved := filepath.FromSlash(refPath)
	switch {
	case filepath.IsAbs(resolved):
	case resolved != "" && os.IsPathSeparator(resolved[0]):
		resolved = filepath.Join(filepath.VolumeName(base), resolved)
	default:
		resolved = filepath.Join(filepath.Dir(base), resolved)
	}
	return canonicalLocalReference(resolved, localPathFormOf(refPath))
}

func resolveURIReference(base string, ref *url.URL, authority uriAuthoritySyntax) (string, error) {
	baseURL, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	if authority.kind != uriAuthorityAbsent {
		return resolveAuthorityReference(baseURL.Scheme, ref, authority)
	}
	if baseURL.Opaque != "" || ref.Opaque != "" {
		return "", errReferenceUnavailable
	}
	resolved := baseURL.ResolveReference(ref)
	baseAuthority := parseURIAuthoritySyntax(base, baseURL.Scheme)
	canonical, ok := canonicalURL(resolved, uriReferenceSyntax{authority: baseAuthority, fragment: uriFragmentAbsent})
	if !ok {
		return "", errors.New("schema reference has an invalid URI path")
	}
	return canonical, nil
}

type uriAuthorityKind uint8

const (
	uriAuthorityInvalid uriAuthorityKind = iota
	uriAuthorityAbsent
	uriAuthorityNonEmpty
	uriAuthorityEmpty
)

type uriAuthoritySyntax struct {
	escapedPath string
	kind        uriAuthorityKind
}

func (a uriAuthoritySyntax) valid() bool {
	return a.kind == uriAuthorityAbsent || a.kind == uriAuthorityNonEmpty || a.kind == uriAuthorityEmpty
}

func (a uriAuthoritySyntax) present() bool {
	return a.kind == uriAuthorityNonEmpty || a.kind == uriAuthorityEmpty
}

type uriFragmentSyntax uint8

const (
	uriFragmentInvalid uriFragmentSyntax = iota
	uriFragmentAbsent
	uriFragmentPresent
)

type uriReferenceSyntax struct {
	authority uriAuthoritySyntax
	fragment  uriFragmentSyntax
}

func uriReferenceSyntaxFor(raw string, parsed *url.URL) uriReferenceSyntax {
	fragment := uriFragmentAbsent
	if strings.IndexByte(raw, '#') >= 0 {
		fragment = uriFragmentPresent
	}
	return uriReferenceSyntax{
		authority: parseURIAuthoritySyntax(raw, parsed.Scheme),
		fragment:  fragment,
	}
}

func canonicalURL(parsed *url.URL, syntax uriReferenceSyntax) (string, bool) {
	if !syntax.authority.valid() || syntax.fragment != uriFragmentAbsent && syntax.fragment != uriFragmentPresent {
		return "", false
	}
	u := *parsed
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = canonicalURIHost(u.Host)
	if !canonicalizeURLPath(&u) {
		return "", false
	}
	query, ok := canonicalEscapedComponent(u.RawQuery)
	if !ok {
		return "", false
	}
	u.RawQuery = query
	if !canonicalizeURLFragment(&u) {
		return "", false
	}
	canonical := preserveAuthoritySyntax(u.String(), &u, syntax.authority)
	if syntax.fragment == uriFragmentPresent && u.Fragment == "" {
		canonical += "#"
	}
	return canonical, true
}

func canonicalizeURLPath(u *url.URL) bool {
	if u.Opaque != "" {
		opaque, ok := canonicalEscapedComponent(u.Opaque)
		if !ok {
			return false
		}
		u.Opaque = opaque
		return true
	}
	escaped, ok := canonicalEscapedComponent(u.EscapedPath())
	if !ok {
		return false
	}
	escaped = removeURLDotSegments(escaped)
	decoded, err := url.PathUnescape(escaped)
	if err != nil {
		return false
	}
	u.Path = decoded
	u.RawPath = escaped
	if (&url.URL{Path: decoded}).EscapedPath() == escaped {
		u.RawPath = ""
	}
	return true
}

func canonicalizeURLFragment(u *url.URL) bool {
	escapedFragment, ok := canonicalEscapedComponent(u.EscapedFragment())
	if !ok {
		return false
	}
	fragment, err := url.PathUnescape(escapedFragment)
	if err != nil {
		return false
	}
	u.Fragment = fragment
	u.RawFragment = escapedFragment
	if (&url.URL{Fragment: fragment}).EscapedFragment() == escapedFragment {
		u.RawFragment = ""
	}
	return true
}

func preserveAuthoritySyntax(canonical string, u *url.URL, authority uriAuthoritySyntax) string {
	if !authority.present() || u.Opaque != "" || u.Host != "" || u.User != nil {
		return canonical
	}
	start := 0
	if u.Scheme != "" {
		start = len(u.Scheme) + 1
	}
	if strings.HasPrefix(canonical[start:], "//") {
		return canonical
	}
	return canonical[:start] + "//" + canonical[start:]
}

func canonicalURIHost(host string) string {
	if !strings.HasPrefix(host, "[") {
		return strings.ToLower(host)
	}
	closingBracket := strings.LastIndexByte(host, ']')
	if closingBracket < 0 {
		return strings.ToLower(host)
	}
	literal := host[1:closingBracket]
	zone := strings.IndexByte(literal, '%')
	if zone < 0 {
		return strings.ToLower(host)
	}
	return "[" + strings.ToLower(literal[:zone]) + literal[zone:] + host[closingBracket:]
}

func parseURIAuthoritySyntax(raw, scheme string) uriAuthoritySyntax {
	rest := raw
	if scheme != "" {
		_, after, ok := strings.Cut(raw, ":")
		if !ok {
			return uriAuthoritySyntax{kind: uriAuthorityInvalid}
		}
		rest = after
	}
	if !strings.HasPrefix(rest, "//") {
		return uriAuthoritySyntax{kind: uriAuthorityAbsent}
	}
	hierarchy := rest[2:]
	if end := strings.IndexAny(hierarchy, "?#"); end >= 0 {
		hierarchy = hierarchy[:end]
	}
	if hierarchy == "" {
		return uriAuthoritySyntax{kind: uriAuthorityEmpty}
	}
	if hierarchy[0] == '/' {
		return uriAuthoritySyntax{kind: uriAuthorityEmpty, escapedPath: hierarchy}
	}
	return uriAuthoritySyntax{kind: uriAuthorityNonEmpty}
}

func resolveAuthorityReference(scheme string, ref *url.URL, authority uriAuthoritySyntax) (string, error) {
	switch authority.kind {
	case uriAuthorityEmpty:
		decodedPath, err := url.PathUnescape(authority.escapedPath)
		if err != nil || strings.IndexByte(decodedPath, 0) >= 0 {
			return "", errors.New("schema reference has an invalid escaped path")
		}
		ref.Path = decodedPath
		ref.RawPath = authority.escapedPath
		plain := &url.URL{Path: decodedPath}
		if plain.EscapedPath() == authority.escapedPath {
			ref.RawPath = ""
		}
	case uriAuthorityNonEmpty:
	case uriAuthorityInvalid, uriAuthorityAbsent:
		return "", errors.New("schema reference has invalid authority syntax")
	default:
		err := errors.New("schema reference has invalid authority syntax")
		return "", err
	}
	ref.Scheme = scheme
	canonical, ok := canonicalURL(ref, uriReferenceSyntax{authority: authority, fragment: uriFragmentAbsent})
	if !ok {
		return "", errors.New("schema reference has an invalid URI path")
	}
	return canonical, nil
}

// removeURLDotSegments applies RFC 3986 path resolution without collapsing
// empty segments, which remain identity-significant for hierarchical URIs.
func removeURLDotSegments(escaped string) string {
	if escaped == "" {
		return ""
	}
	leadingSlash := escaped[0] == '/'
	parts := strings.Split(escaped, "/")
	stack := make([]string, 0, len(parts))
	for _, elem := range parts {
		stack = applyURLDotSegment(stack, elem)
	}
	last := parts[len(parts)-1]
	if last == "." || last == ".." {
		stack = append(stack, "")
	}
	cleaned := strings.Join(stack, "/")
	if leadingSlash && !strings.HasPrefix(cleaned, "/") {
		cleaned = "/" + cleaned
		if cleaned == "/" && len(stack) > 1 {
			cleaned += strings.Repeat("/", len(stack)-1)
		}
	}
	return cleaned
}

func applyURLDotSegment(stack []string, elem string) []string {
	switch elem {
	case ".":
		return stack
	case "..":
		if len(stack) != 0 && (len(stack) != 1 || stack[0] != "") {
			return stack[:len(stack)-1]
		}
		return stack
	default:
		return append(stack, elem)
	}
}

func canonicalEscapedComponent(escaped string) (string, bool) {
	if !strings.Contains(escaped, "%") {
		return escaped, true
	}
	var b strings.Builder
	b.Grow(len(escaped))
	for i := 0; i < len(escaped); i++ {
		if escaped[i] != '%' {
			b.WriteByte(escaped[i])
			continue
		}
		ok := appendCanonicalEscape(&b, escaped[i:])
		if !ok {
			return "", false
		}
		i += 2
	}
	return b.String(), true
}

func appendCanonicalEscape(b *strings.Builder, escaped string) bool {
	if len(escaped) < 3 {
		return false
	}
	hi, hiOK := hexValue(escaped[1])
	lo, loOK := hexValue(escaped[2])
	if !hiOK || !loOK {
		return false
	}
	value := hi<<4 | lo
	if isURIUnreserved(value) {
		b.WriteByte(value)
		return true
	}
	const upperHex = "0123456789ABCDEF"
	b.WriteByte('%')
	b.WriteByte(upperHex[value>>4])
	b.WriteByte(upperHex[value&0xf])
	return true
}

func hexValue(b byte) (byte, bool) {
	switch {
	case b >= '0' && b <= '9':
		return b - '0', true
	case b >= 'a' && b <= 'f':
		return b - 'a' + 10, true
	case b >= 'A' && b <= 'F':
		return b - 'A' + 10, true
	default:
		return 0, false
	}
}

func isURIUnreserved(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' ||
		b >= '0' && b <= '9' || b == '-' || b == '.' || b == '_' || b == '~'
}

func hasEncodedPathSeparator(path string) bool {
	lower := strings.ToLower(path)
	return strings.Contains(lower, "%2f") || strings.Contains(lower, "%00") ||
		os.IsPathSeparator('\\') && strings.Contains(lower, "%5c")
}

func canonicalLocalPath(name string) string {
	cleaned := filepath.Clean(name)
	if filepath.IsAbs(cleaned) {
		return cleaned
	}
	if hasURIScheme(filepath.ToSlash(cleaned)) {
		return "." + string(filepath.Separator) + cleaned
	}
	return cleaned
}

type localPathForm uint8

const (
	localPathInvalid localPathForm = iota
	localPathFile
	localPathDirectory
)

func canonicalLocalReference(name string, form localPathForm) string {
	cleaned := canonicalLocalPath(name)
	switch form {
	case localPathFile:
	case localPathDirectory:
		if !os.IsPathSeparator(cleaned[len(cleaned)-1]) {
			cleaned += string(filepath.Separator)
		}
	case localPathInvalid:
		panic("local path form is invalid")
	default:
		panic("local path form is unknown")
	}
	return cleaned
}

func localPathFormOf(name string) localPathForm {
	if name == "" {
		return localPathFile
	}
	if os.IsPathSeparator(name[len(name)-1]) {
		return localPathDirectory
	}
	start := len(name)
	for start > 0 && !os.IsPathSeparator(name[start-1]) {
		start--
	}
	last := name[start:]
	if last == "." || last == ".." {
		return localPathDirectory
	}
	return localPathFile
}

func isLocalName(name string) bool {
	return filepath.IsAbs(name) || !hasURIScheme(name)
}

func hasURIScheme(name string) bool {
	if len(name) < 2 || !isASCIIAlpha(name[0]) {
		return false
	}
	for i := 1; i < len(name); i++ {
		b := name[i]
		if b == ':' {
			return true
		}
		if !isASCIIAlpha(b) && (b < '0' || b > '9') && b != '+' && b != '-' && b != '.' {
			return false
		}
	}
	return false
}

func isASCIIAlpha(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

func errorIsOnly(err, target error) bool {
	if err == nil {
		return false
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		return errorsAreOnly(joined.Unwrap(), target)
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		if cause := wrapped.Unwrap(); cause != nil {
			return errorIsOnly(cause, target)
		}
	}
	return errors.Is(err, target)
}

func errorsAreOnly(causes []error, target error) bool {
	if len(causes) == 0 {
		return false
	}
	for _, cause := range causes {
		if !errorIsOnly(cause, target) {
			return false
		}
	}
	return true
}

func localSchemaFile(resolved string) (string, bool) {
	u, err := url.Parse(resolved)
	if err == nil && u.Scheme != "" {
		return localFileURIPath(u, uriReferenceSyntaxFor(resolved, u).fragment)
	}
	if !isLocalName(resolved) {
		return "", false
	}
	return canonicalLocalPath(resolved), true
}

// localFileURIPath returns the local filesystem path represented by u.
// fragment carries syntax that net/url does not retain for a trailing '#'.
func localFileURIPath(u *url.URL, fragment uriFragmentSyntax) (string, bool) {
	if !strings.EqualFold(u.Scheme, "file") || u.User != nil || u.RawQuery != "" || u.ForceQuery || fragment != uriFragmentAbsent || u.Fragment != "" {
		return "", false
	}
	if u.Host != "" && !strings.EqualFold(u.Host, "localhost") {
		return "", false
	}
	if hasEncodedPathSeparator(u.EscapedPath()) || u.Path == "" || strings.IndexByte(u.Path, 0) >= 0 {
		return "", false
	}
	file := u.Path
	if filepath.Separator == '\\' && len(file) >= 3 && file[0] == '/' && file[2] == ':' {
		file = file[1:]
	}
	return filepath.Clean(filepath.FromSlash(file)), true
}
