package schema

import (
	"github.com/jacoelho/xsd/internal/vocab"
	"github.com/jacoelho/xsd/xsderrors"
)

// ComplexExtensionModelAdmission is the compile-time projection needed to
// validate an extension model before it is appended to its base content.
type ComplexExtensionModelAdmission struct {
	Extension     ContentModelID
	BaseContent   ContentModelID
	BaseIsAnyType bool
	BaseMixed     bool
	Mixed         bool
}

// ComplexExtensionContentAdmission is the compile-time projection needed before
// an extension model is lowered.
type ComplexExtensionContentAdmission struct {
	BaseSimpleContent bool
	HasModelChild     bool
}

// ModelChildKind is the compile-time projection of the child term being
// admitted into a model group.
type ModelChildKind uint8

const (
	// ModelChildElement admits an element particle.
	ModelChildElement ModelChildKind = iota
	// ModelChildModel admits a nested model group particle.
	ModelChildModel
	// ModelChildWildcard admits a wildcard particle.
	ModelChildWildcard
)

// ModelChildAdmission is the compile-time projection needed to validate a
// model group child without exposing schema syntax to internal compile code.
type ModelChildAdmission struct {
	Kind      ModelChildKind
	ModelKind ModelKind
}

// AddContentModelFunc appends a generated content model to the compiler's
// mutable runtime model table.
type AddContentModelFunc func(ContentModel) (ContentModelID, error)

type modelTextKind uint8

const (
	modelTextInvalid modelTextKind = iota
	modelTextElementOnly
	modelTextMixed
)

// ModelKindForLocal classifies an XSD model-group element local name.
func ModelKindForLocal(local string) (ModelKind, error) {
	switch local {
	case vocab.XSDElemSequence:
		return ModelSequence, nil
	case vocab.XSDElemChoice:
		return ModelChoice, nil
	case vocab.XSDElemAll:
		return ModelAll, nil
	default:
		return 0, xsderrors.SchemaCompile(xsderrors.CodeSchemaContentModel, "unsupported model "+local)
	}
}

// ModelChildAdmissionForLocal classifies an XSD model-group child local name.
func ModelChildAdmissionForLocal(local string) (ModelChildAdmission, error) {
	switch local {
	case vocab.XSDElemElement:
		return ModelChildAdmission{Kind: ModelChildElement}, nil
	case vocab.XSDElemAny:
		return ModelChildAdmission{Kind: ModelChildWildcard}, nil
	case vocab.XSDElemGroup:
		return ModelChildAdmission{Kind: ModelChildModel}, nil
	case vocab.XSDElemSequence, vocab.XSDElemChoice, vocab.XSDElemAll:
		kind, err := ModelKindForLocal(local)
		if err != nil {
			return ModelChildAdmission{}, err
		}
		return ModelChildAdmissionForModelKind(kind), nil
	default:
		return ModelChildAdmission{}, xsderrors.SchemaCompile(xsderrors.CodeSchemaContentModel, "invalid model group child "+local)
	}
}

// ModelChildAdmissionForModelKind classifies a compiled nested model group.
func ModelChildAdmissionForModelKind(kind ModelKind) ModelChildAdmission {
	return ModelChildAdmission{Kind: ModelChildModel, ModelKind: kind}
}

// ValidateModelGroupChildAdmission validates compile-time model group child
// admission rules.
func ValidateModelGroupChildAdmission(parent ModelKind, child ModelChildAdmission) error {
	if parent == ModelAll && child.Kind != ModelChildElement {
		return xsderrors.SchemaCompile(xsderrors.CodeSchemaContentModel, "xs:all can contain only element particles")
	}
	if child.Kind == ModelChildModel && child.ModelKind == ModelAll {
		return xsderrors.SchemaCompile(xsderrors.CodeSchemaContentModel, "xs:all cannot be nested in model groups")
	}
	return nil
}

