package schema

import "github.com/jacoelho/xsd/xsderrors"

func (c *compiler) contentModel(id ContentModelID, msg string) (ContentModel, error) {
	model, ok := c.rt.ContentModel(id)
	if !ok {
		return ContentModel{}, xsderrors.InternalInvariant(msg)
	}
	return model, nil
}

func (c *compiler) compileModel(n *schemaNode, ctx *schemaContext) (ContentModelID, error) {
	if n.kind == schemaKindGroup {
		if ref, ok := schemaGroupRef(n); ok {
			return c.compileModelGroupRef(n, ctx, ref)
		}
	}
	if id, done, err := c.existingModel(n, ctx); done || err != nil {
		return id, err
	}
	if dependencyErr := c.spendComponentDependency(n); dependencyErr != nil {
		return NoContentModel, dependencyErr
	}
	leave, err := c.enterComponent(n)
	if err != nil {
		return NoContentModel, err
	}
	defer leave()
	id, err := c.addModelAt(ContentModel{}, n)
	if err != nil {
		return NoContentModel, err
	}
	c.modelDone[nodeKey(n, ctx)] = id
	c.modelDepth[nodeKey(n, ctx)] = c.elementDepth
	c.compilingModel[nodeKey(n, ctx)] = true
	defer delete(c.compilingModel, nodeKey(n, ctx))
	m, err := c.compileModelValue(n, ctx)
	if err != nil {
		return NoContentModel, err
	}
	c.completeModel(id, m)
	return id, nil
}

func (c *compiler) existingModel(n *schemaNode, ctx *schemaContext) (ContentModelID, bool, error) {
	id, ok := c.modelDone[nodeKey(n, ctx)]
	if !ok {
		return NoContentModel, false, nil
	}
	if !c.compilingModel[nodeKey(n, ctx)] || c.elementDepth > c.modelDepth[nodeKey(n, ctx)] {
		return id, true, nil
	}
	err := SchemaComponentRecursionError(SchemaComponentModelGroup, "")
	return NoContentModel, true, withSchemaCompileLocation(n, err)
}

func (c *compiler) compileModelValue(n *schemaNode, ctx *schemaContext) (ContentModel, error) {
	kind, err := modelKindForNode(n)
	if err != nil {
		return ContentModel{}, err
	}
	occurs, err := parseOccurs(n, c.limits)
	if err != nil {
		return ContentModel{}, err
	}
	if kind == ModelAll {
		if err := ValidateAllModelOccurrence(occurs); err != nil {
			return ContentModel{}, withSchemaCompileLocation(n, err)
		}
	}
	m := ContentModel{Kind: kind, Occurs: occurs}
	if err := c.compileModelChildren(n, ctx, &m); err != nil {
		return ContentModel{}, err
	}
	return m, nil
}

func (c *compiler) compileModelGroupRef(n *schemaNode, ctx *schemaContext, ref string) (ContentModelID, error) {
	source, err := c.resolveModelGroupRef(n, ctx, ref)
	if err != nil {
		return NoContentModel, err
	}
	if id, exists := c.modelDone[nodeKey(source.modelNode, source.raw.ctx)]; exists && c.compilingModel[nodeKey(source.modelNode, source.raw.ctx)] {
		return c.compileRecursiveModelGroupRef(n, source, id)
	}
	if dependencyErr := c.spendComponentDependency(n); dependencyErr != nil {
		return NoContentModel, dependencyErr
	}
	id, err := c.compileModel(source.modelNode, source.raw.ctx)
	if err != nil {
		return NoContentModel, err
	}
	return c.applyModelGroupOccurrence(n, id, source.occurs)
}

type modelGroupRefSource struct {
	modelNode *schemaNode
	raw       schemaComponent
	occurs    Occurrence
	q         QName
}

func (c *compiler) resolveModelGroupRef(n *schemaNode, ctx *schemaContext, ref string) (modelGroupRefSource, error) {
	occurs, err := parseOccurs(n, c.limits)
	if err != nil {
		return modelGroupRefSource{}, err
	}
	q, err := c.resolveQNameChecked(n, ctx, ref)
	if err != nil {
		return modelGroupRefSource{}, err
	}
	label := c.rt.formatName(q)
	raw, ok := c.groupComponents[q]
	if !ok {
		return modelGroupRefSource{}, withSchemaCompileLocation(n, SchemaComponentMissingError(SchemaComponentModelGroup, label))
	}
	modelNode, err := checkTopLevelGroupChildren(raw.sourceNode())
	if err != nil {
		return modelGroupRefSource{}, err
	}
	return modelGroupRefSource{raw: raw, q: q, modelNode: modelNode, occurs: occurs}, nil
}

