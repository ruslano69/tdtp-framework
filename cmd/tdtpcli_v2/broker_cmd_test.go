package main

// broker_cmd_test.go — broker commands through the dispatcher. Pure
// validation runs everywhere; the live RabbitMQ round-trip runs only with
// TDTP_BROKER_TEST=1 (CI has no broker; run it against tdtp-rabbitmq-test).

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// writeBrokerDB creates a sqlite database with a parcels table plus a
// config with database AND broker (rabbitmq) sections.
func writeBrokerDB(t *testing.T) (dir, cfg, db string) {
	t.Helper()
	dir = t.TempDir()
	db = filepath.Join(dir, "parcels.db")
	sdb, err := sql.Open("sqlite", db)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sdb.Close() }()
	for _, q := range []string{
		`CREATE TABLE parcels (ID INTEGER PRIMARY KEY, Label TEXT)`,
		`INSERT INTO parcels VALUES (1,'a'),(2,'b')`,
	} {
		if _, err := sdb.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	cfg = filepath.Join(dir, "broker.yaml")
	yaml := "database:\n  type: sqlite\n  database: " + db + "\n" +
		"broker:\n  type: rabbitmq\n  host: localhost\n  port: 5672\n" +
		"  user: tdtp_test\n  password: tdtp_test_password\n" +
		"  queue: tdtp_v2test\n  vhost: /\n  durable: false\n  auto_delete: true\n"
	if err := os.WriteFile(cfg, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, cfg, db
}

func TestExportBroker_NoConfig(t *testing.T) {
	code, _, _ := runApp(t, "export-broker", "parcels")
	if code != ExitUsage {
		t.Errorf("exit = %d, want %d", code, ExitUsage)
	}
}

func TestExportBroker_NoTable(t *testing.T) {
	_, cfg, _ := writeBrokerDB(t)
	code, _, _ := runApp(t, "--config", cfg, "export-broker")
	if code != ExitUsage {
		t.Errorf("exit = %d, want %d", code, ExitUsage)
	}
}

func TestImportBroker_RejectsPositionals(t *testing.T) {
	_, cfg, _ := writeBrokerDB(t)
	code, _, _ := runApp(t, "--config", cfg, "import-broker", "stray")
	if code != ExitUsage {
		t.Errorf("exit = %d, want %d", code, ExitUsage)
	}
}

func TestImportBroker_BadStrategy(t *testing.T) {
	_, cfg, _ := writeBrokerDB(t)
	code, _, _ := runApp(t, "--config", cfg, "import-broker", "--strategy", "teleport")
	if code != ExitUsage {
		t.Errorf("exit = %d, want %d", code, ExitUsage)
	}
}

func TestImportBroker_BadExpectVar(t *testing.T) {
	_, cfg, _ := writeBrokerDB(t)
	code, _, _ := runApp(t, "--config", cfg, "import-broker", "--expect-var", "novalue")
	if code != ExitUsage {
		t.Errorf("exit = %d, want %d", code, ExitUsage)
	}
}

func TestExportImportBroker_RoundTrip(t *testing.T) {
	if os.Getenv("TDTP_BROKER_TEST") == "" {
		t.Skip("needs live RabbitMQ: TDTP_BROKER_TEST=1")
	}
	_, cfg, db := writeBrokerDB(t)
	if code, _, _ := runApp(t, "--config", cfg, "export-broker", "parcels"); code != ExitOK {
		t.Fatalf("export exit = %d", code)
	}
	if code, _, _ := runApp(t, "--config", cfg, "import-broker", "--table", "parcels_rt"); code != ExitOK {
		t.Fatalf("import exit = %d", code)
	}
	sdb, err := sql.Open("sqlite", db)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sdb.Close() }()
	var n int
	if err := sdb.QueryRow("SELECT COUNT(*) FROM parcels_rt").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("rows = %d, want 2", n)
	}
}

func TestBrokerConfigFromCliconfig(t *testing.T) {
	// The shared builder keeps v1/v2 queue resolution identical; the
	// queue comes from config, never from flags.
	_, cfg, _ := writeBrokerDB(t)
	adb, bcc, err := loadConfigs(&Deps{ConfigPath: cfg}, "export-broker")
	if err != nil {
		t.Fatalf("loadConfigs: %v", err)
	}
	if adb.Type != "sqlite" {
		t.Errorf("adapter type = %q", adb.Type)
	}
	if bcc.Queue != "tdtp_v2test" || bcc.Type != "rabbitmq" {
		t.Errorf("broker = %+v", bcc)
	}
	if !strings.Contains(bcc.Host, "localhost") {
		t.Errorf("host = %q", bcc.Host)
	}
}
