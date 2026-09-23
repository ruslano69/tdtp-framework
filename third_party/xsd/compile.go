package xsd

import (
	xsdSchema "github.com/jacoelho/xsd/internal/schema"
)

// Engine is an immutable compiled schema validator.
type Engine struct {
	rt *xsdSchema.Schema
}

// CompileOptions controls schema compilation resource limits.
type CompileOptions struct {
	// MaxSchemaDepth caps nested schema XML elements. Zero uses the default.
	MaxSchemaDepth int
	// MaxSchemaAttributes caps attributes on one schema XML element. Zero uses the default.
	MaxSchemaAttributes int
	// MaxSchemaTokenBytes caps retained schema XML token payloads. Zero uses the default.
	MaxSchemaTokenBytes int64
	// MaxSchemaSourceBytes caps bytes read from each schema source. Zero uses the default.
	MaxSchemaSourceBytes int64
	// MaxSchemaSources caps explicit source descriptors and distinct resolver-loaded
	// source identities admitted to one compilation. Zero uses the default.
	MaxSchemaSources int
	// MaxSchemaTotalBytes caps aggregate bytes read across all schema sources. Zero uses the default.
	MaxSchemaTotalBytes int64
	// MaxSchemaReferences caps include/import references processed during compilation. Zero uses the default.
	MaxSchemaReferences int
	// MaxSchemaDependencySteps caps aggregate schema-graph expansion, target-context
	// propagation, and component-dependency resolution work. Zero uses the default.
	MaxSchemaDependencySteps int
	// MaxSchemaTargetContexts caps distinct source/effective-target-namespace contexts,
	// including primary and chameleon-derived contexts. Zero uses the default.
	MaxSchemaTargetContexts int
	// MaxSchemaInstantiatedNodes caps aggregate schema node occurrences across effective target contexts. Zero uses the default.
	MaxSchemaInstantiatedNodes int
	// MaxSchemaNames caps interned schema names, including built-ins. Zero means no explicit limit.
	MaxSchemaNames int
	// MaxFiniteOccurs caps finite maxOccurs values. Zero uses the uint32 runtime cap.
	MaxFiniteOccurs uint64
	// MaxContentModelStates caps compiled content-model DFA states. Zero uses the default.
	MaxContentModelStates int
	// MaxContentModelAnalysisSteps caps content-model traversal, determinization, and ambiguity-analysis work. Zero uses the default.
	MaxContentModelAnalysisSteps int
	// MaxSubstitutionClosureEntries caps aggregate transitive substitution-group relationships. Zero uses the default.
	MaxSubstitutionClosureEntries int
	// MaxSimpleUnionMemberEntries caps aggregate flattened simple-union members. Zero uses the default.
	MaxSimpleUnionMemberEntries int
}

// Compile compiles schema sources into an immutable validation engine.
func Compile(sources ...SchemaSource) (*Engine, error) {
	return CompileWithOptions(CompileOptions{}, sources...)
}

// CompileWithOptions compiles schema sources with explicit resource limits.
func CompileWithOptions(opts CompileOptions, sources ...SchemaSource) (*Engine, error) {
	rt, err := xsdSchema.CompileMappedSources(internalCompileOptions(opts), sources, internalSchemaSource)
	if err != nil {
		return nil, err
	}
	return &Engine{rt: rt}, nil
}

func internalCompileOptions(opts CompileOptions) xsdSchema.Options {
	return xsdSchema.Options{
		MaxSchemaDepth:                opts.MaxSchemaDepth,
		MaxSchemaAttributes:           opts.MaxSchemaAttributes,
		MaxSchemaTokenBytes:           opts.MaxSchemaTokenBytes,
		MaxSchemaSourceBytes:          opts.MaxSchemaSourceBytes,
		MaxSchemaSources:              opts.MaxSchemaSources,
		MaxSchemaTotalBytes:           opts.MaxSchemaTotalBytes,
		MaxSchemaReferences:           opts.MaxSchemaReferences,
		MaxSchemaDependencySteps:      opts.MaxSchemaDependencySteps,
		MaxSchemaTargetContexts:       opts.MaxSchemaTargetContexts,
		MaxSchemaInstantiatedNodes:    opts.MaxSchemaInstantiatedNodes,
		MaxSchemaNames:                opts.MaxSchemaNames,
		MaxFiniteOccurs:               opts.MaxFiniteOccurs,
		MaxContentModelStates:         opts.MaxContentModelStates,
		MaxContentModelAnalysisSteps:  opts.MaxContentModelAnalysisSteps,
		MaxSubstitutionClosureEntries: opts.MaxSubstitutionClosureEntries,
		MaxSimpleUnionMemberEntries:   opts.MaxSimpleUnionMemberEntries,
	}
}
