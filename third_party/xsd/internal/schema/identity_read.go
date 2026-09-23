package schema

import "slices"

// IdentityConstraintIDs is an immutable ordered view of identity-constraint
// handles. Its backing slice is never exposed.
type IdentityConstraintIDs struct {
	values []IdentityConstraintID
}

func borrowedIdentityConstraintIDs(values []IdentityConstraintID) IdentityConstraintIDs {
	return IdentityConstraintIDs{values: values}
}

// Len returns the number of constraint handles.
func (r IdentityConstraintIDs) Len() int {
	return len(r.values)
}

// At returns the constraint handle at index.
func (r IdentityConstraintIDs) At(index int) (IdentityConstraintID, bool) {
	if index < 0 || index >= len(r.values) {
		return 0, false
	}
	return r.values[index], true
}

// IsSubsetOf reports whether every constraint handle in r occurs in base.
func (r IdentityConstraintIDs) IsSubsetOf(base IdentityConstraintIDs) bool {
	for _, id := range r.values {
		if !slices.Contains(base.values, id) {
			return false
		}
	}
	return true
}

// IdentityPathRead is an immutable selector-path view.
type IdentityPathRead struct {
	steps      []IdentityStep
	descendant bool
	self       bool
}

// IdentityPathReads is an immutable ordered selector-path view.
type IdentityPathReads struct {
	values []IdentityPathProgramRead
}

func borrowedIdentityPathReads(paths []IdentityPathProgramRead) IdentityPathReads {
	return IdentityPathReads{values: paths}
}

// Len returns the number of selector paths.
func (r IdentityPathReads) Len() int {
	return len(r.values)
}

// At returns selector path index.
func (r IdentityPathReads) At(index int) (IdentityPathRead, bool) {
	if index < 0 || index >= len(r.values) {
		return IdentityPathRead{}, false
	}
	return identityPathReadFromProgram(r.values[index]), true
}

func borrowedIdentityPathRead(path IdentityPath) IdentityPathRead {
	return IdentityPathRead{steps: path.Steps, descendant: path.Descendant, self: path.Self}
}

func identityPathReadFromProgram(path IdentityPathProgramRead) IdentityPathRead {
	return IdentityPathRead{steps: path.steps, descendant: path.descendant, self: path.self}
}

// StepCount returns the number of selector steps.
func (r IdentityPathRead) StepCount() int {
	return len(r.steps)
}

// Step returns selector step index.
func (r IdentityPathRead) Step(index int) (IdentityStep, bool) {
	if index < 0 || index >= len(r.steps) {
		return IdentityStep{}, false
	}
	return r.steps[index], true
}

// Descendant reports whether the path uses descendant matching.
func (r IdentityPathRead) Descendant() bool {
	return r.descendant
}

// Self reports whether the path selects the current node.
func (r IdentityPathRead) Self() bool {
	return r.self
}

// IdentityFieldPathRead is an immutable compiled field-path view.
type IdentityFieldPathRead struct {
	steps            []IdentityStep
	attribute        QName
	attrNamespace    NamespaceID
	descendant       bool
	self             bool
	attr             bool
	attrWildcard     bool
	attrNamespaceSet bool
}

func borrowedIdentityFieldPathRead(path IdentityFieldPath) IdentityFieldPathRead {
	return IdentityFieldPathRead{
		steps:            path.Steps,
		attribute:        path.Attribute,
		attrNamespace:    path.AttrNamespace,
		descendant:       path.Descendant,
		self:             path.Self,
		attr:             path.Attr,
		attrWildcard:     path.AttrWildcard,
		attrNamespaceSet: path.AttrNamespaceSet,
	}
}

func identityFieldPathReadFromProgram(path IdentityPathProgramRead) IdentityFieldPathRead {
	return IdentityFieldPathRead{
		steps:            path.steps,
		attribute:        path.attribute,
		attrNamespace:    path.attributeNamespace,
		descendant:       path.descendant,
		self:             path.self,
		attr:             path.attributePath,
		attrWildcard:     path.attributeWildcard,
		attrNamespaceSet: path.attributeNamespaceSet,
	}
}

