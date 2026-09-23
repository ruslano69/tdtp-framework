package schema

import (
	"errors"
)

type contentRestrictionErrorKind uint8

const (
	contentRestrictionMismatchKind contentRestrictionErrorKind = iota
	contentRestrictionInvariantKind
)

type contentRestrictionError struct {
	message string
	kind    contentRestrictionErrorKind
}

func (e *contentRestrictionError) Error() string { return e.message }

func contentRestrictionMismatch(message string) error {
	return &contentRestrictionError{message: message, kind: contentRestrictionMismatchKind}
}

func contentRestrictionInvariant(message string) error {
	return &contentRestrictionError{message: message, kind: contentRestrictionInvariantKind}
}

// IsContentRestrictionMismatch reports a valid restriction relation that is
// not a subset of its base.
func IsContentRestrictionMismatch(err error) bool {
	var issue *contentRestrictionError
	ok := errors.As(err, &issue)
	return ok && issue.kind == contentRestrictionMismatchKind
}

// IsContentRestrictionInvariant reports invalid runtime metadata encountered
// while evaluating a restriction relation.
func IsContentRestrictionInvariant(err error) bool {
	var issue *contentRestrictionError
	ok := errors.As(err, &issue)
	return ok && issue.kind == contentRestrictionInvariantKind
}

type contentRestrictionValidator struct {
	rt          ParticleRestrictionRuntime
	analysis    *ContentModelAnalysis
	modelStates map[ContentModelID]uint8
	wildcards   map[ContentModelID]bool
	choiceBelow map[ContentModelID]bool
	work        *contentModelWorkState
}

// ContentModelWork charges bounded content-model analysis work.
type ContentModelWork func(steps int) error

type contentModelWorkState struct {
	spend ContentModelWork
	err   error
}

// ValidateContentRestriction validates content-model restriction while
// charging every graph traversal and particle comparison to work.
func ValidateContentRestriction(
	rt ParticleRestrictionRuntime,
	baseID, derivedID ContentModelID,
	work ContentModelWork,
	analysis *ContentModelAnalysis,
) error {
	if rt == nil {
		return contentRestrictionInvariant("content restriction requires runtime")
	}
	if err := requireContentModelWork(work); err != nil {
		return err
	}
	if analysis == nil {
		return contentRestrictionInvariant("content restriction requires content model analysis")
	}
	validator := newContentRestrictionValidator(rt, work, analysis)
	err := validator.validateContentRestriction(baseID, derivedID)
	return validator.finish(err)
}

func newContentRestrictionValidator(
	rt ParticleRestrictionRuntime,
	work ContentModelWork,
	analysis *ContentModelAnalysis,
) contentRestrictionValidator {
	return contentRestrictionValidator{
		rt:          rt,
		analysis:    analysis,
		modelStates: make(map[ContentModelID]uint8),
		wildcards:   make(map[ContentModelID]bool),
		choiceBelow: make(map[ContentModelID]bool),
		work:        &contentModelWorkState{spend: work},
	}
}

func (v contentRestrictionValidator) charge() error {
	return v.spend(1)
}

func (v contentRestrictionValidator) spend(steps int) error {
	if v.work.err == nil {
		v.work.err = v.work.spend(steps)
	}
	return v.work.err
}

func (v contentRestrictionValidator) finish(err error) error {
	if v.work.err != nil {
		return v.work.err
	}
	return err
}

func (v contentRestrictionValidator) validateContentRestriction(baseID, derivedID ContentModelID) error {
	if baseID == NoContentModel || derivedID == NoContentModel {
		return nil
	}
	base, derived, err := v.restrictionModels(baseID, derivedID)
	if err != nil {
		return err
	}
	if err := v.validateContentRestrictionRanges(baseID, derivedID); err != nil {
		return err
	}
	return v.validateContentRestrictionModels(derivedID, base, derived)
}

func (v contentRestrictionValidator) validateContentRestrictionModels(
	derivedID ContentModelID,
	base, derived ContentModel,
) error {
	if base.Kind == ModelAny {
		return nil
	}
	hasNoParticles, err := v.modelHasNoParticles(derivedID)
	if err != nil || hasNoParticles {
		return err
	}
	if handled, err := v.validateSingleWildcardBase(base, derived); handled || err != nil {
		return err
	}
	if handled, err := v.validateKnownGroupRestriction(base, derived); handled || err != nil {
		return err
	}
	return v.validateFallbackContentRestriction(base, derived)
}

