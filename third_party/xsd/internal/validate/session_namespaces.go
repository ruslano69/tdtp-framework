package validate

import (
	"encoding/xml"

	xsdSchema "github.com/jacoelho/xsd/internal/schema"
	"github.com/jacoelho/xsd/internal/value"
)

func (s *session) runtimeName(n xml.Name) xsdSchema.RuntimeName {
	return ResolveRuntimeName(s.rt, n)
}

func (s *session) simpleValueQNameResolver(id xsdSchema.SimpleTypeID) value.Resolver {
	program := s.rt.ValueProgram()
	needs, ok := program.NeedsQNameResolver(id)
	if !ok || !needs {
		return value.Resolver{}
	}
	if s.valueResolver.Notation == nil {
		s.valueResolver.QName = s.qnameResolver()
		s.valueResolver.Notation = s.rt.NotationDeclared
	}
	return s.valueResolver
}

func (s *session) qnameResolver() value.QNameResolver {
	if s.valueResolver.QName == nil {
		s.valueResolver.QName = s.resolveLexicalQName
	}
	return s.valueResolver.QName
}

func (s *session) resolveLexicalQName(v string) (value.ExpandedName, bool) {
	return ResolveLexicalQName(v, s.reader.Lookup)
}