// StepCount returns the number of element steps.
func (r IdentityFieldPathRead) StepCount() int {
	return len(r.steps)
}

// Step returns element step index.
func (r IdentityFieldPathRead) Step(index int) (IdentityStep, bool) {
	if index < 0 || index >= len(r.steps) {
		return IdentityStep{}, false
	}
	return r.steps[index], true
}

// Attribute returns the exact attribute name for the path.
func (r IdentityFieldPathRead) Attribute() QName {
	return r.attribute
}

// AttributeNamespace returns the namespace constraint for an attribute wildcard.
func (r IdentityFieldPathRead) AttributeNamespace() NamespaceID {
	return r.attrNamespace
}

// Descendant reports whether the path uses descendant matching.
func (r IdentityFieldPathRead) Descendant() bool {
	return r.descendant
}

// Self reports whether the path selects the current node.
func (r IdentityFieldPathRead) Self() bool {
	return r.self
}

// IsAttribute reports whether the path selects an attribute.
func (r IdentityFieldPathRead) IsAttribute() bool {
	return r.attr
}

// AttributeWildcard reports whether the path selects attributes by wildcard.
func (r IdentityFieldPathRead) AttributeWildcard() bool {
	return r.attrWildcard
}

// AttributeNamespaceSet reports whether an attribute wildcard constrains namespace.
func (r IdentityFieldPathRead) AttributeNamespaceSet() bool {
	return r.attrNamespaceSet
}

// IdentityNamespaceResolver resolves schema namespace IDs to their URI.
// Runtime identity paths use URI comparison when an instance name is not
// interned in the schema name table.
type IdentityNamespaceResolver interface {
	Namespace(id NamespaceID) string
}

// IdentityPathProgramRead is the immutable execution projection of one
// selector or field path. It is built once with the published schema and
// shared by every validation session using that schema.
//
//nolint:recvcheck // Matches avoids copying this immutable program on the hot validation path; accessors remain value reads.
type IdentityPathProgramRead struct {
	steps                 []IdentityStep
	descendant            bool
	self                  bool
	attribute             QName
	attributeNamespace    NamespaceID
	attributeNamespaceSet bool
	attributePath         bool
	attributeWildcard     bool
}

func identitySelectorPathProgramRead(path IdentityPathRead) IdentityPathProgramRead {
	return IdentityPathProgramRead{
		steps:      slices.Clone(path.steps),
		descendant: path.descendant,
		self:       path.self,
	}
}

// NewIdentitySelectorPathProgramRead constructs a selector-path program from
// an immutable path view.
func NewIdentitySelectorPathProgramRead(path IdentityPathRead) IdentityPathProgramRead {
	return identitySelectorPathProgramRead(path)
}

func identityFieldPathProgramRead(path IdentityFieldPathRead) IdentityPathProgramRead {
	return IdentityPathProgramRead{
		steps:                 slices.Clone(path.steps),
		attribute:             path.attribute,
		attributeNamespace:    path.attrNamespace,
		attributeNamespaceSet: path.attrNamespaceSet,
		descendant:            path.descendant,
		self:                  path.self,
		attributePath:         path.attr,
		attributeWildcard:     path.attrWildcard,
	}
}

// NewIdentityFieldPathProgramRead constructs a field-path program from an
// immutable path view.
func NewIdentityFieldPathProgramRead(path IdentityFieldPathRead) IdentityPathProgramRead {
	return identityFieldPathProgramRead(path)
}

// StepCount returns the number of element steps in the path.
func (p IdentityPathProgramRead) StepCount() int {
	return len(p.steps)
}

// Step returns one path step.
func (p IdentityPathProgramRead) Step(index int) (IdentityStep, bool) {
	if index < 0 || index >= len(p.steps) {
		return IdentityStep{}, false
	}
	return p.steps[index], true
}

