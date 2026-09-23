package value

// The builtin table is fixed XSD metadata. It is independent of any compiler
// or runtime allocation, so every program observes the same IDs and derived
// chains without copying builtin records into its user table.
const (
	builtinAnySimpleType TypeID = iota
	builtinString
	builtinNormalizedString
	builtinToken
	builtinLanguage
	builtinName
	builtinNCName
	builtinBoolean
	builtinDecimal
	builtinInteger
	builtinNonPositiveInteger
	builtinNegativeInteger
	builtinNonNegativeInteger
	builtinPositiveInteger
	builtinLong
	builtinInt
	builtinShort
	builtinByte
	builtinUnsignedLong
	builtinUnsignedInt
	builtinUnsignedShort
	builtinUnsignedByte
	builtinFloat
	builtinDouble
	builtinDuration
	builtinDate
	builtinDateTime
	builtinTime
	builtinGYearMonth
	builtinGYear
	builtinGMonthDay
	builtinGDay
	builtinGMonth
	builtinAnyURI
	builtinHexBinary
	builtinBase64Binary
	builtinQName
	builtinNotation
	builtinID
	builtinIDREF
	builtinIDREFS
	builtinNMTOKEN
	builtinNMTOKENS
	builtinENTITY
	builtinENTITIES
	builtinXMLLang
	builtinXMLSpace
)

// BuiltinTypeCount is the fixed number of XSD and XML-internal simple types.
const BuiltinTypeCount = builtinXMLSpace + 1

type builtinMeta struct {
	name       string
	min        string
	max        string
	base       TypeID
	listItem   TypeID
	minLength  uint32
	variety    Variety
	primitive  PrimitiveKind
	whitespace WhitespaceMode
	builtin    BuiltinKind
	identity   IdentityKind
	fraction   bool
}

