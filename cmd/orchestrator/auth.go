package main

// auth.go — token-based authentication and role gating for the orchestrator API.
//
// UserApps authenticate with a Bearer token. Each token maps to a principal with
// a role and an optional scenario allowlist. Tokens are stored hashed (SHA-256);
// the raw token is shown only once at creation.
//
// Roles (increasing privilege):
//   consumer  — read-only: list scenarios, read jobs and results
//   activator — consumer + run scenarios (within the token's scenario allowlist)
//   admin     — everything, incl. schedules and token management
//
// On first run with an empty token table, a bootstrap admin token is generated
// and printed once. Auth can be disabled with --no-auth for local development.

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// Role is an authorization level.
type Role string

const (
	RoleConsumer  Role = "consumer"
	RoleActivator Role = "activator"
	RoleAdmin     Role = "admin"
)

// roleRank orders roles for "at least" checks.
var roleRank = map[Role]int{RoleConsumer: 1, RoleActivator: 2, RoleAdmin: 3}

// Principal is the authenticated caller attached to a request context.
type Principal struct {
	TokenID   string
	Name      string
	Role      Role
	Scenarios []string // empty = all scenarios allowed
}

// AllowsScenario reports whether the principal may act on the named scenario.
func (p *Principal) AllowsScenario(name string) bool {
	if len(p.Scenarios) == 0 {
		return true // no allowlist = all
	}
	for _, s := range p.Scenarios {
		if s == name {
			return true
		}
	}
	return false
}

type principalCtxKey struct{}

// Authenticator validates Bearer tokens against the DB.
type Authenticator struct {
	db      *OrchestratorDB
	enabled bool
}

// NewAuthenticator creates an authenticator. When enabled is false, every
// request is treated as an admin (local dev only).
func NewAuthenticator(db *OrchestratorDB, enabled bool) *Authenticator {
	return &Authenticator{db: db, enabled: enabled}
}

// hashToken returns SHA-256(raw) as hex — the DB lookup key.
func hashToken(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}

// GenerateToken returns a new random Bearer token (raw, shown once).
func GenerateToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "tdtp_" + hex.EncodeToString(b), nil
}

// Middleware authenticates the request and attaches the principal to its context.
func (a *Authenticator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.enabled {
			// Dev mode: synthetic admin principal.
			ctx := context.WithValue(r.Context(), principalCtxKey{},
				&Principal{Name: "dev", Role: RoleAdmin})
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		raw := bearerToken(r)
		if raw == "" {
			authError(w, "missing or malformed Authorization: Bearer header")
			return
		}
		rec, err := a.db.GetTokenByHash(hashToken(raw))
		if err != nil {
			http.Error(w, "auth lookup failed", http.StatusInternalServerError)
			return
		}
		if rec == nil {
			authError(w, "invalid token")
			return
		}
		_ = a.db.TouchToken(rec.ID)

		p := &Principal{
			TokenID:   rec.ID,
			Name:      rec.Name,
			Role:      Role(rec.Role),
			Scenarios: rec.Scenarios,
		}
		ctx := context.WithValue(r.Context(), principalCtxKey{}, p)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// PrincipalFrom returns the authenticated principal from a request context.
func PrincipalFrom(ctx context.Context) *Principal {
	if p, ok := ctx.Value(principalCtxKey{}).(*Principal); ok {
		return p
	}
	return nil
}

// RequireRole wraps a handler, enforcing a minimum role.
func RequireRole(min Role, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := PrincipalFrom(r.Context())
		if p == nil {
			authError(w, "not authenticated")
			return
		}
		if roleRank[p.Role] < roleRank[min] {
			http.Error(w, "forbidden: requires role "+string(min), http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

// BootstrapAdminToken generates an admin token if the token table is empty.
// Returns the raw token (printed once) or "" if tokens already exist.
func (a *Authenticator) BootstrapAdminToken() (string, error) {
	n, err := a.db.CountTokens()
	if err != nil {
		return "", err
	}
	if n > 0 {
		return "", nil
	}
	raw, err := GenerateToken()
	if err != nil {
		return "", err
	}
	id := uuid.New().String()
	if err := a.db.InsertToken(id, hashToken(raw), "bootstrap-admin", string(RoleAdmin), nil); err != nil {
		return "", err
	}
	return raw, nil
}

// CreateToken issues a new token with the given role/scenarios. Returns raw token.
func (a *Authenticator) CreateToken(name string, role Role, scenarios []string) (string, error) {
	raw, err := GenerateToken()
	if err != nil {
		return "", err
	}
	id := uuid.New().String()
	if err := a.db.InsertToken(id, hashToken(raw), name, string(role), scenarios); err != nil {
		return "", err
	}
	log.Info().Str("name", name).Str("role", string(role)).Msg("token created")
	return raw, nil
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if strings.HasPrefix(h, prefix) {
		return strings.TrimSpace(h[len(prefix):])
	}
	return ""
}

func authError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"` + msg + `"}`))
}