func (v contentRestrictionValidator) restrictionModels(
	baseID, derivedID ContentModelID,
) (base, derived ContentModel, err error) {
	if validationErr := v.validateContentModelGraph(baseID); validationErr != nil {
		return ContentModel{}, ContentModel{}, validationErr
	}
	if validationErr := v.validateContentModelGraph(derivedID); validationErr != nil {
		return ContentModel{}, ContentModel{}, validationErr
	}
	base, err = v.requireContentModel(baseID)
	if err != nil {
		return ContentModel{}, ContentModel{}, err
	}
	derived, err = v.requireContentModel(derivedID)
	if err != nil {
		return ContentModel{}, ContentModel{}, err
	}
	return base, derived, nil
}

func (v contentRestrictionValidator) validateContentRestrictionRanges(baseID, derivedID ContentModelID) error {
	derivedEmptiable, err := v.analysis.ModelEmptiable(derivedID)
	if err != nil {
		return err
	}
	baseEmptiable, err := v.analysis.ModelEmptiable(baseID)
	if err != nil {
		return err
	}
	if derivedEmptiable && !baseEmptiable {
		return contentRestrictionMismatch("content restriction is not subset of base")
	}
	derivedRange, err := v.analysis.ModelCountRange(derivedID)
	if err != nil {
		return err
	}
	baseRange, err := v.analysis.ModelCountRange(baseID)
	if err != nil {
		return err
	}
	if !OccurrenceRangeSubset(derivedRange, baseRange) {
		return contentRestrictionMismatch("content restriction is not subset of base")
	}
	return nil
}

func (v contentRestrictionValidator) validateSingleWildcardBase(base, derived ContentModel) (bool, error) {
	if len(base.Particles) == 1 && base.Particles[0].Kind == ParticleWildcard {
		for _, p := range derived.Particles {
			if wildcardErr := v.validateParticleRestrictsWildcard(base.Particles[0], p); wildcardErr != nil {
				return true, wildcardErr
			}
		}
		return true, nil
	}
	return false, nil
}

func (v contentRestrictionValidator) validateFallbackContentRestriction(base, derived ContentModel) error {
	derivedContainsWildcard, err := v.modelContainsWildcard(derived)
	if err != nil {
		return err
	}
	baseContainsWildcard, err := v.modelContainsWildcard(base)
	if err != nil {
		return err
	}
	if derivedContainsWildcard && !baseContainsWildcard {
		return contentRestrictionMismatch("wildcard restriction is not subset of base")
	}
	if base.Kind != derived.Kind || len(base.Particles) != len(derived.Particles) {
		return contentRestrictionMismatch("content restriction is not subset of base")
	}
	for i := range base.Particles {
		if err := v.validateParticleRestriction(base.Particles[i], derived.Particles[i]); err != nil {
			return err
		}
	}
	return nil
}

func (v contentRestrictionValidator) modelHasNoParticles(id ContentModelID) (bool, error) {
	if id == NoContentModel {
		return true, nil
	}
	model, err := v.requireContentModel(id)
	if err != nil {
		return false, err
	}
	switch model.Kind {
	case ModelEmpty:
		return true, nil
	case ModelSequence, ModelChoice, ModelAll:
		return len(model.Particles) == 0, nil
	case ModelAny:
		return false, nil
	default:
	}
	return false, nil
}

func (v contentRestrictionValidator) particleEffectiveMin(particle Particle) (uint32, error) {
	if particle.Kind == ParticleModel {
		emptiable, err := v.analysis.ModelEmptiable(particle.Model)
		if err != nil {
			return 0, err
		}
		if emptiable {
			return 0, nil
		}
	}
	return particle.Occurs.Min, nil
}

func (v contentRestrictionValidator) validateContentModelGraph(id ContentModelID) error {
	states := v.modelStates
	if states == nil {
		states = make(map[ContentModelID]uint8)
	}
	return v.validateContentModelGraphWithStates(id, states)
}

func (v contentRestrictionValidator) validateContentModelGraphWithStates(id ContentModelID, states map[ContentModelID]uint8) error {
	if err := v.charge(); err != nil {
		return err
	}
	const (
		modelChecking uint8 = iota + 1
		modelChecked
	)
	switch states[id] {
	case modelChecking:
		return contentRestrictionInvariant("content restriction references cyclic content model")
	case modelChecked:
		return nil
	}
	model, err := v.requireContentModel(id)
	if err != nil {
		return err
	}
	if shapeErr := ValidateContentModelShape(model); shapeErr != nil {
		return contentRestrictionInvariant("content restriction references invalid content model: " + shapeErr.Error())
	}
	states[id] = modelChecking
	for _, particle := range model.Particles {
		if err := v.charge(); err != nil {
			return err
		}
		if err := v.validateContentRestrictionGraphParticle(particle, states); err != nil {
			return err
		}
	}
	states[id] = modelChecked
	return nil
}

