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
	"strconv"
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

// maxLiveTokens caps how many unexpired tokens the store will hold.
//
// Every successful login adds an entry that only leaves on expiry, so without
// a ceiling the map is a slow memory leak on a long-running server and a fast
// one for anyone with valid credentials and a loop. The number is far above
// any real usage — this is a backstop, not a quota.
const maxLiveTokens = 10000

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
//
// Expired entries are swept here rather than by a background goroutine: the
// store has no lifecycle of its own to hang one off, and issuing is the only
// moment the map can grow, so it is also the only moment a sweep is needed.
func (s *TokenStore) Issue(username string) (token string, expiresAt time.Time, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, fmt.Errorf("generate token: %w", err)
	}
	token = hex.EncodeToString(raw)
	expiresAt = time.Now().Add(s.ttl)

	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.tokens) >= maxLiveTokens {
		s.sweepLocked(time.Now())

		// Still full after the sweep: every entry is live, so this is real
		// concurrent usage rather than accumulated garbage. Refusing beats
		// growing without bound -- a login that fails loudly can be retried,
		// a server killed by the OOM killer takes every session with it.
		if len(s.tokens) >= maxLiveTokens {
			return "", time.Time{}, fmt.Errorf(
				"token store is full (%d live tokens), try again later", maxLiveTokens)
		}
	}

	s.tokens[token] = tokenInfo{username: username, expiresAt: expiresAt}

	return token, expiresAt, nil
}

// sweepLocked drops every expired entry. Caller must hold the write lock.
func (s *TokenStore) sweepLocked(now time.Time) {
	for tok, info := range s.tokens {
		if now.After(info.expiresAt) {
			delete(s.tokens, tok)
		}
	}
}

// Revoke drops a token immediately, whether or not it had expired. Reports
// whether the token was there to begin with.
func (s *TokenStore) Revoke(token string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, found := s.tokens[token]
	delete(s.tokens, token)

	return found
}

// Len reports how many entries the store currently holds, expired ones
// included. For tests and diagnostics.
func (s *TokenStore) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.tokens)
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

// maxLoginBodyBytes bounds the request body. Credentials are two short
// strings; anything larger is a mistake or an attempt to make an
// unauthenticated endpoint allocate.
const maxLoginBodyBytes = 4 << 10

// handleAPILogin serves POST /api/login. Deliberately never distinguishes
// "unknown username" from "wrong password" in its response — either gives
// a generic 401, so a caller can't use this endpoint to enumerate accounts.
func (s *Server) handleAPILogin(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Auth == nil || !s.cfg.Auth.Enabled {
		// Checked before the method, so a probe cannot tell "auth is off"
		// from "wrong verb" — both look like a route that isn't there.
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}

	// Throttle before bcrypt runs — the point is to bound the work, so the
	// check has to come ahead of the expensive part.
	if ok, retryAfter := s.loginLimiter.allow(clientKey(r), time.Now()); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())+1))
		writeAPIError(w, http.StatusTooManyRequests, "too many login attempts, try again later")
		return
	}

	var req apiLoginRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxLoginBodyBytes)).Decode(&req); err != nil {
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

// bearerToken pulls the token out of an Authorization header, or "" if the
// header is missing or not a Bearer one.
func bearerToken(r *http.Request) string {
	const prefix = "Bearer "

	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, prefix) {
		return ""
	}

	return strings.TrimPrefix(h, prefix)
}

// handleAPILogout serves POST /api/logout: revokes the token it was called
// with. Sits behind requireAuth, so reaching the body means the token was
// valid. Without this, a token handed to the wrong place stays usable for the
// rest of its TTL and the only remedy is restarting the server.
func (s *Server) handleAPILogout(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Auth == nil || !s.cfg.Auth.Enabled {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}

	s.tokens.Revoke(bearerToken(r))

	writeAPIJSON(w, http.StatusOK, map[string]string{"status": "logged out"})
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

		token := bearerToken(r)
		if token == "" {
			writeAPIError(w, http.StatusUnauthorized, "missing or invalid Authorization header")
			return
		}

		if _, ok := s.tokens.Validate(token); !ok {
			writeAPIError(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}

		next(w, r)
	}
}
