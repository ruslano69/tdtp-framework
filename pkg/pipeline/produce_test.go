package pipeline_test

// produce_test.go — ComputeAndRegisterIntegrity, the producer-side
// counterpart to VerifyAndPrepare. Exists specifically to close a gap
// found while live-testing TDTP v1.5: VerifyAndPrepare's Mercury
// pre-flight runs for any packet with Version >= "1.4" and treats an
// empty XXH3 as a hard block (ErrHashNotRegistered), not "not requested".
// A v1.5-encrypted packet that never called this would therefore be
// unimportable the moment --mercury-url is set — which v1.5 decryption
// itself always requires.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/mercury"
	"github.com/ruslano69/tdtp-framework/pkg/pipeline"
)

func makeProduceTestPacket(t *testing.T) *packet.DataPacket {
	t.Helper()
	schema := packet.Schema{
		Fields: []packet.Field{
			{Name: "id", Type: "INTEGER", Key: true},
			{Name: "name", Type: "TEXT"},
		},
	}
	pkts, err := packet.NewGenerator().GenerateReference("produce_test_table", schema, [][]string{
		{"1", "Alice"},
		{"2", "Bob"},
	})
	if err != nil {
		t.Fatalf("GenerateReference: %v", err)
	}
	return pkts[0]
}

func TestComputeAndRegisterIntegrity_LocalOnly(t *testing.T) {
	pkt := makeProduceTestPacket(t)

	if err := pipeline.ComputeAndRegisterIntegrity(context.Background(), pkt, nil, "test-sender"); err != nil {
		t.Fatalf("ComputeAndRegisterIntegrity: %v", err)
	}
	if pkt.Version != "1.4" {
		t.Errorf("Version = %q, want 1.4", pkt.Version)
	}
	if pkt.XXH3 == "" {
		t.Error("XXH3 not stamped")
	}
	if pkt.Schema.XXH3 == "" || pkt.Data.XXH3 == "" {
		t.Error("Schema/Data XXH3 not stamped")
	}
}

func TestComputeAndRegisterIntegrity_WithMercury(t *testing.T) {
	var gotUUID, gotXXH3 string
	var gotPart int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req mercury.RegisterHashRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		gotUUID = req.UUID
		gotPart = req.Part
		gotXXH3 = req.XXH3
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	pkt := makeProduceTestPacket(t)
	client := mercury.NewClient(srv.URL, 5000)

	if err := pipeline.ComputeAndRegisterIntegrity(context.Background(), pkt, client, "test-sender"); err != nil {
		t.Fatalf("ComputeAndRegisterIntegrity: %v", err)
	}
	if gotUUID != pkt.Header.MessageID {
		t.Errorf("registered uuid = %q, want %q", gotUUID, pkt.Header.MessageID)
	}
	if gotPart != pkt.Header.PartNumber {
		t.Errorf("registered part = %d, want %d", gotPart, pkt.Header.PartNumber)
	}
	if gotXXH3 != pkt.XXH3 {
		t.Errorf("registered xxh3 = %q, want %q", gotXXH3, pkt.XXH3)
	}
}

func TestComputeAndRegisterIntegrity_MercuryUnavailable(t *testing.T) {
	pkt := makeProduceTestPacket(t)
	// Point at an address nothing is listening on.
	client := mercury.NewClient("http://127.0.0.1:1", 200)

	err := pipeline.ComputeAndRegisterIntegrity(context.Background(), pkt, client, "test-sender")
	if err == nil {
		t.Fatal("expected error when Mercury is unreachable")
	}
	// Local integrity must still have been stamped before the registration
	// attempt failed — ComputeIntegrity itself never touches the network.
	if pkt.XXH3 == "" {
		t.Error("XXH3 should be stamped locally even if Mercury registration fails")
	}
}

// TestV15EncryptionEnablesVerifyAndPrepare proves the actual bug this
// function fixes: a packet that went through ComputeAndRegisterIntegrity
// (mirroring what a v1.5 encryption call site now does) passes
// VerifyAndPrepare's v1.4+ pre-flight; one that skipped it does not.
func TestV15EncryptionEnablesVerifyAndPrepare(t *testing.T) {
	registered := make(map[string]string) // uuid -> xxh3
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			var req mercury.RegisterHashRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			registered[req.UUID] = req.XXH3
			w.WriteHeader(http.StatusCreated)
		case http.MethodGet:
			// VerifyHash path: GET /api/hashes/{uuid}/{part}?xxh3=...
			uuid := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/hashes/"), "/")[0]
			presented := r.URL.Query().Get("xxh3")
			stored, ok := registered[uuid]
			resp := struct {
				Registered bool   `json:"registered"`
				Match      bool   `json:"match"`
				StoredXXH3 string `json:"stored_xxh3"`
			}{
				Registered: ok,
				Match:      ok && stored == presented,
				StoredXXH3: stored,
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)
		}
	}))
	defer srv.Close()
	client := mercury.NewClient(srv.URL, 5000)

	// Packet WITHOUT the mandatory integrity step (simulates the bug).
	unregistered := makeProduceTestPacket(t)
	unregistered.Version = "1.5" // as EncryptSections would set

	_, err := pipeline.VerifyAndPrepare(context.Background(), unregistered, client, pipeline.FallbackBlock)
	if err == nil {
		t.Fatal("expected VerifyAndPrepare to block a v1.5 packet with no integrity stamp (the bug this function fixes)")
	}

	// Packet WITH the mandatory integrity step.
	registeredPkt := makeProduceTestPacket(t)
	if err := pipeline.ComputeAndRegisterIntegrity(context.Background(), registeredPkt, client, "test-sender"); err != nil {
		t.Fatalf("ComputeAndRegisterIntegrity: %v", err)
	}
	registeredPkt.Version = "1.5" // EncryptSections would set this after

	_, err = pipeline.VerifyAndPrepare(context.Background(), registeredPkt, client, pipeline.FallbackBlock)
	if err != nil {
		t.Fatalf("VerifyAndPrepare should accept a properly integrity-stamped v1.5 packet: %v", err)
	}
}
