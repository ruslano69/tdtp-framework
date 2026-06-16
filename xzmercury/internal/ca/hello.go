package ca

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	// HelloDelay is the mandatory wait before a hello token is issued.
	// Forces every caller to spend 2 seconds before getting a quota for
	// enrollment/authorization — limits DDoS to ~30 requests/min per IP.
	HelloDelay = 2 * time.Second

	// HelloTokenTTL is how long the token is valid after /hello completes.
	// Short enough to prevent hoarding, long enough for a round-trip.
	HelloTokenTTL = 30 * time.Second

	// MaxConcurrentHelloPerIP limits parallel /hello calls from one IP.
	// Prevents per-IP amplification: N goroutines × 2s = N tokens at once.
	MaxConcurrentHelloPerIP = 3
)

// helloToken is a single-use, short-lived quota issued after /hello.
type helloToken struct {
	token     string
	ip        string
	expiresAt time.Time
}

// HelloHandler manages /hello and hello-token validation.
// All enroll/authorize step-1 handlers require a valid token.
type HelloHandler struct {
	mu        sync.Mutex
	tokens    map[string]*helloToken // token → record
	ipPending map[string]*int32      // IP → concurrent /hello count
}

// NewHelloHandler creates the handler and starts token cleanup.
func NewHelloHandler() *HelloHandler {
	h := &HelloHandler{
		tokens:    make(map[string]*helloToken),
		ipPending: make(map[string]*int32),
	}
	go h.sweepExpired()
	return h
}

// ServeHTTP handles GET /hello.
// Sleeps HelloDelay, then issues a single-use quota token.
func (h *HelloHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)

	// Per-IP concurrency limit.
	counter := h.getIPCounter(ip)
	current := atomic.AddInt32(counter, 1)
	defer atomic.AddInt32(counter, -1)

	if current > MaxConcurrentHelloPerIP {
		log.Warn().Str("ip", ip).Int32("concurrent", current).
			Msg("hello: per-IP limit exceeded")
		writeCAError(w, http.StatusTooManyRequests,
			"too many concurrent /hello requests from this IP")
		return
	}

	// Mandatory wait — proof of patience.
	// Uses request context so a dropped connection doesn't block the goroutine.
	select {
	case <-time.After(HelloDelay):
		// normal path
	case <-r.Context().Done():
		return // client disconnected
	}

	// Issue single-use token.
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		writeCAError(w, http.StatusInternalServerError, "token generation failed")
		return
	}
	token := hex.EncodeToString(raw)
	expiresAt := time.Now().UTC().Add(HelloTokenTTL)

	h.mu.Lock()
	h.tokens[token] = &helloToken{token: token, ip: ip, expiresAt: expiresAt}
	h.mu.Unlock()

	log.Info().Str("ip", ip).Str("token", token[:8]+"...").
		Time("expires_at", expiresAt).Msg("hello: token issued")

	writeCAJSON(w, http.StatusOK, map[string]any{
		"hello_token": token,
		"expires_at":  expiresAt,
		"note":        "present X-Hello-Token header in the next enroll/authorize request; token is single-use",
	})
}

// Consume validates and burns the hello token from the request header.
// Returns (ip, nil) on success; writes an error response and returns ("", err) on failure.
// Must be called at the start of every enroll/authorize step-1 handler.
func (h *HelloHandler) Consume(w http.ResponseWriter, r *http.Request) (ip string, ok bool) {
	token := r.Header.Get("X-Hello-Token")
	if token == "" {
		writeCAError(w, http.StatusUnauthorized,
			"X-Hello-Token header required; call GET /hello first (2s wait)")
		return "", false
	}

	h.mu.Lock()
	rec, found := h.tokens[token]
	if found {
		delete(h.tokens, token) // burn — single use
	}
	h.mu.Unlock()

	if !found {
		log.Warn().Str("token", token[:min(8, len(token))]+"...").
			Msg("hello: token not found or already consumed")
		writeCAError(w, http.StatusUnauthorized,
			"hello token not found or already consumed; call GET /hello again")
		return "", false
	}

	if time.Now().UTC().After(rec.expiresAt) {
		log.Warn().Str("token", token[:8]+"...").Msg("hello: token expired")
		writeCAError(w, http.StatusUnauthorized,
			"hello token expired; call GET /hello again")
		return "", false
	}

	return rec.ip, true
}

// getIPCounter returns the atomic counter for an IP, creating it if absent.
func (h *HelloHandler) getIPCounter(ip string) *int32 {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.ipPending[ip]; !ok {
		var zero int32
		h.ipPending[ip] = &zero
	}
	return h.ipPending[ip]
}

// sweepExpired removes expired tokens and idle IP counters periodically.
func (h *HelloHandler) sweepExpired() {
	ticker := time.NewTicker(HelloTokenTTL)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		h.mu.Lock()
		for tok, rec := range h.tokens {
			if now.After(rec.expiresAt) {
				delete(h.tokens, tok)
			}
		}
		// Clean up zero-count IP entries to prevent map growth.
		for ip, cnt := range h.ipPending {
			if atomic.LoadInt32(cnt) == 0 {
				delete(h.ipPending, ip)
			}
		}
		h.mu.Unlock()
	}
}

// clientIP extracts the real client IP, respecting X-Forwarded-For.
func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		// Take the first (original) IP from the chain.
		if idx := strings.IndexByte(fwd, ','); idx >= 0 {
			return strings.TrimSpace(fwd[:idx])
		}
		return strings.TrimSpace(fwd)
	}
	ip := r.RemoteAddr
	if idx := strings.LastIndexByte(ip, ':'); idx >= 0 {
		ip = ip[:idx]
	}
	return strings.Trim(ip, "[]")
}

// Middleware returns an http.Handler that enforces the hello-token gate.
// Use this to wrap enroll/authorize step-1 endpoints.
func (h *HelloHandler) Middleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := h.Consume(w, r); !ok {
			return
		}
		next(w, r)
	}
}

// HelloTokenStats returns current token and IP counter counts (for /metrics).
func (h *HelloHandler) HelloTokenStats() (tokens, ipEntries int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.tokens), len(h.ipPending)
}
