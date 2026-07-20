package commands

import "context"

// opMetricsKey is the context key for the per-invocation OpMetrics side channel.
type opMetricsKey struct{}

// OpMetrics carries the resource name and record count discovered deep inside a
// command's execution (export.go/import.go/pipeline.go) back up to main.go's
// audit-logging call site, without changing any command function's signature.
//
// Why a context side channel instead of (Result, error) return values: tdtpcli
// has ~20 command branches (list, export, import, pipeline, xlsx, broker...)
// each calling a differently-shaped XxxOptions function. Threading a result
// struct through all of them for one cross-cutting concern (audit data) would
// touch every command function's public signature. context.Context is already
// the first parameter everywhere, so this is purely additive.
type OpMetrics struct {
	Resource        string
	RecordsAffected int64
}

// WithOpMetrics attaches a fresh OpMetrics to ctx and returns both — main.go
// keeps the pointer to read it back after the command finishes.
func WithOpMetrics(ctx context.Context) (context.Context, *OpMetrics) {
	m := &OpMetrics{}
	return context.WithValue(ctx, opMetricsKey{}, m), m
}

// recordOpMetrics is called from inside command implementations at the point
// where they already know the resource name and row count (right where they
// print "✓ Total rows: N" etc.) to populate the side channel. No-op if the
// context wasn't set up with WithOpMetrics (e.g. in unit tests / library callers
// that use these commands functions directly without going through tdtpcli main).
func recordOpMetrics(ctx context.Context, resource string, records int64) {
	if m, ok := ctx.Value(opMetricsKey{}).(*OpMetrics); ok {
		m.Resource = resource
		m.RecordsAffected = records
	}
}
