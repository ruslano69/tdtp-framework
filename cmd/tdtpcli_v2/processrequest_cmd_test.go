package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

func writeRequest(t *testing.T, dir, recipient string) (string, string) {
	t.Helper()
	g := packet.NewGenerator()
	p, err := g.GenerateRequest("goods", nil, "client", recipient)
	if err != nil {
		t.Fatal(err)
	}
	data, err := g.ToXML(p, true)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "request.xml")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path, p.Header.MessageID
}

func TestProcessRequestCmd_RoundTripAndJSON(t *testing.T) {
	dir, cfg, _ := writeImportDB(t)
	request, messageID := writeRequest(t, dir, "receiver")
	response := filepath.Join(dir, "response.xml")
	code, stdout, stderr := runApp(t, "--config", cfg, "--json", "process-request", request, "--output", response)
	if code != ExitOK || !strings.Contains(stdout, `"valid":true`) || strings.Contains(stdout, "Processing request") {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	p, err := packet.NewParser().ParseFile(response)
	if err != nil {
		t.Fatal(err)
	}
	if p.Header.Type != packet.TypeResponse || p.Header.InReplyTo != messageID || p.Header.Sender != "receiver" || p.Header.Recipient != "client" || len(p.Data.Rows) != 2 {
		t.Errorf("response header=%+v rows=%d", p.Header, len(p.Data.Rows))
	}
}

func TestProcessRequestCmd_RecipientConfigGate(t *testing.T) {
	t.Setenv("TDTP_LICENSE", "")
	dir, cfg, _ := writeImportDB(t)
	request, _ := writeRequest(t, dir, "receiver")
	pgConfig := "database:\n  type: postgres\n  host: localhost\n  port: 1\n  database: test\n"
	if err := os.WriteFile(filepath.Join(dir, "receiver.yaml"), []byte(pgConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := runApp(t, "--config", cfg, "process-request", request)
	if code != ExitFail || !strings.Contains(stderr, `adapter "postgres" is not licensed`) {
		t.Errorf("recipient config gate exit=%d stderr=%q", code, stderr)
	}
	request, _ = writeRequest(t, dir, "../receiver")
	code, _, stderr = runApp(t, "--config", cfg, "process-request", request)
	if code != ExitFail || !strings.Contains(stderr, "invalid recipient config name") {
		t.Errorf("recipient path exit=%d stderr=%q", code, stderr)
	}
}

func TestProcessRequestCmd_Validation(t *testing.T) {
	if code, _, _ := runApp(t, "process-request"); code != ExitUsage {
		t.Errorf("missing file exit=%d", code)
	}
	if code, _, _ := runApp(t, "process-request", "a", "b"); code != ExitUsage {
		t.Errorf("extra file exit=%d", code)
	}
}
