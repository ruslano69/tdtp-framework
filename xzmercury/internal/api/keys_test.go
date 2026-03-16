package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/ruslano69/xzmercury/internal/acl"
	"github.com/ruslano69/xzmercury/internal/keystore"
	"github.com/ruslano69/xzmercury/internal/ldap"
	"github.com/ruslano69/xzmercury/internal/quota"
	"github.com/ruslano69/xzmercury/internal/request"
)

// newTestHandler создаёт keysHandler с miniredis и mock-зависимостями.
// Используется dev-дефолты LDAP: svc_tdtp/analyst1 → tdtp-pipeline-users, readonly → tdtp-readonly.
func newTestHandler(t *testing.T) *keysHandler {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	mockLDAP, err := ldap.NewMockClient("")
	if err != nil {
		t.Fatalf("ldap.NewMockClient: %v", err)
	}
	defaultACL, err := acl.Load("")
	if err != nil {
		t.Fatalf("acl.Load: %v", err)
	}

	return &keysHandler{
		store:   keystore.New(rdb, "test-secret", time.Hour),
		quota:   quota.New(rdb, 100),
		ldap:    mockLDAP,
		acl:     defaultACL,
		tracker: request.New(rdb),
	}
}

// newHandlerWithQuota создаёт handler с заданным hourly-балансом.
func newHandlerWithQuota(t *testing.T, hourly int) *keysHandler {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	mockLDAP, _ := ldap.NewMockClient("")
	defaultACL, _ := acl.Load("")

	return &keysHandler{
		store:   keystore.New(rdb, "test-secret", time.Hour),
		quota:   quota.New(rdb, hourly),
		ldap:    mockLDAP,
		acl:     defaultACL,
		tracker: request.New(rdb),
	}
}

// postJSON отправляет POST с JSON-телом и возвращает ResponseRecorder.
func postJSON(handler http.HandlerFunc, body any) *httptest.ResponseRecorder {
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()
	handler(rw, req)
	return rw
}

// ────────────────────────────────────────────────────────────────────────────
// POST /api/keys/bind
// ────────────────────────────────────────────────────────────────────────────

func TestBind_Success_NoCaller(t *testing.T) {
	// caller="" → LDAP-проверка пропускается (service-to-service режим)
	h := newTestHandler(t)
	rw := postJSON(h.Bind, map[string]string{
		"package_uuid":  "e6de8dd5-4e9a-4c6b-8f3a-1234567890ab",
		"pipeline_name": "salary-pipeline",
	})

	if rw.Code != http.StatusOK {
		t.Fatalf("Bind() status = %d, want 200. Body: %s", rw.Code, rw.Body.String())
	}

	var resp bindResponse
	_ = json.NewDecoder(rw.Body).Decode(&resp)
	if resp.KeyB64 == "" {
		t.Error("Bind() response missing key_b64")
	}
	if resp.HMAC == "" {
		t.Error("Bind() response missing hmac")
	}
}

func TestBind_Success_AuthorizedCaller(t *testing.T) {
	// svc_tdtp состоит в tdtp-pipeline-users (default_group)
	h := newTestHandler(t)
	rw := postJSON(h.Bind, map[string]string{
		"package_uuid":  "e6de8dd5-4e9a-4c6b-8f3a-1234567890ab",
		"pipeline_name": "salary-pipeline",
		"caller":        "svc_tdtp",
	})

	if rw.Code != http.StatusOK {
		t.Errorf("Bind() status = %d, want 200. Body: %s", rw.Code, rw.Body.String())
	}
}

func TestBind_CallerNotInGroup_Returns403(t *testing.T) {
	// readonly состоит только в tdtp-readonly, не в tdtp-pipeline-users
	h := newTestHandler(t)
	rw := postJSON(h.Bind, map[string]string{
		"package_uuid":  "uuid-1",
		"pipeline_name": "salary-pipeline",
		"caller":        "readonly",
	})

	if rw.Code != http.StatusForbidden {
		t.Errorf("Bind() status = %d, want 403. Body: %s", rw.Code, rw.Body.String())
	}
}

func TestBind_QuotaExceeded_Returns429(t *testing.T) {
	// hourly=0 → любой запрос превышает квоту
	h := newHandlerWithQuota(t, 0)
	rw := postJSON(h.Bind, map[string]string{
		"package_uuid":  "uuid-1",
		"pipeline_name": "salary-pipeline",
	})

	if rw.Code != http.StatusTooManyRequests {
		t.Errorf("Bind() status = %d, want 429. Body: %s", rw.Code, rw.Body.String())
	}
}

func TestBind_MissingUUID_Returns400(t *testing.T) {
	h := newTestHandler(t)
	rw := postJSON(h.Bind, map[string]string{"pipeline_name": "pipeline"})

	if rw.Code != http.StatusBadRequest {
		t.Errorf("Bind() status = %d, want 400", rw.Code)
	}
}

func TestBind_MissingPipeline_Returns400(t *testing.T) {
	h := newTestHandler(t)
	rw := postJSON(h.Bind, map[string]string{"package_uuid": "uuid-1"})

	if rw.Code != http.StatusBadRequest {
		t.Errorf("Bind() status = %d, want 400", rw.Code)
	}
}

