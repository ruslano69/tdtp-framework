package schema

import (
	"cmp"
	"slices"
	"strconv"

	"github.com/jacoelho/xsd/xsderrors"
)

type dfaConfig struct {
	Counters []uint32
	State    uint32
}

type dfaDeterministicState struct {
	Configs []dfaConfig
}

type dfaTransitionSet struct {
	Configs  []dfaConfig
	Particle Particle
}

type particleTermKey struct {
	Element  ElementID
	Wildcard WildcardID
	Kind     ParticleKind
}

func (b *dfaBuilder) compileDeterministicModel(id ContentModelID, start uint32) (CompiledModel, error) {
	caps, capsErr := b.counterCaps()
	if capsErr != nil {
		return CompiledModel{}, capsErr
	}
	compilation := deterministicModelCompilation{
		builder: b, caps: caps, states: make(map[string]uint32),
	}
	startID, err := compilation.start(start)
	if err != nil {
		return CompiledModel{}, err
	}
	if err := compilation.compileRows(); err != nil {
		return CompiledModel{}, err
	}
	if err := b.c.checkCompiledRowsUPA(compilation.rows); err != nil {
		return CompiledModel{}, err
	}
	model, ok := b.c.rt.ContentModel(id)
	if !ok {
		return CompiledModel{}, xsderrors.InternalInvariant("content model DFA references missing content model")
	}
	return CompiledModel{
		Kind:  CompiledModelDFA,
		Rows:  compilation.rows,
		Start: startID,
		Mixed: model.Mixed,
		Empty: compilation.rows[startID].Accept,
	}, nil
}

type deterministicModelCompilation struct {
	builder *dfaBuilder
	caps    []uint32
	states  map[string]uint32
	queue   []dfaDeterministicState
	rows    []CompiledModelRow
}

func (c *deterministicModelCompilation) start(start uint32) (uint32, error) {
	if err := c.builder.c.work.spend(int(c.builder.counters)); err != nil {
		return 0, err
	}
	counters := make([]uint32, c.builder.counters)
	return c.stateID(dfaDeterministicState{Configs: []dfaConfig{{State: start, Counters: counters}}})
}

func (c *deterministicModelCompilation) stateID(state dfaDeterministicState) (uint32, error) {
	if err := c.builder.spendDFAConfigs(state.Configs); err != nil {
		return 0, err
	}
	state.Configs = normalizeDFAConfigs(state.Configs)
	return c.stateIDNormalized(state)
}

func (c *deterministicModelCompilation) stateIDNormalized(state dfaDeterministicState) (uint32, error) {
	if len(state.Configs) > c.builder.limit {
		return 0, xsderrors.SchemaCompile(xsderrors.CodeSchemaLimit, "content model DFA state limit exceeded")
	}
	key := dfaConfigStateKey(state.Configs)
	if id, ok := c.states[key]; ok {
		return id, nil
	}
	if len(c.states) >= c.builder.limit {
		return 0, xsderrors.SchemaCompile(xsderrors.CodeSchemaLimit, "content model DFA state limit exceeded")
	}
	id, err := checkedSchemaUint32(len(c.states), "content model DFA state limit exceeded")
	if err != nil {
		return 0, err
	}
	c.states[key] = id
	c.queue = append(c.queue, state)
	return id, nil
}

func (c *deterministicModelCompilation) compileRows() error {
	for len(c.queue) != 0 {
		state := c.queue[0]
		c.queue[0] = dfaDeterministicState{}
		c.queue = c.queue[1:]
		if err := c.builder.c.work.spend(len(state.Configs) + 1); err != nil {
			return err
		}
		row, err := c.builder.deterministicRow(state, c.caps, c.stateIDNormalized)
		if err != nil {
			return err
		}
		c.rows = append(c.rows, row)
	}
	return nil
}

func (b *dfaBuilder) deterministicRow(state dfaDeterministicState, caps []uint32, stateID func(dfaDeterministicState) (uint32, error)) (CompiledModelRow, error) {
	var row CompiledModelRow
	groups := make(map[particleTermKey]*dfaTransitionSet)
	for _, config := range state.Configs {
		if err := b.collectDeterministicConfig(&row, groups, config, caps); err != nil {
			return CompiledModelRow{}, err
		}
	}
	if err := b.c.work.spend(len(groups)); err != nil {
		return CompiledModelRow{}, err
	}
	keys := sortedParticleTermKeys(groups)
	for _, key := range keys {
		if err := b.appendDeterministicTransition(&row, groups[key], stateID); err != nil {
			return CompiledModelRow{}, err
		}
	}
	return row, nil
}

