package commands

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ruslano69/tdtp-framework/pkg/brokers"
)

type mapTestBroker struct {
	messages chan []byte
	acks     int
	nacks    int
	closed   bool
	onNack   func()
}

func (b *mapTestBroker) Connect(context.Context) error             { return nil }
func (b *mapTestBroker) Close() error                              { b.closed = true; return nil }
func (b *mapTestBroker) Send(context.Context, []byte) error        { return nil }
func (b *mapTestBroker) SendBatch(context.Context, [][]byte) error { return nil }
func (b *mapTestBroker) Ping(context.Context) error                { return nil }
func (b *mapTestBroker) GetBrokerType() string                     { return "rabbitmq" }
func (b *mapTestBroker) Receive(ctx context.Context) ([]byte, error) {
	select {
	case msg := <-b.messages:
		return msg, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (b *mapTestBroker) AckLast() error { b.acks++; return nil }
func (b *mapTestBroker) NackLast(bool) error {
	b.nacks++
	if b.onNack != nil {
		b.onNack()
	}
	return nil
}

type mapTestAuditor struct {
	records []int64
	onSync  func()
}

func (a *mapTestAuditor) RecordSync(_ context.Context, _ string, records int64, _ time.Duration, _ error) {
	a.records = append(a.records, records)
	if a.onSync != nil {
		a.onSync()
	}
}

func mapLoopFixture(t *testing.T) (string, []byte) {
	t.Helper()
	dir := t.TempDir()
	input, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "samples", "employees-plain.tdtp"))
	if err != nil {
		t.Fatal(err)
	}
	config := fmt.Sprintf(`id: map-loop-test
loop_guard:
  source_system: fixture
  target_system: test
input_source:
  broker:
    type: rabbitmq
    queue: original
target_connection:
  type: sqlite
  dsn: %q
targets:
  - id: employees
    table: mapped_employees
    upsert_key: ext_id
    fields:
      - {from: id, to: ext_id}
      - {from: full_name, to: name}
`, filepath.ToSlash(filepath.Join(dir, "mapped.db")))
	path := filepath.Join(dir, "mapping.yaml")
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	return path, input
}

func TestRunMapDrain_AcksMessagesThenStopsAfterIdle(t *testing.T) {
	path, input := mapLoopFixture(t)
	br := &mapTestBroker{messages: make(chan []byte, 2)}
	br.messages <- input
	br.messages <- input
	auditor := &mapTestAuditor{}
	var output bytes.Buffer
	var usedQueue string
	err := RunMap(context.Background(), MapOptions{
		MappingFile: path, InputFile: "broker://override", DryRun: true,
		Drain: 20 * time.Millisecond, Quiet: true, Output: &output, Auditor: auditor,
		newBroker: func(cfg brokers.Config) (brokers.MessageBroker, error) {
			usedQueue = cfg.Queue
			return br, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if usedQueue != "override" || br.acks != 2 || br.nacks != 0 || !br.closed {
		t.Fatalf("queue=%q acks=%d nacks=%d closed=%v", usedQueue, br.acks, br.nacks, br.closed)
	}
	if len(auditor.records) != 2 || auditor.records[0] != 5 || auditor.records[1] != 5 {
		t.Fatalf("audit records=%v, want one entry per message", auditor.records)
	}
	if !strings.Contains(output.String(), "mapped_employees  10 rows") {
		t.Fatalf("drain summary missing: %q", output.String())
	}
}

func TestRunMapListen_ContextShutdownAndNack(t *testing.T) {
	path, input := mapLoopFixture(t)
	t.Run("graceful shutdown", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		br := &mapTestBroker{messages: make(chan []byte, 1)}
		br.messages <- input
		auditor := &mapTestAuditor{onSync: cancel}
		err := RunMap(ctx, MapOptions{
			MappingFile: path, InputFile: "broker://override", Listen: true, DryRun: true,
			Quiet: true, Output: &bytes.Buffer{}, Auditor: auditor,
			newBroker: func(brokers.Config) (brokers.MessageBroker, error) { return br, nil },
		})
		if err != nil || br.acks != 1 || !br.closed || len(auditor.records) != 1 {
			t.Fatalf("err=%v acks=%d closed=%v audit=%v", err, br.acks, br.closed, auditor.records)
		}
	})
	t.Run("bad packet requeued", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		br := &mapTestBroker{messages: make(chan []byte, 1), onNack: cancel}
		br.messages <- []byte("not a TDTP packet")
		var output bytes.Buffer
		err := RunMap(ctx, MapOptions{
			MappingFile: path, InputFile: "broker://override", Listen: true, DryRun: true,
			Quiet: true, Output: &output,
			newBroker: func(brokers.Config) (brokers.MessageBroker, error) { return br, nil },
		})
		if err != nil || br.acks != 0 || br.nacks != 1 || !br.closed {
			t.Fatalf("err=%v acks=%d nacks=%d closed=%v", err, br.acks, br.nacks, br.closed)
		}
		if !strings.Contains(output.String(), "parse error (skipping)") {
			t.Fatalf("parse error was hidden: %q", output.String())
		}
	})
}

var _ brokers.MessageBroker = (*mapTestBroker)(nil)
