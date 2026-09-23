package schema

import (
	"errors"
	"slices"
)

// ModelKind identifies the runtime content-model shape.
type ModelKind uint8

const (
	// ModelEmpty is an empty content model.
	ModelEmpty ModelKind = iota
	// ModelAny is the builtin xs:anyType content model.
	ModelAny
	// ModelSequence is an xs:sequence content model.
	ModelSequence
	// ModelChoice is an xs:choice content model.
	ModelChoice
	// ModelAll is an xs:all content model.
	ModelAll
)

// ParticleKind identifies the active reference in a Particle.
type ParticleKind uint8

const (
	// ParticleElement references an element declaration.
	ParticleElement ParticleKind = iota
	// ParticleModel references a nested content model.
	ParticleModel
	// ParticleWildcard references a wildcard.
	ParticleWildcard
)

// Occurrence stores min/max occurrence constraints.
type Occurrence struct {
	Min       uint32
	Max       uint32
	Unbounded bool
}

// IsExactlyOne reports whether the occurrence range is exactly 1..1.
func (o Occurrence) IsExactlyOne() bool {
	return o.Min == 1 && o.Max == 1 && !o.Unbounded
}

// ContentModel is a runtime content-model tree node.
type ContentModel struct {
	Particles    []Particle
	ChoiceLimits []uint32
	Occurs       Occurrence
	Kind         ModelKind
	Mixed        bool
}

// ContentModelByID resolves and clones a content model from a content-model
// table.
func ContentModelByID(models []ContentModel, id ContentModelID) (ContentModel, bool) {
	if !ValidContentModelID(id, len(models)) {
		return ContentModel{}, false
	}
	return CloneContentModel(models[id]), true
}

// Particle is a tagged union: Kind selects which ID field is active.
type Particle struct {
	Kind     ParticleKind
	Occurs   Occurrence
	Element  ElementID
	Model    ContentModelID
	Wildcard WildcardID
}

// ContentModelRefLimits are table sizes used to validate cross-table particle
// references in a frozen runtime schema.
type ContentModelRefLimits struct {
	ElementCount      int
	ContentModelCount int
	WildcardCount     int
}

// ParticleRestrictionElement is the element-declaration projection needed to
// evaluate particle restriction over frozen runtime metadata.
type ParticleRestrictionElement struct {
	Identities IdentityConstraintIDs
	Fixed      ValueConstraintIdentity
	Type       TypeID
	Block      DerivationMask
	Scope      DeclarationScope
	Nillable   bool
}

// ParticleRestrictionRuntime supplies read-only runtime metadata needed to
// evaluate particle restriction.
type ParticleRestrictionRuntime interface {
	ParticleRuntime
	TypeDerivationRuntime
	ElementRestriction(id ElementID) (ParticleRestrictionElement, bool)
}

// ElementParticle returns an element particle with inactive fields pinned.
func ElementParticle(id ElementID, occurs Occurrence) Particle {
	return Particle{Kind: ParticleElement, Occurs: occurs, Element: id, Model: NoContentModel, Wildcard: NoWildcard}
}

// ModelParticle returns a model particle with inactive fields pinned.
func ModelParticle(id ContentModelID, occurs Occurrence) Particle {
	return Particle{Kind: ParticleModel, Occurs: occurs, Element: NoElement, Model: id, Wildcard: NoWildcard}
}

// WildcardParticle returns a wildcard particle with inactive fields pinned.
func WildcardParticle(id WildcardID, occurs Occurrence) Particle {
	return Particle{Kind: ParticleWildcard, Occurs: occurs, Element: NoElement, Model: NoContentModel, Wildcard: id}
}

// ValidateContentModelShape validates content-model metadata that does not
// require cross-table ID lookup.
func ValidateContentModelShape(model ContentModel) error {
	if err := validateContentModelKindShape(model); err != nil {
		return err
	}
	for _, particle := range model.Particles {
		if err := ValidateParticleShape(particle); err != nil {
			return err
		}
	}
	return nil
}

func validateContentModelKindShape(model ContentModel) error {
	switch model.Kind {
	case ModelEmpty:
		return validateEmptyContentModelShape(model)
	case ModelAny:
		return validateAnyContentModelShape(model)
	case ModelSequence, ModelChoice:
		return validateCompositorContentModelShape(model)
	case ModelAll:
		return validateAllContentModelShape(model)
	default:
		return errors.New("content model has invalid kind")
	}
}