func (b *dfaBuilder) collectDeterministicConfig(row *CompiledModelRow, groups map[particleTermKey]*dfaTransitionSet, config dfaConfig, caps []uint32) error {
	if !ValidUint32Index(config.State, len(b.rows)) {
		return xsderrors.InternalInvariant("content model DFA state out of range")
	}
	source := b.rows[config.State]
	if err := b.c.work.spend(len(source.Accept) + len(source.Edges)); err != nil {
		return err
	}
	if err := b.collectDeterministicAccept(row, source.Accept, config, caps); err != nil {
		return err
	}
	return b.collectDeterministicEdges(groups, source.Edges, config, caps)
}

func (b *dfaBuilder) collectDeterministicAccept(row *CompiledModelRow, accepts []dfaAccept, config dfaConfig, caps []uint32) error {
	for _, accept := range accepts {
		if err := b.c.work.spend(len(accept.Guards)); err != nil {
			return err
		}
		if dfaGuardsOK(config.Counters, caps, accept.Guards) {
			row.Accept = true
			break
		}
	}
	return nil
}

func (b *dfaBuilder) collectDeterministicEdges(groups map[particleTermKey]*dfaTransitionSet, edges []dfaSourceEdge, config dfaConfig, caps []uint32) error {
	for _, edge := range edges {
		if err := b.collectDeterministicEdge(groups, edge, config, caps); err != nil {
			return err
		}
	}
	return nil
}

func (b *dfaBuilder) collectDeterministicEdge(groups map[particleTermKey]*dfaTransitionSet, edge dfaSourceEdge, config dfaConfig, caps []uint32) error {
	if err := b.c.work.spend(len(edge.Guards)); err != nil {
		return err
	}
	if !dfaGuardsOK(config.Counters, caps, edge.Guards) {
		return nil
	}
	if err := b.c.work.spend(len(config.Counters) + len(edge.Actions) + 1); err != nil {
		return err
	}
	counters, err := applyDFAActions(config.Counters, caps, edge.Actions)
	if err != nil {
		return err
	}
	key := particleTermKeyOf(edge.Particle)
	group := groups[key]
	if group == nil {
		group = &dfaTransitionSet{Particle: edge.Particle}
		groups[key] = group
	}
	group.Configs = append(group.Configs, dfaConfig{State: edge.To, Counters: counters})
	return nil
}

func sortedParticleTermKeys(groups map[particleTermKey]*dfaTransitionSet) []particleTermKey {
	keys := make([]particleTermKey, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, compareParticleTermKey)
	return keys
}

func (b *dfaBuilder) appendDeterministicTransition(row *CompiledModelRow, group *dfaTransitionSet, stateID func(dfaDeterministicState) (uint32, error)) error {
	if err := b.spendDFAConfigs(group.Configs); err != nil {
		return err
	}
	group.Configs = normalizeDFAConfigs(group.Configs)
	if len(group.Configs) > b.limit {
		return xsderrors.SchemaCompile(xsderrors.CodeSchemaLimit, "content model DFA state limit exceeded")
	}
	to, err := stateID(dfaDeterministicState{Configs: group.Configs})
	if err != nil {
		return err
	}
	row.Edges = append(row.Edges, CompiledModelEdge{Particle: group.Particle, To: to})
	return nil
}

func (b *dfaBuilder) counterCaps() ([]uint32, error) {
	if err := b.c.work.spend(int(b.counters) + len(b.rows) + 1); err != nil {
		return nil, err
	}
	caps := make([]uint32, b.counters)
	for _, row := range b.rows {
		if err := b.addRowCounterCaps(caps, row); err != nil {
			return nil, err
		}
	}
	return caps, nil
}

func (b *dfaBuilder) addRowCounterCaps(caps []uint32, row dfaSourceRow) error {
	for _, edge := range row.Edges {
		if err := b.c.work.spend(len(edge.Guards) + 1); err != nil {
			return err
		}
		addCounterCaps(caps, edge.Guards)
	}
	for _, accept := range row.Accept {
		if err := b.c.work.spend(len(accept.Guards) + 1); err != nil {
			return err
		}
		addCounterCaps(caps, accept.Guards)
	}
	return nil
}

