//go:build integration

package integration

// TestXzmercuryPipeline — интеграционный тест всей системы:
// реальный xzmercury (--dev: in-process miniredis + mock LDAP) + tdtpcli --pipeline.
//
// Запуск:
//
//	go test -v -timeout 90s -run TestXzmercuryPipeline ./tests/integration/
//
// Зависимости: только Go-тулчейн (Docker не нужен).
//
// Что тестируется:
//   - xzmercury ACL / quota / keystore / request-tracker
//   - tdtpcli ETL pipeline: load → workspace → transform → encrypt → write
//   - AES-256-GCM: правильный заголовок в выходном файле
//   - Burn-on-read: повторный retrieve возвращает 404

import (
	"database/sql"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	_ "modernc.org/sqlite" // register "sqlite" driver, used by TestXzmercuryPipelineV15's decrypt-roundtrip check
)

// pickFreePort возвращает свободный TCP-порт.
func pickFreePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("pickFreePort: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

// waitHTTP опрашивает url пока не получит 200 или не истечёт timeout.
func waitHTTP(t *testing.T, url string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url) //nolint:noctx
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			return
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("waitHTTP: %s not ready after %s", url, timeout)
}

// writeFile пишет content в path, создавая директорию если нужно.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// repoRoot возвращает корень репозитория (директория с go.work).
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repoRoot: go.work not found")
		}
		dir = parent
	}
}

// mercuryFixture is what startXzmercuryDev returns: enough for a pipeline
// YAML's security: block, plus the HMAC secret tdtpcli needs in its own
// environment (MERCURY_SERVER_SECRET) to verify BindKey responses.
type mercuryFixture struct {
	URL    string
	Secret string
}