func validateEmptyContentModelShape(model ContentModel) error {
	if len(model.Particles) != 0 || len(model.ChoiceLimits) != 0 || model.Occurs != (Occurrence{}) {
		return errors.New("empty content model stores inactive fields")
	}
	return nil
}

func validateAnyContentModelShape(model ContentModel) error {
	if len(model.Particles) != 0 || len(model.ChoiceLimits) != 0 || model.Occurs != (Occurrence{}) || !model.Mixed {
		return errors.New("any content model has invalid shape")
	}
	return nil
}

func validateCompositorContentModelShape(model ContentModel) error {
	if !validOccurrence(model.Occurs) {
		return errors.New("content model occurrence is invalid")
	}
	if model.Kind != ModelSequence && len(model.ChoiceLimits) != 0 {
		return errors.New("non-sequence content model stores choice limits")
	}
	return validateChoiceLimits(model)
}

func validateAllContentModelShape(model ContentModel) error {
	if !validOccurrence(model.Occurs) || model.Occurs.Unbounded || model.Occurs.Max > 1 || model.Occurs.Min > 1 {
		return errors.New("all content model occurrence is invalid")
	}
	if len(model.ChoiceLimits) != 0 {
		return errors.New("all content model stores choice limits")
	}
	return nil
}

// ValidateContentModelRuntime validates content-model shape and cross-table
// particle references.
func ValidateContentModelRuntime(model ContentModel, limits ContentModelRefLimits) error {
	if err := ValidateContentModelShape(model); err != nil {
		return err
	}
	for _, particle := range model.Particles {
		if err := validateParticleRuntimeReference(particle, limits); err != nil {
			return err
		}
	}
	return nil
}

func validateParticleRuntimeReference(particle Particle, limits ContentModelRefLimits) error {
	switch particle.Kind {
	case ParticleElement:
		if !ValidElementID(particle.Element, limits.ElementCount) {
			return errors.New("particle references invalid element")
		}
	case ParticleModel:
		if !ValidContentModelID(particle.Model, limits.ContentModelCount) {
			return errors.New("particle references invalid content model")
		}
	case ParticleWildcard:
		if !ValidWildcardID(particle.Wildcard, limits.WildcardCount) {
			return errors.New("particle references invalid wildcard")
		}
	default:
		return errors.New("particle has invalid kind")
	}
	return nil
}

type contentModelGraphState uint8

const (
	contentModelGraphUnchecked contentModelGraphState = iota
	contentModelGraphChecking
	contentModelGraphChecked
)

type contentModelGraphFrame struct {
	id   ContentModelID
	next int
}

func validateContentModelGraph(models []ContentModel) error {
	audit := contentModelGraphAudit{
		models: models,
		state:  make([]contentModelGraphState, len(models)),
		stack:  make([]contentModelGraphFrame, 0, min(len(models), 1_024)),
	}
	for i := range models {
		root := ContentModelID(i)
		if audit.state[root] == contentModelGraphChecked {
			continue
		}
		if err := audit.validateRoot(root); err != nil {
			return err
		}
	}
	return nil
}

type contentModelGraphAudit struct {
	models []ContentModel
	state  []contentModelGraphState
	stack  []contentModelGraphFrame
}

func (a *contentModelGraphAudit) validateRoot(root ContentModelID) error {
	a.state[root] = contentModelGraphChecking
	a.stack = appendDFSFrame(a.stack, contentModelGraphFrame{id: root}, len(a.models))
	for len(a.stack) != 0 {
		if err := a.advance(); err != nil {
			return err
		}
	}
	return nil
}

func (a *contentModelGraphAudit) advance() error {
	top := len(a.stack) - 1
	frame := &a.stack[top]
	child, ok := nextNestedContentModel(a.models[frame.id].Particles, &frame.next)
	if !ok {
		a.state[frame.id] = contentModelGraphChecked
		a.stack = a.stack[:top]
		return nil
	}
	if !ValidContentModelID(child, len(a.models)) {
		return errors.New("content model graph references invalid model")
	}
	switch a.state[child] {
	case contentModelGraphUnchecked:
		a.state[child] = contentModelGraphChecking
		a.stack = appendDFSFrame(a.stack, contentModelGraphFrame{id: child}, len(a.models))
	case contentModelGraphChecking:
		return errors.New("content model graph contains cycle")
	case contentModelGraphChecked:
	}
	return nil
}

