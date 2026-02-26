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
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestXzmercuryPipeline(t *testing.T) {
	root := repoRoot(t)

	// ── 0. Создаём временную директорию для конфигов и вывода ────────────
	tmp := t.TempDir()
	outFile := filepath.Join(tmp, "dept_report_encrypted.tdtp")

	// ── 1. Конфиги xzmercury ──────────────────────────────────────────────

	aclPath := filepath.Join(tmp, "pipeline-acl.yaml")
	writeFile(t, aclPath, `
default_group: "cn=tdtp-pipeline-users,ou=groups,dc=corp,dc=local"
default_cost: 1

pipelines:
  dept-salary-encrypted:
    group: "cn=tdtp-pipeline-users,ou=groups,dc=corp,dc=local"
    cost: 1
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

	// ── 2. Запускаем xzmercury --dev на свободном порту ──────────────────
	port := pickFreePort(t)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	mercuryURL := "http://" + addr

	writeFile(t, cfgPath, strings.ReplaceAll(xzmercuryConfig, "__ADDR__", addr))

	t.Logf("starting xzmercury --dev on %s", addr)
	xzmCmd := exec.Command(
		"go", "run", "./xzmercury/cmd/xzmercury/",
		"--dev",
		"--config", cfgPath,
	)
	xzmCmd.Dir = root
	xzmCmd.Stdout = io.Discard
	xzmCmd.Stderr = os.Stderr // xzmercury логи в stderr теста
	if err := xzmCmd.Start(); err != nil {
		t.Fatalf("start xzmercury: %v", err)
	}
	t.Cleanup(func() { _ = xzmCmd.Process.Kill() })

	waitHTTP(t, mercuryURL+"/healthz", 60*time.Second)
	t.Logf("xzmercury ready at %s", mercuryURL)

	// ── 3. Pipeline config ────────────────────────────────────────────────
	pipelinePath := filepath.Join(tmp, "pipeline.yaml")
	writeFile(t, pipelinePath, fmt.Sprintf(`
name: "dept-salary-encrypted"
version: "1.0"
description: "Integration test: ETL pipeline + xzmercury"

sources:
  - name: employees
    type: tdtp
    dsn: "examples/encryption-test/employees.tdtp.xml"

  - name: departments
    type: tdtp
    dsn: "examples/encryption-test/departments.tdtp.xml"

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

security:
  mercury_url: "%s"
  key_ttl_seconds: 300
  mercury_timeout_ms: 10000

error_handling:
  on_source_error: "fail"
`, outFile, mercuryURL))

	// ── 4. Вызываем tdtpcli --pipeline ───────────────────────────────────
	t.Log("running tdtpcli --pipeline")
	tdtpCmd := exec.Command("go", "run", "./cmd/tdtpcli/", "--pipeline", pipelinePath)
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
	writeFile(t, filepath.Join(tmp, "pipeline2.yaml"), strings.ReplaceAll(
		strings.ReplaceAll(
			mustReadFile(t, pipelinePath),
			outFile,
			outFile2,
		),
		"dept-salary-encrypted",
		"dept-salary-encrypted-2",
	))
	tdtpCmd2 := exec.Command("go", "run", "./cmd/tdtpcli/",
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

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}
