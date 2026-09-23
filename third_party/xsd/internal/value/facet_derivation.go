package value

import "slices"

// validateFacetSource checks the facet families declared by one type before
// typed facet literals are compiled.  Type shape is the source of truth for
// facet applicability; the effective program is checked separately after
// inherited facet values are available.
func validateFacetSource(spec typeDef, facets FacetSpec, own facetProgram) error {
	if err := validateFacetShape(spec, own); err != nil {
		return facetFailure(err.Error())
	}
	for _, entry := range []struct {
		flag    FacetMask
		present bool
	}{
		{FacetLength, facets.Length.Present},
		{FacetMinLength, facets.MinLength.Present},
		{FacetMaxLength, facets.MaxLength.Present},
		{FacetTotalDigits, facets.TotalDigits.Present},
		{FacetFractionDigits, facets.FractionDigits.Present},
	} {
		if own.present&entry.flag != 0 && !entry.present {
			return ErrMetadata
		}
	}
	if !facetMaskAllowed(spec.variety, spec.primitive, own.present) {
		return facetFailure("facet is not allowed for simple type")
	}
	if facets.Fixed&(FacetPattern|FacetEnumeration) != 0 {
		return facetFailure("pattern and enumeration facets cannot be fixed")
	}
	return validateFacetCardinalityShape(own)
}

func facetMaskAllowed(variety Variety, primitive PrimitiveKind, mask FacetMask) bool {
	for bit := FacetLength; bit <= FacetWhiteSpace; bit <<= 1 {
		if mask&bit != 0 && !FacetAllowed(variety, primitive, bit) {
			return false
		}
		mask &^= bit
	}
	return mask == 0
}

// FacetAllowed reports whether one facet is permitted for a simple-type variety and primitive.
func FacetAllowed(variety Variety, primitive PrimitiveKind, bit FacetMask) bool {
	switch variety {
	case Atomic:
		return atomicFacetBitAllowed(primitive, bit)
	case List:
		return listFacetBitAllowed(bit)
	case Union:
		return bit == FacetPattern || bit == FacetEnumeration
	default:
		return false
	}
}

func atomicFacetBitAllowed(primitive PrimitiveKind, bit FacetMask) bool {
	//nolint:exhaustive // FacetMask is a bitmask; composite masks are not individual cases.
	switch bit {
	case FacetPattern, FacetEnumeration, FacetWhiteSpace:
		return true
	case FacetLength, FacetMinLength, FacetMaxLength:
		return primitiveHasLengthFacet(primitive)
	case FacetMinInclusive, FacetMaxInclusive, FacetMinExclusive, FacetMaxExclusive:
		return primitiveHasOrderFacet(primitive)
	case FacetTotalDigits, FacetFractionDigits:
		return primitive == PrimitiveDecimal
	default:
		return false
	}
}

func listFacetBitAllowed(bit FacetMask) bool {
	return bit == FacetLength || bit == FacetMinLength || bit == FacetMaxLength ||
		bit == FacetPattern || bit == FacetEnumeration || bit == FacetWhiteSpace
}

func primitiveHasPartialOrder(kind PrimitiveKind) bool {
	//nolint:exhaustive // Only temporal primitives have partial orders.
	switch kind {
	case PrimitiveDuration, PrimitiveDateTime, PrimitiveTime, PrimitiveDate,
		PrimitiveGYearMonth, PrimitiveGYear, PrimitiveGMonthDay, PrimitiveGDay, PrimitiveGMonth:
		return true
	default:
		return false
	}
}

// validateFacetDerivation owns the restriction contract between a type and
// its direct base. Source shape has already been validated before typed facet
// values are compiled; keeping this step focused on inheritance avoids
// repeating that validation while preserving its original error ordering.
func (p *Program) validateFacetDerivation(id TypeID, own facetProgram) error {
	derived, ok := p.typeDef(id)
	if !ok {
		return ErrMetadata
	}
	if derived.base == NoType {
		return nil
	}
	base, ok := p.typeDef(derived.base)
	if !ok {
		return ErrMetadata
	}
	return validateInheritedFacetDerivation(*derived, *base, own)
}

func validateInheritedFacetDerivation(derived, base typeDef, own facetProgram) error {
	if err := validateTypeDerivation(derived.base, derived, base); err != nil {
		return err
	}
	if err := validateWhitespaceDerivation(derived.whitespace, base.whitespace); err != nil {
		return err
	}
	if err := validateDeclaredCardinalityRestriction(own, base.facets); err != nil {
		return err
	}
	if err := validateDeclaredOrderedRestriction(own, base.facets); err != nil {
		return err
	}
	if err := validateEnumerationRestriction(own, base.facets); err != nil {
		return err
	}
	candidate := mergeFacetPrograms(base.facets, own)
	if err := validateFacetCardinalityShape(candidate); err != nil {
		return err
	}
	if err := validateLengthAncestry(candidate, base.facets); err != nil {
		return err
	}
	return validateFixedFacetDerivation(derived, own, candidate, base)
}