func (v contentRestrictionValidator) validateContentRestrictionGraphParticle(
	particle Particle,
	states map[ContentModelID]uint8,
) error {
	switch particle.Kind {
	case ParticleModel:
		return v.validateContentModelGraphWithStates(particle.Model, states)
	case ParticleElement:
		return v.validateContentRestrictionGraphElement(particle.Element)
	case ParticleWildcard:
		_, err := v.requireWildcard(particle.Wildcard)
		return err
	default:
		return contentRestrictionInvariant("content restriction references invalid particle kind")
	}
}

func (v contentRestrictionValidator) validateContentRestrictionGraphElement(id ElementID) error {
	if _, err := v.requireElementName(id); err != nil {
		return err
	}
	declaration, err := v.elementRestriction(id)
	if err != nil {
		return err
	}
	if declaration.Scope == DeclarationScopeInvalid {
		return contentRestrictionInvariant("content restriction references element declaration with invalid scope")
	}
	return nil
}

func (v contentRestrictionValidator) validateKnownGroupRestriction(base, derived ContentModel) (bool, error) {
	switch {
	case base.Kind == ModelChoice && derived.Kind == ModelChoice:
		return true, v.validateChoiceRestriction(base, derived)
	case base.Kind == ModelSequence && derived.Kind == ModelSequence:
		return true, v.validateOrderedGroupRestriction(base, derived, "sequence restriction is not subset of base")
	case base.Kind == ModelSequence && derived.Kind == ModelChoice:
		return true, v.validatePointlessChoiceRestrictsSequence(base, derived)
	case base.Kind == ModelAll && derived.Kind == ModelAll:
		return true, v.validateOrderedGroupRestriction(base, derived, "all restriction is not subset of base")
	case base.Kind == ModelAll && derived.Kind == ModelSequence:
		return true, v.validateSequenceRestrictsAll(base, derived)
	case base.Kind == ModelChoice && derived.Kind == ModelSequence:
		return true, v.validateSequenceRestrictsChoice(base, derived)
	case base.Kind == ModelSequence && derived.Kind == ModelAll:
		return true, v.validateAllRestrictsSequence(base, derived)
	default:
		return false, nil
	}
}

func (v contentRestrictionValidator) validateAllRestrictsSequence(base, derived ContentModel) error {
	if len(base.Particles) == 1 && len(derived.Particles) == 1 {
		return v.validateParticleRestriction(base.Particles[0], derived.Particles[0])
	}
	return contentRestrictionMismatch("all restriction is not subset of sequence")
}

func (v contentRestrictionValidator) choiceRestrictionBranchAllowed(base []Particle, derived Particle) (bool, error) {
	for _, b := range base {
		allowed, err := v.choiceBranchRestricts(b, derived)
		if err != nil {
			return false, err
		}
		if allowed {
			return true, nil
		}
	}
	return false, nil
}

func (v contentRestrictionValidator) validateChoiceRestriction(base, derived ContentModel) error {
	if !OccurrenceRangeSubset(derived.Occurs, base.Occurs) {
		return contentRestrictionMismatch("choice restriction occurrence is not subset of base")
	}
	requiresXSD11, err := v.choiceRestrictionRequiresXSD11(base, derived)
	if err != nil {
		return err
	}
	if requiresXSD11 {
		return contentRestrictionMismatch("choice restriction requires XSD 1.1 intensional rules")
	}
	baseIndex := 0
	for _, derivedParticle := range derived.Particles {
		matched, err := v.matchChoiceRestrictionBranch(base.Particles, &baseIndex, derivedParticle)
		if err != nil {
			return err
		}
		if !matched {
			return contentRestrictionMismatch("choice restriction branch is not subset of base")
		}
	}
	return nil
}

func (v contentRestrictionValidator) matchChoiceRestrictionBranch(
	base []Particle,
	baseIndex *int,
	derived Particle,
) (bool, error) {
	for *baseIndex < len(base) {
		allowed, err := v.choiceBranchRestricts(base[*baseIndex], derived)
		if err != nil || allowed {
			return allowed, err
		}
		*baseIndex++
	}
	return false, nil
}

