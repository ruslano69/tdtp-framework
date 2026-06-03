package main

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newAuthTestDB(t *testing.T) *OrchestratorDB {
	t.Helper()
	db, err := OpenOrchestratorDB(t.TempDir() + "/orch.db")
	if err != nil {
		t.Fatalf("OpenOrchestratorDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestHashTokenStable(t *testing.T) {
	a := hashToken("tdtp_secret")
	b := hashToken("tdtp_secret")
	if a != b {
		t.Error("hashToken not deterministic")
	}
	if a == hashToken("tdtp_other") {
		t.Error("different tokens hash equal")
	}
}

func TestBootstrapAdminToken(t *testing.T) {
	db := newAuthTestDB(t)
	auth := NewAuthenticator(db, true)

	raw, err := auth.BootstrapAdminToken()
	if err != nil {
		t.Fatalf("BootstrapAdminToken: %v", err)
	}
	if raw == "" {
		t.Fatal("bootstrap should create a token on empty DB")
	}
	// Second call must NOT create another (tokens already exist).
	raw2, err := auth.BootstrapAdminToken()
	if err != nil {
		t.Fatalf("second BootstrapAdminToken: %v", err)
	}
	if raw2 != "" {
		t.Error("bootstrap created a second token when one already existed")
	}

	// The token resolves to an admin principal.
	rec, err := db.GetTokenByHash(hashToken(raw))
	if err != nil || rec == nil {
		t.Fatalf("bootstrap token not found: %v", err)
	}
	if rec.Role != string(RoleAdmin) {
		t.Errorf("bootstrap role = %q, want admin", rec.Role)
	}
}

func TestPrincipalAllowsScenario(t *testing.T) {
	all := &Principal{Scenarios: nil}
	if !all.AllowsScenario("anything") {
		t.Error("empty allowlist should allow all")
	}
	scoped := &Principal{Scenarios: []string{"payroll", "headcount"}}
	if !scoped.AllowsScenario("payroll") {
		t.Error("scoped should allow listed scenario")
	}
	if scoped.AllowsScenario("secret") {
		t.Error("scoped should reject unlisted scenario")
	}
}

func TestMiddleware_RejectsMissingToken(t *testing.T) {
	db := newAuthTestDB(t)
	auth := NewAuthenticator(db, true)

	h := auth.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, httptest.NewRequest(http.MethodGet, "/jobs", nil))
	if rw.Code != http.StatusUnauthorized {
		t.Errorf("missing token = %d, want 401", rw.Code)
	}
}

func TestMiddleware_RejectsInvalidToken(t *testing.T) {
	db := newAuthTestDB(t)
	auth := NewAuthenticator(db, true)

	h := auth.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/jobs", nil)
	req.Header.Set("Authorization", "Bearer tdtp_nonexistent")
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)
	if rw.Code != http.StatusUnauthorized {
		t.Errorf("invalid token = %d, want 401", rw.Code)
	}
}

func TestMiddleware_ValidTokenAttachesPrincipal(t *testing.T) {
	db := newAuthTestDB(t)
	auth := NewAuthenticator(db, true)
	raw, _ := auth.CreateToken("alice", RoleActivator, []string{"payroll"})

	var gotRole Role
	var gotName string
	h := auth.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := PrincipalFrom(r.Context())
		if p != nil {
			gotRole = p.Role
			gotName = p.Name
		}
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/jobs", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("valid token = %d, want 200", rw.Code)
	}
	if gotRole != RoleActivator || gotName != "alice" {
		t.Errorf("principal = %s/%s, want alice/activator", gotName, gotRole)
	}
}

