package main

// ratelimit.go — attempt throttling for /api/login.
//
// Why this exists at all: /api/login is the one route that must stay reachable
// without a token, and every call to it runs bcrypt. That is deliberate — the
// cost is what makes an offline hash useless — but it also means an
// unauthenticated caller can spend ~60ms of server CPU per request. A loop
// against it is both a password-guessing attempt and a denial of service, and
// the same limit answers both.

import (
	"net"
	"net/http"
	"sync"
	"time"
)

const (
	// loginAttemptsPerWindow is how many login attempts one client may make
	// per loginWindow. Generous for a human, useless for a guessing loop:
	// bcrypt at cost 10 already caps a single client near 16 attempts/second,
	// and this brings it to 10 per minute.
	loginAttemptsPerWindow = 10
	loginWindow            = time.Minute

	// maxLimiterEntries caps the limiter's own map, so the defence cannot
	// become the leak. Reached only under a spread of distinct source
	// addresses, at which point the oldest windows are dropped.
	maxLimiterEntries = 4096
)

// loginLimiter counts login attempts per client within a fixed window.
//
// Usable as a zero value — the map is created on first use under the lock.
// That matters because Server is also built directly as a struct literal in
// places (tests, and anything embedding it later), and a limiter that has to
// be constructed turns a missed field into a nil dereference on the login
// path specifically. Security code should not have a way to be silently
// half-installed.
type loginLimiter struct {
	mu       sync.Mutex
	attempts map[string]*attemptWindow
}

type attemptWindow struct {
	count int
	reset time.Time
}

// allow records an attempt from key and reports whether it may proceed.
// retryAfter is how long the caller must wait, and is only meaningful when
// allow returns false.
func (l *loginLimiter) allow(key string, now time.Time) (ok bool, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.attempts == nil {
		l.attempts = make(map[string]*attemptWindow)
	}

	if len(l.attempts) >= maxLimiterEntries {
		l.sweepLocked(now)
	}

	w, found := l.attempts[key]
	if !found || now.After(w.reset) {
		l.attempts[key] = &attemptWindow{count: 1, reset: now.Add(loginWindow)}
		return true, 0
	}

	w.count++
	if w.count > loginAttemptsPerWindow {
		return false, w.reset.Sub(now)
	}

	return true, 0
}

// sweepLocked drops windows that have already elapsed. Caller holds the lock.
func (l *loginLimiter) sweepLocked(now time.Time) {
	for key, w := range l.attempts {
		if now.After(w.reset) {
			delete(l.attempts, key)
		}
	}
}

// clientKey identifies the caller for rate-limiting purposes.
//
// Deliberately RemoteAddr and not X-Forwarded-For: that header is caller-supplied,
// so trusting it would let anyone reset their own counter by varying it. Behind a
// reverse proxy every request therefore shares the proxy's address and the limit
// becomes a global one — correct as a DoS bound, blunter as a per-client one. If
// tdtpserve ever runs behind a proxy for real, the fix is a configured list of
// trusted proxies, not blanket trust in the header.
func clientKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}

	return host
}