func (c *compiler) compileRecursiveModelGroupRef(n *schemaNode, source modelGroupRefSource, id ContentModelID) (ContentModelID, error) {
	if c.elementDepth > c.modelDepth[nodeKey(source.modelNode, source.raw.ctx)] {
		if err := c.spendComponentDependency(n); err != nil {
			return NoContentModel, err
		}
	}
	return c.recursiveModelGroupRef(source.q, id, source.occurs, source.modelNode, source.raw.ctx)
}

func (c *compiler) applyModelGroupOccurrence(n *schemaNode, id ContentModelID, occurs Occurrence) (ContentModelID, error) {
	if occurs.IsExactlyOne() {
		return id, nil
	}
	model, err := c.contentModel(id, "model group reference resolved missing content model")
	if err != nil {
		return NoContentModel, err
	}
	if model.Kind == ModelAll {
		if err := ValidateAllModelOccurrence(occurs); err != nil {
			return NoContentModel, withSchemaCompileLocation(n, err)
		}
	}
	model.Occurs = occurs
	return c.addModelAt(model, n)
}

func (c *compiler) recursiveModelGroupRef(q QName, id ContentModelID, occurs Occurrence, modelNode *schemaNode, ctx *schemaContext) (ContentModelID, error) {
	if c.elementDepth <= c.modelDepth[nodeKey(modelNode, ctx)] {
		err := SchemaComponentRecursionError(SchemaComponentModelGroup, c.rt.formatName(q))
		return NoContentModel, withSchemaCompileLocation(modelNode, err)
	}
	ref := ContentModel{
		Kind:      ModelSequence,
		Occurs:    occurs,
		Particles: []Particle{ModelParticle(id, Occurrence{Min: 1, Max: 1})},
	}
	return c.addModelAt(ref, modelNode)
}

func modelKindForNode(n *schemaNode) (ModelKind, error) {
	if kind, ok := schemaModelKind(n); ok {
		return kind, nil
	}
	return 0, withSchemaCompileLocation(n, xsderrors.InternalInvariant("model node has no typed model kind"))
}

func (c *compiler) compileModelChildren(n *schemaNode, ctx *schemaContext, m *ContentModel) error {
	for _, child := range schemaModelChildren(n) {
		if child.kind == schemaKindAnnotation {
			continue
		}
		if err := c.appendModelChild(m, child, ctx); err != nil {
			return err
		}
	}
	return nil
}

func (c *compiler) appendModelChild(m *ContentModel, child *schemaNode, ctx *schemaContext) error {
	switch child.kind {
	case schemaKindElement:
		p, err := c.compileElementParticle(child, ctx)
		if err != nil {
			return err
		}
		return withSchemaCompileLocation(child, AppendParticle(m, p))
	case schemaKindAny:
		p, err := c.compileWildcardParticle(child, ctx)
		if err != nil {
			return err
		}
		return withSchemaCompileLocation(child, AppendParticle(m, p))
	case schemaKindSequence, schemaKindChoice, schemaKindAll, schemaKindGroup:
		return c.appendNestedModelChild(m, child, ctx)
	case schemaKindForeign, schemaKindXSD, schemaKindSchema, schemaKindSimpleType, schemaKindComplexType, schemaKindAttribute, schemaKindAttributeGroup, schemaKindNotation, schemaKindAnnotation, schemaKindInclude, schemaKindImport, schemaKindAnyAttribute:
	}
	return withSchemaCompileLocation(child, xsderrors.InternalInvariant("model child has no supported typed schema kind"))
}

func (c *compiler) appendNestedModelChild(m *ContentModel, child *schemaNode, ctx *schemaContext) error {
	admission, err := modelChildAdmissionForNode(child)
	if err != nil {
		return err
	}
	if admissionErr := validateModelGroupChildAtNode(child, m.Kind, admission); admissionErr != nil {
		return admissionErr
	}
	childModelID, err := c.compileModel(child, ctx)
	if err != nil {
		return err
	}
	childModel, err := c.contentModel(childModelID, "nested model child references missing content model")
	if err != nil {
		return err
	}
	if admissionErr := validateModelGroupChildAtNode(child, m.Kind, ModelChildAdmissionForModelKind(childModel.Kind)); admissionErr != nil {
		return admissionErr
	}
	if AppendFlattenedModelChild(m, childModel) {
		return nil
	}
	p, ok, err := c.modelParticle(childModelID)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	return withSchemaCompileLocation(child, AppendParticle(m, p))
}

