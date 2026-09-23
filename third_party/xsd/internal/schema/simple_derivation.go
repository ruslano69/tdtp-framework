package schema

import (
	"github.com/jacoelho/xsd/xsderrors"
)

// SimpleTypeFinalRole identifies the schema component role that is applying a
// simple-type final derivation rule.
type SimpleTypeFinalRole uint8

const (
	// SimpleTypeFinalBaseRestriction checks a restriction base simple type.
	SimpleTypeFinalBaseRestriction SimpleTypeFinalRole = iota
	// SimpleTypeFinalListItem checks an xs:list item type.
	SimpleTypeFinalListItem
	// SimpleTypeFinalUnionMember checks an xs:union member type.
	SimpleTypeFinalUnionMember
)

// CheckSimpleRestrictionBase rejects direct restriction of xs:anySimpleType.
func CheckSimpleRestrictionBase(baseID, anySimpleType SimpleTypeID) error {
	if baseID == anySimpleType {
		return xsderrors.SchemaCompile(xsderrors.CodeSchemaReference, "simple type cannot restrict xs:anySimpleType")
	}
	return nil
}

// CheckSimpleTypeFinalAllows maps runtime simple-type final-mask rejection into
// the compile diagnostic for the schema role being derived.
func CheckSimpleTypeFinalAllows(final, derivation DerivationMask, role SimpleTypeFinalRole) error {
	if err := ValidateSimpleTypeFinalAllows(final, derivation); err != nil {
		return xsderrors.SchemaCompile(xsderrors.CodeSchemaReference, simpleTypeFinalRoleMessage(role))
	}
	return nil
}

func simpleListItemListReachError() error {
	return xsderrors.SchemaCompile(xsderrors.CodeSchemaContentModel, "list item type cannot be a list type")
}

type simpleTypeListReachState uint8

const (
	simpleTypeListReachUnchecked simpleTypeListReachState = iota
	simpleTypeListReachChecking
	simpleTypeListReachChecked
)

// simpleTypeListReachability memoizes reachability for the compiler's
// append-only table of completed simple types.
type simpleTypeListReachability struct {
	state   []simpleTypeListReachState
	reaches []bool
	stack   []simpleTypeListReachFrame
}

type simpleTypeListReachFrame struct {
	next     int
	id       SimpleTypeID
	unstable bool
}

func (r *simpleTypeListReachability) reachesList(types []SimpleType, id SimpleTypeID) bool {
	if !ValidSimpleTypeID(id, len(types)) {
		return false
	}
	r.ensureCapacity(len(types))
	switch r.state[id] {
	case simpleTypeListReachChecked:
		return r.reaches[id]
	case simpleTypeListReachChecking:
		return false
	case simpleTypeListReachUnchecked:
	}
	audit := simpleTypeListReachAudit{owner: r, types: types, stack: r.stack[:0]}
	r.state[id] = simpleTypeListReachChecking
	audit.stack = appendSimpleTypeListReachFrame(audit.stack, simpleTypeListReachFrame{id: id}, len(types))
	reaches := audit.run()
	r.stack = audit.stack[:0]
	return reaches
}

func (r *simpleTypeListReachability) ensureCapacity(typeCount int) {
	if missing := typeCount - len(r.state); missing > 0 {
		r.state = append(r.state, make([]simpleTypeListReachState, missing)...)
		r.reaches = append(r.reaches, make([]bool, missing)...)
	}
}

type simpleTypeListReachAudit struct {
	owner *simpleTypeListReachability
	types []SimpleType
	stack []simpleTypeListReachFrame
}

func (a *simpleTypeListReachAudit) run() bool {
	for len(a.stack) != 0 {
		if a.advance() {
			return true
		}
	}
	return false
}

func (a *simpleTypeListReachAudit) advance() bool {
	last := len(a.stack) - 1
	frame := &a.stack[last]
	typ := a.types[frame.id].ValueSpec
	if typ.Variety == SimpleVarietyList {
		a.markReached()
		return true
	}
	if typ.Variety != SimpleVarietyUnion || frame.next == len(typ.Union) {
		a.complete(last)
		return false
	}
	return a.visitMember(frame, typ.Union[frame.next])
}

func (a *simpleTypeListReachAudit) complete(last int) {
	frame := a.stack[last]
	if frame.unstable {
		a.owner.state[frame.id] = simpleTypeListReachUnchecked
	} else {
		a.owner.state[frame.id] = simpleTypeListReachChecked
	}
	a.stack = a.stack[:last]
	if frame.unstable && len(a.stack) != 0 {
		a.stack[len(a.stack)-1].unstable = true
	}
}

func (a *simpleTypeListReachAudit) visitMember(frame *simpleTypeListReachFrame, member SimpleTypeID) bool {
	frame.next++
	if !ValidSimpleTypeID(member, len(a.types)) {
		return false
	}
	switch a.owner.state[member] {
	case simpleTypeListReachChecked:
		if a.owner.reaches[member] {
			a.markReached()
			return true
		}
	case simpleTypeListReachChecking:
		frame.unstable = true
	case simpleTypeListReachUnchecked:
		a.owner.state[member] = simpleTypeListReachChecking
		a.stack = appendSimpleTypeListReachFrame(a.stack, simpleTypeListReachFrame{id: member}, len(a.types))
	}
	return false
}

func (a *simpleTypeListReachAudit) markReached() {
	for _, active := range a.stack {
		a.owner.reaches[active.id] = true
		a.owner.state[active.id] = simpleTypeListReachChecked
	}
	a.stack = a.stack[:0]
}

func appendSimpleTypeListReachFrame(
	stack []simpleTypeListReachFrame,
	frame simpleTypeListReachFrame,
	limit int,
) []simpleTypeListReachFrame {
	if len(stack) < cap(stack) {
		return append(stack, frame)
	}
	if len(stack) >= limit {
		panic("simple type list reachability stack exceeds type count")
	}
	capacity := min(limit, max(1, cap(stack)*2))
	grown := make([]simpleTypeListReachFrame, len(stack), capacity)
	copy(grown, stack)
	return append(grown, frame)
}

func simpleTypeFinalRoleMessage(role SimpleTypeFinalRole) string {
	switch role {
	case SimpleTypeFinalBaseRestriction:
		return "base simple type final blocks restriction"
	case SimpleTypeFinalListItem:
		return "item simple type final blocks list"
	case SimpleTypeFinalUnionMember:
		return "member simple type final blocks union"
	default:
		return "simple type final blocks derivation"
	}
}
