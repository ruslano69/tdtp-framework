package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func mustHash(t *testing.T, password string) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("bcrypt.GenerateFromPassword: %v", err)
	}
	return string(hash)
}

// ─── TokenStore ─────────────────────────────────────────────────────────────

func TestTokenStore_IssueAndValidate(t *testing.T) {
	store := NewTokenStore(time.Hour)

	token, expiresAt, err := store.Issue("alice")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if token == "" {
		t.Fatal("Issue returned empty token")
	}
	if !expiresAt.After(time.Now()) {
		t.Fatal("expiresAt should be in the future")
	}

	username, ok := store.Validate(token)
	if !ok {
		t.Fatal("Validate: expected ok=true for a freshly issued token")
	}
	if username != "alice" {
		t.Fatalf("Validate: expected username=alice, got %q", username)
	}
}

func TestTokenStore_Validate_UnknownToken(t *testing.T) {
	store := NewTokenStore(time.Hour)
	if _, ok := store.Validate("does-not-exist"); ok {
		t.Fatal("Validate: expected ok=false for an unknown token")
	}
}

func TestTokenStore_Validate_ExpiredToken(t *testing.T) {
	store := NewTokenStore(-time.Second) // already expired the instant it's issued
	token, _, err := store.Issue("bob")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, ok := store.Validate(token); ok {
		t.Fatal("Validate: expected ok=false for an expired token")
	}
	// Second call: confirms the lazy-cleanup delete didn't panic or leave
	// the entry in some inconsistent state.
	if _, ok := store.Validate(token); ok {
		t.Fatal("Validate: expired token should stay invalid after cleanup")
	}
}

func TestTokenStore_IssueUniqueness(t *testing.T) {
	store := NewTokenStore(time.Hour)
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		token, _, err := store.Issue("alice")
		if err != nil {
			t.Fatalf("Issue: %v", err)
		}
		if seen[token] {
			t.Fatalf("Issue produced a duplicate token: %s", token)
		}
		seen[token] = true
	}
}

// ─── checkPassword ──────────────────────────────────────────────────────────

func TestCheckPassword(t *testing.T) {
	users := []UserConfig{
		{Username: "alice", PasswordHash: mustHash(t, "correct-horse")},
	}

	if !checkPassword(users, "alice", "correct-horse") {
		t.Error("expected correct username/password to succeed")
	}
	if checkPassword(users, "alice", "wrong-password") {
		t.Error("expected wrong password to fail")
	}
	if checkPassword(users, "unknown-user", "correct-horse") {
		t.Error("expected unknown username to fail")
	}
}

// ─── handleAPILogin ─────────────────────────────────────────────────────────

func newAuthTestServer(t *testing.T, enabled bool) *Server {
	t.Helper()
	cfg := &ServeConfig{}
	if enabled {
		cfg.Auth = &AuthConfig{
			Enabled:  true,
			TokenTTL: "1h",
			Users: []UserConfig{
				{Username: "alice", PasswordHash: mustHash(t, "correct-horse")},
			},
		}
	}
	srv := &Server{cfg: cfg}
	if enabled {
		srv.tokens = NewTokenStore(time.Hour)
	}
	return srv
}

func doLogin(srv *Server, username, password string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(apiLoginRequest{Username: username, Password: password})
	req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.handleAPILogin(rec, req)
	return rec
}

func TestHandleAPILogin_Success(t *testing.T) {
	srv := newAuthTestServer(t, true)
	rec := doLogin(srv, "alice", "correct-horse")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp apiLoginResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Token == "" {
		t.Fatal("expected a non-empty token")
	}
	if !resp.ExpiresAt.After(time.Now()) {
		t.Fatal("expected expires_at in the future")
	}

	// The issued token must actually work against Validate.
	if _, ok := srv.tokens.Validate(resp.Token); !ok {
		t.Fatal("token issued by login did not validate")
	}
}

func TestHandleAPILogin_WrongPassword(t *testing.T) {
	srv := newAuthTestServer(t, true)
	rec := doLogin(srv, "alice", "wrong-password")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestHandleAPILogin_UnknownUser(t *testing.T) {
	srv := newAuthTestServer(t, true)
	rec := doLogin(srv, "nobody", "whatever")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestHandleAPILogin_AuthDisabled(t *testing.T) {
	srv := newAuthTestServer(t, false)
	rec := doLogin(srv, "alice", "correct-horse")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when auth is disabled, got %d", rec.Code)
	}
}

func TestHandleAPILogin_GETRejected(t *testing.T) {
	srv := newAuthTestServer(t, true)
	req := httptest.NewRequest(http.MethodGet, "/api/login", nil)
	rec := httptest.NewRecorder()
	srv.handleAPILogin(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

// ─── requireAuth middleware ─────────────────────────────────────────────────

func okHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func TestRequireAuth_NoHeader(t *testing.T) {
	srv := newAuthTestServer(t, true)
	req := httptest.NewRequest(http.MethodGet, "/api/data/Users", nil)
	rec := httptest.NewRecorder()
	srv.requireAuth(okHandler)(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without Authorization header, got %d", rec.Code)
	}
}

func TestRequireAuth_ValidToken(t *testing.T) {
	srv := newAuthTestServer(t, true)
	token, _, err := srv.tokens.Issue("alice")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/data/Users", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.requireAuth(okHandler)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with a valid token, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRequireAuth_ExpiredToken(t *testing.T) {
	srv := newAuthTestServer(t, true)
	srv.tokens = NewTokenStore(-time.Second)
	token, _, err := srv.tokens.Issue("alice")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/data/Users", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.requireAuth(okHandler)(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with an expired token, got %d", rec.Code)
	}
}

func TestRequireAuth_MalformedHeader(t *testing.T) {
	srv := newAuthTestServer(t, true)
	req := httptest.NewRequest(http.MethodGet, "/api/data/Users", nil)
	req.Header.Set("Authorization", "not-a-bearer-token")
	rec := httptest.NewRecorder()
	srv.requireAuth(okHandler)(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with a malformed header, got %d", rec.Code)
	}
}

// TestRequireAuth_Disabled is the regression check: with auth.enabled false
// (or cfg.Auth nil), every /api/* handler must behave exactly as it did
// before auth existed — no token required at all.
func TestRequireAuth_Disabled(t *testing.T) {
	srv := newAuthTestServer(t, false)
	req := httptest.NewRequest(http.MethodGet, "/api/data/Users", nil)
	rec := httptest.NewRecorder()
	srv.requireAuth(okHandler)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with auth disabled and no token, got %d", rec.Code)
	}
}