// anySimpleType is the one XSD base whose restriction may choose the variety
// and primitive family. Once a list, union, or atomic type exists, restriction
// preserves its semantic value space and item/member structure.
func validateTypeDerivation(baseID TypeID, derived, base typeDef) error {
	// anySimpleType is the only XSD base whose restriction may choose a new
	// variety and primitive family. Its lexical policy is intentionally broad.
	if baseID == builtinAnySimpleType {
		return nil
	}
	if derived.variety != base.variety || derived.primitive != base.primitive || derived.builtin != base.builtin || derived.identity != base.identity {
		return facetFailure("simple type semantic fields do not match base restriction")
	}
	return validateTypeShape(derived, base)
}

func validateTypeShape(derived, base typeDef) error {
	switch base.variety {
	case List:
		if derived.listItem != base.listItem {
			return facetFailure("simple type list item does not match base restriction")
		}
	case Union:
		if !slices.Equal(derived.union, base.union) {
			return facetFailure("simple type union members do not match base restriction")
		}
	case Atomic:
		if derived.listItem != NoType || len(derived.union) != 0 {
			return facetFailure("atomic type shape does not match base restriction")
		}
	default:
		return ErrMetadata
	}
	return nil
}

func validateWhitespaceDerivation(derived, base WhitespaceMode) error {
	if derived < base {
		return facetFailure("whiteSpace cannot loosen base restriction")
	}
	return nil
}

func validateFacetCardinalityShape(f facetProgram) error {
	if f.length.Present && f.minLength.Present && f.length.Value < f.minLength.Value {
		return facetFailure("length cannot be less than minLength")
	}
	if f.length.Present && f.maxLength.Present && f.length.Value > f.maxLength.Value {
		return facetFailure("length cannot exceed maxLength")
	}
	if f.minLength.Present && f.maxLength.Present && f.minLength.Value > f.maxLength.Value {
		return facetFailure("minLength cannot exceed maxLength")
	}
	if f.totalDigits.Present && f.fractionDigits.Present && f.fractionDigits.Value > f.totalDigits.Value {
		return facetFailure("fractionDigits cannot exceed totalDigits")
	}
	return nil
}

func validateDeclaredCardinalityRestriction(derived, base facetProgram) error {
	if derived.length.Present && base.length.Present && derived.length.Value != base.length.Value {
		return facetFailure("length must equal base length")
	}
	if derived.minLength.Present && base.minLength.Present && derived.minLength.Value < base.minLength.Value {
		return facetFailure("minLength cannot be less than base minLength")
	}
	if derived.maxLength.Present && base.maxLength.Present && derived.maxLength.Value > base.maxLength.Value {
		return facetFailure("maxLength cannot exceed base maxLength")
	}
	if derived.totalDigits.Present && base.totalDigits.Present && derived.totalDigits.Value > base.totalDigits.Value {
		return facetFailure("totalDigits cannot exceed base totalDigits")
	}
	if derived.fractionDigits.Present && base.fractionDigits.Present && derived.fractionDigits.Value > base.fractionDigits.Value {
		return facetFailure("fractionDigits cannot exceed base fractionDigits")
	}
	return nil
}

func validateLengthAncestry(candidate, base facetProgram) error {
	if !candidate.length.Present {
		return nil
	}
	if candidate.minLength.Present && !sameCardinality(candidate.minLength, base.minLength) {
		return facetFailure("length requires an inherited minLength with the same value")
	}
	if candidate.maxLength.Present && !sameCardinality(candidate.maxLength, base.maxLength) {
		return facetFailure("length requires an inherited maxLength with the same value")
	}
	return nil
}

func sameCardinality(a, b CardinalityFacet) bool {
	return a.Present == b.Present && (!a.Present || a.Value == b.Value)
}

func validateDeclaredOrderedRestriction(derived, base facetProgram) error {
	if err := validateLowerBoundRestrictions(derived.lower, base.lower); err != nil {
		return err
	}
	return validateUpperBoundRestrictions(derived.upper, base.upper)
}

func validateLowerBoundRestrictions(derived, base []boundValue) error {
	for _, bound := range derived {
		for _, inherited := range base {
			if !orderedLowerRestricts(bound, inherited) {
				return facetFailure("lower ordered facet cannot loosen base restriction")
			}
		}
	}
	return nil
}

func validateUpperBoundRestrictions(derived, base []boundValue) error {
	for _, bound := range derived {
		for _, inherited := range base {
			if !orderedUpperRestricts(bound, inherited) {
				return facetFailure("upper ordered facet cannot loosen base restriction")
			}
		}
	}
	return nil
}

func validateEnumerationRestriction(derived, base facetProgram) error {
	if len(derived.enumGroups) == 0 || len(base.enumGroups) == 0 {
		return nil
	}
	for _, literal := range derived.enumGroups[0] {
		if !enumerationInGroups(literal, base.enumGroups) {
			return facetFailure("enumeration facet cannot loosen base restriction")
		}
	}
	return nil
}

