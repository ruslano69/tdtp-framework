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
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"

	goldap "github.com/go-ldap/ldap/v3"
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

// setupAuth wires the configured authentication backend (token or ldap) and
// returns the middleware to apply to the authenticated route group, plus
// (token mode only) the *Authenticator the /tokens routes need — nil in ldap
// mode, where token management isn't available. A non-nil error (only
// possible in token mode, if bootstrapping the admin token fails) is fatal.
func setupAuth(db *OrchestratorDB, authType, ldapURL, ldapBindDN, ldapBindPass, ldapBaseDN string, noAuth bool) (authMiddleware func(http.Handler) http.Handler, auth *Authenticator, err error) {
	if authType == "ldap" {
		ldapAuth := NewLDAPAuthenticator(LDAPConfig{
			URL:         ldapURL,
			BindDN:      ldapBindDN,
			BindPass:    ldapBindPass,
			BaseDN:      ldapBaseDN,
			GroupAttr:   "memberOf",
			DefaultRole: RoleConsumer,
		})
		log.Info().Str("url", ldapURL).Msg("LDAP authentication enabled")
		return ldapAuth.Middleware, nil, nil
	}

	// "token" (default)
	auth = NewAuthenticator(db, !noAuth)
	if noAuth {
		log.Warn().Msg("AUTH DISABLED (--no-auth) — every request is treated as admin")
		return auth.Middleware, auth, nil
	}
	raw, err := auth.BootstrapAdminToken()
	if err != nil {
		return nil, nil, fmt.Errorf("bootstrap admin token: %w", err)
	}
	if raw != "" {
		log.Warn().Msg("──────────────────────────────────────────────────────────────")
		log.Warn().Str("admin_token", raw).Msg("BOOTSTRAP ADMIN TOKEN — store it now, shown once")
		log.Warn().Msg("──────────────────────────────────────────────────────────────")
	}
	return auth.Middleware, auth, nil
}

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

// ── LDAP authentication ────────────────────────────────────────────────────

// LDAPConfig holds LDAP/AD connection parameters.
type LDAPConfig struct {
	URL      string `yaml:"url"`       // e.g. "ldap://corp.example.com:389"
	BindDN   string `yaml:"bind_dn"`   // service account DN
	BindPass string `yaml:"bind_pass"` // service account password
	BaseDN   string `yaml:"base_dn"`   // search base
	// GroupAttr is the LDAP attribute containing group memberships.
	// Default: "memberOf"
	GroupAttr string `yaml:"group_attr"`
	// RoleMap maps LDAP group DNs to Roles.
	// e.g. "CN=tdtp-admins,...": "admin"
	RoleMap map[string]Role `yaml:"role_map"`
	// DefaultRole is assigned when no group matches. Empty = deny.
	DefaultRole Role `yaml:"default_role"`
}

// LDAPAuthenticator authenticates via LDAP bind + group lookup.
// It binds as the user (password auth), then reads group membership
// to determine role.
type LDAPAuthenticator struct {
	cfg LDAPConfig
}

// NewLDAPAuthenticator creates an LDAPAuthenticator with the given config.
func NewLDAPAuthenticator(cfg LDAPConfig) *LDAPAuthenticator {
	if cfg.GroupAttr == "" {
		cfg.GroupAttr = "memberOf"
	}
	return &LDAPAuthenticator{cfg: cfg}
}

// roleForGroups returns the highest-ranked role matching any of the given
// group DNs in cfg.RoleMap. Returns (role, true) on match or
// (cfg.DefaultRole, cfg.DefaultRole != "") when no group matches.
func (a *LDAPAuthenticator) roleForGroups(groups []string) (Role, bool) {
	best := Role("")
	bestRank := 0
	for _, g := range groups {
		if r, ok := a.cfg.RoleMap[g]; ok {
			if rank := roleRank[r]; rank > bestRank {
				best = r
				bestRank = rank
			}
		}
	}
	if bestRank > 0 {
		return best, true
	}
	if a.cfg.DefaultRole != "" {
		return a.cfg.DefaultRole, true
	}
	return "", false
}

// basicAuth parses "Authorization: Basic <base64(user:pass)>" and returns
// username, password. Returns empty strings on failure.
func basicAuth(r *http.Request) (user, pass string) {
	h := r.Header.Get("Authorization")
	const prefix = "Basic "
	if !strings.HasPrefix(h, prefix) {
		return "", ""
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(h[len(prefix):]))
	if err != nil {
		return "", ""
	}
	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
}

// Middleware satisfies the same contract as Authenticator.Middleware.
// It performs HTTP Basic Auth, binds the user to LDAP, reads group
// membership, and attaches a Principal to the request context.
func (a *LDAPAuthenticator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password := basicAuth(r)
		if username == "" || password == "" {
			authError(w, "missing or malformed Authorization: Basic header")
			return
		}

		// Dial LDAP server.
		conn, err := goldap.DialURL(a.cfg.URL)
		if err != nil {
			log.Warn().Err(err).Str("url", a.cfg.URL).Msg("ldap dial failed")
			authError(w, "authentication service unavailable")
			return
		}
		defer func() { _ = conn.Close() }()

		// First bind as service account to search for user DN.
		if a.cfg.BindDN != "" {
			if err := conn.Bind(a.cfg.BindDN, a.cfg.BindPass); err != nil {
				log.Warn().Err(err).Str("bind_dn", a.cfg.BindDN).Msg("ldap service bind failed")
				authError(w, "authentication service unavailable")
				return
			}
		}

		// Search for the user entry and retrieve group attribute.
		groupAttr := a.cfg.GroupAttr
		if groupAttr == "" {
			groupAttr = "memberOf"
		}
		filter := fmt.Sprintf("(sAMAccountName=%s)", goldap.EscapeFilter(username))
		searchReq := goldap.NewSearchRequest(
			a.cfg.BaseDN,
			goldap.ScopeWholeSubtree,
			goldap.NeverDerefAliases,
			1, 0, false,
			filter,
			[]string{"dn", groupAttr},
			nil,
		)
		result, err := conn.Search(searchReq)
		if err != nil {
			log.Warn().Err(err).Str("user", username).Msg("ldap search failed")
			authError(w, "ldap search failed")
			return
		}
		if len(result.Entries) == 0 {
			authError(w, "invalid credentials")
			return
		}
		userDN := result.Entries[0].DN

		// Bind as the user to verify their password.
		if err := conn.Bind(userDN, password); err != nil {
			authError(w, "invalid credentials")
			return
		}

		// Collect group memberships.
		groups := result.Entries[0].GetAttributeValues(groupAttr)

		// Determine role from group membership.
		role, ok := a.roleForGroups(groups)
		if !ok {
			authError(w, "access denied: no matching role")
			return
		}

		p := &Principal{Name: username, Role: role}
		ctx := context.WithValue(r.Context(), principalCtxKey{}, p)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
