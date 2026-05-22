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

// fakeHash is a valid 32-char hex string used across tests.
const fakeHash = "a3f8b2c1d4e5f6a7b8c9d0e1f2a3b4c5"

// newHashClient creates a Client pointed at the test server.
func newHashClient(srv *httptest.Server) *mercury.Client {
	return mercury.NewClient(srv.URL, 3000)
}

// ─── RegisterHash ─────────────────────────────────────────────────────────────

func TestRegisterHash_Created(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/api/hashes/") {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("X-Caller") == "" {
			t.Error("X-Caller header missing")
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"hash": fakeHash, "expires_in": "24h0m0s"})
	}))
	defer srv.Close()

	err := newHashClient(srv).RegisterHash(
		context.Background(), fakeHash, "payroll", "axapta-prod", "1.4", "svc-account")
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestRegisterHash_AlreadyRegistered_IsIdempotent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Server returns 200 OK for already-registered
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"hash": fakeHash, "expires_in": "23h59m"})
	}))
	defer srv.Close()

	// Must not return an error — idempotent
	err := newHashClient(srv).RegisterHash(
		context.Background(), fakeHash, "payroll", "axapta-prod", "1.4", "svc-account")
	if err != nil {
		t.Errorf("already-registered should be treated as success, got: %v", err)
	}
}

// TestRegisterHash_PreV14_Skipped: pre-1.4 packets bypass hash registration entirely.
func TestRegisterHash_PreV14_Skipped(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	for _, ver := range []string{"1.0", "1.3.1", ""} {
		err := newHashClient(srv).RegisterHash(
			context.Background(), fakeHash, "t", "s", ver, "caller")
		if err != nil {
			t.Errorf("version %q: expected nil (skip), got %v", ver, err)
		}
	}
	if called {
		t.Error("server should not be called for pre-1.4 packets")
	}
}

func TestRegisterHash_MercuryDown_ReturnsUnavailable(t *testing.T) {
	// Point at a closed server to simulate Mercury unavailable
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close() // close immediately

	err := newHashClient(srv).RegisterHash(
		context.Background(), fakeHash, "t", "s", "1.4", "caller")
	if !errors.Is(err, mercury.ErrMercuryUnavailable) {
		t.Errorf("expected ErrMercuryUnavailable, got %v", err)
	}
}

func TestRegisterHash_EmptyHash_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	err := newHashClient(srv).RegisterHash(
		context.Background(), "", "t", "s", "1.4", "caller")
	if err == nil {
		t.Error("expected error for empty hash")
	}
}

// ─── VerifyHash ───────────────────────────────────────────────────────────────

func TestVerifyHash_Registered(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"registered":         true,
			"hash":               fakeHash,
			"table":              "payroll_q1",
			"sender":             "axapta-prod",
			"packet_version":     "1.4",
			"expires_in_seconds": 86399,
		})
	}))
	defer srv.Close()

	rec, err := newHashClient(srv).VerifyHash(context.Background(), fakeHash, "1.4")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if rec == nil {
		t.Fatal("expected non-nil HashRecord")
	}
	if rec.TableName != "payroll_q1" {
		t.Errorf("TableName = %q, want payroll_q1", rec.TableName)
	}
	if rec.ExpiresInSeconds != 86399 {
		t.Errorf("ExpiresInSeconds = %d, want 86399", rec.ExpiresInSeconds)
	}
}

// TestVerifyHash_NotRegistered: server says registered:false → ErrHashNotRegistered.
func TestVerifyHash_NotRegistered(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"registered": false, "hash": fakeHash})
	}))
	defer srv.Close()

	_, err := newHashClient(srv).VerifyHash(context.Background(), fakeHash, "1.4")
	if !errors.Is(err, mercury.ErrHashNotRegistered) {
		t.Errorf("expected ErrHashNotRegistered, got %v", err)
	}
}

// TestVerifyHash_MercuryDown_FailClosed: unavailable Mercury → BLOCK (fail-closed).
func TestVerifyHash_MercuryDown_FailClosed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()

	_, err := newHashClient(srv).VerifyHash(context.Background(), fakeHash, "1.4")
	if !errors.Is(err, mercury.ErrMercuryUnavailable) {
		t.Errorf("expected ErrMercuryUnavailable (fail-closed), got %v", err)
	}
}

// TestVerifyHash_PreV14_PassThrough: pre-1.4 packets bypass verification.
func TestVerifyHash_PreV14_PassThrough(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer srv.Close()

	for _, ver := range []string{"1.0", "1.3.1", ""} {
		rec, err := newHashClient(srv).VerifyHash(context.Background(), fakeHash, ver)
		if err != nil || rec != nil {
			t.Errorf("version %q: expected (nil, nil) pass-through, got (%v, %v)", ver, rec, err)
		}
	}
	if called {
		t.Error("server should not be called for pre-1.4 packets")
	}
}

// TestVerifyHash_ServerError_ReturnsError: 500 → ErrMercuryError.
func TestVerifyHash_ServerError_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := newHashClient(srv).VerifyHash(context.Background(), fakeHash, "1.4")
	if !errors.Is(err, mercury.ErrMercuryError) {
		t.Errorf("expected ErrMercuryError for 500, got %v", err)
	}
}

// ─── Behaviour contract ───────────────────────────────────────────────────────

// TestHashNotBurnedOnRead: Verify server is called N times without destructive side effects.
// (Server-side burn-on-read test is in xzmercury/internal/hashstore/store_test.go;
// here we verify the client sends N GETs and each returns the same result.)
func TestHashNotBurnedOnRead(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		json.NewEncoder(w).Encode(map[string]any{
			"registered": true, "hash": fakeHash, "table": "t",
			"sender": "s", "packet_version": "1.4",
		})
	}))
	defer srv.Close()

	c := newHashClient(srv)
	for range 5 {
		rec, err := c.VerifyHash(context.Background(), fakeHash, "1.4")
		if err != nil || rec == nil {
			t.Fatalf("read failed: err=%v rec=%v", err, rec)
		}
	}
	if callCount != 5 {
		t.Errorf("expected 5 GET calls, got %d", callCount)
	}
}
