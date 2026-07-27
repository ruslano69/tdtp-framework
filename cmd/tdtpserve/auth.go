package main

// auth.go — password login -> short-lived opaque token, guarding /api/*.
// See AUTH_PLAN.md for the full design rationale. Summary:
//
//   - Passwords hash with bcrypt (golang.org/x/crypto/bcrypt, already an
//     indirect dependency), never SHA-256 — SHA-256 is fine for a token
//     (already high-entropy random), not for a human-chosen password.
//   - Tokens are opaque random strings held in an in-memory map with a
//     TTL, not JWT: no new dependency, instant revoke, and losing tokens
//     on restart (forcing re-login) is an acceptable tradeoff for a
//     single-process internal tool.
//   - Disabled by default (cfg.Auth == nil or Enabled == false): every
//     /api/* handler behaves exactly as before auth existed.

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// tokenInfo is one issued login token's server-side state.
type tokenInfo struct {
	username  string
	expiresAt time.Time
}

// TokenStore issues and validates short-lived opaque login tokens.
type TokenStore struct {
	mu     sync.RWMutex
	tokens map[string]tokenInfo
	ttl    time.Duration
}

// NewTokenStore creates a token store issuing tokens valid for ttl.
func NewTokenStore(ttl time.Duration) *TokenStore {
	return &TokenStore{tokens: make(map[string]tokenInfo), ttl: ttl}
}

// Issue creates a new random token for username, valid for the store's ttl.
func (s *TokenStore) Issue(username string) (token string, expiresAt time.Time, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, fmt.Errorf("generate token: %w", err)
	}
	token = hex.EncodeToString(raw)
	expiresAt = time.Now().Add(s.ttl)

	s.mu.Lock()
	s.tokens[token] = tokenInfo{username: username, expiresAt: expiresAt}
	s.mu.Unlock()

	return token, expiresAt, nil
}

// Validate reports whether token exists and hasn't expired. An expired
// token is dropped from the store as a side effect of being checked —
// lazy cleanup, no separate sweeper goroutine in this first version.
func (s *TokenStore) Validate(token string) (username string, ok bool) {
	s.mu.RLock()
	info, found := s.tokens[token]
	s.mu.RUnlock()
	if !found {
		return "", false
	}
	if time.Now().After(info.expiresAt) {
		s.mu.Lock()
		delete(s.tokens, token)
		s.mu.Unlock()
		return "", false
	}
	return info.username, true
}

// dummyPasswordHash is compared against for unknown usernames, so a login
// attempt for a nonexistent account takes the same time as one for a real
// account with a wrong password — otherwise response timing leaks which
// usernames exist.
var dummyPasswordHash, _ = bcrypt.GenerateFromPassword([]byte("not-a-real-account"), bcrypt.DefaultCost)

// checkPassword verifies username/password against cfg's configured users.
func checkPassword(users []UserConfig, username, password string) bool {
	hash := dummyPasswordHash
	found := false
	for _, u := range users {
		if u.Username == username {
			hash = []byte(u.PasswordHash)
			found = true
			break
		}
	}
	err := bcrypt.CompareHashAndPassword(hash, []byte(password))
	return found && err == nil
}

// apiLoginRequest is the POST /api/login request body.
type apiLoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// apiLoginResponse is the POST /api/login response body.
type apiLoginResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// handleAPILogin serves POST /api/login. Deliberately never distinguishes
// "unknown username" from "wrong password" in its response — either gives
// a generic 401, so a caller can't use this endpoint to enumerate accounts.
func (s *Server) handleAPILogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	if s.cfg.Auth == nil || !s.cfg.Auth.Enabled {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}

	var req apiLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}

	if !checkPassword(s.cfg.Auth.Users, req.Username, req.Password) {
		writeAPIError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}

	token, expiresAt, err := s.tokens.Issue(req.Username)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to issue token: "+err.Error())
		return
	}

	writeAPIJSON(w, http.StatusOK, apiLoginResponse{Token: token, ExpiresAt: expiresAt})
}

// requireAuth wraps an /api/* handler with a Bearer-token check. A no-op
// pass-through when auth isn't configured/enabled — every handler behaves
// exactly as it did before auth existed.
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.Auth == nil || !s.cfg.Auth.Enabled {
			next(w, r)
			return
		}

		const prefix = "Bearer "
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, prefix) {
			writeAPIError(w, http.StatusUnauthorized, "missing or invalid Authorization header")
			return
		}
		token := strings.TrimPrefix(authHeader, prefix)

		if _, ok := s.tokens.Validate(token); !ok {
			writeAPIError(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}

		next(w, r)
	}
}