func (v contentRestrictionValidator) choiceRestrictionRequiresXSD11(base, derived ContentModel) (bool, error) {
	if base.Occurs.IsExactlyOne() && derived.Occurs.Min < base.Occurs.Min {
		return true, nil
	}
	shorterUnbounded, err := v.shorterChoiceRequiresXSD11(base, derived)
	if err != nil || shorterUnbounded {
		return shorterUnbounded, err
	}
	return v.particlesContainNestedChoice(derived.Particles)
}

func (v contentRestrictionValidator) shorterChoiceRequiresXSD11(base, derived ContentModel) (bool, error) {
	if !base.Occurs.IsExactlyOne() || !derived.Occurs.IsExactlyOne() || len(derived.Particles) >= len(base.Particles) {
		return false, nil
	}
	for _, particle := range derived.Particles {
		if particle.Kind != ParticleModel {
			continue
		}
		rangeForParticle, err := v.analysis.ParticleCountRange(particle)
		if err != nil {
			return false, err
		}
		if rangeForParticle.Unbounded {
			return true, nil
		}
	}
	return false, nil
}

func (v contentRestrictionValidator) particlesContainNestedChoice(particles []Particle) (bool, error) {
	for _, particle := range particles {
		contains, err := v.particleContainsNestedChoice(particle)
		if err != nil {
			return false, err
		}
		if contains {
			return true, nil
		}
	}
	return false, nil
}

func (v contentRestrictionValidator) particleContainsNestedChoice(p Particle) (bool, error) {
	if err := v.charge(); err != nil {
		return false, err
	}
	if p.Kind != ParticleModel {
		return false, nil
	}
	return v.modelContainsChoiceBelow(p.Model)
}

func (v contentRestrictionValidator) modelContainsChoiceBelow(id ContentModelID) (bool, error) {
	if result, ok := v.choiceBelow[id]; ok {
		return result, nil
	}
	model, err := v.requireContentModel(id)
	if err != nil {
		return false, err
	}
	for _, p := range model.Particles {
		contains, err := v.particleContainsChoiceBelow(p)
		if err != nil {
			return false, err
		}
		if contains {
			v.choiceBelow[id] = true
			return true, nil
		}
	}
	v.choiceBelow[id] = false
	return false, nil
}

func (v contentRestrictionValidator) particleContainsChoiceBelow(particle Particle) (bool, error) {
	if err := v.charge(); err != nil {
		return false, err
	}
	if particle.Kind != ParticleModel {
		return false, nil
	}
	nested, err := v.requireContentModel(particle.Model)
	if err != nil {
		return false, err
	}
	if nested.Kind == ModelChoice {
		return true, nil
	}
	return v.modelContainsChoiceBelow(particle.Model)
}

func (v contentRestrictionValidator) choiceBranchRestricts(base, derived Particle) (bool, error) {
	candidate := derived
	if base.Kind != ParticleModel && base.Occurs.IsExactlyOne() {
		normalize, err := v.particleNeedsChoiceBranchNormalization(derived)
		if err != nil {
			return false, err
		}
		if normalize {
			candidate.Occurs = Occurrence{Min: 1, Max: 1}
		}
	}
	err := v.validateParticleRestriction(base, candidate)
	if err == nil {
		return true, nil
	}
	if IsContentRestrictionMismatch(err) {
		return false, nil
	}
	return false, err
}

func (v contentRestrictionValidator) particleNeedsChoiceBranchNormalization(p Particle) (bool, error) {
	effectiveMin, err := v.particleEffectiveMin(p)
	if err != nil {
		return false, err
	}
	if effectiveMin > 0 {
		return true, nil
	}
	rangeForParticle, err := v.analysis.ParticleCountRange(p)
	if err != nil {
		return false, err
	}
	return rangeForParticle.Unbounded || rangeForParticle.Max > 1, nil
}

func (v contentRestrictionValidator) validateOrderedGroupRestriction(base, derived ContentModel, msg string) error {
	if !OccurrenceRangeSubset(derived.Occurs, base.Occurs) {
		return contentRestrictionMismatch(msg)
	}
	matcher := orderedGroupRestrictionMatcher{validator: v, base: base.Particles, message: msg}
	for _, derivedParticle := range derived.Particles {
		if err := matcher.match(derivedParticle); err != nil {
			return err
		}
	}
	return matcher.validateRemainder()
}

type orderedGroupRestrictionMatcher struct {
	validator contentRestrictionValidator
	message   string
	base      []Particle
	next      int
}

