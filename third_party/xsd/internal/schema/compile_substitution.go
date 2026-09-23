package schema

import (
	"errors"
	"strconv"

	"github.com/jacoelho/xsd/internal/vocab"
	"github.com/jacoelho/xsd/xsderrors"
)

func (c *compiler) compileSubstitutions() error {
	compilation := newSubstitutionCompilation(c)
	if err := compilation.linkHeads(); err != nil {
		return err
	}
	compilation.propagateInheritedTypes()
	if err := compilation.rejectCycles(); err != nil {
		return err
	}
	if err := compilation.finalizeElementConstraints(); err != nil {
		return err
	}
	table, err := c.rt.buildSubstitutionTable(compilation.elements, c.limits.MaxSubstitutionClosureEntries)
	if err != nil {
		return c.substitutionTableError(err, compilation.elements)
	}
	c.installFinalizedElements(compilation.elements, table)
	c.pendingElementConstraints = nil
	return nil
}

type substitutionCompilation struct {
	compiler     *compiler
	elements     []ElementDecl
	children     [][]ElementID
	indegree     []uint8
	inheritsType []bool
	members      []QName
}

func newSubstitutionCompilation(c *compiler) substitutionCompilation {
	elements := c.elementCopies()
	return substitutionCompilation{
		compiler: c, elements: elements,
		children: make([][]ElementID, len(elements)),
		indegree: make([]uint8, len(elements)), inheritsType: make([]bool, len(elements)),
		members: sortedBuildQNames(&c.rt, c.elementComponents),
	}
}

func (s *substitutionCompilation) linkHeads() error {
	for _, member := range s.members {
		if err := s.linkHead(member); err != nil {
			return err
		}
	}
	return nil
}

func (s *substitutionCompilation) linkHead(memberQName QName) error {
	raw := s.compiler.elementComponents[memberQName]
	source := raw.sourceNode().semantic.element()
	if source == nil {
		return xsderrors.InternalInvariant("element substitution source is not typed")
	}
	if !source.SubstitutionGroup.Lexical.Present {
		return nil
	}
	headLexical := source.SubstitutionGroup.Lexical.Value
	member, ok := s.compiler.elementDone[memberQName]
	if !ok || !ValidElementID(member, len(s.elements)) {
		return xsderrors.InternalInvariant("substitution member was not compiled")
	}
	s.inheritsType[member] = elementUsesSubstitutionType(raw.sourceNode())
	headQName, err := s.compiler.resolveQNameChecked(raw.sourceNode(), raw.ctx, headLexical)
	if err != nil {
		return err
	}
	head, ok := s.compiler.elementDone[headQName]
	if !ok {
		return nil
	}
	if !ValidElementID(head, len(s.elements)) {
		return xsderrors.InternalInvariant("substitution head was not compiled")
	}
	s.elements[member].SubstHead = head
	s.children[head] = append(s.children[head], member)
	s.indegree[member] = 1
	return nil
}

func (s *substitutionCompilation) propagateInheritedTypes() {
	queue := make([]ElementID, 0, len(s.elements))
	for id, degree := range s.indegree {
		if degree == 0 {
			queue = append(queue, ElementID(id))
		}
	}
	for next := 0; next < len(queue); next++ {
		head := queue[next]
		for _, member := range s.children[head] {
			if s.inheritsType[member] {
				s.elements[member].Type = s.elements[head].Type
			}
			s.indegree[member] = 0
			queue = append(queue, member)
		}
	}
}

func (s *substitutionCompilation) rejectCycles() error {
	for _, memberQName := range s.members {
		member, ok := s.compiler.elementDone[memberQName]
		if !ok || s.indegree[member] == 0 {
			continue
		}
		return s.cycleError(member)
	}
	return nil
}

func (s *substitutionCompilation) cycleError(member ElementID) error {
	cycle, err := substitutionCycleElement(member, s.elements)
	if err != nil {
		return err
	}
	name := s.elements[cycle].Name
	diagnostic := xsderrors.SchemaCompile(xsderrors.CodeSchemaReference, "cyclic substitution group "+s.compiler.rt.formatName(name))
	if raw, exists := s.compiler.elementComponents[name]; exists {
		return withSchemaCompileLocation(raw.sourceNode(), diagnostic)
	}
	return diagnostic
}

func (s *substitutionCompilation) finalizeElementConstraints() error {
	for _, pending := range s.compiler.pendingElementConstraints {
		if err := s.finalizeElementConstraint(pending); err != nil {
			return err
		}
	}
	return nil
}

