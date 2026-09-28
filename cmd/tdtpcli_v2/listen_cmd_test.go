package main

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestListenCmd_ValidationAndBrokerType(t *testing.T) {
	_, cfg, db := writeImportDB(t)
	if code, _, _ := runApp(t, "--config", cfg, "listen", "--strategy", "teleport"); code != ExitUsage {
		t.Errorf("invalid strategy exit = %d, want %d", code, ExitUsage)
	}
	if code, _, _ := runApp(t, "--config", cfg, "listen", "unexpected"); code != ExitUsage {
		t.Errorf("positional argument exit = %d, want %d", code, ExitUsage)
	}
	brokerCfg := filepath.Join(t.TempDir(), "rabbitmq.yaml")
	yaml := "database:\n  type: sqlite\n  database: " + db +
		"\nbroker:\n  type: rabbitmq\n  queue: test\n"
	if err := os.WriteFile(brokerCfg, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := runApp(t, "--config", brokerCfg, "listen")
	if code != ExitFail || !strings.Contains(stderr, "supports Kafka only") {
		t.Errorf("RabbitMQ listen exit=%d stderr=%q", code, stderr)
	}
}

func TestListenCmd_CompatAndHelp(t *testing.T) {
	if code, stdout, _ := runApp(t, "listen", "--help"); code != ExitOK || !strings.Contains(stdout, "Kafka") {
		t.Errorf("listen help exit=%d output=%q", code, stdout)
	}
	code, _, stderr := runApp(t, "--listen")
	if code != ExitUsage || !strings.Contains(stderr, "--listen is deprecated") {
		t.Errorf("compat exit=%d stderr=%q", code, stderr)
	}
}

// This is opt-in because CI's nokafka build has no broker. It exercises the
// real Kafka consumer, cancellation, SQLite import and JSON stream contract.
func TestListenKafkaE2E(t *testing.T) {
	if os.Getenv("TDTP_BROKER_TEST") != "1" {
		t.Skip("set TDTP_BROKER_TEST=1 with Kafka on localhost:9092")
	}
	t.Setenv("TDTP_LICENSE", "") // SQLite is available on Community.
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "listen.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec("CREATE TABLE listen_src (id INTEGER PRIMARY KEY, name TEXT, version INTEGER); INSERT INTO listen_src VALUES (1, 'alpha', 1), (2, 'beta', 2)"); err != nil {
		t.Fatal(err)
	}
	id := time.Now().UnixNano()
	queue := fmt.Sprintf("tdtp-v2-listen-%d", id)
	container := os.Getenv("KAFKA_CONTAINER")
	if container == "" {
		container = "tdtp-kafka"
	}
	admin := func(args ...string) ([]byte, error) {
		return exec.Command("docker", append([]string{"exec", container, "kafka-topics", "--bootstrap-server", "localhost:9092"}, args...)...).CombinedOutput()
	}
	if output, err := admin("--create", "--topic", queue, "--partitions", "1", "--replication-factor", "1"); err != nil {
		t.Fatalf("create Kafka topic: %v: %s", err, output)
	}
	t.Cleanup(func() { _, _ = admin("--delete", "--topic", queue) })
	cfg := filepath.Join(dir, "listen.yaml")
	yaml := fmt.Sprintf("database:\n  type: sqlite\n  database: %s\nbroker:\n  type: kafka\n  brokers: [\"localhost:9092\"]\n  queue: %s\n  consumer_group: tdtp-v2-listen-%d\n", dbPath, queue, id)
	if err := os.WriteFile(cfg, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	checkpoint := filepath.Join(dir, "checkpoint.yaml")
	if code, _, stderr := runApp(t, "--config", cfg, "sync-incremental", "listen_src",
		"--tracking-field", "version", "--checkpoint-file", checkpoint, "--to-broker"); code != ExitOK {
		t.Fatalf("sync-incremental --to-broker exit=%d stderr=%q", code, stderr)
	}
	if _, err := os.Stat(checkpoint); err != nil {
		t.Fatalf("checkpoint after broker send: %v", err)
	}
	if _, err := db.Exec("DROP TABLE listen_src"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var stdout, stderr bytes.Buffer
	done := make(chan int, 1)
	go func() {
		done <- NewApp().Run(ctx, []string{"--config", cfg, "--json", "listen"}, &stdout, &stderr)
	}()
	imported := false
	for !imported && ctx.Err() == nil {
		var n int
		if err := db.QueryRow("SELECT COUNT(*) FROM listen_src").Scan(&n); err == nil && n == 2 {
			imported = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	cancel()
	select {
	case code := <-done:
		if code != ExitOK {
			t.Errorf("listen exit=%d stderr=%q", code, stderr.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("listen did not stop after context cancellation")
	}
	if !imported {
		t.Fatalf("listener did not import both rows: %s", stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); !strings.Contains(got, `"valid":true`) || !strings.Contains(got, queue) || strings.Count(got, "\n") != 0 {
		t.Errorf("JSON stdout = %q", got)
	}
}