func validateModelGroupChildAtNode(n *schemaNode, parent ModelKind, child ModelChildAdmission) error {
	return withSchemaCompileLocation(n, ValidateModelGroupChildAdmission(parent, child))
}

func modelChildAdmissionForNode(n *schemaNode) (ModelChildAdmission, error) {
	switch n.kind {
	case schemaKindElement:
		return ModelChildAdmission{Kind: ModelChildElement}, nil
	case schemaKindAny:
		return ModelChildAdmission{Kind: ModelChildWildcard}, nil
	case schemaKindGroup:
		return ModelChildAdmission{Kind: ModelChildModel}, nil
	case schemaKindSequence, schemaKindChoice, schemaKindAll:
		kind, ok := schemaModelKind(n)
		if !ok {
			return ModelChildAdmission{}, withSchemaCompileLocation(n, xsderrors.InternalInvariant("model group node has no typed model kind"))
		}
		return ModelChildAdmissionForModelKind(kind), nil
	case schemaKindForeign, schemaKindXSD, schemaKindSchema, schemaKindSimpleType, schemaKindComplexType, schemaKindAttribute, schemaKindAttributeGroup, schemaKindNotation, schemaKindAnnotation, schemaKindInclude, schemaKindImport, schemaKindAnyAttribute:
	}
	return ModelChildAdmission{}, withSchemaCompileLocation(n, xsderrors.InternalInvariant("model child has no typed schema kind"))
}

func validateModelOccurrence(n *schemaNode, limits Limits) error {
	if n.kind == schemaKindGroup {
		if err := checkGroupOccurrenceAttributes(n); err != nil {
			return err
		}
	}
	occurs, err := parseOccurs(n, limits)
	if err != nil {
		return err
	}
	if n.kind == schemaKindAll {
		if err := ValidateAllModelOccurrence(occurs); err != nil {
			return withSchemaCompileLocation(n, err)
		}
	}
	return validateModelGroupSyntax(n, limits)
}

func (c *compiler) modelParticle(id ContentModelID) (Particle, bool, error) {
	model, err := c.contentModel(id, "model particle references missing content model")
	if err != nil {
		return Particle{}, false, err
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
		modelID, err = c.addModelAt(normalized, c.modelSources[id])
		if err != nil {
			return Particle{}, false, err
		}
	}
	return ModelParticle(modelID, occurs), true, nil
}

func (c *compiler) validateComplexExtensionModelAdmission(baseID ComplexTypeID, base ComplexType, ext ContentModelID, contentKind ContentKind) error {
	return ValidateComplexExtensionModelAdmission(&c.rt, ComplexExtensionModelAdmission{
		Extension:     ext,
		BaseContent:   base.Content,
		BaseIsAnyType: baseID == c.rt.builtinIDs().AnyType,
		BaseMixed:     base.Mixed(),
		Mixed:         contentKind.Mixed(),
	})
}

func parseOccurs(n *schemaNode, limits Limits) (Occurrence, error) {
	attrs, err := occurrenceAttrs(n)
	if err != nil {
		return Occurrence{}, withSchemaCompileLocation(n, err)
	}
	occurs, err := ParseOccurrence(attrs, limits)
	if err != nil {
		return Occurrence{}, withSchemaCompileLocation(n, err)
	}
	return occurs, nil
}

func occurrenceAttrs(n *schemaNode) (OccurrenceAttrs, error) {
	source := n.semantic.particle()
	if source == nil {
		return OccurrenceAttrs{}, xsderrors.InternalInvariant("particle node has no typed particle source")
	}
	return OccurrenceAttrs{
		MinOccurs:    source.MinOccurs.Value,
		MaxOccurs:    source.MaxOccurs.Value,
		HasMinOccurs: source.MinOccurs.Present,
		HasMaxOccurs: source.MaxOccurs.Present,
	}, nil
}

func (c *compiler) compileContentModels() error {
	models, err := c.compileContentModelsBuild()
	if err != nil {
		return err
	}
	return c.installCompiledModels(models)
}

func (c *compiler) checkCompiledModelsUPA() error {
	return c.checkContentModelsUPABuild()
}

func (c *compiler) checkCompiledElementDeclarationsConsistent() error {
	return c.checkContentModelElementDeclarationsConsistentBuild()
}