// FinalStep returns the terminal element step when the path has one.
func (p IdentityPathProgramRead) FinalStep() (IdentityStep, bool) {
	count := p.StepCount()
	if count == 0 {
		return IdentityStep{}, false
	}
	return p.Step(count - 1)
}

// Descendant reports whether the path uses descendant matching.
func (p IdentityPathProgramRead) Descendant() bool { return p.descendant }

// Self reports whether the path selects the current node.
func (p IdentityPathProgramRead) Self() bool { return p.self }

// IsAttribute reports whether the path selects an attribute.
func (p IdentityPathProgramRead) IsAttribute() bool { return p.attributePath }

// Attribute returns the exact attribute name for the path.
func (p IdentityPathProgramRead) Attribute() QName { return p.attribute }

// AttributeNamespace returns the namespace constraint for an attribute wildcard.
func (p IdentityPathProgramRead) AttributeNamespace() NamespaceID {
	return p.attributeNamespace
}

// AttributeNamespaceSet reports whether an attribute wildcard constrains namespace.
func (p IdentityPathProgramRead) AttributeNamespaceSet() bool {
	return p.attributeNamespaceSet
}

// AttributeWildcard reports whether the path selects attributes by wildcard.
func (p IdentityPathProgramRead) AttributeWildcard() bool { return p.attributeWildcard }

// Matches reports whether the path matches namePath from baseDepth to
// currentDepth.
//
//nolint:gocognit // Keep depth validation and step dispatch on one hot path.
func (p *IdentityPathProgramRead) Matches(
	names IdentityNamespaceResolver,
	namePath []RuntimeName,
	baseDepth, currentDepth int,
) bool {
	if p.self {
		return currentDepth == baseDepth
	}
	if !p.descendant && len(p.steps) == 0 {
		return baseDepth >= 0 && currentDepth == baseDepth && currentDepth <= len(namePath)
	}
	var rel []RuntimeName
	var ok bool
	if p.descendant {
		rel, ok = identityDescendantPath(namePath, baseDepth, currentDepth, p.StepCount())
	} else {
		rel, ok = identityDirectPath(namePath, baseDepth, currentDepth, p.StepCount())
	}
	if !ok {
		return false
	}
	for index := range p.StepCount() {
		step, ok := p.Step(index)
		if !ok || !identityProgramStepMatches(names, rel[index], step) {
			return false
		}
	}
	return true
}

func identityDirectPath(namePath []RuntimeName, baseDepth, currentDepth, stepCount int) ([]RuntimeName, bool) {
	if currentDepth < baseDepth || baseDepth < 0 || currentDepth > len(namePath) {
		return nil, false
	}
	rel := namePath[baseDepth:currentDepth]
	return rel, len(rel) == stepCount
}

func identityDescendantPath(namePath []RuntimeName, baseDepth, currentDepth, stepCount int) ([]RuntimeName, bool) {
	if currentDepth < baseDepth || baseDepth < 0 || currentDepth > len(namePath) {
		return nil, false
	}
	rel := namePath[baseDepth:currentDepth]
	if len(rel) < stepCount {
		return nil, false
	}
	return rel[len(rel)-stepCount:], true
}

// AttributeMatches reports whether name matches the path's attribute branch.
func (p IdentityPathProgramRead) AttributeMatches(names IdentityNamespaceResolver, name RuntimeName) bool {
	if !p.attributePath {
		return false
	}
	if !p.attributeWildcard {
		return name.Known && p.attribute == name.Name
	}
	if !p.attributeNamespaceSet {
		return true
	}
	if name.Known {
		return p.attributeNamespace == name.Name.Namespace
	}
	return names.Namespace(p.attributeNamespace) == name.NS
}

func identityProgramStepMatches(names IdentityNamespaceResolver, name RuntimeName, step IdentityStep) bool {
	if !step.Wildcard {
		return name.Known && name.Name == step.Name
	}
	if !step.NamespaceSet {
		return true
	}
	if name.Known {
		return name.Name.Namespace == step.Namespace
	}
	return name.NS == names.Namespace(step.Namespace)
}