func enumerationInGroups(literal parsedValue, groups [][]parsedValue) bool {
	for _, group := range groups {
		matched := false
		for _, candidate := range group {
			if equalParsed(&literal, &candidate) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func orderedLowerRestricts(derived, base boundValue) bool {
	relation, ok := compareDerivationBounds(derived, base)
	if !ok {
		return false
	}
	return OrderedFacetLowerRestricts(boundKind(derived), boundKind(base), relation)
}

func orderedUpperRestricts(derived, base boundValue) bool {
	relation, ok := compareDerivationBounds(derived, base)
	if !ok {
		return false
	}
	return OrderedFacetUpperRestricts(boundKind(derived), boundKind(base), relation)
}

func compareDerivationBounds(derived, base boundValue) (OrderedFacetRelation, bool) {
	if derived.value.atom.kind != PrimitiveTime || base.value.atom.kind != PrimitiveTime {
		return compareValue(&derived.value, &base.value)
	}
	left := rawTimeProjection(derived)
	right := rawTimeProjection(base)
	return CompareTimePartial(left, right), true
}

func rawTimeProjection(bound boundValue) TimeValue {
	time := bound.value.atom.time
	time.second += int(bound.timeDayOffset) * daySeconds
	return time
}

func boundKind(bound boundValue) OrderedFacetBound {
	if bound.exclusive {
		return OrderedFacetBound{Kind: OrderedFacetBoundExclusive}
	}
	return OrderedFacetBound{Kind: OrderedFacetBoundInclusive}
}

func validateFixedFacetDerivation(derived typeDef, own, candidate facetProgram, base typeDef) error {
	fixed := base.facets.fixed
	if fixed&^fixedFacetMask != 0 {
		return ErrMetadata
	}
	if fixed&FacetWhiteSpace != 0 && derived.whitespace != base.whitespace {
		return facetFailure("fixed whiteSpace facet cannot change")
	}
	if err := validateFixedCardinality(fixed, candidate, base.facets); err != nil {
		return err
	}
	return validateFixedBounds(fixed, own, base.facets)
}

func validateFixedCardinality(fixed FacetMask, candidate, base facetProgram) error {
	for _, entry := range []struct {
		flag FacetMask
		base CardinalityFacet
		got  CardinalityFacet
	}{
		{FacetLength, base.length, candidate.length},
		{FacetMinLength, base.minLength, candidate.minLength},
		{FacetMaxLength, base.maxLength, candidate.maxLength},
		{FacetTotalDigits, base.totalDigits, candidate.totalDigits},
		{FacetFractionDigits, base.fractionDigits, candidate.fractionDigits},
	} {
		if fixed&entry.flag != 0 && !sameCardinality(entry.got, entry.base) {
			return facetFailure("fixed cardinality facet cannot change")
		}
	}
	return nil
}

func validateFixedBounds(fixed FacetMask, own, base facetProgram) error {
	for _, entry := range []struct {
		own  []boundValue
		base []boundValue
		flag FacetMask
	}{
		{own: own.lower, base: base.lower, flag: FacetMinInclusive},
		{own: own.lower, base: base.lower, flag: FacetMinExclusive},
		{own: own.upper, base: base.upper, flag: FacetMaxInclusive},
		{own: own.upper, base: base.upper, flag: FacetMaxExclusive},
	} {
		if err := validateFixedBound(fixed, entry.flag, entry.own, entry.base); err != nil {
			return err
		}
	}
	return nil
}

func validateFixedBound(fixed FacetMask, flag FacetMask, own, base []boundValue) error {
	if fixed&flag == 0 {
		return nil
	}
	boundKind := OrderedFacetBoundInclusive
	if flag == FacetMinExclusive || flag == FacetMaxExclusive {
		boundKind = OrderedFacetBoundExclusive
	}
	baseBound, ok := findBound(base, boundKind)
	if !ok {
		return ErrMetadata
	}
	declared, hasDeclared := findBound(own, boundKind)
	if hasDeclared && !equalParsed(&declared.value, &baseBound.value) {
		return facetFailure("fixed ordered facet cannot change")
	}
	return nil
}

func findBound(bounds []boundValue, kind OrderedFacetBoundKind) (boundValue, bool) {
	// The effective program retains every inherited bound so partial-order
	// restrictions can be checked against the complete ancestry. A fixed facet
	// names the effective value from the nearest declaration, which is the last
	// bound of that kind in declaration order.
	for _, bound := range slices.Backward(bounds) {
		if boundKind(bound).Kind == kind {
			return bound, true
		}
	}
	return boundValue{}, false
}

const fixedFacetMask = FacetLength |
	FacetMinLength |
	FacetMaxLength |
	FacetTotalDigits |
	FacetFractionDigits |
	FacetMinInclusive |
	FacetMaxInclusive |
	FacetMinExclusive |
	FacetMaxExclusive |
	FacetWhiteSpace
