package schema

import (
	"errors"
	"maps"

	"github.com/jacoelho/xsd/xsderrors"
)

// PublishSchema validates compiler-owned state, constructs one immutable
// validation program, and seals the result. The caller must provide exclusive
// access to build for the duration of the call. On success, build is cleared
// and the returned schema owns all validation-facing storage; previously
// retained aliases may be mutated without affecting the schema.
func publishSchema(build *schemaBuild, work ContentModelWork) (*Schema, error) {
	if build == nil {
		return nil, errors.New("nil schema build")
	}
	if err := requireContentModelWork(work); err != nil {
		return nil, err
	}
	program, err := sealSchemaProgram(build, work)
	if err != nil {
		return nil, err
	}
	*build = schemaBuild{}
	return &Schema{program: program}, nil
}

func sealSchemaProgram(build *schemaBuild, work ContentModelWork) (schemaProgram, error) {
	if err := validateSchemaBuildIDDomain(build); err != nil {
		return schemaProgram{}, err
	}
	if err := validateSchemaBuildOwnership(build); err != nil {
		return schemaProgram{}, err
	}
	contentAnalysis, err := NewContentModelAnalysis(build, work)
	if err != nil {
		return schemaProgram{}, err
	}
	validation := schemaValidation{
		build:            build,
		contentModelWork: work,
		contentAnalysis:  contentAnalysis,
	}
	if err = validateSchema(&validation); err != nil {
		return schemaProgram{}, err
	}
	program, err := newSchemaProgram(build, work)
	if err != nil {
		return schemaProgram{}, schemaBuildValidationError(err)
	}
	if build.valueBuilder == nil {
		return schemaProgram{}, xsderrors.InternalInvariant("schema build has no value program builder")
	}
	valueProgram, err := build.valueBuilder.Seal()
	if err != nil {
		return schemaProgram{}, valueBuilderError(err)
	}
	if valueProgram == nil {
		return schemaProgram{}, xsderrors.InternalInvariant("value builder returned nil program")
	}
	program.Value = valueProgram
	return program, nil
}

func validateSchemaBuildIDDomain(build *schemaBuild) error {
	tables := [...]struct {
		name   string
		length int
	}{
		{name: "namespace", length: len(build.Names.namespaces)},
		{name: "local-name", length: len(build.Names.locals)},
		{name: "simple-type", length: len(build.SimpleTypes)},
		{name: "complex-type", length: len(build.ComplexTypes)},
		{name: "element-declaration", length: len(build.Elements)},
		{name: "attribute-declaration", length: len(build.Attributes)},
		{name: "content-model", length: len(build.Models)},
		{name: "compiled-content-model", length: len(build.CompiledModels)},
		{name: "attribute-use-set", length: len(build.AttributeUseSets)},
		{name: "wildcard-definition", length: len(build.Wildcards)},
		{name: "identity-constraint", length: len(build.Identities)},
	}
	for _, table := range tables {
		if !validRuntimeIDTableLength(table.length) {
			return xsderrors.InternalInvariant("schema " + table.name + " table exceeds runtime ID domain")
		}
	}
	return nil
}

func newSchemaProgram(build *schemaBuild, work ContentModelWork) (schemaProgram, error) {
	compiledModels, err := newCompiledModelReads(build.CompiledModels, work)
	if err != nil {
		return schemaProgram{}, err
	}
	if err = indexAllContentModels(compiledModels, build, work); err != nil {
		return schemaProgram{}, err
	}
	typeDerivations, err := newTypeDerivationReadForTypes(
		build.Builtin.AnyType,
		build.SimpleTypes,
		build.ComplexTypes,
	)
	if err != nil {
		return schemaProgram{}, err
	}
	names := newNameReadView(&build.Names)
	reads := schemaProgram{
		GlobalAttributes:      maps.Clone(build.GlobalAttributes),
		GlobalElements:        maps.Clone(build.GlobalElements),
		GlobalTypes:           maps.Clone(build.GlobalTypes),
		Substitutions:         build.Substitutions,
		Names:                 names,
		Notations:             newNotationReadMap(&build.Names, build.Notations),
		Attributes:            newAttributeDeclReadsForDecls(build.Attributes),
		TypeDerivations:       typeDerivations,
		SimpleTypeUnavailable: append([]bool(nil), build.simpleTypeUnavailable...),
		ComplexTypes:          newComplexTypeReads(build.ComplexTypes),
		Wildcards:             newWildcardViews(&build.Names, build.Wildcards),
		CompiledModels:        compiledModels,
		Elements:              newElementReadTable(build.Elements, build.ComplexTypes),
		IdentityDispatch:      newIdentityDispatchRead(build.Identities, names),
	}
	reads.AttributeUseSets = newAttributeUseSetReads(&build.Names, build.AttributeUseSets, build.SimpleTypes)
	return reads, nil
}