// IdentityPathProgramReads is an immutable ordered view of path programs.
type IdentityPathProgramReads struct {
	values []IdentityPathProgramRead
}

// Len returns the number of path programs.
func (r IdentityPathProgramReads) Len() int { return len(r.values) }

// At returns a path program by index.
func (r IdentityPathProgramReads) At(index int) (IdentityPathProgramRead, bool) {
	if index < 0 || index >= len(r.values) {
		return IdentityPathProgramRead{}, false
	}
	return r.values[index], true
}

// IdentityCompiledFieldProgramRead is an immutable compiled field-path group.
type IdentityCompiledFieldProgramRead struct {
	paths []IdentityPathProgramRead
	field int
}

// IdentityCompiledFieldProgramReads is an immutable ordered field-program view.
type IdentityCompiledFieldProgramReads struct {
	values []IdentityCompiledFieldProgramRead
}

// Len returns the number of compiled field programs.
func (r IdentityCompiledFieldProgramReads) Len() int { return len(r.values) }

// At returns a compiled field program by index.
func (r IdentityCompiledFieldProgramReads) At(index int) (IdentityCompiledFieldProgramRead, bool) {
	if index < 0 || index >= len(r.values) {
		return IdentityCompiledFieldProgramRead{}, false
	}
	return r.values[index], true
}

// Field returns the declared field index.
func (r IdentityCompiledFieldProgramRead) Field() int { return r.field }

// PathCount returns the number of path alternatives.
func (r IdentityCompiledFieldProgramRead) PathCount() int { return len(r.paths) }

// Path returns one path alternative.
func (r IdentityCompiledFieldProgramRead) Path(index int) (IdentityPathProgramRead, bool) {
	if index < 0 || index >= len(r.paths) {
		return IdentityPathProgramRead{}, false
	}
	return r.paths[index], true
}

// IdentityConstraintProgramRead is the immutable dispatch program for one
// identity constraint.
type IdentityConstraintProgramRead struct {
	selectors          []IdentityPathProgramRead
	elementFields      []IdentityCompiledFieldProgramRead
	attributeFields    map[QName][]IdentityCompiledFieldProgramRead
	attributeWildcards []IdentityCompiledFieldProgramRead
	refer              IdentityConstraintID
	kind               IdentityKind
	fieldCount         int
}

// Selectors returns selector path programs.
func (p IdentityConstraintProgramRead) Selectors() IdentityPathProgramReads {
	return IdentityPathProgramReads{values: p.selectors}
}

// ElementFields returns element field-path programs.
func (p IdentityConstraintProgramRead) ElementFields() IdentityCompiledFieldProgramReads {
	return IdentityCompiledFieldProgramReads{values: p.elementFields}
}

// AttributeFields returns exact-name attribute field-path programs.
func (p IdentityConstraintProgramRead) AttributeFields(name QName) IdentityCompiledFieldProgramReads {
	return IdentityCompiledFieldProgramReads{values: p.attributeFields[name]}
}

// AttributeWildcardFields returns wildcard attribute field-path programs.
func (p IdentityConstraintProgramRead) AttributeWildcardFields() IdentityCompiledFieldProgramReads {
	return IdentityCompiledFieldProgramReads{values: p.attributeWildcards}
}

// Refer returns the referenced key for keyref constraints.
func (p IdentityConstraintProgramRead) Refer() IdentityConstraintID { return p.refer }

// Kind returns the identity constraint kind.
func (p IdentityConstraintProgramRead) Kind() IdentityKind { return p.kind }

// FieldCount returns the declared identity field count.
func (p IdentityConstraintProgramRead) FieldCount() int { return p.fieldCount }

// IdentitySelectorDispatchRead is one immutable selector dispatch candidate.
type IdentitySelectorDispatchRead struct {
	path       IdentityPathProgramRead
	constraint IdentityConstraintID
}

// Constraint returns the owning identity constraint.
func (r IdentitySelectorDispatchRead) Constraint() IdentityConstraintID { return r.constraint }