var builtinMetadata = [...]builtinMeta{
	{name: "anySimpleType", base: NoType, primitive: PrimitiveString, whitespace: WhitespacePreserve},
	{name: "string", base: builtinAnySimpleType, primitive: PrimitiveString, whitespace: WhitespacePreserve},
	{name: "normalizedString", base: builtinString, primitive: PrimitiveString, whitespace: WhitespaceReplace},
	{name: "token", base: builtinNormalizedString, primitive: PrimitiveString, whitespace: WhitespaceCollapse},
	{name: "language", base: builtinToken, primitive: PrimitiveString, whitespace: WhitespaceCollapse, builtin: BuiltinLanguage},
	{name: "Name", base: builtinToken, primitive: PrimitiveString, whitespace: WhitespaceCollapse, builtin: BuiltinName},
	{name: "NCName", base: builtinName, primitive: PrimitiveString, whitespace: WhitespaceCollapse, builtin: BuiltinNCName},
	{name: "boolean", base: builtinAnySimpleType, primitive: PrimitiveBoolean, whitespace: WhitespaceCollapse},
	{name: "decimal", base: builtinAnySimpleType, primitive: PrimitiveDecimal, whitespace: WhitespaceCollapse},
	{name: "integer", base: builtinDecimal, primitive: PrimitiveDecimal, whitespace: WhitespaceCollapse, builtin: BuiltinInteger, fraction: true},
	{name: "nonPositiveInteger", base: builtinInteger, primitive: PrimitiveDecimal, whitespace: WhitespaceCollapse, builtin: BuiltinInteger, max: "0", fraction: true},
	{name: "negativeInteger", base: builtinNonPositiveInteger, primitive: PrimitiveDecimal, whitespace: WhitespaceCollapse, builtin: BuiltinInteger, max: "-1", fraction: true},
	{name: "nonNegativeInteger", base: builtinInteger, primitive: PrimitiveDecimal, whitespace: WhitespaceCollapse, builtin: BuiltinInteger, min: "0", fraction: true},
	{name: "positiveInteger", base: builtinNonNegativeInteger, primitive: PrimitiveDecimal, whitespace: WhitespaceCollapse, min: "1", fraction: true, builtin: BuiltinInteger},
	{name: "long", base: builtinInteger, primitive: PrimitiveDecimal, whitespace: WhitespaceCollapse, min: "-9223372036854775808", max: "9223372036854775807", fraction: true, builtin: BuiltinInteger},
	{name: "int", base: builtinLong, primitive: PrimitiveDecimal, whitespace: WhitespaceCollapse, min: "-2147483648", max: "2147483647", fraction: true, builtin: BuiltinInteger},
	{name: "short", base: builtinInt, primitive: PrimitiveDecimal, whitespace: WhitespaceCollapse, min: "-32768", max: "32767", fraction: true, builtin: BuiltinInteger},
	{name: "byte", base: builtinShort, primitive: PrimitiveDecimal, whitespace: WhitespaceCollapse, min: "-128", max: "127", fraction: true, builtin: BuiltinInteger},
	{name: "unsignedLong", base: builtinNonNegativeInteger, primitive: PrimitiveDecimal, whitespace: WhitespaceCollapse, min: "0", max: "18446744073709551615", fraction: true, builtin: BuiltinInteger},
	{name: "unsignedInt", base: builtinUnsignedLong, primitive: PrimitiveDecimal, whitespace: WhitespaceCollapse, min: "0", max: "4294967295", fraction: true, builtin: BuiltinInteger},
	{name: "unsignedShort", base: builtinUnsignedInt, primitive: PrimitiveDecimal, whitespace: WhitespaceCollapse, min: "0", max: "65535", fraction: true, builtin: BuiltinInteger},
	{name: "unsignedByte", base: builtinUnsignedShort, primitive: PrimitiveDecimal, whitespace: WhitespaceCollapse, min: "0", max: "255", fraction: true, builtin: BuiltinInteger},
	{name: "float", base: builtinAnySimpleType, primitive: PrimitiveFloat, whitespace: WhitespaceCollapse},
	{name: "double", base: builtinAnySimpleType, primitive: PrimitiveDouble, whitespace: WhitespaceCollapse},
	{name: "duration", base: builtinAnySimpleType, primitive: PrimitiveDuration, whitespace: WhitespaceCollapse},
	{name: dateTypeName, base: builtinAnySimpleType, primitive: PrimitiveDate, whitespace: WhitespaceCollapse},
	{name: "dateTime", base: builtinAnySimpleType, primitive: PrimitiveDateTime, whitespace: WhitespaceCollapse},
	{name: "time", base: builtinAnySimpleType, primitive: PrimitiveTime, whitespace: WhitespaceCollapse},
	{name: "gYearMonth", base: builtinAnySimpleType, primitive: PrimitiveGYearMonth, whitespace: WhitespaceCollapse},
	{name: "gYear", base: builtinAnySimpleType, primitive: PrimitiveGYear, whitespace: WhitespaceCollapse},
	{name: "gMonthDay", base: builtinAnySimpleType, primitive: PrimitiveGMonthDay, whitespace: WhitespaceCollapse},
	{name: "gDay", base: builtinAnySimpleType, primitive: PrimitiveGDay, whitespace: WhitespaceCollapse},
	{name: "gMonth", base: builtinAnySimpleType, primitive: PrimitiveGMonth, whitespace: WhitespaceCollapse},
	{name: "anyURI", base: builtinAnySimpleType, primitive: PrimitiveAnyURI, whitespace: WhitespaceCollapse},
	{name: "hexBinary", base: builtinAnySimpleType, primitive: PrimitiveHexBinary, whitespace: WhitespaceCollapse},
	{name: "base64Binary", base: builtinAnySimpleType, primitive: PrimitiveBase64Binary, whitespace: WhitespaceCollapse},
	{name: "QName", base: builtinAnySimpleType, primitive: PrimitiveQName, whitespace: WhitespaceCollapse},
	{name: "NOTATION", base: builtinAnySimpleType, primitive: PrimitiveNotation, whitespace: WhitespaceCollapse},
	{name: "ID", base: builtinNCName, primitive: PrimitiveString, whitespace: WhitespaceCollapse, builtin: BuiltinNCName, identity: IdentityID},
	{name: "IDREF", base: builtinNCName, primitive: PrimitiveString, whitespace: WhitespaceCollapse, builtin: BuiltinNCName, identity: IdentityIDREF},
	{name: "IDREFS", base: builtinAnySimpleType, listItem: builtinIDREF, variety: List, primitive: PrimitiveString, whitespace: WhitespaceCollapse, identity: IdentityIDREFList, minLength: 1},
	{name: "NMTOKEN", base: builtinToken, primitive: PrimitiveString, whitespace: WhitespaceCollapse, builtin: BuiltinNMTOKEN},
	{name: "NMTOKENS", base: builtinAnySimpleType, listItem: builtinNMTOKEN, variety: List, primitive: PrimitiveString, whitespace: WhitespaceCollapse, minLength: 1},
	{name: "ENTITY", base: builtinNCName, primitive: PrimitiveString, whitespace: WhitespaceCollapse, builtin: BuiltinEntity},
	{name: "ENTITIES", base: builtinAnySimpleType, listItem: builtinENTITY, variety: List, primitive: PrimitiveString, whitespace: WhitespaceCollapse, minLength: 1},
	{name: "xml:lang", base: builtinString, primitive: PrimitiveString, whitespace: WhitespaceCollapse, builtin: BuiltinXMLLang},
	{name: "xml:space", base: builtinString, primitive: PrimitiveString, whitespace: WhitespaceCollapse, builtin: BuiltinXMLSpace},
}

