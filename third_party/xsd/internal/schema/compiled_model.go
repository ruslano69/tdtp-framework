package schema

import (
	"errors"
)

// CompiledModelKind identifies the runtime representation used for a compiled
// content model.
type CompiledModelKind uint8

const (
	// CompiledModelEmpty is the compiled representation for empty content.
	CompiledModelEmpty CompiledModelKind = iota
	// CompiledModelAny is the compiled representation for xs:anyType content.
	CompiledModelAny
	// CompiledModelAll is the compiled representation for xs:all content.
	CompiledModelAll
	// CompiledModelDFA is the compiled DFA representation.
	CompiledModelDFA
)

// ValidCompiledModelKind reports whether kind is a known compiled-model kind.
func ValidCompiledModelKind(kind CompiledModelKind) bool {
	switch kind {
	case CompiledModelEmpty, CompiledModelAny, CompiledModelAll, CompiledModelDFA:
		return true
	default:
		return false
	}
}

// CompiledModel stores a runtime-ready content model.
type CompiledModel struct {
	Rows      []CompiledModelRow
	All       []CompiledAllTerm
	Source    ContentModelID
	Start     uint32
	AllBitLen uint32
	Kind      CompiledModelKind
	Mixed     bool
	Empty     bool
}

type dfaRowIndex struct {
	nameToEdge    map[QName]uint32
	wildcardEdges []uint32
}

func (idx dfaRowIndex) enabled() bool {
	return idx.nameToEdge != nil
}

// CompiledModelRow stores a compiled DFA state.
type CompiledModelRow struct {
	Edges         []CompiledModelEdge
	index         dfaRowIndex
	CountParticle Particle
	Min           uint32
	Max           uint32
	Accept        bool
	Counted       bool
	Unbounded     bool
}

// CompiledModelEdge stores one transition in a compiled DFA row.
type CompiledModelEdge struct {
	Particle Particle
	To       uint32
}

// CompiledAllTerm stores one xs:all term in compiled form.
type CompiledAllTerm struct {
	Particle Particle
	Required bool
}

// CompiledModelRuntime supplies element, wildcard, content, and substitution
// metadata needed while constructing compiled content models.
type CompiledModelRuntime interface {
	ParticleRuntime
	DFARowIndexRuntime
}

// SameCompiledParticle reports whether two compiled particles reference the
// same runtime term. Occurrence and nested model IDs are intentionally ignored:
// compiled transitions contain direct element or wildcard particles.
func SameCompiledParticle(a, b Particle) bool {
	return a.Kind == b.Kind && a.Element == b.Element && a.Wildcard == b.Wildcard
}

const compiledDFARowIndexMinEdges = 8

type dfaRowIndexAnalysis struct {
	rt      DFARowIndexRuntime
	work    ContentModelWork
	entries map[ElementID][]QName
}

// IndexCompiledModelsRows builds row indexes for a model table and
// reuses each substitution head's expanded entries across the table.
func IndexCompiledModelsRows(rt DFARowIndexRuntime, models []CompiledModel, work ContentModelWork) error {
	if err := requireContentModelWork(work); err != nil {
		return err
	}
	analysis := newDFARowIndexAnalysis(rt, work)
	for i := range models {
		if err := spendContentModelWork(work); err != nil {
			return err
		}
		if err := analysis.indexModel(&models[i]); err != nil {
			return err
		}
	}
	return nil
}

func newDFARowIndexAnalysis(rt DFARowIndexRuntime, work ContentModelWork) *dfaRowIndexAnalysis {
	return &dfaRowIndexAnalysis{
		rt:      rt,
		work:    work,
		entries: make(map[ElementID][]QName),
	}
}

func (a *dfaRowIndexAnalysis) indexModel(model *CompiledModel) error {
	if model.Kind != CompiledModelDFA {
		return nil
	}
	for i := range model.Rows {
		if err := spendContentModelWork(a.work); err != nil {
			return err
		}
		if err := a.indexRow(&model.Rows[i]); err != nil {
			return err
		}
	}
	return nil
}

func (a *dfaRowIndexAnalysis) indexRow(row *CompiledModelRow) error {
	if len(row.Edges) < compiledDFARowIndexMinEdges {
		return nil
	}
	if err := spendContentModelWorkN(a.work, len(row.Edges)); err != nil {
		return err
	}
	index := make(map[QName]uint32, len(row.Edges))
	var wildcards []uint32
	for pos, edge := range row.Edges {
		indexed, err := a.indexEdge(index, &wildcards, uint32(pos), edge)
		if err != nil {
			return err
		}
		if !indexed {
			return nil
		}
	}
	row.index = dfaRowIndex{nameToEdge: index, wildcardEdges: wildcards}
	return nil
}

func (a *dfaRowIndexAnalysis) indexEdge(
	index map[QName]uint32,
	wildcards *[]uint32,
	position uint32,
	edge CompiledModelEdge,
) (bool, error) {
	switch edge.Particle.Kind {
	case ParticleElement:
		return a.indexElementEdge(index, position, edge.Particle.Element)
	case ParticleWildcard:
		*wildcards = append(*wildcards, position)
		return true, nil
	case ParticleModel:
		return false, nil
	default:
		return false, errors.New("compiled content model index has invalid edge particle")
	}
}

func (a *dfaRowIndexAnalysis) indexElementEdge(index map[QName]uint32, position uint32, element ElementID) (bool, error) {
	name, ok := a.rt.ElementName(element)
	if !ok {
		return false, errors.New("compiled content model index references invalid element")
	}
	if !indexEdgeName(index, name, position) {
		return false, nil
	}
	entries, err := a.substitutionEntries(element)
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if err := spendContentModelWork(a.work); err != nil {
			return false, err
		}
		if !indexEdgeName(index, entry, position) {
			return false, nil
		}
	}
	return true, nil
}

func (a *dfaRowIndexAnalysis) substitutionEntries(id ElementID) ([]QName, error) {
	if entries, ok := a.entries[id]; ok {
		return entries, nil
	}
	var entries []QName
	var workErr error
	a.rt.ForEachSubstitutionEntry(id, func(name QName, _ ElementID) bool {
		workErr = spendContentModelWork(a.work)
		if workErr != nil {
			return false
		}
		entries = append(entries, name)
		return true
	})
	if workErr != nil {
		return nil, workErr
	}
	a.entries[id] = entries
	return entries, nil
}

func indexEdgeName(index map[QName]uint32, name QName, pos uint32) bool {
	if prev, ok := index[name]; ok {
		return prev == pos
	}
	index[name] = pos
	return true
}

// CompiledCountingException reports whether overlapping counted-row edges are
// the one deterministic loop/exit pair produced for a fixed repeated particle.
func CompiledCountingException(index uint32, row CompiledModelRow, a, b CompiledModelEdge) bool {
	if !row.Counted || row.Unbounded || row.Min != row.Max {
		return false
	}
	aLoop := a.To == index && SameCompiledParticle(a.Particle, row.CountParticle)
	bLoop := b.To == index && SameCompiledParticle(b.Particle, row.CountParticle)
	return (aLoop && b.To != index) || (bLoop && a.To != index)
}

// DFARowIndexRuntime supplies declarations needed to validate a DFA row index.
type DFARowIndexRuntime interface {
	ElementName(id ElementID) (QName, bool)
	ForEachSubstitutionEntry(id ElementID, fn func(QName, ElementID) bool)
}
