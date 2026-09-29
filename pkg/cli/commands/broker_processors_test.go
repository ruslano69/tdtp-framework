package commands

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	_ "github.com/ruslano69/tdtp-framework/pkg/adapters/sqlite"
	"github.com/ruslano69/tdtp-framework/pkg/brokers"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	_ "modernc.org/sqlite"
)

// captureBroker records what ExportToBroker actually sends.
type captureBroker struct{ sent [][]byte }

func (b *captureBroker) Connect(context.Context) error { return nil }
func (b *captureBroker) Close() error                  { return nil }
func (b *captureBroker) Send(_ context.Context, m []byte) error {
	b.sent = append(b.sent, append([]byte(nil), m...))
	return nil
}
func (b *captureBroker) SendBatch(ctx context.Context, ms [][]byte) error {
	for _, m := range ms {
		if err := b.Send(ctx, m); err != nil {
			return err
		}
	}
	return nil
}
func (b *captureBroker) Receive(context.Context) ([]byte, error) { return nil, nil }
func (b *captureBroker) Ping(context.Context) error              { return nil }
func (b *captureBroker) GetBrokerType() string                   { return "capture" }

// --export-broker --integrity stamps v1.4 hashes before compression,
// local-only without a Mercury URL. Asserted on the wire messages:
// version 1.4 plus non-empty packet fingerprint (the --hash checksum is
// a different, 64-bit, compressed-blob mechanism).
func TestExportToBrokerWithOptions_Integrity(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "b.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT);
		INSERT INTO users VALUES (1,'a@x.io'),(2,'b@x.io')`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	capture := &captureBroker{}
	orig := newExportBroker
	newExportBroker = func(*BrokerConfig) (brokers.MessageBroker, error) { return capture, nil }
	t.Cleanup(func() { newExportBroker = orig })

	err = ExportToBrokerWithOptions(context.Background(),
		&adapters.Config{Type: "sqlite", DSN: dbPath},
		&BrokerConfig{Type: "rabbitmq", Queue: "q"}, "users", nil,
		BrokerExportOptions{IntegrityV14: true})
	if err != nil {
		t.Fatalf("ExportToBrokerWithOptions: %v", err)
	}
	if len(capture.sent) == 0 {
		t.Fatal("nothing was sent")
	}
	for _, m := range capture.sent {
		pkt, err := packet.NewParser().ParseBytes(m)
		if err != nil {
			t.Fatalf("sent message does not parse: %v", err)
		}
		if pkt.Version != "1.4" {
			t.Errorf("version = %q, want 1.4 (integrity stamp)", pkt.Version)
		}
		if pkt.XXH3 == "" {
			t.Error("packet carries no xxh3 fingerprint")
		}
	}
}

// --export-broker --mask used to send addresses in clear: ExportToBroker took
// procMgr and never called it. Asserted on the messages that left, not on
// whether a processor was configured.
func TestExportToBroker_AppliesRowProcessors(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "b.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT);
		INSERT INTO users VALUES (1,'ivan.petrov@mail.ru'),(2,'olga.sidorova@mail.ru')`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	capture := &captureBroker{}
	orig := newExportBroker
	newExportBroker = func(*BrokerConfig) (brokers.MessageBroker, error) { return capture, nil }
	t.Cleanup(func() { newExportBroker = orig })

	pm := NewRowProcessors()
	if err := pm.AddMaskProcessor("email"); err != nil {
		t.Fatal(err)
	}
	err = ExportToBroker(context.Background(), &adapters.Config{Type: "sqlite", DSN: dbPath},
		&BrokerConfig{Type: "rabbitmq", Queue: "q"}, "users", nil,
		false, 3, "zstd", pm, 0, "", false, false)
	if err != nil {
		t.Fatalf("ExportToBroker: %v", err)
	}
	if len(capture.sent) == 0 {
		t.Fatal("nothing was sent")
	}
	for _, m := range capture.sent {
		pkt, err := packet.NewParser().ParseBytes(m)
		if err != nil {
			t.Fatalf("sent message does not parse: %v", err)
		}
		for _, r := range pkt.GetRows() {
			if strings.Contains(r[1], "petrov") || strings.Contains(r[1], "sidorova") {
				t.Errorf("address left in clear on the wire: %q", r[1])
			}
		}
	}
}