// Path returns the selector path program.
func (r IdentitySelectorDispatchRead) Path() IdentityPathProgramRead { return r.path }

// IdentitySelectorDispatchReads is an immutable selector candidate view.
type IdentitySelectorDispatchReads struct {
	values []IdentitySelectorDispatchRead
}

// Len returns the number of selector candidates.
func (r IdentitySelectorDispatchReads) Len() int { return len(r.values) }

// At returns a selector candidate by index.
func (r IdentitySelectorDispatchReads) At(index int) (IdentitySelectorDispatchRead, bool) {
	if index < 0 || index >= len(r.values) {
		return IdentitySelectorDispatchRead{}, false
	}
	return r.values[index], true
}

// IdentityFieldDispatchRead is one immutable element-field dispatch candidate.
type IdentityFieldDispatchRead struct {
	path       IdentityPathProgramRead
	field      int
	constraint IdentityConstraintID
}

// Constraint returns the owning identity constraint.
func (r IdentityFieldDispatchRead) Constraint() IdentityConstraintID { return r.constraint }

// Path returns the field path program.
func (r IdentityFieldDispatchRead) Path() IdentityPathProgramRead { return r.path }

// Field returns the declared field index.
func (r IdentityFieldDispatchRead) Field() int { return r.field }

// IdentityFieldDispatchReads is an immutable field candidate view.
type IdentityFieldDispatchReads struct {
	values []IdentityFieldDispatchRead
}

// Len returns the number of field candidates.
func (r IdentityFieldDispatchReads) Len() int { return len(r.values) }

// At returns a field candidate by index.
func (r IdentityFieldDispatchReads) At(index int) (IdentityFieldDispatchRead, bool) {
	if index < 0 || index >= len(r.values) {
		return IdentityFieldDispatchRead{}, false
	}
	return r.values[index], true
}

type identitySelectorDispatchIndex struct {
	exact     map[QName][]IdentitySelectorDispatchRead
	namespace map[string][]IdentitySelectorDispatchRead
	any       []IdentitySelectorDispatchRead
}

type identityFieldDispatchIndex struct {
	exact     map[QName][]IdentityFieldDispatchRead
	namespace map[string][]IdentityFieldDispatchRead
	any       []IdentityFieldDispatchRead
}

// IdentityDispatchRead is the immutable identity selector and field index
// derived while publishing a schema. Validation sessions retain only their
// active scope membership and scratch slices.
type IdentityDispatchRead struct {
	programs      []IdentityConstraintProgramRead
	selectors     identitySelectorDispatchIndex
	selfSelectors []IdentitySelectorDispatchRead
	elements      identityFieldDispatchIndex
	elementSelf   []IdentityFieldDispatchRead
}

// ConstraintCount returns the number of published identity constraints.
func (d IdentityDispatchRead) ConstraintCount() int { return len(d.programs) }

// Program returns the immutable dispatch program for an identity constraint.
func (d IdentityDispatchRead) Program(id IdentityConstraintID) (IdentityConstraintProgramRead, bool) {
	if !ValidIdentityConstraintID(id, len(d.programs)) {
		return IdentityConstraintProgramRead{}, false
	}
	return d.programs[id], true
}

// AttributeFields returns exact-name attribute field programs for a constraint.
func (d IdentityDispatchRead) AttributeFields(id IdentityConstraintID, name QName) (IdentityCompiledFieldProgramReads, bool) {
	program, ok := d.Program(id)
	if !ok {
		return IdentityCompiledFieldProgramReads{}, false
	}
	return program.AttributeFields(name), true
}

func (d IdentityDispatchRead) identityConstraint(id IdentityConstraintID) (IdentityConstraintRead, bool) {
	program, ok := d.Program(id)
	if !ok {
		return IdentityConstraintRead{}, false
	}
	return IdentityConstraintRead{program: program}, true
}

