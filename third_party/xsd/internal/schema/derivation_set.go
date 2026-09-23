package schema

import (
	"github.com/jacoelho/xsd/internal/lex"

	"github.com/jacoelho/xsd/internal/vocab"
	"github.com/jacoelho/xsd/xsderrors"
)

const derivationAll = "#all"

// ParseDerivationSet parses an XSD block/final derivation set and returns
// compile diagnostics for invalid lexical values.
func ParseDerivationSet(value, label string, allowed DerivationMask) (DerivationMask, error) {
	parser := derivationSetParser{label: label, allowed: allowed}
	for token := range lex.XMLFieldsSeq(value) {
		if err := parser.add(token); err != nil {
			return 0, err
		}
	}
	if parser.seenAll {
		return allowed, nil
	}
	return parser.mask, nil
}

type derivationSetParser struct {
	label   string
	allowed DerivationMask
	mask    DerivationMask
	seenAll bool
}

func (p *derivationSetParser) add(token string) error {
	if token == derivationAll {
		return p.addAll()
	}
	if p.seenAll {
		return p.combinationError()
	}
	bit, ok := compileDerivationToken(token)
	if !ok {
		return xsderrors.SchemaCompile(xsderrors.CodeSchemaInvalidAttribute, "invalid "+p.label+" value "+token)
	}
	if p.allowed&bit == 0 {
		return xsderrors.SchemaCompile(xsderrors.CodeSchemaInvalidAttribute, p.label+" cannot contain "+token)
	}
	p.mask |= bit
	return nil
}

func (p *derivationSetParser) addAll() error {
	if p.seenAll || p.mask != 0 {
		return p.combinationError()
	}
	p.seenAll = true
	return nil
}

func (p *derivationSetParser) combinationError() error {
	return xsderrors.SchemaCompile(xsderrors.CodeSchemaInvalidAttribute, p.label+" cannot combine #all with other values")
}

func compileDerivationToken(token string) (DerivationMask, bool) {
	switch token {
	case vocab.XSDElemExtension:
		return DerivationExtension, true
	case vocab.XSDElemRestriction:
		return DerivationRestriction, true
	case "substitution":
		return DerivationSubstitution, true
	case vocab.XSDElemList:
		return DerivationList, true
	case vocab.XSDElemUnion:
		return DerivationUnion, true
	default:
		return 0, false
	}
}
