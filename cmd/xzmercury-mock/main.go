// xzmercury-mock — минимальный mock-сервер xZMercury для E2E тестирования.
//
// Реализует:
//   POST /api/keys/bind     — генерирует AES-256 ключ, сохраняет в памяти, возвращает {key_b64, hmac}
//   POST /api/keys/retrieve — возвращает ключ по UUID и удаляет (burn-on-read)
//   GET  /healthz           — liveness probe
//
// Запуск:
//   go run ./cmd/xzmercury-mock/ --addr :3000 --secret dev-secret
//
// Переменные окружения (альтернатива флагам):
//   MOCK_ADDR          — адрес (по умолчанию :3000)
//   MERCURY_SERVER_SECRET — HMAC-секрет (по умолчанию "dev-secret")
package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

type keyEntry struct {
	KeyB64    string
	Pipeline  string
	BoundAt   time.Time
}

var (
	mu      sync.Mutex
	keyStore = make(map[string]keyEntry) // uuid → entry
)

func main() {
	addr := flag.String("addr", envOr("MOCK_ADDR", ":3000"), "listen address")
	secret := flag.String("secret", envOr("MERCURY_SERVER_SECRET", "dev-secret"), "HMAC server secret")
	flag.Parse()

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handleHealthz)
	mux.HandleFunc("/api/keys/bind", makeBindHandler(*secret))
	mux.HandleFunc("/api/keys/retrieve", makeRetrieveHandler())

	log.Printf("[xzmercury-mock] listening on %s  secret=%q", *addr, *secret)
	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, `{"status":"ok"}`)
}

func makeBindHandler(secret string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			PackageUUID  string `json:"package_uuid"`
			PipelineName string `json:"pipeline_name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.PackageUUID == "" {
			http.Error(w, "package_uuid required", http.StatusBadRequest)
			return
		}

		// Генерируем AES-256 ключ
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			http.Error(w, "key generation failed", http.StatusInternalServerError)
			return
		}
		keyB64 := base64.StdEncoding.EncodeToString(key)

		// HMAC-SHA256(uuid, secret)
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(req.PackageUUID))
		hmacHex := hex.EncodeToString(mac.Sum(nil))

		// Сохраняем в памяти
		mu.Lock()
		keyStore[req.PackageUUID] = keyEntry{
			KeyB64:   keyB64,
			Pipeline: req.PipelineName,
			BoundAt:  time.Now().UTC(),
		}
		mu.Unlock()

		log.Printf("[bind] uuid=%s pipeline=%s", req.PackageUUID, req.PipelineName)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"key_b64": keyB64,
			"hmac":    hmacHex,
		})
	}
}

func makeRetrieveHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			PackageUUID string `json:"package_uuid"`
			Credentials string `json:"credentials"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}

		mu.Lock()
		entry, ok := keyStore[req.PackageUUID]
		if ok {
			delete(keyStore, req.PackageUUID) // burn-on-read
		}
		mu.Unlock()

		if !ok {
			log.Printf("[retrieve] uuid=%s NOT FOUND (already burned or never bound)", req.PackageUUID)
			http.Error(w, "key not found or already consumed", http.StatusNotFound)
			return
		}

		log.Printf("[retrieve] uuid=%s BURNED (pipeline=%s bound_at=%s)", req.PackageUUID, entry.Pipeline, entry.BoundAt.Format(time.RFC3339))

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"key_b64": entry.KeyB64,
		})
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