func (m *orderedGroupRestrictionMatcher) match(derived Particle) error {
	for m.next < len(m.base) {
		err := m.validator.validateParticleRestriction(m.base[m.next], derived)
		if err == nil {
			m.next++
			return nil
		}
		if !IsContentRestrictionMismatch(err) {
			return err
		}
		emptiable, err := m.validator.analysis.ParticleEmptiable(m.base[m.next])
		if err != nil {
			return err
		}
		if !emptiable {
			return contentRestrictionMismatch(m.message)
		}
		m.next++
	}
	return contentRestrictionMismatch(m.message)
}

func (m *orderedGroupRestrictionMatcher) validateRemainder() error {
	for ; m.next < len(m.base); m.next++ {
		emptiable, err := m.validator.analysis.ParticleEmptiable(m.base[m.next])
		if err != nil {
			return err
		}
		if !emptiable {
			return contentRestrictionMismatch(m.message)
		}
	}
	return nil
}

func (v contentRestrictionValidator) validateSequenceRestrictsAll(base, derived ContentModel) error {
	if !OccurrenceRangeSubset(derived.Occurs, base.Occurs) {
		return contentRestrictionMismatch("sequence restriction occurrence is not subset of all")
	}
	return v.validateMappedGroupRestriction(base, derived, "sequence restriction particle is not subset of all", "sequence restriction omits required all particle")
}

func (v contentRestrictionValidator) validateMappedGroupRestriction(base, derived ContentModel, particleMsg, omittedMsg string) error {
	mapped := make([]bool, len(base.Particles))
	for _, derivedParticle := range derived.Particles {
		match, err := v.findMappedGroupParticle(base.Particles, mapped, derivedParticle)
		if err != nil {
			return err
		}
		if match < 0 {
			return contentRestrictionMismatch(particleMsg)
		}
		mapped[match] = true
	}
	return v.validateUnmappedGroupParticles(base.Particles, mapped, omittedMsg)
}

func (v contentRestrictionValidator) findMappedGroupParticle(
	base []Particle,
	mapped []bool,
	derived Particle,
) (int, error) {
	for i, baseParticle := range base {
		if mapped[i] {
			continue
		}
		err := v.validateParticleRestriction(baseParticle, derived)
		if err == nil {
			return i, nil
		}
		if !IsContentRestrictionMismatch(err) {
			return -1, err
		}
	}
	return -1, nil
}

func (v contentRestrictionValidator) validateUnmappedGroupParticles(
	base []Particle,
	mapped []bool,
	message string,
) error {
	for i, baseParticle := range base {
		if mapped[i] {
			continue
		}
		emptiable, err := v.analysis.ParticleEmptiable(baseParticle)
		if err != nil {
			return err
		}
		if !emptiable {
			return contentRestrictionMismatch(message)
		}
	}
	return nil
}

func (v contentRestrictionValidator) validateSequenceRestrictsChoice(base, derived ContentModel) error {
	if !OccurrenceRangeSubset(SequenceChoiceRange(derived), base.Occurs) {
		return contentRestrictionMismatch("sequence restriction occurrence is not subset of choice")
	}
	for _, derivedParticle := range derived.Particles {
		allowed, err := v.choiceRestrictionBranchAllowed(base.Particles, derivedParticle)
		if err != nil {
			return err
		}
		if !allowed {
			return contentRestrictionMismatch("sequence restriction particle is not subset of choice")
		}
	}
	return nil
}

func (v contentRestrictionValidator) validatePointlessChoiceRestrictsSequence(base, derived ContentModel) error {
	if !derived.Occurs.IsExactlyOne() || len(derived.Particles) != 1 {
		return contentRestrictionMismatch("choice restriction of sequence is forbidden")
	}
	return v.validateChoiceBranchRestrictsSequence(base, derived.Particles[0])
}

func (v contentRestrictionValidator) validateChoiceBranchRestrictsSequence(base ContentModel, derived Particle) error {
	for i, baseParticle := range base.Particles {
		err := v.validateParticleRestriction(baseParticle, derived)
		if err != nil {
			if !IsContentRestrictionMismatch(err) {
				return err
			}
			continue
		}
		emptiable, err := v.sequenceRemainderEmptiable(base.Particles, i)
		if err != nil {
			return err
		}
		if emptiable {
			return nil
		}
	}
	return contentRestrictionMismatch("choice restriction branch is not subset of sequence")
}

func (v contentRestrictionValidator) sequenceRemainderEmptiable(particles []Particle, selected int) (bool, error) {
	for i, p := range particles {
		if i == selected {
			continue
		}
		emptiable, err := v.analysis.ParticleEmptiable(p)
		if err != nil {
			return false, err
		}
		if !emptiable {
			return false, nil
		}
	}
	return true, nil
}