func nextNestedContentModel(particles []Particle, next *int) (ContentModelID, bool) {
	for *next < len(particles) && particles[*next].Kind != ParticleModel {
		*next++
	}
	if *next == len(particles) {
		return NoContentModel, false
	}
	child := particles[*next].Model
	*next++
	return child, true
}

// ComplexContentExtendsBase reports whether derived preserves base as the
// leading content of a complex-type extension.
func ComplexContentExtendsBase(rt ContentModelRuntime, baseID, derivedID ContentModelID) bool {
	if baseID == derivedID || ModelHasNoParticles(rt, baseID) {
		return true
	}
	base, ok := rt.ContentModel(baseID)
	if !ok {
		return false
	}
	derived, ok := rt.ContentModel(derivedID)
	if !ok {
		return false
	}
	if derived.Kind != ModelSequence {
		return false
	}
	if !derived.Occurs.IsExactlyOne() {
		return false
	}
	if base.Kind == ModelSequence && base.Occurs.IsExactlyOne() {
		return len(derived.Particles) >= len(base.Particles) &&
			slices.Equal(derived.Particles[:len(base.Particles)], base.Particles)
	}
	return len(derived.Particles) != 0 &&
		derived.Particles[0] == ModelParticle(baseID, Occurrence{Min: 1, Max: 1})
}

func validateChoiceLimits(model ContentModel) error {
	if len(model.ChoiceLimits) == 0 {
		return nil
	}
	if model.Kind != ModelSequence {
		return errors.New("choice limits require sequence content model")
	}
	var prev uint32
	for i, slot := range model.ChoiceLimits {
		if err := validateChoiceLimit(model, i, slot, prev); err != nil {
			return err
		}
		prev = slot
	}
	return nil
}

func validateChoiceLimit(model ContentModel, index int, slot, previous uint32) error {
	if !ValidUint32Index(slot, len(model.Particles)) {
		return errors.New("choice limit references invalid particle")
	}
	if index != 0 && slot <= previous {
		return errors.New("choice limits are not sorted")
	}
	particle := model.Particles[slot]
	if particle.Kind != ParticleElement || particle.Occurs.Min > 1 || (!particle.Occurs.Unbounded && particle.Occurs.Max <= 1) {
		return errors.New("choice limit references invalid particle shape")
	}
	return nil
}

// RestrictionRepeatedChoiceParticles derives the derived sequence particle
// slots that must be limited to one match because they restrict a repeated
// base choice, charging all analysis to work.
func RestrictionRepeatedChoiceParticles(
	models []ContentModel,
	baseID, derivedID ContentModelID,
	rt ParticleRestrictionRuntime,
	work ContentModelWork,
	analysis *ContentModelAnalysis,
) ([]uint32, error) {
	if err := validateRepeatedChoiceInputs(models, baseID, derivedID, rt, work, analysis); err != nil {
		return nil, err
	}
	base := models[baseID]
	derived := models[derivedID]
	if base.Kind != ModelSequence || derived.Kind != ModelSequence {
		return nil, nil
	}
	validator := newContentRestrictionValidator(rt, work, analysis)
	if err := validateRepeatedChoiceGraphs(validator, baseID, derivedID); err != nil {
		return nil, err
	}
	selector := restrictionRepeatedChoiceSelector{
		models:    models,
		base:      base.Particles,
		validator: validator,
	}
	for derivedIndex, derivedParticle := range derived.Particles {
		if err := selector.match(derivedIndex, derivedParticle); err != nil {
			return nil, err
		}
	}
	return selector.limits, validator.finish(nil)
}

func validateRepeatedChoiceInputs(
	models []ContentModel,
	baseID, derivedID ContentModelID,
	rt ParticleRestrictionRuntime,
	work ContentModelWork,
	analysis *ContentModelAnalysis,
) error {
	if rt == nil {
		return errors.New("choice-limit derivation requires runtime")
	}
	if err := requireContentModelWork(work); err != nil {
		return err
	}
	if analysis == nil {
		return errors.New("choice-limit derivation requires content model analysis")
	}
	if !ValidContentModelID(baseID, len(models)) || !ValidContentModelID(derivedID, len(models)) {
		return errors.New("choice-limit derivation references invalid content model")
	}
	return nil
}

func validateRepeatedChoiceGraphs(validator contentRestrictionValidator, base, derived ContentModelID) error {
	if err := validator.validateContentModelGraph(base); err != nil {
		return err
	}
	return validator.validateContentModelGraph(derived)
}

