package schema

import (
	"github.com/jacoelho/xsd/internal/vocab"
)

// DerivationAttrRule identifies one XSD derivation-set attribute and the mask
// allowed for that attribute.
type DerivationAttrRule struct {
	Name    string
	Label   string
	Allowed DerivationMask
}

const (
	complexTypeBlockLabel = "complexType block"
	complexTypeFinalLabel = "complexType final"
	simpleTypeFinalLabel  = "simpleType final"
	elementBlockLabel     = "element block"
	elementFinalLabel     = "element final"
)

func complexTypeBlockDerivation() DerivationAttrRule {
	return DerivationAttrRule{Name: vocab.XSDAttrBlock, Label: complexTypeBlockLabel, Allowed: DerivationComplexMask}
}

func complexTypeFinalDerivation() DerivationAttrRule {
	return DerivationAttrRule{Name: vocab.XSDAttrFinal, Label: complexTypeFinalLabel, Allowed: DerivationComplexMask}
}

func simpleTypeFinalDerivation() DerivationAttrRule {
	return DerivationAttrRule{Name: vocab.XSDAttrFinal, Label: simpleTypeFinalLabel, Allowed: DerivationSimpleFinalMask}
}

func elementBlockDerivation() DerivationAttrRule {
	return DerivationAttrRule{Name: vocab.XSDAttrBlock, Label: elementBlockLabel, Allowed: DerivationBlockDefaultMask}
}

func elementFinalDerivation() DerivationAttrRule {
	return DerivationAttrRule{Name: vocab.XSDAttrFinal, Label: elementFinalLabel, Allowed: DerivationComplexMask}
}

// ParseDerivationAttrWithDefault parses a derivation-set attribute or applies
// the schema default restricted to the rule's allowed derivation class.
func ParseDerivationAttrWithDefault(attr LexicalAttribute, def DerivationMask, rule DerivationAttrRule) (DerivationMask, error) {
	if attr.Present {
		return ParseDerivationSet(attr.Value, rule.Label, rule.Allowed)
	}
	return def & rule.Allowed, nil
}
