package schema

import (
	"strings"

	"github.com/jacoelho/xsd/internal/lex"

	"github.com/jacoelho/xsd/xsderrors"
)

// IdentityNameResolver maps parsed identity XPath QName tokens through the
// schema namespace context and runtime name table.
type IdentityNameResolver interface {
	ResolveIdentityQName(parts QNameParts) (QName, error)
	ResolveIdentityWildcardNamespace(prefix string) (NamespaceID, error)
}

// ParseIdentityPaths parses selector XPath branches for an identity constraint.
func ParseIdentityPaths(xpath string, resolver IdentityNameResolver) ([]IdentityPath, error) {
	out := make([]IdentityPath, 0, strings.Count(xpath, "|")+1)
	for part := range strings.SplitSeq(xpath, "|") {
		part = lex.TrimXMLWhitespaceString(part)
		if part == "" {
			return nil, xsderrors.SchemaCompile(xsderrors.CodeSchemaIdentity, "identity selector XPath branch is empty")
		}
		desc := false
		if rest, ok := parseIdentityDescendantPrefix(part); ok {
			desc = true
			part = rest
		}
		if part == "." && !desc {
			out = append(out, IdentityPath{Self: true})
			continue
		}
		steps, err := parseIdentitySteps(part, resolver)
		if err != nil {
			return nil, err
		}
		out = append(out, IdentityPath{Descendant: desc, Steps: steps})
	}
	return out, nil
}

// ParseIdentityFieldPaths parses field XPath branches for an identity constraint.
func ParseIdentityFieldPaths(xpath string, resolver IdentityNameResolver) ([]IdentityFieldPath, error) {
	out := make([]IdentityFieldPath, 0, strings.Count(xpath, "|")+1)
	for part := range strings.SplitSeq(xpath, "|") {
		part = lex.TrimXMLWhitespaceString(part)
		if part == "" {
			return nil, xsderrors.SchemaCompile(xsderrors.CodeSchemaIdentity, "identity field XPath branch is empty")
		}
		path, err := parseIdentityFieldPathBranch(part, resolver)
		if err != nil {
			return nil, err
		}
		out = append(out, path)
	}
	return out, nil
}

func parseIdentityFieldPathBranch(part string, resolver IdentityNameResolver) (IdentityFieldPath, error) {
	desc := false
	if rest, ok := parseIdentityDescendantPrefix(part); ok {
		desc = true
		part = rest
	}
	if part == "" {
		return IdentityFieldPath{}, xsderrors.SchemaCompile(xsderrors.CodeSchemaIdentity, "identity field XPath branch is empty")
	}
	if part == "." && !desc {
		return IdentityFieldPath{Self: true, Attribute: NoQName()}, nil
	}
	attribute, err := parseIdentityFieldAttribute(part, resolver)
	if err != nil {
		return IdentityFieldPath{}, err
	}
	part = attribute.elementPath
	var steps []IdentityStep
	if part != "" {
		steps, err = parseIdentitySteps(part, resolver)
		if err != nil {
			return IdentityFieldPath{}, err
		}
	}
	return IdentityFieldPath{
		Descendant:       desc,
		Attr:             attribute.present,
		AttrWildcard:     attribute.name.wildcard,
		AttrNamespaceSet: attribute.name.namespaceSet,
		AttrNamespace:    attribute.name.namespace,
		Steps:            steps,
		Attribute:        attribute.name.name,
	}, nil
}

type identityFieldAttribute struct {
	elementPath string
	name        identityNameTest
	present     bool
}

func parseIdentityFieldAttribute(part string, resolver IdentityNameResolver) (identityFieldAttribute, error) {
	if strings.HasPrefix(part, "/") {
		return identityFieldAttribute{}, xsderrors.SchemaCompile(xsderrors.CodeSchemaIdentity, "invalid identity field XPath "+part)
	}
	elementPath, step := splitIdentityLastStep(part)
	if name, ok := strings.CutPrefix(step, "@"); ok {
		if name == "" {
			return identityFieldAttribute{}, xsderrors.SchemaCompile(xsderrors.CodeSchemaIdentity, "invalid identity field XPath "+part)
		}
		attrName, err := parseIdentityNameTestParts(name, resolver)
		return identityFieldAttribute{elementPath: elementPath, name: attrName, present: true}, err
	}
	name, ok := parseIdentityAxisStep(step, "attribute")
	if ok && name == "" {
		return identityFieldAttribute{}, xsderrors.SchemaCompile(xsderrors.CodeSchemaIdentity, "invalid identity field XPath "+part)
	}
	if !ok {
		return identityFieldAttribute{elementPath: part, name: identityNameTest{name: NoQName()}}, nil
	}
	attrName, err := parseIdentityNameTestParts(name, resolver)
	return identityFieldAttribute{elementPath: elementPath, name: attrName, present: true}, err
}

func splitIdentityLastStep(path string) (elementPath, lastStep string) {
	idx := strings.LastIndex(path, "/")
	if idx < 0 {
		return "", path
	}
	return path[:idx], path[idx+1:]
}

func parseIdentityDescendantPrefix(path string) (string, bool) {
	if rest, ok := strings.CutPrefix(path, ".//"); ok {
		return lex.TrimXMLWhitespaceString(rest), true
	}
	if rest, ok := strings.CutPrefix(path, ". //"); ok {
		return lex.TrimXMLWhitespaceString(rest), true
	}
	return path, false
}

