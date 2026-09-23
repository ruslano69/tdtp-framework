package uriref

import (
	"errors"
	"strings"
)

// ErrOpaqueBase is returned when a relative path cannot be resolved against
// a non-hierarchical base URI.
var ErrOpaqueBase = errors.New("relative URI reference cannot be resolved against an opaque base")

type components struct {
	scheme       string
	authority    string
	path         string
	query        string
	fragment     string
	hasScheme    bool
	hasAuthority bool
	hasQuery     bool
	hasFragment  bool
}

// Parts is the parsed component view of a validated Reference. Presence flags
// preserve the distinction between absent and explicitly empty components.
type Parts struct {
	Scheme       string
	Authority    string
	Path         string
	Query        string
	Fragment     string
	HasScheme    bool
	HasAuthority bool
	HasQuery     bool
	HasFragment  bool
}

// Parts returns a copy of the reference's parsed components.
func (r Reference) Parts() Parts {
	c := r.components
	return Parts{
		Scheme: c.scheme, Authority: c.authority, Path: c.path, Query: c.query,
		Fragment: c.fragment, HasScheme: c.hasScheme, HasAuthority: c.hasAuthority,
		HasQuery: c.hasQuery, HasFragment: c.hasFragment,
	}
}

// Resolve resolves ref against base using the validating RFC 2396 section 5.2
// algorithm. It retains excess leading ".." segments, as shown in RFC 2396
// Appendix C, and does not normalize dot segments in absolute-path or
// scheme-bearing references.
func Resolve(base, ref Reference) (Reference, error) {
	b := base.components
	r := ref.components
	if isSameDocumentReference(r) {
		return resolveSameDocumentReference(base, r)
	}
	if r.hasScheme {
		return ref, nil
	}
	if r.hasAuthority {
		r.scheme, r.hasScheme = b.scheme, b.hasScheme
		return compose(r)
	}
	if r.path != "" && r.path[0] == '/' {
		r.scheme, r.hasScheme = b.scheme, b.hasScheme
		r.authority, r.hasAuthority = b.authority, b.hasAuthority
		return compose(r)
	}
	switch {
	case opaque(b):
		if r.path != "" {
			return Reference{}, ErrOpaqueBase
		}
		// RFC 2396 permits relative resolution only for hierarchical bases.
		// Query-only references are retained as a narrow XML Base extension,
		// preserving the opaque path while replacing its query.
		r.path = b.path
	case r.path == "":
		r.path = b.path
	default:
		r.path = removeRelativeDotSegments(mergePath(b, r.path))
	}
	r.scheme, r.hasScheme = b.scheme, b.hasScheme
	r.authority, r.hasAuthority = b.authority, b.hasAuthority
	return compose(r)
}

func isSameDocumentReference(c components) bool {
	return c.path == "" && !c.hasScheme && !c.hasAuthority && !c.hasQuery
}

func resolveSameDocumentReference(base Reference, ref components) (Reference, error) {
	without := base.WithoutFragment()
	if !ref.hasFragment {
		return without, nil
	}
	b := base.components
	return compose(components{
		scheme: b.scheme, authority: b.authority, path: b.path, query: b.query,
		fragment: ref.fragment, hasScheme: b.hasScheme, hasAuthority: b.hasAuthority,
		hasQuery: b.hasQuery, hasFragment: true,
	})
}

func compose(c components) (Reference, error) {
	return Parse(spell(c))
}

func spell(c components) string {
	size := len(c.path)
	if c.hasScheme {
		size += len(c.scheme) + 1
	}
	if c.hasAuthority {
		size += len(c.authority) + 2
	}
	if c.hasQuery {
		size += len(c.query) + 1
	}
	if c.hasFragment {
		size += len(c.fragment) + 1
	}
	var out strings.Builder
	out.Grow(size)
	if c.hasScheme {
		out.WriteString(c.scheme)
		out.WriteByte(':')
	}
	if c.hasAuthority {
		out.WriteString("//")
		out.WriteString(c.authority)
	}
	out.WriteString(c.path)
	if c.hasQuery {
		out.WriteByte('?')
		out.WriteString(c.query)
	}
	if c.hasFragment {
		out.WriteByte('#')
		out.WriteString(c.fragment)
	}
	return out.String()
}

func split(raw string) components {
	var c components
	rest := raw
	if i := strings.IndexByte(rest, '#'); i >= 0 {
		c.fragment, c.hasFragment = rest[i+1:], true
		rest = rest[:i]
	}
	if i := strings.IndexByte(rest, '?'); i >= 0 {
		c.query, c.hasQuery = rest[i+1:], true
		rest = rest[:i]
	}
	if i := schemeSeparator(rest); i >= 0 {
		c.scheme, c.hasScheme = rest[:i], true
		rest = rest[i+1:]
	}
	if strings.HasPrefix(rest, "//") {
		c.hasAuthority = true
		rest = rest[2:]
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			c.authority, c.path = rest[:i], rest[i:]
		} else {
			c.authority = rest
		}
	} else {
		c.path = rest
	}
	return c
}

func schemeSeparator(raw string) int {
	for i := range len(raw) {
		switch raw[i] {
		case ':':
			return i
		case '/':
			return -1
		}
	}
	return -1
}

func opaque(c components) bool {
	return c.hasScheme && !c.hasAuthority && (c.path == "" || c.path[0] != '/')
}

func mergePath(base components, ref string) string {
	if i := strings.LastIndexByte(base.path, '/'); i >= 0 {
		return base.path[:i+1] + ref
	}
	if base.hasAuthority {
		return "/" + ref
	}
	return ref
}

func removeRelativeDotSegments(path string) string {
	absolute := strings.HasPrefix(path, "/")
	trailing := strings.HasSuffix(path, "/") || strings.HasSuffix(path, "/.") || strings.HasSuffix(path, "/..") || path == "." || path == ".."
	parts := strings.Split(path, "/")
	stack := make([]string, 0, len(parts))
	start := 0
	if absolute {
		start = 1
	}
	for _, part := range parts[start:] {
		stack = applyRelativeDotSegment(stack, part)
	}
	result := strings.Join(stack, "/")
	if absolute {
		result = "/" + result
	}
	if trailing && result != "" && !strings.HasSuffix(result, "/") {
		result += "/"
	}
	return result
}

func applyRelativeDotSegment(stack []string, part string) []string {
	switch part {
	case ".":
		return stack
	case "..":
		if len(stack) > 0 && stack[len(stack)-1] != ".." {
			return stack[:len(stack)-1]
		}
	}
	return append(stack, part)
}
