package main

import (
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
