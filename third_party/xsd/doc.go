// Package xsd compiles XML Schema 1.0 documents into an immutable Engine and
// validates XML instance documents with per-call streaming state.
//
// File resolves local xs:include and xs:import schemaLocation values relative
// to the referencing file. Bytes copies caller-owned schema bytes. Open defers
// repeatable stream acquisition until compilation, where compilation limits
// apply.
// Compile uses only sources passed to it unless callers attach a Resolver with
// SchemaSource.WithResolver. HTTP and network fetches are never performed by
// default.
//
// Compilation and validation are synchronous. Callers that need interruption
// must provide resolver, opener, and reader implementations they can unblock.
//
// Instance validation is streaming. Engine.Validate consumes an io.Reader with
// a low-level byte XML parser, rejects DTD declarations, rejects non-UTF-8
// instance documents, and keeps mutable validation state inside the call.
package xsd
