// Package value owns the XSD simple-value semantic kernel.
//
// It parses normalized lexical text once into typed values, applies the
// constraining facets of a sealed type program, and returns canonical and
// document-identity projections. It has no schema, XML stream, or validation
// session dependencies; callers provide namespace and notation resolution at
// the boundary.
package value