func (v contentRestrictionValidator) modelContainsWildcard(model ContentModel) (bool, error) {
	for _, particle := range model.Particles {
		contains, err := v.particleContainsWildcard(particle)
		if err != nil {
			return false, err
		}
		if contains {
			return true, nil
		}
	}
	return false, nil
}

func (v contentRestrictionValidator) particleContainsWildcard(p Particle) (bool, error) {
	if err := v.charge(); err != nil {
		return false, err
	}
	switch p.Kind {
	case ParticleWildcard:
		return true, nil
	case ParticleModel:
		return v.modelIDContainsWildcard(p.Model)
	case ParticleElement:
		return false, nil
	default:
	}
	return false, nil
}

func (v contentRestrictionValidator) modelIDContainsWildcard(id ContentModelID) (bool, error) {
	if result, ok := v.wildcards[id]; ok {
		return result, nil
	}
	model, err := v.requireContentModel(id)
	if err != nil {
		return false, err
	}
	result, err := v.modelContainsWildcard(model)
	if err != nil {
		return false, err
	}
	v.wildcards[id] = result
	return result, nil
}

func (v contentRestrictionValidator) validateParticleRestriction(base, derived Particle) error {
	if err := v.charge(); err != nil {
		return err
	}
	derivedRange, err := v.analysis.ParticleCountRange(derived)
	if err != nil {
		return err
	}
	baseRange, err := v.analysis.ParticleCountRange(base)
	if err != nil {
		return err
	}
	if !OccurrenceRangeSubset(derivedRange, baseRange) {
		return contentRestrictionMismatch("particle restriction occurrence is not subset of base")
	}
	switch base.Kind {
	case ParticleWildcard:
		return v.validateParticleRestrictsWildcard(base, derived)
	case ParticleElement:
		return v.validateParticleRestrictsElement(base, derived)
	case ParticleModel:
		return v.validateParticleRestrictsModel(base, derived)
	default:
		return nil
	}
}

func (v contentRestrictionValidator) validateParticleRestrictsElement(base, derived Particle) error {
	handled, err := v.validateParticleElementKind(base, derived)
	if err != nil || handled {
		return err
	}
	baseElement, derivedElement, err := v.restrictedElementIDs(base.Element, derived.Element)
	if err != nil {
		return err
	}
	baseDecl, derivedDecl, err := v.restrictedElementDeclarations(baseElement, derivedElement)
	if err != nil {
		return err
	}
	if baseDecl.Scope == DeclarationScopeGlobal && derivedDecl.Scope == DeclarationScopeGlobal {
		return nil
	}
	if err := v.validateRestrictedElementType(baseDecl, derivedDecl); err != nil {
		return err
	}
	return validateRestrictedElementProperties(baseDecl, derivedDecl)
}

func (v contentRestrictionValidator) validateParticleElementKind(base, derived Particle) (bool, error) {
	switch derived.Kind {
	case ParticleWildcard:
		return true, contentRestrictionMismatch("wildcard restriction is not subset of element")
	case ParticleModel:
		return true, v.validateModelParticleRestrictsElement(base, derived.Model)
	case ParticleElement:
		return false, nil
	default:
		return true, nil
	}
}

func (v contentRestrictionValidator) validateModelParticleRestrictsElement(base Particle, modelID ContentModelID) error {
	model, err := v.requireContentModel(modelID)
	if err != nil {
		return err
	}
	if model.Kind != ModelChoice {
		return contentRestrictionMismatch("model group restriction is not subset of element")
	}
	for _, particle := range model.Particles {
		allowed, err := v.choiceBranchRestricts(base, particle)
		if err != nil {
			return err
		}
		if !allowed {
			return contentRestrictionMismatch("choice restriction branch is not subset of element")
		}
	}
	return nil
}

func (v contentRestrictionValidator) restrictedElementIDs(base, derived ElementID) (baseID, derivedID ElementID, err error) {
	baseName, err := v.requireElementName(base)
	if err != nil {
		return NoElement, NoElement, err
	}
	derivedName, err := v.requireElementName(derived)
	if err != nil {
		return NoElement, NoElement, err
	}
	if baseName != derivedName {
		member, ok := v.SubstitutionMemberByName(base, derivedName)
		if !ok || member != derived {
			return NoElement, NoElement, contentRestrictionMismatch("element restriction name is not subset of base")
		}
		base = member
	}
	return base, derived, nil
}

