package schema

// ContentModelRuntime supplies content models for model-shape algorithms.
type ContentModelRuntime interface {
	ContentModel(id ContentModelID) (ContentModel, bool)
}

// ParticleRuntime supplies read-only runtime tables needed for particle
// matching semantics.
type ParticleRuntime interface {
	ContentModelRuntime
	ElementName(id ElementID) (QName, bool)
	Wildcard(id WildcardID) (Wildcard, bool)
	ForEachSubstitutionMember(id ElementID, fn func(ElementID) bool)
	SubstitutionMemberByName(id ElementID, name QName) (ElementID, bool)
}

// ModelHasNoParticles reports whether modelID is structurally empty.
func ModelHasNoParticles(rt ContentModelRuntime, modelID ContentModelID) bool {
	if modelID == NoContentModel {
		return true
	}
	model, ok := rt.ContentModel(modelID)
	if !ok {
		return false
	}
	switch model.Kind {
	case ModelEmpty:
		return true
	case ModelSequence, ModelChoice, ModelAll:
		return len(model.Particles) == 0
	case ModelAny:
		return false
	default:
	}
	return false
}

// SequenceChoiceRange reports the range for a sequence that restricts a choice.
func SequenceChoiceRange(model ContentModel) Occurrence {
	particleCount := saturatingUint32(len(model.Particles))
	if model.Occurs.Unbounded {
		return Occurrence{Min: saturatingMul(model.Occurs.Min, particleCount), Unbounded: true}
	}
	return Occurrence{Min: saturatingMul(model.Occurs.Min, particleCount), Max: saturatingMul(model.Occurs.Max, particleCount)}
}

// AddOccurrenceRanges saturates sequence ranges before applying occurrence limits.
func AddOccurrenceRanges(a, b Occurrence) Occurrence {
	return Occurrence{Min: saturatingAdd(a.Min, b.Min), Max: saturatingAdd(a.Max, b.Max), Unbounded: a.Unbounded || b.Unbounded}
}

// UnionOccurrenceRanges returns the range accepted by either occurrence range.
func UnionOccurrenceRanges(a, b Occurrence) Occurrence {
	minOccurs := min(b.Min, a.Min)
	if a.Unbounded || b.Unbounded {
		return Occurrence{Min: minOccurs, Unbounded: true}
	}
	maxOccurs := max(b.Max, a.Max)
	return Occurrence{Min: minOccurs, Max: maxOccurs}
}

// MultiplyOccurrence applies occurrence constraints to an accepted range.
func MultiplyOccurrence(a, b Occurrence) Occurrence {
	minOccurs := saturatingMul(a.Min, b.Min)
	if a.Unbounded || b.Unbounded {
		return Occurrence{Min: minOccurs, Unbounded: true}
	}
	return Occurrence{Min: minOccurs, Max: saturatingMul(a.Max, b.Max)}
}

// OccurrenceRangeSubset reports whether derived is a subset of base.
func OccurrenceRangeSubset(derived, base Occurrence) bool {
	if derived.Min < base.Min {
		return false
	}
	if base.Unbounded {
		return true
	}
	if derived.Unbounded {
		return false
	}
	return derived.Max <= base.Max
}

func saturatingUint32(n int) uint32 {
	if n < 0 || uint64(n) > uint64(^uint32(0)) {
		return ^uint32(0)
	}
	return uint32(n)
}

func saturatingAdd(a, b uint32) uint32 {
	if ^uint32(0)-a < b {
		return ^uint32(0)
	}
	return a + b
}

func saturatingMul(a, b uint32) uint32 {
	if a == 0 || b == 0 {
		return 0
	}
	if a > ^uint32(0)/b {
		return ^uint32(0)
	}
	return a * b
}