// SelectorExact returns selector candidates keyed by an interned name.
func (d IdentityDispatchRead) SelectorExact(name QName) IdentitySelectorDispatchReads {
	return IdentitySelectorDispatchReads{values: d.selectors.exact[name]}
}

// SelectorNamespace returns selector candidates keyed by namespace URI.
func (d IdentityDispatchRead) SelectorNamespace(namespace string) IdentitySelectorDispatchReads {
	return IdentitySelectorDispatchReads{values: d.selectors.namespace[namespace]}
}

// SelectorAny returns selectors without a terminal name test.
func (d IdentityDispatchRead) SelectorAny() IdentitySelectorDispatchReads {
	return IdentitySelectorDispatchReads{values: d.selectors.any}
}

// SelfSelectors returns selectors matching the current node.
func (d IdentityDispatchRead) SelfSelectors() IdentitySelectorDispatchReads {
	return IdentitySelectorDispatchReads{values: d.selfSelectors}
}

// ElementExact returns element field candidates keyed by an interned name.
func (d IdentityDispatchRead) ElementExact(name QName) IdentityFieldDispatchReads {
	return IdentityFieldDispatchReads{values: d.elements.exact[name]}
}

// ElementNamespace returns element field candidates keyed by namespace URI.
func (d IdentityDispatchRead) ElementNamespace(namespace string) IdentityFieldDispatchReads {
	return IdentityFieldDispatchReads{values: d.elements.namespace[namespace]}
}

// ElementAny returns element field candidates without a terminal name test.
func (d IdentityDispatchRead) ElementAny() IdentityFieldDispatchReads {
	return IdentityFieldDispatchReads{values: d.elements.any}
}

// ElementSelf returns element field candidates matching the current node.
func (d IdentityDispatchRead) ElementSelf() IdentityFieldDispatchReads {
	return IdentityFieldDispatchReads{values: d.elementSelf}
}

func newIdentityDispatchRead(identities []IdentityConstraint, names nameReadView) IdentityDispatchRead {
	dispatch := IdentityDispatchRead{programs: make([]IdentityConstraintProgramRead, len(identities))}
	constraint := IdentityConstraintID(0)
	for _, identity := range identities {
		program := newIdentityConstraintProgramRead(identity)
		dispatch.programs[constraint] = program
		addIdentityConstraintDispatch(&dispatch, constraint, program, names)
		constraint++
	}
	return dispatch
}

func addIdentityConstraintDispatch(d *IdentityDispatchRead, constraint IdentityConstraintID, program IdentityConstraintProgramRead, names nameReadView) {
	addIdentitySelectorDispatchBranches(d, constraint, program.selectors, names)
	addIdentityFieldDispatchBranches(d, constraint, program.elementFields, names)
}

func addIdentitySelectorDispatchBranches(d *IdentityDispatchRead, constraint IdentityConstraintID, paths []IdentityPathProgramRead, names nameReadView) {
	for _, path := range paths {
		branch := IdentitySelectorDispatchRead{path: path, constraint: constraint}
		if path.Self() {
			d.selfSelectors = append(d.selfSelectors, branch)
		} else if step, ok := path.FinalStep(); ok {
			addIdentitySelectorDispatch(&d.selectors, step, branch, names)
		} else {
			d.selectors.any = append(d.selectors.any, branch)
		}
	}
}

func addIdentityFieldDispatchBranches(d *IdentityDispatchRead, constraint IdentityConstraintID, fields []IdentityCompiledFieldProgramRead, names nameReadView) {
	for _, field := range fields {
		for _, path := range field.paths {
			branch := IdentityFieldDispatchRead{path: path, field: field.field, constraint: constraint}
			if step, ok := path.FinalStep(); ok {
				addIdentityFieldDispatch(&d.elements, step, branch, names)
			} else {
				d.elementSelf = append(d.elementSelf, branch)
			}
		}
	}
}