type restrictionRepeatedChoiceSelector struct {
	models    []ContentModel
	base      []Particle
	validator contentRestrictionValidator
	limits    []uint32
	baseIndex int
}

func (s *restrictionRepeatedChoiceSelector) match(derivedIndex int, derived Particle) error {
	base, matched, err := s.nextRestrictedBase(derived)
	if err != nil || !matched {
		return err
	}
	if !restrictionRepeatedChoiceParticle(s.models, base, derived) {
		return nil
	}
	index, ok := NewUint32Index(derivedIndex)
	if !ok {
		return errors.New("choice-limit particle index exceeds uint32")
	}
	s.limits = append(s.limits, index)
	return nil
}

func (s *restrictionRepeatedChoiceSelector) nextRestrictedBase(derived Particle) (Particle, bool, error) {
	for s.baseIndex < len(s.base) {
		base := s.base[s.baseIndex]
		s.baseIndex++
		err := s.validator.validateParticleRestriction(base, derived)
		if err == nil {
			return base, true, nil
		}
		if !IsContentRestrictionMismatch(err) {
			return Particle{}, false, err
		}
	}
	return Particle{}, false, nil
}

func restrictionRepeatedChoiceParticle(models []ContentModel, baseParticle, derivedParticle Particle) bool {
	if baseParticle.Kind != ParticleModel || baseParticle.Occurs.IsExactlyOne() {
		return false
	}
	if !ValidContentModelID(baseParticle.Model, len(models)) {
		return false
	}
	model := models[baseParticle.Model]
	if model.Kind != ModelChoice || derivedParticle.Kind != ParticleElement {
		return false
	}
	return derivedParticle.Occurs.Min <= 1 && derivedParticle.Occurs.Unbounded
}

// RestrictionChoiceLimitUpdate is a content-model copy that must be assigned to
// one restricting complex type so repeated-choice limits stay owner-private.
type RestrictionChoiceLimitUpdate struct {
	Model       ContentModel
	ComplexType ComplexTypeID
}

// RestrictionChoiceLimitUpdates derives owner-private content-model copies for
// restricting complex types whose particles need repeated-choice limits,
// charging all analysis to work.
func RestrictionChoiceLimitUpdates(
	rt ParticleRestrictionRuntime,
	complexTypes []ComplexType,
	models []ContentModel,
	anyType ComplexTypeID,
	work ContentModelWork,
	analysis *ContentModelAnalysis,
) ([]RestrictionChoiceLimitUpdate, error) {
	if err := validateChoiceLimitUpdateInputs(rt, work); err != nil {
		return nil, err
	}
	builder := restrictionChoiceLimitBuilder{
		rt:           rt,
		complexTypes: complexTypes,
		models:       models,
		anyType:      anyType,
		work:         work,
		analysis:     analysis,
	}
	var updates []RestrictionChoiceLimitUpdate
	for i, ct := range complexTypes {
		id, err := spendChoiceLimitComplexType(work, i)
		if err != nil {
			return nil, err
		}
		update, ok, err := builder.update(id, ct)
		if err != nil {
			return nil, err
		}
		if ok {
			updates = append(updates, update)
		}
	}
	return updates, nil
}

func validateChoiceLimitUpdateInputs(rt ParticleRestrictionRuntime, work ContentModelWork) error {
	if rt == nil {
		return errors.New("choice-limit derivation requires runtime")
	}
	return requireContentModelWork(work)
}

func spendChoiceLimitComplexType(work ContentModelWork, index int) (ComplexTypeID, error) {
	if err := spendContentModelWork(work); err != nil {
		return NoComplexType, err
	}
	raw, ok := newRuntimeID(index)
	if !ok {
		return NoComplexType, errors.New("complex type index limit exceeded")
	}
	return ComplexTypeID(raw), nil
}

type restrictionChoiceLimitBuilder struct {
	rt           ParticleRestrictionRuntime
	work         ContentModelWork
	analysis     *ContentModelAnalysis
	complexTypes []ComplexType
	models       []ContentModel
	anyType      ComplexTypeID
}

