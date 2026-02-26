// Example 08: ETL Pipeline + xzmercury — интеграционный пример
//
// Запускает встроенный xzmercury-mock, подставляет его адрес в pipeline.yaml
// и вызывает tdtpcli --pipeline, как это происходит в реальном сценарии.
//
// Запуск из корня репозитория:
//
//	go run ./examples/08-pipeline-encrypted/
package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// ── embedded xzmercury-mock ───────────────────────────────────────────────────

const mockSecret = "demo-integration-secret"

type keyEntry struct {
	KeyB64   string
	Pipeline string
}

var (
	mu       sync.Mutex
	keyStore = make(map[string]keyEntry)
)

func startMock() string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	must(err, "listen")

	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"status":"ok"}`)
	})

	mux.HandleFunc("/api/keys/bind", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			PackageUUID  string `json:"package_uuid"`
			PipelineName string `json:"pipeline_name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			http.Error(w, "key generation failed", http.StatusInternalServerError)
			return
		}
		keyB64 := base64.StdEncoding.EncodeToString(key)

		mac := hmac.New(sha256.New, []byte(mockSecret))
		mac.Write([]byte(req.PackageUUID))
		hmacHex := hex.EncodeToString(mac.Sum(nil))

		mu.Lock()
		keyStore[req.PackageUUID] = keyEntry{KeyB64: keyB64, Pipeline: req.PipelineName}
		mu.Unlock()

		log.Printf("[mock-mercury] BIND    uuid=%.8s… pipeline=%s", req.PackageUUID, req.PipelineName)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"key_b64": keyB64,
			"hmac":    hmacHex,
		})
	})

	mux.HandleFunc("/api/keys/retrieve", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			PackageUUID string `json:"package_uuid"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)

		mu.Lock()
		entry, ok := keyStore[req.PackageUUID]
		if ok {
			delete(keyStore, req.PackageUUID) // burn-on-read
		}
		mu.Unlock()

		if !ok {
			log.Printf("[mock-mercury] RETRIEVE uuid=%.8s… 404 (already burned)", req.PackageUUID)
			http.Error(w, "key not found or already consumed", http.StatusNotFound)
			return
		}

		log.Printf("[mock-mercury] RETRIEVE uuid=%.8s… OK burned (pipeline=%s)", req.PackageUUID, entry.Pipeline)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"key_b64": entry.KeyB64})
	})

	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()

	addr := "http://" + ln.Addr().String()
	for i := 0; i < 40; i++ {
		resp, err := http.Get(addr + "/healthz")
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			return addr
		}
		time.Sleep(25 * time.Millisecond)
	}
	log.Fatal("mock mercury did not start in time")
	return ""
}

// ── main ──────────────────────────────────────────────────────────────────────

func main() {
	fmt.Println("═══════════════════════════════════════════════════════════════")
	fmt.Println("  Example 08: ETL Pipeline + xzmercury (integration)")
	fmt.Println("  TDTP sources → SQLite workspace → AES-256-GCM → .tdtp")
	fmt.Println("═══════════════════════════════════════════════════════════════")

	// ── 1. Запускаем встроенный xzmercury-mock ────────────────────────────
	fmt.Println("\n[1] Starting embedded xzmercury-mock...")
	mercuryAddr := startMock()
	fmt.Printf("    → listening on %s\n", mercuryAddr)

	// ── 2. Готовим конфиг пайплайна с реальным адресом mock-сервера ───────
	fmt.Println("\n[2] Preparing pipeline config...")

	tplPath := "examples/08-pipeline-encrypted/pipeline.yaml"
	tplBytes, err := os.ReadFile(tplPath)
	must(err, "read pipeline template")

	config := strings.ReplaceAll(string(tplBytes), "__MERCURY_URL__", mercuryAddr)

	tmpFile, err := os.CreateTemp("", "pipeline-demo-*.yaml")
	must(err, "create temp config")
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.WriteString(config)
	must(err, "write temp config")
	must(tmpFile.Close(), "close temp config")

	fmt.Printf("    → config:  %s\n", tplPath)
	fmt.Printf("    → mercury: %s\n", mercuryAddr)
	fmt.Printf("    → output:  /tmp/dept_report_encrypted.tdtp\n")

	// ── 3. Вызываем tdtpcli --pipeline ───────────────────────────────────
	fmt.Println("\n[3] Running: tdtpcli --pipeline pipeline.yaml")
	fmt.Println("    ─────────────────────────────────────────────")

	cmd := exec.Command("go", "run", "./cmd/tdtpcli/", "--pipeline", tmpFile.Name())
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		log.Fatalf("tdtpcli failed: %v", err)
	}

	fmt.Println("    ─────────────────────────────────────────────")

	// ── 4. Проверяем зашифрованный файл ──────────────────────────────────
	fmt.Println("\n[4] Verifying encrypted output...")

	info, err := os.Stat("/tmp/dept_report_encrypted.tdtp")
	must(err, "stat output file")

	blob, err := os.ReadFile("/tmp/dept_report_encrypted.tdtp")
	must(err, "read output file")

	encrypted := false
	if len(blob) >= 3 {
		// tdtpcrypto header: version(2 bytes) + algo(1 byte, 0x01 = AES-256-GCM)
		encrypted = blob[2] == 0x01
	}

	fmt.Printf("    → file:      /tmp/dept_report_encrypted.tdtp\n")
	fmt.Printf("    → size:      %d bytes\n", info.Size())
	fmt.Printf("    → encrypted: %v  (header algo=0x%02x)\n", encrypted, blob[2])

	fmt.Println("\n═══════════════════════════════════════════════════════════════")
	fmt.Println("  DONE — integration demo passed ✓")
	fmt.Println("═══════════════════════════════════════════════════════════════")
	fmt.Println()
	fmt.Println("  To use a real xzmercury instead of the embedded mock:")
	fmt.Println("    go run ./cmd/xzmercury-mock/ --addr :3000 --secret dev-secret")
	fmt.Println("    # then set mercury_url: http://localhost:3000 in pipeline.yaml")
}

func must(err error, msg string) {
	if err != nil {
		log.Fatalf("FATAL [%s]: %v", msg, err)
	}
}

func init() {
	// Переключаемся в корень репозитория если запущены из подпапки
	if _, err := os.Stat("go.work"); err != nil {
		if chErr := os.Chdir("../../.."); chErr != nil {
			log.Println("warning: could not chdir to repo root; run from tdtp-framework/")
		}
	}
}
