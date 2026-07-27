package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ─── TokenStore: рост ограничен ─────────────────────────────────────────────

// Протухшие записи раньше удалялись только при повторной валидации того же
// токена — то есть никогда, если клиент за ним не возвращался.
func TestTokenStoreSweepsExpiredOnIssue(t *testing.T) {
	s := NewTokenStore(time.Hour)

	// Набиваем до потолка записями, которые уже протухли.
	past := time.Now().Add(-time.Hour)
	s.mu.Lock()
	for i := 0; i < maxLiveTokens; i++ {
		s.tokens[fmt.Sprintf("stale-%d", i)] = tokenInfo{username: "ghost", expiresAt: past}
	}
	s.mu.Unlock()

	if got := s.Len(); got != maxLiveTokens {
		t.Fatalf("setup: %d entries, want %d", got, maxLiveTokens)
	}

	token, _, err := s.Issue("alice")
	if err != nil {
		t.Fatalf("issue after sweep should succeed: %v", err)
	}
	if got := s.Len(); got != 1 {
		t.Errorf("after sweep the store holds %d entries, want 1 (the fresh token)", got)
	}
	if user, ok := s.Validate(token); !ok || user != "alice" {
		t.Error("the freshly issued token must validate")
	}
}

// Если протухших нет, а потолок достигнут — выдача обязана отказать, а не
// расти дальше.
func TestTokenStoreRefusesWhenFullOfLiveTokens(t *testing.T) {
	s := NewTokenStore(time.Hour)

	future := time.Now().Add(time.Hour)
	s.mu.Lock()
	for i := 0; i < maxLiveTokens; i++ {
		s.tokens[fmt.Sprintf("live-%d", i)] = tokenInfo{username: "bob", expiresAt: future}
	}
	s.mu.Unlock()

	if _, _, err := s.Issue("alice"); err == nil {
		t.Fatal("expected Issue to refuse when every entry is live")
	}
	if got := s.Len(); got != maxLiveTokens {
		t.Errorf("store grew past the cap: %d entries", got)
	}
}

func TestTokenStoreRevoke(t *testing.T) {
	s := NewTokenStore(time.Hour)
	token, _, err := s.Issue("alice")
	if err != nil {
		t.Fatal(err)
	}

	if !s.Revoke(token) {
		t.Error("Revoke should report the token was present")
	}
	if _, ok := s.Validate(token); ok {
		t.Error("a revoked token must not validate")
	}
	if s.Revoke(token) {
		t.Error("revoking twice should report absent the second time")
	}
}

// ─── rate limit на /api/login ───────────────────────────────────────────────

func TestLoginLimiterAllowsThenBlocks(t *testing.T) {
	var l loginLimiter // нулевое значение обязано работать
	now := time.Now()

	for i := 0; i < loginAttemptsPerWindow; i++ {
		if ok, _ := l.allow("10.0.0.1", now); !ok {
			t.Fatalf("attempt %d blocked, the limit is %d", i+1, loginAttemptsPerWindow)
		}
	}

	ok, retryAfter := l.allow("10.0.0.1", now)
	if ok {
		t.Fatal("expected the attempt past the limit to be blocked")
	}
	if retryAfter <= 0 || retryAfter > loginWindow {
		t.Errorf("retryAfter = %v, want it within (0, %v]", retryAfter, loginWindow)
	}
}

func TestLoginLimiterIsPerClient(t *testing.T) {
	var l loginLimiter
	now := time.Now()

	for i := 0; i < loginAttemptsPerWindow+1; i++ {
		l.allow("10.0.0.1", now)
	}

	if ok, _ := l.allow("10.0.0.2", now); !ok {
		t.Error("a different client must not inherit someone else's counter")
	}
}

func TestLoginLimiterWindowResets(t *testing.T) {
	var l loginLimiter
	now := time.Now()

	for i := 0; i < loginAttemptsPerWindow+1; i++ {
		l.allow("10.0.0.1", now)
	}
	if ok, _ := l.allow("10.0.0.1", now); ok {
		t.Fatal("setup: client should be blocked")
	}

	if ok, _ := l.allow("10.0.0.1", now.Add(loginWindow+time.Second)); !ok {
		t.Error("the window should have elapsed and the client be allowed again")
	}
}

func TestClientKeyStripsPort(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/login", nil)
	req.RemoteAddr = "192.0.2.10:54321"

	if got := clientKey(req); got != "192.0.2.10" {
		t.Errorf("clientKey = %q, want %q", got, "192.0.2.10")
	}
}

