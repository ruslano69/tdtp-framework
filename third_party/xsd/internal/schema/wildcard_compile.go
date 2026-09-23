package schema

import (
	"strings"

	"github.com/jacoelho/xsd/internal/lex"

	"github.com/jacoelho/xsd/internal/uriref"
	"github.com/jacoelho/xsd/xsderrors"
)

const (
	wildcardNamespaceAny             = "##any"
	wildcardNamespaceOther           = "##other"
	wildcardNamespaceLocal           = "##local"
	wildcardNamespaceTargetNamespace = "##targetNamespace"
)

// NamespaceInterner interns namespace URIs while parsing wildcard namespace
// declarations.
type NamespaceInterner interface {
	InternNamespace(uri string) (NamespaceID, error)
}

// WildcardAttrs is the raw wildcard attribute projection from xs:any or
// xs:anyAttribute.
type WildcardAttrs struct {
	Namespace          string
	ProcessContents    string
	TargetNamespace    string
	HasNamespace       bool
	HasProcessContents bool
}

// ParseWildcard parses compile-time wildcard namespace and processContents
// attributes.
func ParseWildcard(names NamespaceInterner, attrs WildcardAttrs) (Wildcard, error) {
	wildcard, err := parseWildcardNamespace(names, attrs)
	if err != nil {
		return Wildcard{}, err
	}
	process, err := parseWildcardProcessContents(attrs)
	if err != nil {
		return Wildcard{}, err
	}
	wildcard.Process = process
	return wildcard, nil
}

func parseWildcardNamespace(names NamespaceInterner, attrs WildcardAttrs) (Wildcard, error) {
	nsSpec := wildcardNamespaceAny
	if attrs.HasNamespace {
		nsSpec = attrs.Namespace
	}
	switch nsSpec {
	case wildcardNamespaceAny:
		return Wildcard{Mode: WildcardAny}, nil
	case wildcardNamespaceOther:
		ns, err := internWildcardNamespace(names, attrs.TargetNamespace)
		if err != nil {
			return Wildcard{}, err
		}
		return Wildcard{Mode: WildcardOther, OtherThan: ns}, nil
	case wildcardNamespaceLocal:
		return Wildcard{Mode: WildcardLocal}, nil
	case wildcardNamespaceTargetNamespace:
		ns, err := internWildcardNamespace(names, attrs.TargetNamespace)
		if err != nil {
			return Wildcard{}, err
		}
		return Wildcard{Mode: WildcardTargetNamespace, Namespaces: []NamespaceID{ns}}, nil
	default:
		namespaces, err := parseWildcardNamespaceList(names, attrs.TargetNamespace, nsSpec)
		if err != nil {
			return Wildcard{}, err
		}
		return Wildcard{Mode: WildcardList, Namespaces: namespaces}, nil
	}
}

func parseWildcardNamespaceList(names NamespaceInterner, targetNS, nsSpec string) ([]NamespaceID, error) {
	var namespaces []NamespaceID
	for part := range lex.XMLFieldsSeq(nsSpec) {
		uri, err := wildcardNamespaceURI(part, targetNS)
		if err != nil {
			return nil, err
		}
		ns, err := internWildcardNamespace(names, uri)
		if err != nil {
			return nil, err
		}
		namespaces = append(namespaces, ns)
	}
	return NormalizeNamespaceList(namespaces), nil
}

func wildcardNamespaceURI(part, targetNS string) (string, error) {
	switch part {
	case wildcardNamespaceLocal:
		return "", nil
	case wildcardNamespaceTargetNamespace:
		return targetNS, nil
	default:
		if strings.HasPrefix(part, "##") {
			return "", xsderrors.SchemaCompile(xsderrors.CodeSchemaInvalidAttribute, "invalid wildcard namespace "+part)
		}
		if _, err := uriref.Check(part); err != nil {
			return "", xsderrors.SchemaCompile(xsderrors.CodeSchemaInvalidAttribute, "invalid wildcard namespace "+part)
		}
		return part, nil
	}
}

func parseWildcardProcessContents(attrs WildcardAttrs) (ProcessContents, error) {
	const processContentsStrict = "strict"

	process := processContentsStrict
	if attrs.HasProcessContents {
		process = attrs.ProcessContents
	}
	switch process {
	case "skip":
		return ProcessSkip, nil
	case "lax":
		return ProcessLax, nil
	case processContentsStrict:
		return ProcessStrict, nil
	default:
		return 0, xsderrors.SchemaCompile(xsderrors.CodeSchemaInvalidAttribute, "invalid processContents")
	}
}

func internWildcardNamespace(names NamespaceInterner, uri string) (NamespaceID, error) {
	if names == nil {
		return 0, xsderrors.InternalInvariant("wildcard parser requires namespace interner")
	}
	return names.InternNamespace(uri)
}