func (b *dfaBuilder) spendDFAConfigs(configs []dfaConfig) error {
	if err := b.c.work.spend(len(configs) + 1); err != nil {
		return err
	}
	for _, config := range configs {
		if err := b.c.work.spend(len(config.Counters)); err != nil {
			return err
		}
	}
	return nil
}

func addCounterCaps(caps []uint32, guards []compiledGuard) {
	for _, guard := range guards {
		if !ValidUint32Index(guard.Slot, len(caps)) || guard.N == 0 {
			continue
		}
		caps[guard.Slot] = max(caps[guard.Slot], guard.N-1)
	}
}

func dfaGuardsOK(counters, caps []uint32, guards []compiledGuard) bool {
	for _, guard := range guards {
		if !dfaGuardOK(counters, caps, guard) {
			return false
		}
	}
	return true
}

func dfaGuardOK(counters, caps []uint32, guard compiledGuard) bool {
	if !ValidUint32Index(guard.Slot, len(counters)) || !ValidUint32Index(guard.Slot, len(caps)) {
		return false
	}
	count := counters[guard.Slot]
	switch guard.Kind {
	case compiledGuardExitMin:
		return guard.N == 0 || count >= guard.N-1
	case compiledGuardLoopMax:
		return guard.N > 0 && count < guard.N-1
	default:
		return false
	}
}

func applyDFAActions(counters, caps []uint32, actions []compiledAction) ([]uint32, error) {
	out := slices.Clone(counters)
	for _, action := range actions {
		if !ValidUint32Index(action.Slot, len(out)) {
			return nil, xsderrors.InternalInvariant("content model DFA action references invalid counter")
		}
		switch action.Kind {
		case compiledActionInc:
			if out[action.Slot] < caps[action.Slot] {
				out[action.Slot]++
			}
		case compiledActionReset:
			out[action.Slot] = 0
		default:
			return nil, xsderrors.InternalInvariant("content model DFA action kind out of range")
		}
	}
	return out, nil
}

// normalizeDFAConfigs canonicalizes DFA state keys before map lookup.
func normalizeDFAConfigs(configs []dfaConfig) []dfaConfig {
	configs = slices.Clone(configs)
	slices.SortFunc(configs, compareDFAConfig)
	return slices.CompactFunc(configs, func(a, b dfaConfig) bool {
		return compareDFAConfig(a, b) == 0
	})
}

func compareDFAConfig(a, b dfaConfig) int {
	if n := cmp.Compare(a.State, b.State); n != 0 {
		return n
	}
	if n := cmp.Compare(len(a.Counters), len(b.Counters)); n != 0 {
		return n
	}
	for i := range a.Counters {
		if n := cmp.Compare(a.Counters[i], b.Counters[i]); n != 0 {
			return n
		}
	}
	return 0
}

func dfaConfigStateKey(configs []dfaConfig) string {
	var b []byte
	for _, config := range configs {
		b = append(b, 's')
		b = strconv.AppendUint(b, uint64(config.State), 10)
		b = append(b, ':')
		for _, counter := range config.Counters {
			b = strconv.AppendUint(b, uint64(counter), 10)
			b = append(b, ',')
		}
		b = append(b, ';')
	}
	return string(b)
}

func particleTermKeyOf(p Particle) particleTermKey {
	switch p.Kind {
	case ParticleElement:
		return particleTermKey{Kind: p.Kind, Element: p.Element}
	case ParticleWildcard:
		return particleTermKey{Kind: p.Kind, Wildcard: p.Wildcard}
	case ParticleModel:
		return particleTermKey{Kind: p.Kind, Element: p.Element, Wildcard: p.Wildcard}
	default:
	}
	return particleTermKey{Kind: p.Kind, Element: p.Element, Wildcard: p.Wildcard}
}

func compareParticleTermKey(a, b particleTermKey) int {
	if n := cmp.Compare(a.Kind, b.Kind); n != 0 {
		return n
	}
	if n := cmp.Compare(a.Element, b.Element); n != 0 {
		return n
	}
	return cmp.Compare(a.Wildcard, b.Wildcard)
}