func newIdentityCompiledFieldProgramReads(fields []CompiledIdentityField) []IdentityCompiledFieldProgramRead {
	programs := make([]IdentityCompiledFieldProgramRead, 0, len(fields))
	for _, field := range fields {
		program := IdentityCompiledFieldProgramRead{field: field.Field}
		program.paths = make([]IdentityPathProgramRead, 0, len(field.Paths))
		for _, path := range field.Paths {
			program.paths = append(program.paths, identityFieldPathProgramRead(borrowedIdentityFieldPathRead(path)))
		}
		programs = append(programs, program)
	}
	return programs
}

func newIdentityCompiledFieldProgramMapReads(fields map[QName][]CompiledIdentityField) map[QName][]IdentityCompiledFieldProgramRead {
	if fields == nil {
		return nil
	}
	programs := make(map[QName][]IdentityCompiledFieldProgramRead, len(fields))
	for name, field := range fields {
		programs[name] = newIdentityCompiledFieldProgramReads(field)
	}
	return programs
}

func addIdentitySelectorDispatch(index *identitySelectorDispatchIndex, step IdentityStep, branch IdentitySelectorDispatchRead, names nameReadView) {
	if !step.Wildcard {
		if index.exact == nil {
			index.exact = make(map[QName][]IdentitySelectorDispatchRead)
		}
		index.exact[step.Name] = append(index.exact[step.Name], branch)
		return
	}
	if !step.NamespaceSet {
		index.any = append(index.any, branch)
		return
	}
	if index.namespace == nil {
		index.namespace = make(map[string][]IdentitySelectorDispatchRead)
	}
	key := names.Namespace(step.Namespace)
	index.namespace[key] = append(index.namespace[key], branch)
}

func addIdentityFieldDispatch(index *identityFieldDispatchIndex, step IdentityStep, branch IdentityFieldDispatchRead, names nameReadView) {
	if !step.Wildcard {
		if index.exact == nil {
			index.exact = make(map[QName][]IdentityFieldDispatchRead)
		}
		index.exact[step.Name] = append(index.exact[step.Name], branch)
		return
	}
	if !step.NamespaceSet {
		index.any = append(index.any, branch)
		return
	}
	if index.namespace == nil {
		index.namespace = make(map[string][]IdentityFieldDispatchRead)
	}
	key := names.Namespace(step.Namespace)
	index.namespace[key] = append(index.namespace[key], branch)
}

// CompiledIdentityFieldRead is an immutable compiled field lookup view.
type CompiledIdentityFieldRead struct {
	paths []IdentityPathProgramRead
	field int
}

// CompiledIdentityFieldReads is an immutable ordered compiled-field view.
type CompiledIdentityFieldReads struct {
	values []IdentityCompiledFieldProgramRead
}

func borrowedCompiledIdentityFieldReads(fields []IdentityCompiledFieldProgramRead) CompiledIdentityFieldReads {
	return CompiledIdentityFieldReads{values: fields}
}

// Len returns the number of compiled fields.
func (r CompiledIdentityFieldReads) Len() int {
	return len(r.values)
}

// At returns compiled field index.
func (r CompiledIdentityFieldReads) At(index int) (CompiledIdentityFieldRead, bool) {
	if index < 0 || index >= len(r.values) {
		return CompiledIdentityFieldRead{}, false
	}
	return compiledIdentityFieldReadFromProgram(r.values[index]), true
}

func compiledIdentityFieldReadFromProgram(field IdentityCompiledFieldProgramRead) CompiledIdentityFieldRead {
	return CompiledIdentityFieldRead(field)
}

// Field returns the declared field index.
func (r CompiledIdentityFieldRead) Field() int {
	return r.field
}

// PathCount returns the number of compiled path alternatives.
func (r CompiledIdentityFieldRead) PathCount() int {
	return len(r.paths)
}

// Path returns compiled path index.
func (r CompiledIdentityFieldRead) Path(index int) (IdentityFieldPathRead, bool) {
	if index < 0 || index >= len(r.paths) {
		return IdentityFieldPathRead{}, false
	}
	return identityFieldPathReadFromProgram(r.paths[index]), true
}