func (b *restrictionChoiceLimitBuilder) update(
	index ComplexTypeID,
	ct ComplexType,
) (RestrictionChoiceLimitUpdate, bool, error) {
	content, err := restrictionChoiceLimitContentIDs(b.complexTypes, b.models, b.anyType, ct)
	if err != nil || !content.eligible {
		return RestrictionChoiceLimitUpdate{}, false, err
	}
	repeated, err := RestrictionRepeatedChoiceParticles(b.models, content.base, content.derived, b.rt, b.work, b.analysis)
	if err != nil {
		return RestrictionChoiceLimitUpdate{}, false, err
	}
	if len(repeated) == 0 {
		return RestrictionChoiceLimitUpdate{}, false, nil
	}
	model := CloneContentModel(b.models[content.derived])
	if len(model.ChoiceLimits) != 0 && !slices.Equal(model.ChoiceLimits, repeated) {
		return RestrictionChoiceLimitUpdate{}, false, errors.New("choice-limit restriction source model already has different choice limits")
	}
	model.ChoiceLimits = slices.Clone(repeated)
	return RestrictionChoiceLimitUpdate{Model: model, ComplexType: index}, true, nil
}

type restrictionChoiceLimitContent struct {
	base     ContentModelID
	derived  ContentModelID
	eligible bool
}

func restrictionChoiceLimitContentIDs(
	complexTypes []ComplexType,
	models []ContentModel,
	anyType ComplexTypeID,
	ct ComplexType,
) (restrictionChoiceLimitContent, error) {
	if ct.Derivation != DerivationKindRestriction {
		return restrictionChoiceLimitContent{}, nil
	}
	baseID, ok := ct.Base.Complex()
	if !ok || baseID == anyType {
		return restrictionChoiceLimitContent{}, nil
	}
	if !ValidComplexTypeID(baseID, len(complexTypes)) {
		return restrictionChoiceLimitContent{}, errors.New("choice-limit restriction references invalid base complex type")
	}
	if !ValidContentModelID(ct.Content, len(models)) {
		return restrictionChoiceLimitContent{}, errors.New("choice-limit restriction references invalid derived content model")
	}
	baseContent := complexTypes[baseID].Content
	if !ValidContentModelID(baseContent, len(models)) {
		return restrictionChoiceLimitContent{}, errors.New("choice-limit restriction references invalid base content model")
	}
	return restrictionChoiceLimitContent{base: baseContent, derived: ct.Content, eligible: true}, nil
}

// ValidateChoiceLimitDerivations validates that every ContentModel.ChoiceLimits
// entry is exactly justified by restricting complex-type derivations, and that
// limited content models are not shared outside those owners. All analysis is
// charged to work.
func ValidateChoiceLimitDerivations(
	rt ParticleRestrictionRuntime,
	complexTypes []ComplexType,
	models []ContentModel,
	anyType ComplexTypeID,
	work ContentModelWork,
	analysis *ContentModelAnalysis,
) error {
	if err := requireChoiceLimitAnalysis(rt, work, "choice-limit validation requires runtime"); err != nil {
		return err
	}
	audit := choiceLimitDerivationAudit{
		rt:           rt,
		complexTypes: complexTypes,
		models:       models,
		anyType:      anyType,
		work:         work,
		analysis:     analysis,
		expected:     make(map[ContentModelID][]uint32),
		owners:       make(map[ContentModelID][]ComplexTypeID),
	}
	if err := audit.collect(); err != nil {
		return err
	}
	return audit.validateModels()
}

type choiceLimitDerivationAudit struct {
	rt           ParticleRestrictionRuntime
	work         ContentModelWork
	analysis     *ContentModelAnalysis
	expected     map[ContentModelID][]uint32
	owners       map[ContentModelID][]ComplexTypeID
	complexTypes []ComplexType
	models       []ContentModel
	anyType      ComplexTypeID
}

func (a *choiceLimitDerivationAudit) collect() error {
	for i, complexType := range a.complexTypes {
		if err := a.collectComplexType(i, complexType); err != nil {
			return err
		}
	}
	return nil
}

func (a *choiceLimitDerivationAudit) collectComplexType(index int, complexType ComplexType) error {
	if err := spendContentModelWork(a.work); err != nil {
		return err
	}
	raw, ok := newRuntimeID(index)
	if !ok {
		return errors.New("complex type index limit exceeded")
	}
	if !ValidContentModelID(complexType.Content, len(a.models)) {
		return nil
	}
	a.owners[complexType.Content] = append(a.owners[complexType.Content], ComplexTypeID(raw))
	repeated, expected, err := a.expectedForRestriction(complexType)
	if err != nil || !expected {
		return err
	}
	if previous, ok := a.expected[complexType.Content]; ok && !slices.Equal(previous, repeated) {
		return errors.New("content model choice limits have conflicting derivations")
	}
	a.expected[complexType.Content] = repeated
	return nil
}

