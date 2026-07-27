package commands

// export_integrity_test.go — regression test for a real bug found live
// while wiring TDTP v1.5's mandatory integrity step: integrityProc used to
// call packet.ComputeIntegrity BEFORE embedding the @MRC Dictionary entry
// (Mercury base URL, added whenever mercuryClient+mercuryURL are set) —
// meaning the stamped Schema.XXH3 was computed over a Schema that was
// about to change, so a consumer's VerifyIntegrity always failed with a
// schema hash mismatch. 100% reproducible whenever --integrity and
// --mercury-url were combined (which v1.5 encryption now always does).
// Nothing exercised this combination before — no prior test existed.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/mercury"
)

func newRegisterHashMock(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req mercury.RegisterHashRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.WriteHeader(http.StatusCreated)
	}))
}

func TestIntegrityProc_SchemaHashCoversMRCEntry(t *testing.T) {
	srv := newRegisterHashMock(t)
	defer srv.Close()

	schema := packet.Schema{
		Fields: []packet.Field{
			{Name: "id", Type: "INTEGER", Key: true},
			{Name: "name", Type: "TEXT"},
		},
	}
	pkts, err := packet.NewGenerator().GenerateReference("integrity_mrc_table", schema, [][]string{
		{"1", "Alice"},
	})
	if err != nil {
		t.Fatalf("GenerateReference: %v", err)
	}
	pkt := pkts[0]
	pkt.MaterializeRows()

	mercuryURL := srv.URL
	p := &integrityProc{
		mercuryClient: mercury.NewClient(mercuryURL, 5000),
		mercuryURL:    mercuryURL,
		caller:        "test",
	}
	if err := p.ProcessPacket(context.Background(), pkt); err != nil {
		t.Fatalf("ProcessPacket: %v", err)
	}

	// The @MRC entry must actually be present — otherwise this test proves
	// nothing about the ordering bug it's meant to catch.
	if pkt.Schema.Dictionary == nil {
		t.Fatal("expected @MRC Dictionary entry, Dictionary is nil")
	}
	foundMRC := false
	for _, e := range pkt.Schema.Dictionary.Entries {
		if e.Short == "@MRC" {
			foundMRC = true
			if e.Full != mercuryURL {
				t.Errorf("@MRC entry = %q, want %q", e.Full, mercuryURL)
			}
		}
	}
	if !foundMRC {
		t.Fatal("expected @MRC entry in Dictionary, not found")
	}

	// The actual regression check: the stamped hash must match the final
	// Schema content, @MRC entry included. A real consumer's
	// VerifyIntegrity does exactly this recomputation.
	if err := packet.VerifyIntegrity(pkt); err != nil {
		t.Errorf("VerifyIntegrity failed on the integrityProc's own output: %v", err)
	}
}

func TestIntegrityProc_LocalOnly_NoMRCEntry(t *testing.T) {
	// No Mercury client → no @MRC entry, no registration call, but the
	// packet must still be internally self-consistent.
	schema := packet.Schema{
		Fields: []packet.Field{
			{Name: "id", Type: "INTEGER", Key: true},
		},
	}
	pkts, err := packet.NewGenerator().GenerateReference("integrity_local_table", schema, [][]string{
		{"1"},
	})
	if err != nil {
		t.Fatalf("GenerateReference: %v", err)
	}
	pkt := pkts[0]
	pkt.MaterializeRows()

	p := &integrityProc{}
	if err := p.ProcessPacket(context.Background(), pkt); err != nil {
		t.Fatalf("ProcessPacket: %v", err)
	}
	if pkt.Schema.Dictionary != nil {
		t.Error("Dictionary should stay nil when no Mercury client/URL is configured")
	}
	if err := packet.VerifyIntegrity(pkt); err != nil {
		t.Errorf("VerifyIntegrity failed: %v", err)
	}
}
