package schema

import "errors"

// ContentModelAnalysis is the bounded owner of content-model emptiability,
// count-range, substitution-name, and particle-overlap facts.
type ContentModelAnalysis struct {
	rt        ParticleRuntime
	work      ContentModelWork
	names     map[ElementID]particleAcceptedNames
	emptiable map[ContentModelID]bool
	ranges    map[ContentModelID]Occurrence
	emptyPath map[ContentModelID]bool
	rangePath map[ContentModelID]bool
}

type particleAcceptedNames struct {
	byName  map[QName]ElementID
	ordered []QName
}

// NewContentModelAnalysis creates a reusable bounded analysis context.
func NewContentModelAnalysis(rt ParticleRuntime, work ContentModelWork) (*ContentModelAnalysis, error) {
	if rt == nil {
		return nil, errors.New("content model analysis runtime is nil")
	}
	if err := requireContentModelWork(work); err != nil {
		return nil, err
	}
	return &ContentModelAnalysis{
		rt:        rt,
		work:      work,
		names:     make(map[ElementID]particleAcceptedNames),
		emptiable: make(map[ContentModelID]bool),
		ranges:    make(map[ContentModelID]Occurrence),
		emptyPath: make(map[ContentModelID]bool),
		rangePath: make(map[ContentModelID]bool),
	}, nil
}

// ParticleEmptiable reports whether p accepts an empty sequence, charging and
// memoizing every nested-model derivation.
func (a *ContentModelAnalysis) ParticleEmptiable(p Particle) (bool, error) {
	return a.cachedParticleEmptiable(p)
}

// ModelEmptiable reports whether a model accepts an empty sequence, charging
// and memoizing every nested-model derivation.
func (a *ContentModelAnalysis) ModelEmptiable(id ContentModelID) (bool, error) {
	if id == NoContentModel {
		return true, nil
	}
	return a.cachedModelEmptiable(id)
}

// ModelCountRange derives the number of elements a model can consume,
// charging and memoizing every nested-model derivation.
func (a *ContentModelAnalysis) ModelCountRange(id ContentModelID) (Occurrence, error) {
	if id == NoContentModel {
		return Occurrence{}, nil
	}
	return a.cachedModelCountRange(id)
}

// ParticleCountRange derives the number of elements p can consume, charging
// and memoizing every nested-model derivation.
func (a *ContentModelAnalysis) ParticleCountRange(p Particle) (Occurrence, error) {
	if err := spendContentModelWork(a.work); err != nil {
		return Occurrence{}, err
	}
	var term Occurrence
	switch p.Kind {
	case ParticleElement, ParticleWildcard:
		term = Occurrence{Min: 1, Max: 1}
	case ParticleModel:
		var err error
		term, err = a.ModelCountRange(p.Model)
		if err != nil {
			return Occurrence{}, err
		}
	}
	return MultiplyOccurrence(term, p.Occurs), nil
}

func (a *ContentModelAnalysis) cachedModelCountRange(id ContentModelID) (Occurrence, error) {
	if result, ok := a.ranges[id]; ok {
		return result, nil
	}
	if a.rangePath[id] {
		return Occurrence{}, errors.New("content model count analysis contains a cycle")
	}
	if err := spendContentModelWork(a.work); err != nil {
		return Occurrence{}, err
	}
	model, ok := a.rt.ContentModel(id)
	if !ok {
		return Occurrence{}, errors.New("content model count analysis references missing content model")
	}
	a.rangePath[id] = true
	defer delete(a.rangePath, id)
	term, err := a.modelTermCountRange(model)
	if err != nil {
		return Occurrence{}, err
	}
	result := MultiplyOccurrence(term, model.Occurs)
	a.ranges[id] = result
	return result, nil
}

func (a *ContentModelAnalysis) modelTermCountRange(model ContentModel) (Occurrence, error) {
	switch model.Kind {
	case ModelEmpty:
		return Occurrence{}, nil
	case ModelAny:
		return Occurrence{Unbounded: true}, nil
	case ModelSequence, ModelAll:
		return a.sequenceCountRange(model.Particles)
	case ModelChoice:
		return a.choiceCountRange(model.Particles)
	}
	return Occurrence{}, nil
}

func (a *ContentModelAnalysis) sequenceCountRange(particles []Particle) (Occurrence, error) {
	var result Occurrence
	for _, particle := range particles {
		child, err := a.ParticleCountRange(particle)
		if err != nil {
			return Occurrence{}, err
		}
		result = AddOccurrenceRanges(result, child)
	}
	return result, nil
}