func parseIdentityNameTest(lexical string, resolver IdentityNameResolver) (IdentityStep, error) {
	parsed, err := parseIdentityNameTestParts(lexical, resolver)
	if err != nil {
		return IdentityStep{}, err
	}
	return IdentityStep{
		Name:         parsed.name,
		Namespace:    parsed.namespace,
		Wildcard:     parsed.wildcard,
		NamespaceSet: parsed.namespaceSet,
	}, nil
}

type identityNameTest struct {
	name         QName
	namespace    NamespaceID
	wildcard     bool
	namespaceSet bool
}

func parseIdentityNameTestParts(lexical string, resolver IdentityNameResolver) (identityNameTest, error) {
	lexical = lex.TrimXMLWhitespaceString(lexical)
	if lexical == "*" {
		return identityNameTest{name: NoQName(), wildcard: true}, nil
	}
	prefix, wildcard, err := parseIdentityQNamePrefixWildcard(lexical)
	if err != nil {
		return identityNameTest{}, err
	}
	if wildcard {
		if resolver == nil {
			return identityNameTest{}, xsderrors.InternalInvariant("identity XPath parser requires name resolver")
		}
		nsID, nsErr := resolver.ResolveIdentityWildcardNamespace(prefix)
		if nsErr != nil {
			return identityNameTest{}, nsErr
		}
		return identityNameTest{name: NoQName(), wildcard: true, namespaceSet: true, namespace: nsID}, nil
	}
	q, err := parseIdentityQName(lexical, resolver)
	if err != nil {
		return identityNameTest{}, err
	}
	return identityNameTest{name: q}, nil
}

func parseIdentitySteps(path string, resolver IdentityNameResolver) ([]IdentityStep, error) {
	if err := validateIdentityElementPath(path); err != nil {
		return nil, err
	}
	steps := make([]IdentityStep, 0, strings.Count(path, "/")+1)
	for part := range strings.SplitSeq(path, "/") {
		step, present, err := parseIdentityElementStep(part, resolver)
		if err != nil {
			return nil, err
		}
		if !present {
			continue
		}
		steps = append(steps, step)
	}
	return steps, nil
}

func validateIdentityElementPath(path string) error {
	if strings.Contains(path, "@") {
		return xsderrors.SchemaCompile(xsderrors.CodeSchemaIdentity, "invalid identity XPath "+path)
	}
	if strings.ContainsAny(path, "[]()") || strings.HasPrefix(path, "/") || strings.HasSuffix(path, "/") {
		return xsderrors.SchemaCompile(xsderrors.CodeSchemaIdentity, "invalid identity XPath "+path)
	}
	return nil
}

func parseIdentityElementStep(part string, resolver IdentityNameResolver) (IdentityStep, bool, error) {
	part = lex.TrimXMLWhitespaceString(part)
	if part == "" {
		return IdentityStep{}, false, xsderrors.SchemaCompile(xsderrors.CodeSchemaIdentity, "invalid identity XPath step")
	}
	if strings.Contains(part, "::") {
		name, err := parseIdentityChildAxisStep(part)
		if err != nil {
			return IdentityStep{}, false, err
		}
		part = name
	}
	if part == "." {
		return IdentityStep{}, false, nil
	}
	step, err := parseIdentityNameTest(part, resolver)
	return step, true, err
}

func parseIdentityChildAxisStep(part string) (string, error) {
	name, ok := parseIdentityAxisStep(part, "child")
	if !ok {
		return "", xsderrors.SchemaCompile(xsderrors.CodeSchemaIdentity, "invalid identity XPath step "+part)
	}
	if name == "" || name == "." {
		return "", xsderrors.SchemaCompile(xsderrors.CodeSchemaIdentity, "invalid identity XPath step child::")
	}
	return name, nil
}

func parseIdentityAxisStep(part, axis string) (string, bool) {
	part = lex.TrimXMLWhitespaceString(part)
	rest, ok := strings.CutPrefix(part, axis)
	if !ok {
		return "", false
	}
	rest = lex.TrimXMLWhitespaceString(rest)
	rest, ok = strings.CutPrefix(rest, "::")
	if !ok {
		return "", false
	}
	return lex.TrimXMLWhitespaceString(rest), true
}

func parseIdentityQName(lexical string, resolver IdentityNameResolver) (QName, error) {
	parts, err := ParseQNameParts(lexical)
	if err != nil {
		return QName{}, err
	}
	if resolver == nil {
		return QName{}, xsderrors.InternalInvariant("identity XPath parser requires name resolver")
	}
	return resolver.ResolveIdentityQName(parts)
}

func parseIdentityQNamePrefixWildcard(lexical string) (string, bool, error) {
	lexical = lex.TrimXMLWhitespaceString(lexical)
	prefix, local, ok := strings.Cut(lexical, ":")
	if !ok || local != "*" {
		return "", false, nil
	}
	if prefix == "" || !lex.IsNCName(prefix) {
		return "", true, xsderrors.SchemaCompile(xsderrors.CodeSchemaReference, invalidQNameMessagePrefix+lexical)
	}
	return prefix, true, nil
}
