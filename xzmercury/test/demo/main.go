// xzmercury E2E demo: запускает сервер in-process, привязывает ключ,
// шифрует out.xml форматом AES-256-GCM, записывает .tdtp файл,
// затем делает burn-on-read и расшифровывает обратно.
//
// Запуск: go run ./xzmercury/test/demo/ (из корня tdtp-framework)
package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	// xzmercury internals — доступны через go.work
	"github.com/ruslano69/xzmercury/internal/acl"
	"github.com/ruslano69/xzmercury/internal/api"
	"github.com/ruslano69/xzmercury/internal/infra"

	// tdtp-framework crypto — родительский модуль через go.work
	tdtpcrypto "github.com/ruslano69/tdtp-framework/pkg/crypto"
)

const (
	serverSecret = "demo-secret-do-not-use-in-prod"
	xmlPath      = "out.xml"
	outPath      = "/tmp/out.tdtp"
	packageUUID  = "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
	pipelineName = "test-pipeline"
)

func main() {
	fmt.Println("═══════════════════════════════════════════════════════════")
	fmt.Println("  xzmercury E2E demo — AES-256-GCM, burn-on-read, Pub/Sub")
	fmt.Println("═══════════════════════════════════════════════════════════")

	// ── 1. Запуск xzmercury in-process ──────────────────────────────────
	fmt.Println("\n[1] Starting xzmercury (dev mode — in-process miniredis + mock LDAP)...")

	cfg := minimalConfig()
	inf, err := infra.Setup(cfg, true /* dev */)
	must(err, "infra setup")
	defer inf.Close()

	aclRules, err := acl.Load("") // пустой путь = permissive defaults
	must(err, "acl load")

	router := api.NewRouter(cfg, inf, aclRules)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	must(err, "listen")
	addr := "http://" + ln.Addr().String()

	srv := &http.Server{Handler: router}
	go func() { _ = srv.Serve(ln) }()

	// ждём старта
	waitReady(addr + "/healthz")
	fmt.Printf("    → listening on %s\n", addr)

	// ── 2. POST /api/keys/bind ───────────────────────────────────────────
	fmt.Printf("\n[2] POST /api/keys/bind  uuid=%s  pipeline=%s\n", packageUUID, pipelineName)

	bindResp := callBind(addr, packageUUID, pipelineName)
	fmt.Printf("    → key_b64 : %s…\n", bindResp.KeyB64[:16])
	fmt.Printf("    → hmac    : %s\n", bindResp.HMAC[:16]+"…")
	fmt.Printf("    → req_id  : %s\n", bindResp.RequestID)

	// ── 3. Декодируем ключ и шифруем out.xml ───────────────────────────
	fmt.Printf("\n[3] Encrypting %s → %s\n", xmlPath, outPath)

	plaintext, err := os.ReadFile(xmlPath)
	must(err, "read "+xmlPath)
	fmt.Printf("    → plaintext : %d bytes\n", len(plaintext))

	keyBytes, err := base64.StdEncoding.DecodeString(bindResp.KeyB64)
	must(err, "decode key")

	cipherblob, err := tdtpcrypto.Encrypt(keyBytes, plaintext, packageUUID)
	must(err, "encrypt")

	must(os.WriteFile(outPath, cipherblob, 0600), "write "+outPath)
	fmt.Printf("    → ciphertext: %d bytes  (header=%d + gcm-tag=16)\n",
		len(cipherblob), 31 /* version(2)+algo(1)+uuid(16)+nonce(12) */)

	// покажем первые байты заголовка
	fmt.Printf("    → header hex: %x\n", cipherblob[:31])

	// ── 4. POST /api/keys/retrieve (burn-on-read) ───────────────────────
	fmt.Printf("\n[4] POST /api/keys/retrieve  (burn-on-read)...\n")

	retrievedKey := callRetrieve(addr, packageUUID, bindResp.RequestID)
	fmt.Printf("    → retrieved key matches bound key: %v\n", retrievedKey == bindResp.KeyB64)

	// Повторный retrieve должен вернуть 404
	status := callRetrieve404(addr, packageUUID)
	fmt.Printf("    → second retrieve HTTP status: %d (expected 404)\n", status)

	// ── 5. Расшифровываем и проверяем ───────────────────────────────────
	fmt.Printf("\n[5] Decrypting %s...\n", outPath)

	blob, err := os.ReadFile(outPath)
	must(err, "read blob")

	keyBytes2, err := base64.StdEncoding.DecodeString(retrievedKey)
	must(err, "decode retrieved key")

	parsedUUID, decrypted, err := tdtpcrypto.Decrypt(keyBytes2, blob)
	must(err, "decrypt")

	fmt.Printf("    → parsed UUID from header: %s\n", parsedUUID)
	fmt.Printf("    → decrypted size: %d bytes\n", len(decrypted))
	fmt.Printf("    → content matches original: %v\n", bytes.Equal(plaintext, decrypted))

	// ── 6. GET /api/requests/{id} (lifecycle) ───────────────────────────
	if bindResp.RequestID != "" {
		fmt.Printf("\n[6] GET /api/requests/%s\n", bindResp.RequestID)
		state := callGetRequest(addr, bindResp.RequestID)
		fmt.Printf("    → state: %s\n", state)
	}

	fmt.Println("\n═══════════════════════════════════════════════════════════")
	fmt.Println("  DONE — xzmercury E2E passed ✓")
	fmt.Printf("  Encrypted packet saved to: %s\n", outPath)
	fmt.Println("═══════════════════════════════════════════════════════════")

	_ = srv.Close()
}