func (v contentRestrictionValidator) restrictedElementDeclarations(
	base, derived ElementID,
) (baseDecl, derivedDecl ParticleRestrictionElement, err error) {
	baseDecl, err = v.elementRestriction(base)
	if err != nil {
		return ParticleRestrictionElement{}, ParticleRestrictionElement{}, err
	}
	derivedDecl, err = v.elementRestriction(derived)
	if err != nil {
		return ParticleRestrictionElement{}, ParticleRestrictionElement{}, err
	}
	if baseDecl.Scope == DeclarationScopeInvalid || derivedDecl.Scope == DeclarationScopeInvalid {
		return ParticleRestrictionElement{}, ParticleRestrictionElement{}, contentRestrictionInvariant("content restriction references element declaration with invalid scope")
	}
	return baseDecl, derivedDecl, nil
}

func (v contentRestrictionValidator) validateRestrictedElementType(
	base, derived ParticleRestrictionElement,
) error {
	const excluded = DerivationExtension | DerivationList | DerivationUnion
	mask, ok, err := deriveTypeMask(v.rt, derived.Type, base.Type, v.spend)
	if err != nil {
		return err
	}
	if !ok || mask&excluded != 0 {
		return contentRestrictionMismatch("element restriction type is not derived from base")
	}
	return nil
}

func validateRestrictedElementProperties(base, derived ParticleRestrictionElement) error {
	if derived.Nillable && !base.Nillable {
		return contentRestrictionMismatch("element restriction nillable is not subset of base")
	}
	if derived.Block&base.Block != base.Block {
		return contentRestrictionMismatch("element restriction block is not subset of base")
	}
	if !derived.Identities.IsSubsetOf(base.Identities) {
		return contentRestrictionMismatch("element restriction identity constraints are not subset of base")
	}
	if base.Fixed.Present && !FixedValueConstraintEqual(base.Fixed, derived.Fixed) {
		return contentRestrictionMismatch("element restriction fixed value is not subset of base")
	}
	return nil
}

func (v contentRestrictionValidator) validateParticleRestrictsModel(base, derived Particle) error {
	model, err := v.requireContentModel(base.Model)
	if err != nil {
		return err
	}
	if model.Kind == ModelChoice {
		return v.validateParticleRestrictsChoiceModel(base, derived, model)
	}
	if len(model.Particles) == 1 {
		return v.validateParticleRestriction(model.Particles[0], derived)
	}
	if derived.Kind == ParticleWildcard {
		return contentRestrictionMismatch("wildcard restriction is not subset of model group")
	}
	if model.Kind == ModelSequence && derived.Kind == ParticleElement {
		return v.validateElementParticleRestrictsSequenceModel(model, derived)
	}
	if derived.Kind != ParticleModel {
		return nil
	}
	return v.validateContentRestriction(base.Model, derived.Model)
}

func (v contentRestrictionValidator) validateParticleRestrictsChoiceModel(base, derived Particle, model ContentModel) error {
	if derived.Kind == ParticleModel {
		derivedModel, err := v.requireContentModel(derived.Model)
		if err != nil {
			return err
		}
		if derivedModel.Kind == ModelChoice && derived.Occurs.Min < base.Occurs.Min {
			return contentRestrictionMismatch("choice restriction occurrence is not subset of base")
		}
		switch derivedModel.Kind {
		case ModelChoice:
			return v.validateChoiceRestriction(model, derivedModel)
		case ModelSequence:
			return v.validateSequenceRestrictsChoice(model, derivedModel)
		case ModelEmpty, ModelAny, ModelAll:
		default:
			return errors.New("content restriction references invalid model kind")
		}
	}
	allowed, err := v.choiceRestrictionBranchAllowed(model.Particles, derived)
	if err != nil {
		return err
	}
	if !allowed {
		return contentRestrictionMismatch("choice restriction branch is not subset of base")
	}
	return nil
}

func (v contentRestrictionValidator) validateElementParticleRestrictsSequenceModel(base ContentModel, derived Particle) error {
	for i, baseParticle := range base.Particles {
		err := v.validateParticleRestriction(baseParticle, derived)
		if err == nil {
			return v.validateElementSequenceRemainder(base.Particles, i)
		}
		if !IsContentRestrictionMismatch(err) {
			return err
		}
	}
	return contentRestrictionMismatch("sequence restriction particle is not subset of base")
}