func TestRequireRole(t *testing.T) {
	db := newAuthTestDB(t)
	auth := NewAuthenticator(db, true)
	consumerTok, _ := auth.CreateToken("bob", RoleConsumer, nil)

	// Wrap an admin-only handler.
	adminHandler := RequireRole(RoleAdmin, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	full := auth.Middleware(http.HandlerFunc(adminHandler))

	req := httptest.NewRequest(http.MethodGet, "/schedules", nil)
	req.Header.Set("Authorization", "Bearer "+consumerTok)
	rw := httptest.NewRecorder()
	full.ServeHTTP(rw, req)

	if rw.Code != http.StatusForbidden {
		t.Errorf("consumer hitting admin route = %d, want 403", rw.Code)
	}
}

func TestNoAuthMode_TreatsAsAdmin(t *testing.T) {
	db := newAuthTestDB(t)
	auth := NewAuthenticator(db, false) // disabled

	adminHandler := RequireRole(RoleAdmin, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	full := auth.Middleware(http.HandlerFunc(adminHandler))

	rw := httptest.NewRecorder()
	full.ServeHTTP(rw, httptest.NewRequest(http.MethodGet, "/schedules", nil))
	if rw.Code != http.StatusOK {
		t.Errorf("no-auth mode admin route = %d, want 200", rw.Code)
	}
}

// ── LDAP authenticator tests ───────────────────────────────────────────────

// TestLDAPRoleMap exercises roleForGroups without touching a real server.
func TestLDAPRoleMap(t *testing.T) {
	cfg := LDAPConfig{
		GroupAttr: "memberOf",
		RoleMap: map[string]Role{
			"CN=tdtp-admins,DC=corp,DC=example,DC=com":     RoleAdmin,
			"CN=tdtp-activators,DC=corp,DC=example,DC=com": RoleActivator,
			"CN=tdtp-viewers,DC=corp,DC=example,DC=com":    RoleConsumer,
		},
	}
	a := NewLDAPAuthenticator(cfg)

	cases := []struct {
		name   string
		groups []string
		want   Role
		ok     bool
	}{
		{
			name:   "admin group wins",
			groups: []string{"CN=tdtp-admins,DC=corp,DC=example,DC=com", "CN=tdtp-viewers,DC=corp,DC=example,DC=com"},
			want:   RoleAdmin,
			ok:     true,
		},
		{
			name:   "activator only",
			groups: []string{"CN=tdtp-activators,DC=corp,DC=example,DC=com"},
			want:   RoleActivator,
			ok:     true,
		},
		{
			name:   "no matching group, no default",
			groups: []string{"CN=other-group,DC=corp,DC=example,DC=com"},
			want:   "",
			ok:     false,
		},
		{
			name:   "empty groups, no default",
			groups: nil,
			want:   "",
			ok:     false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, gotOK := a.roleForGroups(tc.groups)
			if gotOK != tc.ok {
				t.Errorf("ok = %v, want %v", gotOK, tc.ok)
			}
			if got != tc.want {
				t.Errorf("role = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestLDAPRoleMap_DefaultRole verifies DefaultRole is used when no group matches.
func TestLDAPRoleMap_DefaultRole(t *testing.T) {
	cfg := LDAPConfig{
		GroupAttr:   "memberOf",
		RoleMap:     map[string]Role{"CN=tdtp-admins,DC=corp,DC=example,DC=com": RoleAdmin},
		DefaultRole: RoleConsumer,
	}
	a := NewLDAPAuthenticator(cfg)

	// No matching group → falls back to DefaultRole.
	role, ok := a.roleForGroups([]string{"CN=other,DC=corp,DC=example,DC=com"})
	if !ok {
		t.Fatal("expected ok=true with DefaultRole set")
	}
	if role != RoleConsumer {
		t.Errorf("role = %q, want consumer", role)
	}

	// Admin group matches → wins over DefaultRole.
	role, ok = a.roleForGroups([]string{"CN=tdtp-admins,DC=corp,DC=example,DC=com"})
	if !ok {
		t.Fatal("expected ok=true for admin group")
	}
	if role != RoleAdmin {
		t.Errorf("role = %q, want admin", role)
	}
}

// TestLDAPMiddleware_MissingBasicAuth checks that requests without
// Authorization: Basic are rejected with 401.
func TestLDAPMiddleware_MissingBasicAuth(t *testing.T) {
	a := NewLDAPAuthenticator(LDAPConfig{URL: "ldap://127.0.0.1:1"})
	h := a.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, httptest.NewRequest(http.MethodGet, "/jobs", nil))
	if rw.Code != http.StatusUnauthorized {
		t.Errorf("no basic auth = %d, want 401", rw.Code)
	}
}

// TestLDAPMiddleware_UnreachableServer verifies that a connection refused
// to an unreachable LDAP server results in 401 (service unavailable is
// returned as auth failure to avoid leaking topology information).
func TestLDAPMiddleware_UnreachableServer(t *testing.T) {
	a := NewLDAPAuthenticator(LDAPConfig{URL: "ldap://127.0.0.1:1"})
	h := a.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Build a request with valid-looking HTTP Basic Auth credentials.
	creds := base64.StdEncoding.EncodeToString([]byte("testuser:testpass"))
	req := httptest.NewRequest(http.MethodGet, "/jobs", nil)
	req.Header.Set("Authorization", "Basic "+creds)

	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)
	// Connection refused → 401 (auth unavailable treated as auth failure).
	if rw.Code != http.StatusUnauthorized {
		t.Errorf("unreachable ldap = %d, want 401", rw.Code)
	}
}

// TestBasicAuth unit-tests the basicAuth helper directly.
func TestBasicAuth(t *testing.T) {
	cases := []struct {
		header   string
		wantUser string
		wantPass string
	}{
		{
			header:   "Basic " + base64.StdEncoding.EncodeToString([]byte("alice:s3cr3t")),
			wantUser: "alice",
			wantPass: "s3cr3t",
		},
		{
			// Password containing a colon — only first colon is split.
			header:   "Basic " + base64.StdEncoding.EncodeToString([]byte("bob:pass:word")),
			wantUser: "bob",
			wantPass: "pass:word",
		},
		{header: "", wantUser: "", wantPass: ""},
		{header: "Bearer sometoken", wantUser: "", wantPass: ""},
		{header: "Basic !!notbase64!!", wantUser: "", wantPass: ""},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if tc.header != "" {
			req.Header.Set("Authorization", tc.header)
		}
		u, p := basicAuth(req)
		if u != tc.wantUser || p != tc.wantPass {
			t.Errorf("basicAuth(%q) = (%q, %q), want (%q, %q)",
				tc.header, u, p, tc.wantUser, tc.wantPass)
		}
	}
}