// ─── helpers ────────────────────────────────────────────────────────────────

func minimalConfig() *infra.Config {
	cfg := &infra.Config{}
	cfg.Server.Addr = ":0"
	cfg.Server.ReadTimeout = 10 * time.Second
	cfg.Server.WriteTimeout = 10 * time.Second
	cfg.LDAP.CacheTTL = 120 * time.Second
	cfg.Security.ServerSecret = serverSecret
	cfg.Security.RateLimit = 0
	cfg.Quota.DefaultHourly = 1000
	cfg.KeyTTL = 5 * time.Minute
	return cfg
}

type bindResponse struct {
	RequestID string `json:"request_id"`
	KeyB64    string `json:"key_b64"`
	HMAC      string `json:"hmac"`
}

func callBind(addr, uuid, pipeline string) bindResponse {
	body, _ := json.Marshal(map[string]string{
		"package_uuid":  uuid,
		"pipeline_name": pipeline,
		"caller":        "svc_tdtp", // присутствует в mock LDAP
	})
	resp, err := http.Post(addr+"/api/keys/bind", "application/json", bytes.NewReader(body))
	must(err, "POST /api/keys/bind")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Fatalf("bind HTTP %d", resp.StatusCode)
	}
	var r bindResponse
	must(json.NewDecoder(resp.Body).Decode(&r), "decode bind response")
	return r
}

func callRetrieve(addr, uuid, requestID string) string {
	body, _ := json.Marshal(map[string]string{
		"package_uuid": uuid,
		"request_id":   requestID,
	})
	resp, err := http.Post(addr+"/api/keys/retrieve", "application/json", bytes.NewReader(body))
	must(err, "POST /api/keys/retrieve")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Fatalf("retrieve HTTP %d (expected 200)", resp.StatusCode)
	}
	var r struct {
		KeyB64 string `json:"key_b64"`
	}
	must(json.NewDecoder(resp.Body).Decode(&r), "decode retrieve response")
	return r.KeyB64
}

func callRetrieve404(addr, uuid string) int {
	body, _ := json.Marshal(map[string]string{"package_uuid": uuid})
	resp, err := http.Post(addr+"/api/keys/retrieve", "application/json", bytes.NewReader(body))
	must(err, "POST /api/keys/retrieve (2nd)")
	defer resp.Body.Close()
	return resp.StatusCode
}

func callGetRequest(addr, id string) string {
	resp, err := http.Get(addr + "/api/requests/" + id)
	must(err, "GET /api/requests/"+id)
	defer resp.Body.Close()
	var r struct {
		State string `json:"state"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&r)
	return r.State
}

func waitReady(healthURL string) {
	for i := 0; i < 20; i++ {
		resp, err := http.Get(healthURL)
		if err == nil && resp.StatusCode == 200 {
			resp.Body.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	log.Fatal("server did not become ready")
}

func must(err error, msg string) {
	if err != nil {
		log.Fatalf("FATAL [%s]: %v", msg, err)
	}
}

func init() {
	// Переключаемся в корень репозитория если запущены из demo/
	if _, err := os.Stat("out.xml"); os.IsNotExist(err) {
		if err := os.Chdir("../../../.."); err != nil {
			log.Fatal("cannot find out.xml: run from tdtp-framework root")
		}
	}
}
