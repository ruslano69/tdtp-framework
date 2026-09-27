package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ruslano69/tdtp-framework/pkg/brokers"
)

func writeMapFixture(t *testing.T, adapter string) (mappingFile, inputFile, databaseFile string) {
	t.Helper()
	dir := t.TempDir()
	inputFile, _ = filepath.Abs("../../docs/samples/employees-plain.tdtp")
	databaseFile = filepath.Join(dir, "mapped.db")
	dsn := filepath.ToSlash(databaseFile)
	if adapter != "sqlite" {
		dsn = "unused-for-license-refusal"
	}
	content := fmt.Sprintf(`id: map-v2-test
loop_guard:
  source_system: fixture
  target_system: test
target_connection:
  type: %s
  dsn: %q
targets:
  - id: employees
    table: mapped_employees
    upsert_key: ext_id
    fields:
      - {from: id, to: ext_id}
      - {from: full_name, to: name}
`, adapter, dsn)
	mappingFile = filepath.Join(dir, "mapping.yaml")
	if err := os.WriteFile(mappingFile, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return mappingFile, inputFile, databaseFile
}

func TestMapCmd_Validation(t *testing.T) {
	for _, args := range [][]string{
		{"map"},
		{"map", "mapping.yaml"},
		{"map", "mapping.yaml", "--input", "file.xml", "--drain", "nonsense"},
		{"map", "mapping.yaml", "--input", "file.xml", "--drain", "0s"},
		{"map", "mapping.yaml", "--input", "file.xml", "--listen"},
		{"map", "mapping.yaml", "--input", "file.xml", "--foreign"},
	} {
		code, _, stderr := runApp(t, args...)
		if code != ExitUsage || stderr == "" {
			t.Errorf("%v: exit=%d stderr=%q, want visible usage error", args, code, stderr)
		}
	}
	if got, _, ok := compatResolve([]string{"--map", "mapping.yaml", "--input", "file.xml"}); !ok || strings.Join(got, " ") != "map mapping.yaml --input file.xml" {
		t.Fatalf("legacy --map resolved to %v (ok=%v)", got, ok)
	}
}

func TestMapCmd_DryRunAndSQLiteImport(t *testing.T) {
	mappingFile, inputFile, databaseFile := writeMapFixture(t, "sqlite")
	// The loop guard writes under the user's home. Keep this test isolated.
	t.Setenv("USERPROFILE", t.TempDir())
	code, stdout, stderr := runApp(t, "map", mappingFile, "--input", inputFile, "--dry-run")
	if code != ExitOK || !strings.Contains(stdout, `[dry-run] target="mapped_employees"`) {
		t.Fatalf("dry-run: exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if _, err := os.Stat(databaseFile); !os.IsNotExist(err) {
		t.Fatalf("dry-run unexpectedly created a database: %v", err)
	}

	code, stdout, stderr = runApp(t, "--quiet", "--map", mappingFile, "--input", inputFile)
	if code != ExitOK || !strings.Contains(stdout, "mapped_employees  5 rows") {
		t.Fatalf("quiet import: exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	db, err := sql.Open("sqlite", databaseFile)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM mapped_employees").Scan(&count); err != nil || count != 5 {
		t.Fatalf("mapped rows: count=%d err=%v", count, err)
	}
}

func TestMapCmd_JSONAndTargetLicense(t *testing.T) {
	mappingFile, inputFile, _ := writeMapFixture(t, "sqlite")
	code, stdout, stderr := runApp(t, "--json", "map", mappingFile, "--input", inputFile, "--dry-run")
	var verdict struct {
		Valid   bool   `json:"valid"`
		Mapping string `json:"mapping"`
	}
	if err := json.Unmarshal([]byte(stdout), &verdict); err != nil || code != ExitOK || !verdict.Valid || verdict.Mapping != mappingFile {
		t.Fatalf("JSON verdict: exit=%d err=%v stdout=%q", code, err, stdout)
	}
	if !strings.Contains(stderr, `[dry-run] target="mapped_employees"`) {
		t.Fatalf("dry-run progress should be on stderr in JSON mode: %q", stderr)
	}

	communityEnv(t)
	mappingFile, inputFile, _ = writeMapFixture(t, "postgres")
	code, _, stderr = runApp(t, "map", mappingFile, "--input", inputFile)
	if code != ExitFail || !strings.Contains(stderr, `database adapter "postgres" is not licensed`) {
		t.Fatalf("target adapter bypassed license gate: exit=%d stderr=%q", code, stderr)
	}
}

func TestMapCmd_RabbitMQModes(t *testing.T) {
	if os.Getenv("TDTP_BROKER_TEST") == "" {
		t.Skip("needs live RabbitMQ: TDTP_BROKER_TEST=1")
	}
	t.Setenv("USERPROFILE", t.TempDir())
	port := 5672
	if raw := os.Getenv("TDTP_BROKER_PORT"); raw != "" {
		if _, err := fmt.Sscan(raw, &port); err != nil {
			t.Fatal(err)
		}
	}
	_, inputFile, _ := writeMapFixture(t, "sqlite")
	packet, err := os.ReadFile(inputFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []struct {
		name string
		flag []string
	}{
		{"one-shot", nil},
		{"drain", []string{"--drain", "100ms"}},
		{"listen", []string{"--listen"}},
	} {
		t.Run(mode.name, func(t *testing.T) {
			dir := t.TempDir()
			queue := fmt.Sprintf("tdtp_map_v2_%d", time.Now().UnixNano())
			dbFile := filepath.Join(dir, "mapped.db")
			mappingFile := filepath.Join(dir, "mapping.yaml")
			config := fmt.Sprintf(`id: map-rabbitmq-test
loop_guard:
  source_system: fixture
  target_system: test
input_source:
  broker:
    type: rabbitmq
    host: localhost
    port: %d
    user: guest
    password: guest
    queue: %s
    durable: true
    auto_delete: true
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
`, port, queue, filepath.ToSlash(dbFile))
			if err := os.WriteFile(mappingFile, []byte(config), 0o600); err != nil {
				t.Fatal(err)
			}
			broker, err := brokers.New(brokers.Config{
				Type: "rabbitmq", Host: "localhost", Port: port,
				User: "guest", Password: "guest", Queue: queue, Durable: true, AutoDelete: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if err := broker.Connect(ctx); err != nil {
				t.Fatal(err)
			}
			if err := broker.Send(ctx, packet); err != nil {
				t.Fatal(err)
			}
			if mode.name == "drain" {
				if err := broker.Send(ctx, packet); err != nil {
					t.Fatal(err)
				}
			}
			defer func() { _ = broker.Close() }()
			args := append([]string{"--quiet", "map", mappingFile, "--input", "broker://" + queue}, mode.flag...)
			wantRows := 5
			var auditDB string
			if mode.name == "drain" {
				wantRows = 10
				auditDB = filepath.Join(dir, "audit.db")
				auditCfg := writeAuditCfg(t, "  database:\n    type: sqlite\n    dsn: "+filepath.ToSlash(auditDB)+"\n    table: audit_log\n    batch_size: 0\n    auto_create_table: true\n")
				args = append([]string{"--config", auditCfg}, args...)
			}
			var stdout, stderr bytes.Buffer
			runCtx := ctx
			var cancel context.CancelFunc
			if mode.name == "listen" {
				runCtx, cancel = context.WithTimeout(ctx, 350*time.Millisecond)
				defer cancel()
			}
			code := NewApp().Run(runCtx, args, &stdout, &stderr)
			if code != ExitOK || !strings.Contains(stdout.String(), fmt.Sprintf("mapped_employees  %d rows", wantRows)) {
				t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			db, err := sql.Open("sqlite", dbFile)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = db.Close() }()
			var count int
			if err := db.QueryRow("SELECT COUNT(*) FROM mapped_employees").Scan(&count); err != nil || count != 5 {
				t.Fatalf("mapped rows: count=%d err=%v", count, err)
			}
			if auditDB != "" {
				auditConn, err := sql.Open("sqlite", auditDB)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = auditConn.Close() }()
				var entries, rows int
				err = auditConn.QueryRow(`SELECT COUNT(*), COALESCE(SUM(records_affected), 0) FROM audit_log WHERE metadata LIKE '%map:listen%'`).Scan(&entries, &rows)
				if err != nil || entries != 2 || rows != 10 {
					t.Fatalf("per-message audit: entries=%d rows=%d err=%v", entries, rows, err)
				}
			}
		})
	}
}