func (s *substitutionCompilation) finalizeElementConstraint(pending pendingElementConstraint) error {
	if !ValidElementID(pending.element, len(s.elements)) {
		return xsderrors.InternalInvariant("pending element constraint references invalid element")
	}
	decl := s.elements[pending.element]
	if decl.Default != nil || decl.Fixed != nil {
		return xsderrors.InternalInvariant("pending element constraint targets finalized declaration")
	}
	switch pending.kind {
	case DeclarationValueConstraintDefault:
		decl.Default = &ValueConstraint{Lexical: pending.lexical}
	case DeclarationValueConstraintFixed:
		decl.Fixed = &ValueConstraint{Lexical: pending.lexical}
	case DeclarationValueConstraintNone, DeclarationValueConstraintConflict:
		return xsderrors.InternalInvariant("pending element constraint has invalid kind")
	default:
		err := xsderrors.InternalInvariant("pending element constraint has invalid kind")
		return err
	}
	if err := s.compiler.validateElementValueConstraints(&decl, pending.node, s.compiler.simpleTypeUnavailable); err != nil {
		return withSchemaCompileLocation(pending.node, err)
	}
	s.elements[pending.element] = decl
	return nil
}

func (c *compiler) substitutionTableError(err error, elements []ElementDecl) error {
	var cycle SubstitutionCycleError
	if errors.As(err, &cycle) {
		name, nameOK := c.rt.ElementName(cycle.Element)
		if nameOK {
			return xsderrors.SchemaCompile(xsderrors.CodeSchemaReference, "cyclic substitution group "+c.rt.formatName(name))
		}
	}
	var limitErr SubstitutionClosureLimitError
	if errors.As(err, &limitErr) {
		return xsderrors.SchemaCompile(xsderrors.CodeSchemaLimit, "substitution-group closure exceeds MaxSubstitutionClosureEntries ("+strconv.Itoa(limitErr.Limit)+")")
	}
	var membership SubstitutionMembershipError
	if errors.As(err, &membership) &&
		ValidElementID(membership.Member, len(elements)) && ValidElementID(membership.Head, len(elements)) {
		member := elements[membership.Member]
		head := elements[membership.Head]
		diagnostic := substitutionMembershipDiagnostic(membership.Cause, SubstitutionMembershipLabels{
			MemberName: c.rt.formatName(member.Name),
			MemberType: c.rt.TypeLabel(member.Type),
			HeadName:   c.rt.formatName(head.Name),
			HeadType:   c.rt.TypeLabel(head.Type),
		})
		if raw, exists := c.elementComponents[member.Name]; exists {
			return withSchemaCompileLocation(raw.sourceNode(), diagnostic)
		}
		return diagnostic
	}
	var diagnostic *xsderrors.Error
	if errors.As(err, &diagnostic) && diagnostic != nil {
		return err
	}
	return xsderrors.InternalInvariant(err.Error())
}

func substitutionCycleElement(start ElementID, elements []ElementDecl) (ElementID, error) {
	current := start
	for range elements {
		if !ValidElementID(current, len(elements)) {
			return NoElement, xsderrors.InternalInvariant("substitution cycle references invalid element")
		}
		current = elements[current].SubstHead
	}
	if !ValidElementID(current, len(elements)) {
		return NoElement, xsderrors.InternalInvariant("substitution residual does not lead to a cycle")
	}
	return current, nil
}

func elementUsesSubstitutionType(n *schemaNode) bool {
	source := n.semantic.element()
	if source == nil {
		return false
	}
	if source.Type.Lexical.Present {
		return false
	}
	if n.firstXS(vocab.XSDElemSimpleType) != nil || n.firstXS(vocab.XSDElemComplexType) != nil {
		return false
	}
	return source.SubstitutionGroup.Lexical.Present
}

func (c *compiler) resolveTypeQName(q QName) (TypeID, error) {
	if id, ok := c.simpleDone[q]; ok {
		return SimpleRef(id), nil
	}
	if id, ok := c.complexDone[q]; ok {
		return ComplexRef(id), nil
	}
	if _, ok := c.simpleComponents[q]; ok {
		id, err := c.compileSimpleByQName(q)
		if err != nil {
			return TypeID{}, err
		}
		return SimpleRef(id), nil
	}
	if _, ok := c.complexComponents[q]; ok {
		id, err := c.compileComplexByQName(q)
		if err != nil {
			return TypeID{}, err
		}
		return ComplexRef(id), nil
	}
	err := SchemaComponentMissingError(SchemaComponentType, c.rt.formatName(q))
	return TypeID{}, err
}

func (c *compiler) typeQNameKnown(q QName) bool {
	return c.simpleTypeQNameKnown(q) || c.complexTypeQNameKnown(q)
}

func (c *compiler) typeQNameMayBeUnavailable(q QName) bool {
	return c.rt.namespaceURI(q.Namespace) != vocab.XSDNamespaceURI
}

func (c *compiler) simpleTypeQNameKnown(q QName) bool {
	if _, ok := c.simpleDone[q]; ok {
		return true
	}
	_, ok := c.simpleComponents[q]
	return ok
}

func (c *compiler) complexTypeQNameKnown(q QName) bool {
	if _, ok := c.complexDone[q]; ok {
		return true
	}
	_, ok := c.complexComponents[q]
	return ok
}