func (v contentRestrictionValidator) validateElementSequenceRemainder(base []Particle, selected int) error {
	emptiable, err := v.sequenceRemainderEmptiable(base, selected)
	if err != nil {
		return err
	}
	if !emptiable {
		return contentRestrictionMismatch("sequence restriction omits required base particle")
	}
	return nil
}

func (v contentRestrictionValidator) validateParticleRestrictsWildcard(base, derived Particle) error {
	if err := v.charge(); err != nil {
		return err
	}
	switch derived.Kind {
	case ParticleElement:
		return v.validateElementRestrictsWildcard(base.Wildcard, derived.Element)
	case ParticleWildcard:
		return v.validateWildcardRestrictsWildcard(base.Wildcard, derived.Wildcard)
	case ParticleModel:
		return v.validateModelRestrictsWildcard(base, derived.Model)
	}
	return nil
}

func (v contentRestrictionValidator) validateElementRestrictsWildcard(base WildcardID, derived ElementID) error {
	baseWildcard, err := v.requireWildcard(base)
	if err != nil {
		return err
	}
	derivedName, err := v.requireElementName(derived)
	if err != nil {
		return err
	}
	if !WildcardAllowsNamespace(baseWildcard, derivedName.Namespace) {
		return contentRestrictionMismatch("element restriction is not allowed by wildcard")
	}
	return nil
}

func (v contentRestrictionValidator) validateWildcardRestrictsWildcard(base, derived WildcardID) error {
	derivedWildcard, err := v.requireWildcard(derived)
	if err != nil {
		return err
	}
	baseWildcard, err := v.requireWildcard(base)
	if err != nil {
		return err
	}
	if !WildcardSubset(derivedWildcard, baseWildcard) {
		return contentRestrictionMismatch("wildcard restriction is not subset of base")
	}
	return nil
}

func (v contentRestrictionValidator) validateModelRestrictsWildcard(base Particle, derived ContentModelID) error {
	model, err := v.requireContentModel(derived)
	if err != nil {
		return err
	}
	for _, child := range model.Particles {
		if err := v.validateParticleRestrictsWildcard(base, child); err != nil {
			return err
		}
	}
	return nil
}

func (v contentRestrictionValidator) requireContentModel(id ContentModelID) (ContentModel, error) {
	model, ok := v.ContentModel(id)
	if !ok {
		return ContentModel{}, contentRestrictionInvariant("content restriction references missing content model")
	}
	return model, nil
}

func (v contentRestrictionValidator) ContentModel(id ContentModelID) (ContentModel, bool) {
	if v.charge() != nil {
		return ContentModel{}, false
	}
	return v.rt.ContentModel(id)
}

func (v contentRestrictionValidator) ElementName(id ElementID) (QName, bool) {
	if v.charge() != nil {
		return QName{}, false
	}
	return v.rt.ElementName(id)
}

func (v contentRestrictionValidator) Wildcard(id WildcardID) (Wildcard, bool) {
	if v.charge() != nil {
		return Wildcard{}, false
	}
	return v.rt.Wildcard(id)
}

func (v contentRestrictionValidator) ForEachSubstitutionMember(id ElementID, fn func(ElementID) bool) {
	v.rt.ForEachSubstitutionMember(id, func(member ElementID) bool {
		return v.charge() == nil && fn(member)
	})
}

func (v contentRestrictionValidator) SubstitutionMemberByName(id ElementID, name QName) (ElementID, bool) {
	if v.charge() != nil {
		return NoElement, false
	}
	return v.rt.SubstitutionMemberByName(id, name)
}

func (v contentRestrictionValidator) requireElementName(id ElementID) (QName, error) {
	name, ok := v.ElementName(id)
	if !ok {
		if err := v.finish(nil); err != nil {
			return QName{}, err
		}
		return QName{}, contentRestrictionInvariant("content restriction references missing element name")
	}
	return name, nil
}

func (v contentRestrictionValidator) elementRestriction(id ElementID) (ParticleRestrictionElement, error) {
	if err := v.charge(); err != nil {
		return ParticleRestrictionElement{}, err
	}
	decl, ok := v.rt.ElementRestriction(id)
	if !ok {
		return ParticleRestrictionElement{}, contentRestrictionInvariant("content restriction references missing element declaration")
	}
	return decl, nil
}

func (v contentRestrictionValidator) requireWildcard(id WildcardID) (Wildcard, error) {
	wildcard, ok := v.Wildcard(id)
	if !ok {
		return Wildcard{}, contentRestrictionInvariant("content restriction references missing wildcard")
	}
	return wildcard, nil
}
