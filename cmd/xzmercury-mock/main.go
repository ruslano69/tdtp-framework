// xzmercury-mock — минимальный mock-сервер xZMercury для E2E тестирования.
//
// Реализует:
//
//	POST /api/keys/bind     — генерирует AES-256 ключ, сохраняет в памяти, возвращает {key_b64, hmac}
//	POST /api/keys/retrieve — возвращает ключ по UUID и удаляет (burn-on-read)
//	POST /api/hashes/       — регистрирует xxh3 пакета за слотом uuid:part (SET NX)
//	GET  /api/hashes/{uuid}/{part}?xxh3=… — сверяет предъявленный xxh3 с сохранённым
//	GET  /healthz           — liveness probe
//
// Запуск:
//
//	go run ./cmd/xzmercury-mock/ --addr :3000 --secret dev-secret
//
// Переменные окружения (альтернатива флагам):
//
//	MOCK_ADDR          — адрес (по умолчанию :3000)
//	MERCURY_SERVER_SECRET — HMAC-секрет (по умолчанию "dev-secret")
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
	"strconv"
	"strings"
	"sync"
	"time"
)

type keyEntry struct {
	KeyB64   string
	Pipeline string
	BoundAt  time.Time
}

// hashEntry — одна регистрация целостности пакета (v1.4+).
type hashEntry struct {
	XXH3          string
	TableName     string
	Sender        string
	PacketVersion string
	RegisteredAt  time.Time
}

var (
	mu        sync.Mutex
	keyStore  = make(map[string]keyEntry)  // uuid → entry
	hashStore = make(map[string]hashEntry) // "uuid:part" → entry
)

func main() {
	addr := flag.String("addr", envOr("MOCK_ADDR", ":3000"), "listen address")
	secret := flag.String("secret", envOr("MERCURY_SERVER_SECRET", "dev-secret"), "HMAC server secret")
	flag.Parse()

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handleHealthz)
	mux.HandleFunc("/api/keys/bind", makeBindHandler(*secret))
	mux.HandleFunc("/api/keys/retrieve", makeRetrieveHandler())
	mux.HandleFunc("/api/hashes/", handleHashes)

	log.Printf("[xzmercury-mock] listening on %s  secret=%q", *addr, *secret)
	if err := http.ListenAndServe(*addr, mux); err != nil { //nolint:gosec // G114: mock server, no timeout needed
		log.Fatalf("server error: %v", err)
	}
}

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintln(w, `{"status":"ok"}`)
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
		_ = json.NewEncoder(w).Encode(map[string]string{
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
		_ = json.NewEncoder(w).Encode(map[string]string{
			"key_b64": entry.KeyB64,
		})
	}
}

// handleHashes реализует реестр целостности, который появился в клиенте вместе
// с TDTP v1.4 (pkg/mercury/hashclient.go) и до сих пор отсутствовал здесь.
// Из-за этого любой `--export --enc` пакета v1.4+ падал против мока на
// HTTP 404 в RegisterHash — то есть mock не покрывал единственный сценарий,
// ради которого его и запускают в E2E-тестах.
//
// Один обработчик на префикс: POST регистрирует, GET сверяет — так же, как
// у настоящего сервиса, где это один ресурс.
func handleHashes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		handleHashRegister(w, r)
	case http.MethodGet:
		handleHashVerify(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleHashRegister — POST /api/hashes/
//
// Семантика SET NX, как у настоящего реестра: слот uuid:part занимается один
// раз. Повторная регистрация даёт 409, и это не ошибка ретрая, а сигнал —
// либо продюсер повторяется, либо слот занял кто-то другой; сохранённый хеш
// в обоих случаях остаётся прежним.
func handleHashRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UUID          string `json:"uuid"`
		Part          int    `json:"part"`
		XXH3          string `json:"xxh3"`
		TableName     string `json:"table"`
		Sender        string `json:"sender"`
		PacketVersion string `json:"packet_version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.UUID == "" || req.XXH3 == "" {
		http.Error(w, "uuid and xxh3 are required", http.StatusBadRequest)
		return
	}

	slot := fmt.Sprintf("%s:%d", req.UUID, req.Part)

	mu.Lock()
	_, taken := hashStore[slot]
	if !taken {
		hashStore[slot] = hashEntry{
			XXH3:          req.XXH3,
			TableName:     req.TableName,
			Sender:        req.Sender,
			PacketVersion: req.PacketVersion,
			RegisteredAt:  time.Now(),
		}
	}
	mu.Unlock()

	if taken {
		log.Printf("[hash-register] slot=%s CONFLICT (already registered)", slot)
		http.Error(w, "slot already registered", http.StatusConflict)
		return
	}

	log.Printf("[hash-register] slot=%s xxh3=%s table=%s sender=%s version=%s",
		slot, req.XXH3, req.TableName, req.Sender, req.PacketVersion)
	w.WriteHeader(http.StatusCreated)
}

// handleHashVerify — GET /api/hashes/{uuid}/{part}?xxh3=…
//
// Отвечает 200 всегда, когда запрос разобран: «не зарегистрирован» и
// «не совпало» — это данные ответа (registered/match), а не HTTP-ошибки.
// Так устроен клиент: он различает ErrHashNotRegistered и ErrHashTampered
// именно по этим полям, а на не-200 реагирует как на сбой самого Mercury.
func handleHashVerify(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/hashes/")
	parts := strings.Split(rest, "/")
	if len(parts) != 2 || parts[0] == "" {
		http.Error(w, "expected /api/hashes/{uuid}/{part}", http.StatusBadRequest)
		return
	}
	uuid, partStr := parts[0], parts[1]
	part, err := strconv.Atoi(partStr)
	if err != nil {
		http.Error(w, "part must be an integer", http.StatusBadRequest)
		return
	}
	claimed := r.URL.Query().Get("xxh3")

	slot := fmt.Sprintf("%s:%d", uuid, part)

	mu.Lock()
	entry, found := hashStore[slot]
	mu.Unlock()

	resp := map[string]any{
		"registered":         found,
		"match":              found && entry.XXH3 == claimed,
		"uuid":               uuid,
		"part":               part,
		"stored_xxh3":        entry.XXH3,
		"table":              entry.TableName,
		"sender":             entry.Sender,
		"packet_version":     entry.PacketVersion,
		"expires_in_seconds": int64(0), // mock не истекает
	}

	log.Printf("[hash-verify] slot=%s registered=%v match=%v", slot, found, resp["match"])

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