func TestBind_InvalidJSON_Returns400(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte("{broken")))
	rw := httptest.NewRecorder()
	h.Bind(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Errorf("Bind() status = %d, want 400", rw.Code)
	}
}

func TestBind_ResponseContainsRequestID(t *testing.T) {
	h := newTestHandler(t)
	rw := postJSON(h.Bind, map[string]string{
		"package_uuid":  "uuid-with-tracker",
		"pipeline_name": "salary-pipeline",
		"caller":        "svc_tdtp",
	})

	if rw.Code != http.StatusOK {
		t.Fatalf("Bind() status = %d. Body: %s", rw.Code, rw.Body.String())
	}

	var resp bindResponse
	_ = json.NewDecoder(rw.Body).Decode(&resp)
	if resp.RequestID == "" {
		t.Error("Bind() response missing request_id")
	}
}

// ────────────────────────────────────────────────────────────────────────────
// POST /api/keys/retrieve
// ────────────────────────────────────────────────────────────────────────────

func TestRetrieve_Success_BurnOnRead(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	uuid := "e6de8dd5-4e9a-4c6b-8f3a-1234567890ab"

	// Привязываем ключ напрямую через store
	if _, err := h.store.Bind(ctx, uuid, "pipeline"); err != nil {
		t.Fatalf("store.Bind() error = %v", err)
	}

	rw := postJSON(h.Retrieve, map[string]string{"package_uuid": uuid})

	if rw.Code != http.StatusOK {
		t.Fatalf("Retrieve() status = %d, want 200. Body: %s", rw.Code, rw.Body.String())
	}

	var resp retrieveResponse
	_ = json.NewDecoder(rw.Body).Decode(&resp)
	if resp.KeyB64 == "" {
		t.Error("Retrieve() response missing key_b64")
	}
}

func TestRetrieve_SecondCall_Returns404(t *testing.T) {
	// Burn-on-read: повторный вызов возвращает 404
	h := newTestHandler(t)
	ctx := context.Background()
	uuid := "burn-uuid"

	_, _ = h.store.Bind(ctx, uuid, "pipeline")

	postJSON(h.Retrieve, map[string]string{"package_uuid": uuid}) // первый — успех
	rw := postJSON(h.Retrieve, map[string]string{"package_uuid": uuid}) // второй — 404

	if rw.Code != http.StatusNotFound {
		t.Errorf("второй Retrieve() status = %d, want 404", rw.Code)
	}
}

func TestRetrieve_NeverBound_Returns404(t *testing.T) {
	h := newTestHandler(t)
	rw := postJSON(h.Retrieve, map[string]string{"package_uuid": "unknown-uuid"})

	if rw.Code != http.StatusNotFound {
		t.Errorf("Retrieve() status = %d, want 404", rw.Code)
	}
}

func TestRetrieve_MissingUUID_Returns400(t *testing.T) {
	h := newTestHandler(t)
	rw := postJSON(h.Retrieve, map[string]string{})

	if rw.Code != http.StatusBadRequest {
		t.Errorf("Retrieve() status = %d, want 400", rw.Code)
	}
}

func TestRetrieve_InvalidJSON_Returns400(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte("{broken")))
	rw := httptest.NewRecorder()
	h.Retrieve(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Errorf("Retrieve() status = %d, want 400", rw.Code)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Полный цикл: Bind → Retrieve (как видит клиент)
// ────────────────────────────────────────────────────────────────────────────

func TestFullCycle_BindThenRetrieve(t *testing.T) {
	h := newTestHandler(t)
	uuid := "full-cycle-uuid"

	// 1. Bind
	bindRW := postJSON(h.Bind, map[string]string{
		"package_uuid":  uuid,
		"pipeline_name": "salary-pipeline",
	})
	if bindRW.Code != http.StatusOK {
		t.Fatalf("Bind() status = %d. Body: %s", bindRW.Code, bindRW.Body.String())
	}

	var bindResp bindResponse
	_ = json.NewDecoder(bindRW.Body).Decode(&bindResp)

	// 2. Retrieve
	retrieveRW := postJSON(h.Retrieve, map[string]string{"package_uuid": uuid})
	if retrieveRW.Code != http.StatusOK {
		t.Fatalf("Retrieve() status = %d. Body: %s", retrieveRW.Code, retrieveRW.Body.String())
	}

	var retrieveResp retrieveResponse
	_ = json.NewDecoder(retrieveRW.Body).Decode(&retrieveResp)

	// Ключ должен совпасть
	if retrieveResp.KeyB64 != bindResp.KeyB64 {
		t.Error("Retrieve() вернул ключ, отличный от Bind()")
	}

	// 3. Повторный Retrieve → 404 (burn-on-read)
	rw := postJSON(h.Retrieve, map[string]string{"package_uuid": uuid})
	if rw.Code != http.StatusNotFound {
		t.Errorf("повторный Retrieve() status = %d, want 404", rw.Code)
	}
}