func countingException(a, b dfaSourceEdge) bool {
	return complementaryCountingGuards(a.Guards, b.Guards) || complementaryCountingGuards(b.Guards, a.Guards)
}

func complementaryCountingGuards(loopEdge, exitEdge []compiledGuard) bool {
	for _, loop := range loopEdge {
		if loop.Kind != compiledGuardLoopMax {
			continue
		}
		for _, exit := range exitEdge {
			if exit.Kind == compiledGuardExitMin && exit.Slot == loop.Slot && exit.N == loop.N {
				return true
			}
		}
	}
	return false
}

func composeEntry(guards []compiledGuard, actions []compiledAction, entry dfaEntry) dfaEntry {
	return dfaEntry{
		Pos:     entry.Pos,
		Guards:  appendGuards(guards, entry.Guards),
		Actions: appendActions(actions, entry.Actions),
	}
}

func appendGuards(a, b []compiledGuard) []compiledGuard {
	out := slices.Clone(a)
	out = append(out, b...)
	slices.SortFunc(out, compareCompiledGuard)
	return slices.Compact(out)
}

func appendActions(a, b []compiledAction) []compiledAction {
	out := slices.Clone(a)
	out = append(out, b...)
	return out
}

func resetActions(counters []uint32, except uint32) []compiledAction {
	var out []compiledAction
	for _, slot := range counters {
		if slot == except {
			continue
		}
		out = append(out, compiledAction{Slot: slot, Kind: compiledActionReset})
	}
	return out
}

func mergeCounters(a, b []uint32) []uint32 {
	out := slices.Clone(a)
	out = append(out, b...)
	slices.Sort(out)
	return slices.Compact(out)
}

func normalizeDFAEntries(entries []dfaEntry) []dfaEntry {
	entries = slices.Clone(entries)
	slices.SortFunc(entries, compareDFAEntry)
	return slices.CompactFunc(entries, func(a, b dfaEntry) bool {
		return compareDFAEntry(a, b) == 0
	})
}

func compareDFAEntry(a, b dfaEntry) int {
	if n := cmp.Compare(a.Pos, b.Pos); n != 0 {
		return n
	}
	if n := compareCompiledGuards(a.Guards, b.Guards); n != 0 {
		return n
	}
	return compareCompiledActions(a.Actions, b.Actions)
}

func compareCompiledGuards(a, b []compiledGuard) int {
	if n := cmp.Compare(len(a), len(b)); n != 0 {
		return n
	}
	for i := range a {
		if n := compareCompiledGuard(a[i], b[i]); n != 0 {
			return n
		}
	}
	return 0
}

func compareCompiledGuard(a, b compiledGuard) int {
	if n := cmp.Compare(a.Slot, b.Slot); n != 0 {
		return n
	}
	if n := cmp.Compare(a.Kind, b.Kind); n != 0 {
		return n
	}
	return cmp.Compare(a.N, b.N)
}

func compareCompiledActions(a, b []compiledAction) int {
	if n := cmp.Compare(len(a), len(b)); n != 0 {
		return n
	}
	for i := range a {
		if n := cmp.Compare(a[i].Slot, b[i].Slot); n != 0 {
			return n
		}
		if n := cmp.Compare(a[i].Kind, b[i].Kind); n != 0 {
			return n
		}
	}
	return 0
}

func dfaStateKey(entries []dfaEntry) string {
	var b []byte
	for _, e := range entries {
		b = strconv.AppendInt(b, int64(e.Pos), 10)
		b = append(b, ':')
		for _, g := range e.Guards {
			b = append(b, 'g')
			b = strconv.AppendUint(b, uint64(g.Slot), 10)
			b = append(b, '/')
			b = strconv.AppendUint(b, uint64(g.Kind), 10)
			b = append(b, '/')
			b = strconv.AppendUint(b, uint64(g.N), 10)
			b = append(b, ',')
		}
		b = append(b, ':')
		for _, a := range e.Actions {
			b = append(b, 'a')
			b = strconv.AppendUint(b, uint64(a.Slot), 10)
			b = append(b, '/')
			b = strconv.AppendUint(b, uint64(a.Kind), 10)
			b = append(b, ',')
		}
		b = append(b, ';')
	}
	return string(b)
}