func (a *ContentModelAnalysis) choiceCountRange(particles []Particle) (Occurrence, error) {
	var result Occurrence
	for i, particle := range particles {
		child, err := a.ParticleCountRange(particle)
		if err != nil {
			return Occurrence{}, err
		}
		if i == 0 {
			result = child
		} else {
			result = UnionOccurrenceRanges(result, child)
		}
	}
	return result, nil
}

// Overlap reports one element name accepted by both particles.
func (a *ContentModelAnalysis) Overlap(left, right Particle) (QName, bool, error) {
	if err := spendContentModelWork(a.work); err != nil {
		return QName{}, false, err
	}
	switch {
	case left.Kind == ParticleModel:
		return a.modelStartOverlap(left.Model, right)
	case right.Kind == ParticleModel:
		return a.modelStartOverlap(right.Model, left)
	case left.Kind == ParticleWildcard && right.Kind == ParticleWildcard:
		return a.wildcardOverlap(left.Wildcard, right.Wildcard)
	default:
		return a.elementParticleOverlap(left, right)
	}
}

func (a *ContentModelAnalysis) wildcardOverlap(left, right WildcardID) (QName, bool, error) {
	leftWildcard, leftOK := a.rt.Wildcard(left)
	rightWildcard, rightOK := a.rt.Wildcard(right)
	return QName{}, leftOK && rightOK && WildcardsOverlap(leftWildcard, rightWildcard), nil
}

func (a *ContentModelAnalysis) elementParticleOverlap(left, right Particle) (QName, bool, error) {
	if left.Kind == ParticleElement {
		if name, ok, err := a.elementOverlap(left.Element, right); ok || err != nil {
			return name, ok, err
		}
	}
	if right.Kind == ParticleElement {
		return a.elementOverlap(right.Element, left)
	}
	return QName{}, false, nil
}

func (a *ContentModelAnalysis) modelStartOverlap(id ContentModelID, particle Particle) (QName, bool, error) {
	model, ok := a.rt.ContentModel(id)
	if !ok {
		return QName{}, false, errors.New("content model overlap analysis references missing content model")
	}
	switch model.Kind {
	case ModelAll, ModelChoice, ModelSequence:
	case ModelEmpty, ModelAny:
		return QName{}, false, nil
	default:
		overlaps := false
		return QName{}, overlaps, nil
	}
	for _, child := range model.Particles {
		match, err := a.modelStartParticleOverlap(model.Kind, child, particle)
		if match.overlap || err != nil {
			return match.name, match.overlap, err
		}
		if match.stop {
			break
		}
	}
	return QName{}, false, nil
}

type modelStartParticleMatch struct {
	name    QName
	overlap bool
	stop    bool
}

func (a *ContentModelAnalysis) modelStartParticleOverlap(
	kind ModelKind,
	child, particle Particle,
) (modelStartParticleMatch, error) {
	if err := spendContentModelWork(a.work); err != nil {
		return modelStartParticleMatch{}, err
	}
	name, overlap, err := a.Overlap(child, particle)
	if overlap || err != nil || kind != ModelSequence {
		return modelStartParticleMatch{name: name, overlap: overlap}, err
	}
	emptiable, err := a.cachedParticleEmptiable(child)
	return modelStartParticleMatch{stop: !emptiable}, err
}

func (a *ContentModelAnalysis) elementOverlap(id ElementID, particle Particle) (QName, bool, error) {
	accepted, ok, err := a.acceptedNames(id)
	if err != nil || !ok {
		return QName{}, false, err
	}
	switch particle.Kind {
	case ParticleElement:
		return a.acceptedElementOverlap(accepted, particle.Element)
	case ParticleWildcard:
		return a.acceptedWildcardOverlap(accepted, particle.Wildcard)
	case ParticleModel:
	}
	return QName{}, false, nil
}

func (a *ContentModelAnalysis) acceptedElementOverlap(accepted particleAcceptedNames, id ElementID) (QName, bool, error) {
	other, ok, err := a.acceptedNames(id)
	if err != nil || !ok {
		return QName{}, false, err
	}
	for _, name := range accepted.ordered {
		if err := spendContentModelWork(a.work); err != nil {
			return QName{}, false, err
		}
		if _, exists := other.byName[name]; exists {
			return name, true, nil
		}
	}
	return QName{}, false, nil
}

