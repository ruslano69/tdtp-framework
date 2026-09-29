package commands

import (
	"context"
	"testing"
)

// TestWithOpMetrics_ReturnsFreshPointer verifies WithOpMetrics attaches a
// non-nil, zero-valued OpMetrics to the context and hands back the same pointer.
func TestWithOpMetrics_ReturnsFreshPointer(t *testing.T) {
	ctx, m := WithOpMetrics(context.Background())

	if m == nil {
		t.Fatal("WithOpMetrics returned a nil *OpMetrics")
	}
	if m.Resource != "" || m.RecordsAffected != 0 {
		t.Errorf("fresh OpMetrics not zero-valued: %+v", *m)
	}

	// The same pointer must be retrievable from the returned context.
	got, ok := ctx.Value(opMetricsKey{}).(*OpMetrics)
	if !ok {
		t.Fatal("context does not carry an *OpMetrics")
	}
	if got != m {
		t.Error("context pointer differs from the returned pointer")
	}
}

// TestRecordOpMetrics_RoundTrip mirrors the main.go usage: set up the side
// channel, let a command implementation record into it, then read it back.
func TestRecordOpMetrics_RoundTrip(t *testing.T) {
	ctx, m := WithOpMetrics(context.Background())

	recordOpMetrics(ctx, "dbo.Orders", 1234)

	if m.Resource != "dbo.Orders" {
		t.Errorf("Resource = %q, want %q", m.Resource, "dbo.Orders")
	}
	if m.RecordsAffected != 1234 {
		t.Errorf("RecordsAffected = %d, want %d", m.RecordsAffected, 1234)
	}
}

// TestRecordOpMetrics_NoSetup covers the documented contract: recordOpMetrics
// on a context WITHOUT WithOpMetrics is a silent no-op (library callers / unit
// tests that invoke command functions directly must not panic).
func TestRecordOpMetrics_NoSetup(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("recordOpMetrics panicked on a bare context: %v", r)
		}
	}()

	recordOpMetrics(context.Background(), "whatever", 99)
}

// TestRecordOpMetrics_LastWriteWins confirms that a later record overwrites an
// earlier one (a command that discovers a corrected count reports the latest).
func TestRecordOpMetrics_LastWriteWins(t *testing.T) {
	ctx, m := WithOpMetrics(context.Background())

	recordOpMetrics(ctx, "first", 1)
	recordOpMetrics(ctx, "second", 2)

	if m.Resource != "second" || m.RecordsAffected != 2 {
		t.Errorf("last write did not win: %+v", *m)
	}
}