// AppendParticle applies compile-time particle admission rules and appends p to
// model. Zero-count particles are omitted from the lowered model.
func AppendParticle(model *ContentModel, p Particle) error {
	if err := ValidateModelGroupChildAdmission(model.Kind, ModelChildAdmission{Kind: modelParticleChildKind(p)}); err != nil {
		return err
	}
	if p.Occurs.Max == 0 && !p.Occurs.Unbounded {
		return nil
	}
	if model.Kind == ModelAll && (p.Occurs.Unbounded || p.Occurs.Max > 1) {
		return xsderrors.SchemaCompile(xsderrors.CodeSchemaOccurrence, "xs:all particles cannot repeat")
	}
	model.Particles = append(model.Particles, p)
	return nil
}

func modelParticleChildKind(p Particle) ModelChildKind {
	switch p.Kind {
	case ParticleElement:
		return ModelChildElement
	case ParticleWildcard:
		return ModelChildWildcard
	case ParticleModel:
		return ModelChildModel
	default:
	}
	return ModelChildModel
}

// ValidateComplexExtensionModelAdmission validates compile-time complex-content
// extension model admission rules.
func ValidateComplexExtensionModelAdmission(rt ContentModelRuntime, admission ComplexExtensionModelAdmission) error {
	if !admission.BaseIsAnyType && admission.BaseMixed && !admission.Mixed {
		return xsderrors.SchemaCompile(xsderrors.CodeSchemaContentModel, "complexContent extension cannot drop mixed base content")
	}
	extension, ok := rt.ContentModel(admission.Extension)
	if !ok {
		return xsderrors.InternalInvariant("complex extension references missing extension content model")
	}
	if extension.Kind == ModelAll && !ModelHasNoParticles(rt, admission.BaseContent) {
		return xsderrors.SchemaCompile(xsderrors.CodeSchemaContentModel, "complexContent extension cannot use xs:all")
	}
	if admission.BaseContent == NoContentModel {
		return nil
	}
	base, ok := rt.ContentModel(admission.BaseContent)
	if !ok {
		return xsderrors.InternalInvariant("complex extension references missing base content model")
	}
	if base.Kind == ModelAll && len(base.Particles) != 0 {
		return xsderrors.SchemaCompile(xsderrors.CodeSchemaContentModel, "complexContent extension cannot add particles to xs:all base")
	}
	return nil
}

// ValidateComplexExtensionContentAdmission validates compile-time complex-content
// extension admission rules that are knowable before model lowering.
func ValidateComplexExtensionContentAdmission(admission ComplexExtensionContentAdmission) error {
	if admission.BaseSimpleContent && admission.HasModelChild {
		return xsderrors.SchemaCompile(xsderrors.CodeSchemaContentModel, "complexContent extension cannot add particles to simple content")
	}
	return nil
}

// ExtendSequenceModel lowers a complex-content extension by combining base and
// extension content into a sequence model when both sides contribute particles.
func ExtendSequenceModel(rt ContentModelRuntime, add AddContentModelFunc, baseID, extID ContentModelID) (ContentModelID, error) {
	if baseID == NoContentModel {
		return extID, nil
	}
	base, ok := rt.ContentModel(baseID)
	if !ok {
		return NoContentModel, xsderrors.InternalInvariant("sequence extension references missing base content model")
	}
	ext, ok := rt.ContentModel(extID)
	if !ok {
		return NoContentModel, xsderrors.InternalInvariant("sequence extension references missing extension content model")
	}
	textKind := modelTextElementOnly
	if base.Mixed || ext.Mixed {
		textKind = modelTextMixed
	}
	if ModelHasNoParticles(rt, baseID) {
		return modelWithTextKind(rt, add, extID, textKind)
	}
	if ModelHasNoParticles(rt, extID) {
		return modelWithTextKind(rt, add, baseID, textKind)
	}
	m := ContentModel{Kind: ModelSequence, Occurs: Occurrence{Min: 1, Max: 1}, Mixed: textKind == modelTextMixed}
	if err := appendSequenceExtensionOperand(rt, add, &m, baseID, base); err != nil {
		return NoContentModel, err
	}
	if err := appendSequenceExtensionOperand(rt, add, &m, extID, ext); err != nil {
		return NoContentModel, err
	}
	return add(m)
}

func appendSequenceExtensionOperand(rt ContentModelRuntime, add AddContentModelFunc, target *ContentModel, id ContentModelID, model ContentModel) error {
	if model.Kind == ModelSequence && model.Occurs.IsExactlyOne() {
		target.Particles = append(target.Particles, model.Particles...)
		return nil
	}
	return AppendModelParticle(rt, add, target, id)
}