// IdentityConstraintRead exposes validation-facing identity-constraint
// behavior without exposing raw compiled identity metadata.
type IdentityConstraintRead struct {
	program IdentityConstraintProgramRead
}

func newIdentityConstraintReads(identities []IdentityConstraint) []IdentityConstraintRead {
	out := make([]IdentityConstraintRead, len(identities))
	for i := range identities {
		out[i] = newIdentityConstraintRead(identities[i])
	}
	return out
}

func newIdentityConstraintRead(identity IdentityConstraint) IdentityConstraintRead {
	return IdentityConstraintRead{program: newIdentityConstraintProgramRead(identity)}
}

func newIdentityConstraintProgramRead(identity IdentityConstraint) IdentityConstraintProgramRead {
	program := IdentityConstraintProgramRead{
		selectors:          newIdentitySelectorPathProgramReads(identity.Selector),
		elementFields:      newIdentityCompiledFieldProgramReads(identity.ElementFields),
		attributeFields:    newIdentityCompiledFieldProgramMapReads(identity.AttributeFields),
		attributeWildcards: newIdentityCompiledFieldProgramReads(identity.AttributeWildcardFields),
		refer:              identity.Refer,
		kind:               identity.Kind,
		fieldCount:         len(identity.Fields),
	}
	return program
}

func newIdentitySelectorPathProgramReads(paths []IdentityPath) []IdentityPathProgramRead {
	programs := make([]IdentityPathProgramRead, len(paths))
	for index, path := range paths {
		programs[index] = identitySelectorPathProgramRead(borrowedIdentityPathRead(path))
	}
	return programs
}

// ElementIdentityConstraintIDs returns an immutable view of the constraints
// attached to id.
func ElementIdentityConstraintIDs(reads [][]IdentityConstraintID, id ElementID) (IdentityConstraintIDs, bool) {
	if !ValidElementID(id, len(reads)) {
		return IdentityConstraintIDs{}, false
	}
	return borrowedIdentityConstraintIDs(reads[id]), true
}

func identityConstraintReadByIDPtr(reads []IdentityConstraintRead, id IdentityConstraintID) (*IdentityConstraintRead, bool) {
	if !ValidIdentityConstraintID(id, len(reads)) {
		return nil, false
	}
	return &reads[id], true
}

// identityConstraintReadByID returns the aggregate validation read for id.
func identityConstraintReadByID(reads []IdentityConstraintRead, id IdentityConstraintID) (IdentityConstraintRead, bool) {
	ic, ok := identityConstraintReadByIDPtr(reads, id)
	if !ok {
		return IdentityConstraintRead{}, false
	}
	return *ic, true
}

// FieldCount returns the declared identity field count.
func (r IdentityConstraintRead) FieldCount() int {
	return r.program.FieldCount()
}

// SelectorPaths returns the immutable selector paths.
func (r IdentityConstraintRead) SelectorPaths() IdentityPathReads {
	return borrowedIdentityPathReads(r.program.selectors)
}

// ElementFields returns the immutable element-field lookup.
func (r IdentityConstraintRead) ElementFields() CompiledIdentityFieldReads {
	return borrowedCompiledIdentityFieldReads(r.program.elementFields)
}

// AttributeFields returns the immutable exact-name attribute-field lookup.
func (r IdentityConstraintRead) AttributeFields(name QName) CompiledIdentityFieldReads {
	return borrowedCompiledIdentityFieldReads(r.program.attributeFields[name])
}

// AttributeWildcardFields returns the immutable wildcard attribute-field lookup.
func (r IdentityConstraintRead) AttributeWildcardFields() CompiledIdentityFieldReads {
	return borrowedCompiledIdentityFieldReads(r.program.attributeWildcards)
}

// Refer returns the referenced key for keyref constraints.
func (r IdentityConstraintRead) Refer() IdentityConstraintID {
	return r.program.Refer()
}

// Kind returns the identity constraint kind.
func (r IdentityConstraintRead) Kind() IdentityKind {
	return r.program.Kind()
}
