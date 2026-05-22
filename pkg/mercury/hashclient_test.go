package mercury_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/mercury"
)

const (
	testUUID = "550e8400-e29b-41d4-a716-446655440000"
	testXXH3 = "a3f8b2c1d4e5f6a7b8c9d0e1f2a3b4c5"
	testPart = 0
)

func newHashClient(srv *httptest.Server) *mercury.Client {
	return mercury.NewClient(srv.URL, 3000)
}

// ─── RegisterHash ─────────────────────────────────────────────────────────────

func TestRegisterHash_Created(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/api/hashes/") {
			t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("X-Caller") == "" {
			t.Error("X-Caller missing")
		}
		// Verify body contains uuid, part, xxh3
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["uuid"] != testUUID {
			t.Errorf("uuid = %v", body["uuid"])
		}
		if body["xxh3"] != testXXH3 {
			t.Errorf("xxh3 = %v", body["xxh3"])
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"uuid": testUUID, "part": 0, "xxh3": testXXH3, "expires_in": "24h0m0s",
		})
	}))
	defer srv.Close()

	err := newHashClient(srv).RegisterHash(
		context.Background(), testUUID, testPart, testXXH3, "payroll", "svc-prod", "1.4")
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

// TestRegisterHash_Conflict_409: slot already taken → ErrHashRegisterFailed.
func TestRegisterHash_Conflict_409(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{"error": "hash already registered for this UUID+part"})
	}))
	defer srv.Close()

	err := newHashClient(srv).RegisterHash(
		context.Background(), testUUID, testPart, testXXH3, "t", "s", "1.4")
	if !errors.Is(err, mercury.ErrHashRegisterFailed) {
		t.Errorf("expected ErrHashRegisterFailed for 409, got %v", err)
	}
}

// TestRegisterHash_PreV14_Skipped: no HTTP call for pre-1.4 versions.
func TestRegisterHash_PreV14_Skipped(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer srv.Close()

	for _, ver := range []string{"1.0", "1.3.1", ""} {
		err := newHashClient(srv).RegisterHash(
			context.Background(), testUUID, testPart, testXXH3, "t", "s", ver)
		if err != nil {
			t.Errorf("version %q: expected nil (skip), got %v", ver, err)
		}
	}
	if called {
		t.Error("server must not be called for pre-1.4 packets")
	}
}

func TestRegisterHash_MercuryDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()

	err := newHashClient(srv).RegisterHash(
		context.Background(), testUUID, testPart, testXXH3, "t", "s", "1.4")
	if !errors.Is(err, mercury.ErrMercuryUnavailable) {
		t.Errorf("expected ErrMercuryUnavailable, got %v", err)
	}
}

func TestRegisterHash_EmptyXXH3_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	err := newHashClient(srv).RegisterHash(
		context.Background(), testUUID, testPart, "", "t", "s", "1.4")
	if err == nil {
		t.Error("expected error for empty xxh3")
	}
}

// ─── VerifyHash ───────────────────────────────────────────────────────────────

func TestVerifyHash_Match(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check URL shape: /api/hashes/{uuid}/{part}?xxh3=...
		if !strings.Contains(r.URL.Path, testUUID) {
			t.Errorf("uuid missing in path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("xxh3") != testXXH3 {
			t.Errorf("xxh3 query param = %q", r.URL.Query().Get("xxh3"))
		}
		json.NewEncoder(w).Encode(map[string]any{
			"registered": true, "match": true,
			"uuid": testUUID, "part": 0,
			"stored_xxh3":        testXXH3,
			"table":              "payroll_q1",
			"sender":             "svc-prod",
			"packet_version":     "1.4",
			"expires_in_seconds": 86399,
		})
	}))
	defer srv.Close()

	rec, err := newHashClient(srv).VerifyHash(context.Background(), testUUID, testPart, testXXH3, "1.4")
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if rec == nil {
		t.Fatal("expected non-nil record")
	}
	if rec.TableName != "payroll_q1" {
		t.Errorf("TableName = %q", rec.TableName)
	}
}

// TestVerifyHash_NotRegistered: registered:false → ErrHashNotRegistered.
func TestVerifyHash_NotRegistered(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"registered": false})
	}))
	defer srv.Close()

	_, err := newHashClient(srv).VerifyHash(context.Background(), testUUID, testPart, testXXH3, "1.4")
	if !errors.Is(err, mercury.ErrHashNotRegistered) {
		t.Errorf("expected ErrHashNotRegistered, got %v", err)
	}
}

// TestVerifyHash_Tampered: registered:true but match:false → ErrHashTampered.
func TestVerifyHash_Tampered(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"registered":  true,
			"match":       false,
			"stored_xxh3": "ffffffffffffffffffffffffffffffff",
		})
	}))
	defer srv.Close()

	fakeXXH3 := "ffffffffffffffffffffffffffffffff"
	_, err := newHashClient(srv).VerifyHash(context.Background(), testUUID, testPart, fakeXXH3, "1.4")
	if !errors.Is(err, mercury.ErrHashTampered) {
		t.Errorf("expected ErrHashTampered, got %v", err)
	}
}

// TestVerifyHash_MercuryDown_FailClosed: unavailable → BLOCK.
func TestVerifyHash_MercuryDown_FailClosed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()

	_, err := newHashClient(srv).VerifyHash(context.Background(), testUUID, testPart, testXXH3, "1.4")
	if !errors.Is(err, mercury.ErrMercuryUnavailable) {
		t.Errorf("expected ErrMercuryUnavailable (fail-closed), got %v", err)
	}
}

// TestVerifyHash_PreV14_PassThrough: no HTTP call, returns (nil, nil).
func TestVerifyHash_PreV14_PassThrough(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer srv.Close()

	for _, ver := range []string{"1.0", "1.3.1", ""} {
		rec, err := newHashClient(srv).VerifyHash(context.Background(), testUUID, testPart, testXXH3, ver)
		if err != nil || rec != nil {
			t.Errorf("version %q: expected (nil,nil) pass-through, got (%v, %v)", ver, rec, err)
		}
	}
	if called {
		t.Error("server must not be called for pre-1.4 packets")
	}
}

// ─── Behaviour contract ───────────────────────────────────────────────────────

// TestHashNotBurnedOnRead: client sends N GETs, each returns the same result.
func TestHashNotBurnedOnRead(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		json.NewEncoder(w).Encode(map[string]any{
			"registered": true, "match": true,
			"uuid": testUUID, "part": 0,
			"stored_xxh3": testXXH3, "table": "t",
			"sender": "s", "packet_version": "1.4",
		})
	}))
	defer srv.Close()

	c := newHashClient(srv)
	for range 5 {
		rec, err := c.VerifyHash(context.Background(), testUUID, testPart, testXXH3, "1.4")
		if err != nil || rec == nil {
			t.Fatalf("read failed: err=%v rec=%v", err, rec)
		}
	}
	if calls != 5 {
		t.Errorf("expected 5 GET calls (not burn-on-read), got %d", calls)
	}
}