// Сквозь handler: лимит должен срабатывать ДО bcrypt, поэтому проверяем его
// на заведомо неверном пароле — самом дешёвом способе жечь чужой CPU.
func TestHandleAPILoginRateLimited(t *testing.T) {
	srv := newAuthTestServer(t, true)

	var last *httptest.ResponseRecorder
	for i := 0; i < loginAttemptsPerWindow+1; i++ {
		last = doLoginFrom(srv, "alice", "wrong", "203.0.113.7:1000")
	}

	if last.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 past the limit, got %d: %s", last.Code, last.Body.String())
	}
	if last.Header().Get("Retry-After") == "" {
		t.Error("a 429 should carry Retry-After")
	}

	// Правильный пароль с того же адреса тоже отбивается: лимит про нагрузку,
	// а не про правильность.
	if rec := doLoginFrom(srv, "alice", "correct-horse", "203.0.113.7:1000"); rec.Code != http.StatusTooManyRequests {
		t.Errorf("expected the limit to apply regardless of credentials, got %d", rec.Code)
	}

	// Другой адрес не затронут.
	if rec := doLoginFrom(srv, "alice", "correct-horse", "203.0.113.8:1000"); rec.Code != http.StatusOK {
		t.Errorf("a different client should still log in, got %d: %s", rec.Code, rec.Body.String())
	}
}

func doLoginFrom(srv *Server, username, password, remoteAddr string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(apiLoginRequest{Username: username, Password: password})
	req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(body))
	req.RemoteAddr = remoteAddr
	rec := httptest.NewRecorder()
	srv.handleAPILogin(rec, req)
	return rec
}

// ─── размер тела ────────────────────────────────────────────────────────────

func TestHandleAPILoginRejectsOversizedBody(t *testing.T) {
	srv := newAuthTestServer(t, true)

	huge := `{"username":"alice","password":"` + strings.Repeat("x", maxLoginBodyBytes*2) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(huge))
	req.RemoteAddr = "198.51.100.5:2000"
	rec := httptest.NewRecorder()
	srv.handleAPILogin(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for a body past %d bytes, got %d", maxLoginBodyBytes, rec.Code)
	}
}

// ─── /api/logout ────────────────────────────────────────────────────────────

func TestHandleAPILogoutRevokesToken(t *testing.T) {
	srv := newAuthTestServer(t, true)

	rec := doLoginFrom(srv, "alice", "correct-horse", "198.51.100.9:3000")
	if rec.Code != http.StatusOK {
		t.Fatalf("login failed: %d %s", rec.Code, rec.Body.String())
	}
	var resp apiLoginResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}

	// Токен работает.
	if _, ok := srv.tokens.Validate(resp.Token); !ok {
		t.Fatal("setup: the fresh token should validate")
	}

	req := httptest.NewRequest(http.MethodPost, "/api/logout", nil)
	req.Header.Set("Authorization", "Bearer "+resp.Token)
	out := httptest.NewRecorder()
	srv.requireAuth(srv.handleAPILogout)(out, req)

	if out.Code != http.StatusOK {
		t.Fatalf("logout returned %d: %s", out.Code, out.Body.String())
	}
	if _, ok := srv.tokens.Validate(resp.Token); ok {
		t.Error("the token must stop working after logout")
	}

	// Повторный вызов тем же токеном уже не проходит requireAuth.
	again := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/logout", nil)
	req2.Header.Set("Authorization", "Bearer "+resp.Token)
	srv.requireAuth(srv.handleAPILogout)(again, req2)
	if again.Code != http.StatusUnauthorized {
		t.Errorf("a revoked token should give 401, got %d", again.Code)
	}
}

func TestHandleAPILogoutRequiresPOST(t *testing.T) {
	srv := newAuthTestServer(t, true)
	token, _, err := srv.tokens.Issue("alice")
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/logout", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.requireAuth(srv.handleAPILogout)(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

// Регрессия: с выключенной авторизацией /api/* остаётся ровно как был.
func TestLogoutIsInvisibleWhenAuthDisabled(t *testing.T) {
	srv := newAuthTestServer(t, false)

	req := httptest.NewRequest(http.MethodPost, "/api/logout", nil)
	rec := httptest.NewRecorder()
	srv.requireAuth(srv.handleAPILogout)(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 when auth is off, got %d", rec.Code)
	}
}