func modelWithTextKind(rt ContentModelRuntime, add AddContentModelFunc, id ContentModelID, textKind modelTextKind) (ContentModelID, error) {
	var mixed bool
	switch textKind {
	case modelTextElementOnly:
	case modelTextMixed:
		mixed = true
	case modelTextInvalid:
		return NoContentModel, xsderrors.InternalInvariant("invalid model text kind")
	default:
		return NoContentModel, xsderrors.InternalInvariant("unknown model text kind")
	}
	if id == NoContentModel {
		return id, nil
	}
	model, ok := rt.ContentModel(id)
	if !ok {
		return NoContentModel, xsderrors.InternalInvariant("model mixed update references missing content model")
	}
	if model.Mixed == mixed {
		return id, nil
	}
	model.Mixed = mixed
	return add(model)
}

// AppendModelParticle appends a model particle for id when the referenced
// model can contribute content.
func AppendModelParticle(rt ContentModelRuntime, add AddContentModelFunc, model *ContentModel, id ContentModelID) error {
	p, ok, err := CompileModelParticle(rt, add, id)
	if err != nil || !ok {
		return err
	}
	model.Particles = append(model.Particles, p)
	return nil
}

// CompileModelParticle lowers a referenced content model into a particle. Repeated
// model references are normalized through a generated exactly-one model slot.
func CompileModelParticle(rt ContentModelRuntime, add AddContentModelFunc, id ContentModelID) (Particle, bool, error) {
	model, ok := rt.ContentModel(id)
	if !ok {
		return Particle{}, false, xsderrors.InternalInvariant("model particle references missing content model")
	}
	occurs := model.Occurs
	if occurs.Max == 0 && !occurs.Unbounded {
		return Particle{}, false, nil
	}
	modelID := id
	if !occurs.IsExactlyOne() {
		normalized := model
		normalized.Occurs = Occurrence{Min: 1, Max: 1}
		var err error
		modelID, err = add(normalized)
		if err != nil {
			return Particle{}, false, err
		}
	}
	return ModelParticle(modelID, occurs), true, nil
}

// AppendFlattenedModelChild appends child particles directly into model when
// the nested model can be lowered without changing occurrence semantics.
func AppendFlattenedModelChild(model *ContentModel, child ContentModel) bool {
	if model.Kind == ModelChoice && child.Kind == ModelChoice && child.Occurs.IsExactlyOne() {
		model.Particles = append(model.Particles, child.Particles...)
		return true
	}
	if model.Kind != ModelSequence {
		return false
	}
	if (child.Kind == ModelSequence || child.Kind == ModelChoice) && len(child.Particles) == 1 {
		p := child.Particles[0]
		if canFlattenSingleParticleModel(child.Occurs, p.Occurs) {
			p.Occurs = MultiplyOccurrence(p.Occurs, child.Occurs)
			model.Particles = append(model.Particles, p)
			return true
		}
	}
	if child.Kind == ModelSequence && len(child.Particles) > 1 && child.Occurs.IsExactlyOne() {
		model.Particles = append(model.Particles, child.Particles...)
		return true
	}
	return false
}

func canFlattenSingleParticleModel(modelOccurs, particleOccurs Occurrence) bool {
	return occurrenceProductRepresentable(modelOccurs, particleOccurs) &&
		(modelOccurs.IsExactlyOne() ||
			particleOccurs.Min == 0 ||
			particleOccurs.IsExactlyOne() ||
			(particleOccurs.Unbounded && (modelOccurs.Min > 0 || particleOccurs.Min == 1)) ||
			(!modelOccurs.Unbounded && modelOccurs.Min == modelOccurs.Max))
}

func occurrenceProductRepresentable(a, b Occurrence) bool {
	const maxUint32 = ^uint32(0)
	if a.Min != 0 && b.Min > maxUint32/a.Min {
		return false
	}
	if a.Unbounded || b.Unbounded {
		return true
	}
	return a.Max == 0 || b.Max <= maxUint32/a.Max
}