func (a *ContentModelAnalysis) acceptedWildcardOverlap(accepted particleAcceptedNames, id WildcardID) (QName, bool, error) {
	wildcard, ok := a.rt.Wildcard(id)
	if !ok {
		return QName{}, false, nil
	}
	for _, name := range accepted.ordered {
		if err := spendContentModelWork(a.work); err != nil {
			return QName{}, false, err
		}
		if WildcardAllowsNamespace(wildcard, name.Namespace) {
			return name, true, nil
		}
	}
	return QName{}, false, nil
}

func (a *ContentModelAnalysis) acceptedNames(id ElementID) (particleAcceptedNames, bool, error) {
	if accepted, ok := a.names[id]; ok {
		return accepted, true, nil
	}
	name, ok := a.rt.ElementName(id)
	if !ok {
		return particleAcceptedNames{}, false, nil
	}
	if err := spendContentModelWork(a.work); err != nil {
		return particleAcceptedNames{}, false, err
	}
	accepted := particleAcceptedNames{
		ordered: []QName{name},
		byName:  map[QName]ElementID{name: id},
	}
	collector := particleAcceptedNameCollector{analysis: a, head: id, accepted: &accepted}
	a.rt.ForEachSubstitutionMember(id, collector.collect)
	if collector.err != nil {
		return particleAcceptedNames{}, false, collector.err
	}
	a.names[id] = accepted
	return accepted, true, nil
}

type particleAcceptedNameCollector struct {
	err      error
	analysis *ContentModelAnalysis
	accepted *particleAcceptedNames
	head     ElementID
}

func (c *particleAcceptedNameCollector) collect(member ElementID) bool {
	c.err = spendContentModelWork(c.analysis.work)
	if c.err != nil {
		return false
	}
	memberName, ok := c.analysis.rt.ElementName(member)
	if !ok {
		return true
	}
	allowed, ok := c.analysis.rt.SubstitutionMemberByName(c.head, memberName)
	if !ok || allowed != member {
		return true
	}
	if _, exists := c.accepted.byName[memberName]; !exists {
		c.accepted.ordered = append(c.accepted.ordered, memberName)
		c.accepted.byName[memberName] = member
	}
	return true
}

func (a *ContentModelAnalysis) cachedParticleEmptiable(particle Particle) (bool, error) {
	if err := spendContentModelWork(a.work); err != nil {
		return false, err
	}
	if particle.Occurs.Min == 0 {
		return true, nil
	}
	if particle.Kind != ParticleModel {
		return false, nil
	}
	return a.ModelEmptiable(particle.Model)
}

func (a *ContentModelAnalysis) cachedModelEmptiable(id ContentModelID) (bool, error) {
	if result, ok := a.emptiable[id]; ok {
		return result, nil
	}
	if a.emptyPath[id] {
		return false, errors.New("content model emptiability analysis contains a cycle")
	}
	model, ok := a.rt.ContentModel(id)
	if !ok {
		return false, errors.New("content model emptiability analysis references missing content model")
	}
	if err := spendContentModelWork(a.work); err != nil {
		return false, err
	}
	a.emptyPath[id] = true
	defer delete(a.emptyPath, id)
	result, err := a.modelEmptiableValue(model)
	if err != nil {
		return false, err
	}
	a.emptiable[id] = result
	return result, nil
}

func (a *ContentModelAnalysis) modelEmptiableValue(model ContentModel) (bool, error) {
	if model.Occurs.Min == 0 || model.Kind == ModelEmpty || model.Kind == ModelAny {
		return true, nil
	}
	switch model.Kind {
	case ModelSequence, ModelAll:
		return a.allParticlesEmptiable(model.Particles)
	case ModelChoice:
		return a.anyParticleEmptiable(model.Particles)
	case ModelEmpty, ModelAny:
		return false, nil
	default:
	}
	return false, nil
}

func (a *ContentModelAnalysis) allParticlesEmptiable(particles []Particle) (bool, error) {
	for _, particle := range particles {
		emptiable, err := a.cachedParticleEmptiable(particle)
		if err != nil {
			return false, err
		}
		if !emptiable {
			return false, nil
		}
	}
	return true, nil
}

func (a *ContentModelAnalysis) anyParticleEmptiable(particles []Particle) (bool, error) {
	for _, particle := range particles {
		emptiable, err := a.cachedParticleEmptiable(particle)
		if err != nil {
			return false, err
		}
		if emptiable {
			return true, nil
		}
	}
	return false, nil
}