var builtinDefinitions = buildBuiltinDefinitions()

func buildBuiltinDefinitions() [BuiltinTypeCount]typeDef {
	var definitions [BuiltinTypeCount]typeDef
	for id, m := range builtinMetadata {
		definitions[id] = typeDef{
			variety:         m.variety,
			primitive:       m.primitive,
			whitespace:      m.whitespace,
			builtin:         m.builtin,
			identity:        m.identity,
			needsQName:      m.primitive == PrimitiveQName || m.primitive == PrimitiveNotation,
			needsQNameKnown: true,
			base:            m.base,
			listItem:        NoType,
			facets:          builtinFacetProgram(m),
		}
		if m.variety == List {
			definitions[id].listItem = m.listItem
		}
	}
	return definitions
}

func builtinTypeDef(id TypeID) (*typeDef, bool) {
	if id >= BuiltinTypeCount {
		return nil, false
	}
	return &builtinDefinitions[id], true
}

func builtinFacetProgram(m builtinMeta) facetProgram {
	f := facetProgram{}
	if m.fraction {
		f.present |= FacetFractionDigits
		f.fractionDigits = CardinalityFacet{Present: true}
	}
	if m.minLength != 0 {
		f.present |= FacetMinLength
		f.minLength = CardinalityFacet{Value: m.minLength, Present: true}
	}
	if m.min != "" {
		if d, err := ParseDecimalCanonical(m.min); err == nil {
			f.present |= FacetMinInclusive
			f.lower = append(f.lower, boundValue{value: decimalLiteralValue(d)})
		}
	}
	if m.max != "" {
		if d, err := ParseDecimalCanonical(m.max); err == nil {
			f.present |= FacetMaxInclusive
			f.upper = append(f.upper, boundValue{value: decimalLiteralValue(d)})
		}
	}
	return f
}

func decimalLiteralValue(d DecimalValue) parsedValue {
	return parsedValue{
		canonical: d.IntegerCanonicalText(),
		identity:  identityKey(PrimitiveDecimal, d.CanonicalText()),
		atom:      atomicValue{kind: PrimitiveDecimal, decimal: d},
	}
}

// BuiltinType returns the canonical builtin ID for a primitive family.
func BuiltinType(kind PrimitiveKind) TypeID {
	switch kind {
	case PrimitiveString:
		return builtinString
	case PrimitiveBoolean:
		return builtinBoolean
	case PrimitiveDecimal:
		return builtinDecimal
	case PrimitiveFloat:
		return builtinFloat
	case PrimitiveDouble:
		return builtinDouble
	case PrimitiveDuration:
		return builtinDuration
	case PrimitiveDateTime:
		return builtinDateTime
	case PrimitiveTime:
		return builtinTime
	case PrimitiveDate:
		return builtinDate
	case PrimitiveGYearMonth:
		return builtinGYearMonth
	case PrimitiveGYear:
		return builtinGYear
	case PrimitiveGMonthDay:
		return builtinGMonthDay
	case PrimitiveGDay:
		return builtinGDay
	case PrimitiveGMonth:
		return builtinGMonth
	case PrimitiveHexBinary:
		return builtinHexBinary
	case PrimitiveBase64Binary:
		return builtinBase64Binary
	case PrimitiveAnyURI:
		return builtinAnyURI
	case PrimitiveQName:
		return builtinQName
	case PrimitiveNotation:
		return builtinNotation
	default:
		return NoType
	}
}

// BuiltinTypeID returns a fixed builtin ID by its XSD local name.
func BuiltinTypeID(local string) (TypeID, bool) {
	for id, meta := range builtinMetadata {
		if meta.name == local {
			return TypeID(id), true
		}
	}
	return NoType, false
}

// BuiltinTypeName returns the fixed local name for id.
func BuiltinTypeName(id TypeID) (string, bool) {
	if id >= BuiltinTypeCount {
		return "", false
	}
	return builtinMetadata[id].name, true
}