func (a *choiceLimitDerivationAudit) expectedForRestriction(complexType ComplexType) ([]uint32, bool, error) {
	if complexType.Derivation != DerivationKindRestriction {
		return nil, false, nil
	}
	baseID, ok := complexType.Base.Complex()
	if !ok || baseID == a.anyType || !ValidComplexTypeID(baseID, len(a.complexTypes)) {
		return nil, false, nil
	}
	repeated, err := RestrictionRepeatedChoiceParticles(
		a.models,
		a.complexTypes[baseID].Content,
		complexType.Content,
		a.rt,
		a.work,
		a.analysis,
	)
	if err != nil || len(repeated) == 0 {
		return nil, false, err
	}
	return repeated, true, nil
}

func (a *choiceLimitDerivationAudit) validateModels() error {
	for i, model := range a.models {
		if err := a.validateModel(i, model); err != nil {
			return err
		}
	}
	return nil
}

func (a *choiceLimitDerivationAudit) validateModel(index int, model ContentModel) error {
	if err := spendContentModelWork(a.work); err != nil {
		return err
	}
	raw, ok := newRuntimeID(index)
	if !ok {
		return errors.New("content model index limit exceeded")
	}
	id := ContentModelID(raw)
	if !slices.Equal(model.ChoiceLimits, a.expected[id]) {
		return errors.New("content model choice limits do not match complex restrictions")
	}
	if len(model.ChoiceLimits) == 0 {
		return nil
	}
	return a.validateOwners(id, model)
}

func (a *choiceLimitDerivationAudit) validateOwners(id ContentModelID, model ContentModel) error {
	for _, owner := range a.owners[id] {
		if err := a.validateOwner(owner, model); err != nil {
			return err
		}
	}
	return nil
}

func (a *choiceLimitDerivationAudit) validateOwner(ownerID ComplexTypeID, model ContentModel) error {
	if err := spendContentModelWork(a.work); err != nil {
		return err
	}
	if !ValidComplexTypeID(ownerID, len(a.complexTypes)) {
		return errors.New("limited content model has invalid restriction owner")
	}
	owner := a.complexTypes[ownerID]
	if owner.Derivation != DerivationKindRestriction {
		return errors.New("limited content model is used outside restricting complex type")
	}
	baseID, ok := owner.Base.Complex()
	if !ok || baseID == a.anyType || !ValidComplexTypeID(baseID, len(a.complexTypes)) {
		return errors.New("limited content model has invalid restriction owner")
	}
	repeated, err := RestrictionRepeatedChoiceParticles(
		a.models,
		a.complexTypes[baseID].Content,
		owner.Content,
		a.rt,
		a.work,
		a.analysis,
	)
	if err != nil {
		return err
	}
	if !slices.Equal(repeated, model.ChoiceLimits) {
		return errors.New("limited content model owner does not derive choice limits")
	}
	return nil
}

func spendContentModelWork(work ContentModelWork) error {
	if err := requireContentModelWork(work); err != nil {
		return err
	}
	return work(1)
}

func requireContentModelWork(work ContentModelWork) error {
	if work == nil {
		return errors.New("content-model work budget is required")
	}
	return nil
}

func requireChoiceLimitAnalysis(rt ParticleRestrictionRuntime, work ContentModelWork, missingRuntime string) error {
	if rt == nil {
		return errors.New(missingRuntime)
	}
	return requireContentModelWork(work)
}

func validOccurrence(o Occurrence) bool {
	if o.Unbounded {
		return o.Max == 0
	}
	return o.Max >= o.Min
}

// ValidateParticleShape validates particle metadata that does not require
// cross-table ID lookup.
func ValidateParticleShape(p Particle) error {
	if !validOccurrence(p.Occurs) {
		return errors.New("particle occurrence is invalid")
	}
	switch p.Kind {
	case ParticleElement, ParticleModel, ParticleWildcard:
	default:
		return errors.New("particle has invalid kind")
	}
	if p.Kind != ParticleElement && p.Element != NoElement {
		return errors.New("particle stores element ID for non-element kind")
	}
	if p.Kind != ParticleModel && p.Model != NoContentModel {
		return errors.New("particle stores content model ID for non-model kind")
	}
	if p.Kind != ParticleWildcard && p.Wildcard != NoWildcard {
		return errors.New("particle stores wildcard ID for non-wildcard kind")
	}
	return nil
}