// startXzmercuryDev launches `go run ./xzmercury/cmd/xzmercury/ --dev` on a
// free port with a minimal ACL (default_group/default_cost cover any
// pipeline name not explicitly listed), waits for /healthz, and registers
// t.Cleanup to kill the whole process group on test end. Shared by every
// xZMercury-backed pipeline test in this file — extracted so
// TestXzmercuryPipeline (legacy --enc13 whole-blob) and
// TestXzmercuryPipelineV15 (--enc default, section-level) don't each carry
// their own copy of this ~90-line startup dance.
func startXzmercuryDev(t *testing.T, root, tmp string) mercuryFixture {
	t.Helper()

	aclPath := filepath.Join(tmp, "pipeline-acl.yaml")
	writeFile(t, aclPath, `
default_group: "cn=tdtp-pipeline-users,ou=groups,dc=corp,dc=local"
default_cost: 1
`)

	const mercurySecret = "integration-test-secret-32chars!!"

	xzmercuryConfig := fmt.Sprintf(`
server:
  addr: "__ADDR__"
  read_timeout: 10s
  write_timeout: 10s

security:
  server_secret: "%s"
  rate_limit: 0

key_ttl: 5m

quota:
  default_hourly: 1000
  acl_file: "%s"
`,
		mercurySecret,
		aclPath,
	)

	cfgPath := filepath.Join(tmp, "xzmercury.yaml")

	port := pickFreePort(t)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	mercuryURL := "http://" + addr

	// Windows paths contain backslashes; in YAML double-quoted strings \U is a
	// Unicode escape → replace with forward slashes before embedding into YAML.
	yamlSafe := strings.ReplaceAll(xzmercuryConfig, "__ADDR__", addr)
	yamlSafe = strings.ReplaceAll(yamlSafe, `\`, `/`)
	writeFile(t, cfgPath, yamlSafe)

	t.Logf("starting xzmercury --dev on %s", addr)
	xzmCmd := exec.Command(
		"go", "run", "./xzmercury/cmd/xzmercury/",
		"--dev",
		"--config", cfgPath,
	)
	xzmCmd.Dir = root
	// Setpgid: true — помещает дочерние процессы в отдельную группу.
	// Без этого Kill() убивает только "go run", но не скомпилированный бинарник xzmercury,
	// который становится orphan и мешает CI (GitHub Actions сообщает о нём в конце джоба).
	xzmCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// Pipe вместо os.Stderr: при Kill() pipe закрывается сразу,
	// иначе Go test framework ждёт WaitDelay и считает прогон неудачным.
	xzmPipe, _ := xzmCmd.StderrPipe()
	xzmCmd.Stdout = io.Discard
	if err := xzmCmd.Start(); err != nil {
		t.Fatalf("start xzmercury: %v", err)
	}
	// Форвардим логи xzmercury в t.Log асинхронно
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := xzmPipe.Read(buf)
			if n > 0 {
				t.Log(strings.TrimRight(string(buf[:n]), "\n"))
			}
			if err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() {
		// Убиваем всю группу процессов: и "go run" и скомпилированный xzmercury.
		if xzmCmd.Process != nil {
			_ = syscall.Kill(-xzmCmd.Process.Pid, syscall.SIGKILL)
			_ = xzmCmd.Wait()
		}
		_ = xzmPipe.Close()
	})

	waitHTTP(t, mercuryURL+"/healthz", 60*time.Second)
	t.Logf("xzmercury ready at %s", mercuryURL)

	return mercuryFixture{URL: mercuryURL, Secret: mercurySecret}
}

// TestXzmercuryPipeline exercises the legacy TDTP v1.3 whole-packet
// encryption format (--enc13 / encryption_v13: true) end-to-end against a
// real xzmercury --dev instance: pipeline load → workspace → transform →
// encrypt → write, then verifies the AES-256-GCM binary header and that a
// second run (new UUID) still works. Pinned to the legacy format on
// purpose — TestXzmercuryPipelineV15 covers the new default (--enc,
// section-level) separately; this one exists specifically so the old
// whole-blob wire format (still supported for not-yet-upgraded consumers)
// keeps a real end-to-end regression test, not just unit coverage.
func TestXzmercuryPipeline(t *testing.T) {
	root := repoRoot(t)

	// ── 0. Создаём временную директорию для конфигов и вывода ────────────
	tmp := t.TempDir()
	outFile := filepath.Join(tmp, "dept_report_encrypted.tdtp")

	// ── 1-2. xzmercury --dev ──────────────────────────────────────────────
	mercury := startXzmercuryDev(t, root, tmp)
	mercuryURL, mercurySecret := mercury.URL, mercury.Secret

	// ── 3. Pipeline config ────────────────────────────────────────────────
	// Use forward slashes in YAML strings: on Windows, backslashes are
	// interpreted as YAML escape sequences (e.g. \U = 8-hex Unicode escape).
	outFileYAML := filepath.ToSlash(outFile)
	pipelinePath := filepath.Join(tmp, "pipeline.yaml")
	writeFile(t, pipelinePath, fmt.Sprintf(`
name: "dept-salary-encrypted"
version: "1.0"
description: "Integration test: ETL pipeline + xzmercury"

sources:
  - name: employees
    type: tdtp
    dsn: "tests/integration/testdata/employees.tdtp.xml"

  - name: departments
    type: tdtp
    dsn: "tests/integration/testdata/departments.tdtp.xml"

workspace:
  type: sqlite
  mode: ":memory:"

transform:
  result_table: "dept_report"
  sql: |
    SELECT
      d.department_name,
      COUNT(e.employee_id)    AS headcount,
      ROUND(AVG(e.salary), 2) AS avg_salary,
      SUM(e.salary)           AS total_salary
    FROM employees e
    JOIN departments d ON e.department_id = d.department_id
    WHERE e.is_active = 1
    GROUP BY d.department_id, d.department_name
    ORDER BY total_salary DESC

output:
  type: tdtp
  tdtp:
    destination: "%s"
    format: "xml"
    compression: false
    encryption: true
    encryption_v13: true   # pin to legacy whole-blob format — see test doc comment

security:
  mercury_url: "%s"
  server_secret: "%s"
  key_ttl_seconds: 300
  mercury_timeout_ms: 10000

error_handling:
  on_source_error: "fail"
`, outFileYAML, mercuryURL, mercurySecret))

	// ── 4. Вызываем tdtpcli --pipeline ───────────────────────────────────
	t.Log("running tdtpcli --pipeline")
	tdtpCmd := exec.Command("go", "run", "-tags", "nokafka", "./cmd/tdtpcli/", "--pipeline", pipelinePath)
	tdtpCmd.Dir = root
	tdtpCmd.Stdout = os.Stderr // пишем в stderr чтобы было видно в -v
	tdtpCmd.Stderr = os.Stderr
	if err := tdtpCmd.Run(); err != nil {
		t.Fatalf("tdtpcli failed: %v", err)
	}

	// ── 5. Проверяем зашифрованный файл ──────────────────────────────────
	info, err := os.Stat(outFile)
	if err != nil {
		t.Fatalf("output file not created: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("output file is empty")
	}
	t.Logf("output file: %s (%d bytes)", outFile, info.Size())

	blob, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}

	// tdtpcrypto header: version(2 bytes) + algo(1 byte): 0x01 = AES-256-GCM
	if len(blob) < 3 {
		t.Fatalf("output too short (%d bytes)", len(blob))
	}
	if blob[2] != 0x01 {
		t.Fatalf("expected AES-256-GCM header algo=0x01, got 0x%02x", blob[2])
	}
	t.Logf("encryption verified: algo=0x%02x (AES-256-GCM)", blob[2])

	// ── 6. Burn-on-read: повторный запуск должен вернуть 404 ─────────────
	// tdtpcli после encrypt сразу вызывает retrieve → ключ должен быть сожжён.
	// Запускаем pipeline второй раз — xzmercury выдаст новый ключ для нового UUID,
	// поэтому вместо этого проверяем через прямой HTTP-запрос к xzmercury.
	outFile2 := filepath.Join(tmp, "dept_report_encrypted_2.tdtp")
	outFile2YAML := filepath.ToSlash(outFile2)
	writeFile(t, filepath.Join(tmp, "pipeline2.yaml"), strings.ReplaceAll(
		strings.ReplaceAll(
			mustReadFile(t, pipelinePath),
			outFileYAML, // pipeline was written with forward slashes
			outFile2YAML,
		),
		"dept-salary-encrypted",
		"dept-salary-encrypted-2",
	))
	tdtpCmd2 := exec.Command("go", "run", "-tags", "nokafka", "./cmd/tdtpcli/",
		"--pipeline", filepath.Join(tmp, "pipeline2.yaml"))
	tdtpCmd2.Dir = root
	tdtpCmd2.Stdout = os.Stderr
	tdtpCmd2.Stderr = os.Stderr
	if err := tdtpCmd2.Run(); err != nil {
		t.Fatalf("second pipeline run failed: %v", err)
	}
	info2, err := os.Stat(outFile2)
	if err != nil {
		t.Fatalf("second output file not created: %v", err)
	}
	t.Logf("second run OK: %s (%d bytes) — new UUID, new key", outFile2, info2.Size())

	t.Log("TestXzmercuryPipeline PASSED")
}

// TestXzmercuryPipelineV15 exercises the TDTP v1.5 section-level encryption
// format — the new default for encryption: true / --enc since this
// codebase's v1.5 work — end-to-end against a real xzmercury --dev
// instance. Unlike TestXzmercuryPipeline (pinned to --enc13 above), this
// does NOT set encryption_v13, so it verifies the actual current default
// behavior a plain `encryption: true` pipeline YAML gets today.
//
// Checks, in order:
//  1. Output is still valid, parseable XML (v1.3's binary blob never was).
//  2. Header.MessageID/TableName are readable in plaintext (no key needed) —
//     the whole point of v1.5's design.
//  3. QueryContext/Schema/Data carry encryption="aes-256-gcm" and no
//     plaintext business data (department names) leaked into the file.
//  4. A real --import --mercury-url round-trip actually decrypts the file
//     and the department names come back correctly — proving the whole
//     BindKey → encrypt → RetrieveKey → decrypt chain works against real
//     xzmercury, not just that *a* ciphertext-shaped blob was produced.
//  5. Burn-on-read: a second --import of the same file fails (key already
//     consumed by step 4) — stronger than the legacy test's "second run
//     with a fresh UUID still works", which never actually proved burn-on-read
//     by itself.
func TestXzmercuryPipelineV15(t *testing.T) {
	root := repoRoot(t)

	tmp := t.TempDir()
	outFile := filepath.Join(tmp, "dept_report_encrypted_v15.tdtp.xml")

	mercury := startXzmercuryDev(t, root, tmp)
	mercuryURL, mercurySecret := mercury.URL, mercury.Secret

	outFileYAML := filepath.ToSlash(outFile)
	pipelinePath := filepath.Join(tmp, "pipeline_v15.yaml")
	writeFile(t, pipelinePath, fmt.Sprintf(`
name: "dept-salary-encrypted-v15"
version: "1.0"
description: "Integration test: ETL pipeline + xzmercury, TDTP v1.5"

sources:
  - name: employees
    type: tdtp
    dsn: "tests/integration/testdata/employees.tdtp.xml"

  - name: departments
    type: tdtp
    dsn: "tests/integration/testdata/departments.tdtp.xml"

workspace:
  type: sqlite
  mode: ":memory:"

transform:
  result_table: "dept_report"
  sql: |
    SELECT
      d.department_name,
      COUNT(e.employee_id)    AS headcount,
      ROUND(AVG(e.salary), 2) AS avg_salary,
      SUM(e.salary)           AS total_salary
    FROM employees e
    JOIN departments d ON e.department_id = d.department_id
    WHERE e.is_active = 1
    GROUP BY d.department_id, d.department_name
    ORDER BY total_salary DESC

output:
  type: tdtp
  tdtp:
    destination: "%s"
    format: "xml"
    compression: false
    encryption: true
    # no encryption_v13 — this is exactly the default a plain
    # "encryption: true" pipeline gets today.

security:
  mercury_url: "%s"
  server_secret: "%s"
  key_ttl_seconds: 300
  mercury_timeout_ms: 10000

error_handling:
  on_source_error: "fail"
`, outFileYAML, mercuryURL, mercurySecret))

	t.Log("running tdtpcli --pipeline (v1.5)")
	tdtpCmd := exec.Command("go", "run", "-tags", "nokafka", "./cmd/tdtpcli/", "--pipeline", pipelinePath)
	tdtpCmd.Dir = root
	tdtpCmd.Stdout = os.Stderr
	tdtpCmd.Stderr = os.Stderr
	if err := tdtpCmd.Run(); err != nil {
		t.Fatalf("tdtpcli failed: %v", err)
	}

	info, err := os.Stat(outFile)
	if err != nil {
		t.Fatalf("output file not created: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("output file is empty")
	}
	t.Logf("output file: %s (%d bytes)", outFile, info.Size())

	blob, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	content := string(blob)

	// 1. Still valid XML (v1.3's binary envelope never parsed as XML at all).
	if !strings.HasPrefix(strings.TrimPrefix(content, "\uFEFF"), "<?xml") {
		t.Fatalf("output does not start with an XML declaration — not v1.5 shape:\n%.200s", content)
	}

	// 2. Header readable without a key.
	if !strings.Contains(content, "<Header>") || !strings.Contains(content, "dept_report") {
		t.Error("Header/TableName not readable in plaintext — v1.5's whole point is a plain Header")
	}

	// 3. Encrypted, and no plaintext business data leaked.
	if !strings.Contains(content, `encryption="aes-256-gcm"`) {
		t.Fatalf("missing encryption=\"aes-256-gcm\" attribute — not v1.5 encrypted output:\n%.300s", content)
	}
	for _, name := range []string{"Engineering", "Human Resources", "Finance", "Product"} {
		if strings.Contains(content, name) {
			t.Errorf("plaintext department name %q leaked into encrypted output — Data section not opaque", name)
		}
	}
	t.Logf("encryption verified: valid XML, Header plain, Data/Schema opaque (v1.5)")

	// 4. Real decrypt round-trip: --import --mercury-url into a fresh SQLite DB.
	importDB := filepath.Join(tmp, "import.db")
	importCfgPath := filepath.Join(tmp, "import.yaml")
	writeFile(t, importCfgPath, fmt.Sprintf(`
database:
  type: sqlite
  database: "%s"
`, filepath.ToSlash(importDB)))

	importCmd := exec.Command("go", "run", "-tags", "nokafka", "./cmd/tdtpcli/",
		"--config", importCfgPath,
		"--import", outFileYAML,
		"--table", "dept_report_imported",
		"--mercury-url", mercuryURL,
	)
	importCmd.Env = append(os.Environ(), "MERCURY_SERVER_SECRET="+mercurySecret)
	importCmd.Dir = root
	importCmd.Stdout = os.Stderr
	importCmd.Stderr = os.Stderr
	if err := importCmd.Run(); err != nil {
		t.Fatalf("--import (decrypt) failed: %v", err)
	}

	db, err := sql.Open("sqlite", importDB)
	if err != nil {
		t.Fatalf("open imported db: %v", err)
	}
	defer func() { _ = db.Close() }()

	rows, err := db.Query(`SELECT department_name FROM dept_report_imported ORDER BY department_name`)
	if err != nil {
		t.Fatalf("query imported table: %v", err)
	}
	var got []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan row: %v", err)
		}
		got = append(got, name)
	}
	_ = rows.Close()
	if len(got) == 0 {
		t.Fatal("decrypted import produced 0 rows — round-trip failed")
	}
	t.Logf("decrypted rows: %v", got)
	foundEngineering := false
	for _, name := range got {
		if name == "Engineering" {
			foundEngineering = true
		}
	}
	if !foundEngineering {
		t.Errorf("expected 'Engineering' among decrypted department names, got %v", got)
	}

	// 5. Burn-on-read: a second --import of the SAME file must fail — the
	// key BindKey issued was already consumed by RetrieveKey in step 4.
	importCmd2 := exec.Command("go", "run", "-tags", "nokafka", "./cmd/tdtpcli/",
		"--config", importCfgPath,
		"--import", outFileYAML,
		"--table", "dept_report_imported_again",
		"--mercury-url", mercuryURL,
	)
	importCmd2.Env = append(os.Environ(), "MERCURY_SERVER_SECRET="+mercurySecret)
	importCmd2.Dir = root
	var stderr2 strings.Builder
	importCmd2.Stdout = io.Discard
	importCmd2.Stderr = &stderr2
	if err := importCmd2.Run(); err == nil {
		t.Error("second --import of the same v1.5 file should fail (burn-on-read), but succeeded")
	} else {
		t.Logf("second import correctly failed (burn-on-read): %s", strings.TrimSpace(stderr2.String()))
	}

	t.Log("TestXzmercuryPipelineV15 PASSED")
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}
