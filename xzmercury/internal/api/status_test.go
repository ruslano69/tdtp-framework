package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeGuard implements CAGuard for status tests.
type fakeGuard struct {
	valid bool
	perms []string
}

func (f *fakeGuard) Valid() bool           { return f.valid }
func (f *fakeGuard) Permissions() []string { return f.perms }

func decodeStatus(t *testing.T, h http.HandlerFunc) map[string]any {
	t.Helper()
	rw := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	h(rw, req)
	if rw.Code != http.StatusOK {
		t.Fatalf("status code = %d, want 200", rw.Code)
	}
	var out map[string]any
	if err := json.NewDecoder(rw.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

func TestStatus_DevMode(t *testing.T) {
	// dev=true, no CA guard.
	out := decodeStatus(t, handleStatus(true, nil))
	if out["mode"] != "dev" {
		t.Errorf("mode = %v, want dev", out["mode"])
	}
	if out["dev"] != true {
		t.Errorf("dev = %v, want true", out["dev"])
	}
	if out["ca_authorized"] != false {
		t.Errorf("ca_authorized = %v, want false", out["ca_authorized"])
	}
}

func TestStatus_ProdAuthorized(t *testing.T) {
	guard := &fakeGuard{valid: true, perms: []string{"etl", "enc"}}
	out := decodeStatus(t, handleStatus(false, guard))
	if out["mode"] != "prod" {
		t.Errorf("mode = %v, want prod", out["mode"])
	}
	if out["ca_authorized"] != true {
		t.Errorf("ca_authorized = %v, want true", out["ca_authorized"])
	}
	perms, ok := out["permissions"].([]any)
	if !ok || len(perms) != 2 {
		t.Errorf("permissions = %v, want [etl enc]", out["permissions"])
	}
}

func TestStatus_ProdSessionExpired(t *testing.T) {
	// prod mode but CA session invalid → ca_authorized=false.
	guard := &fakeGuard{valid: false, perms: []string{"etl"}}
	out := decodeStatus(t, handleStatus(false, guard))
	if out["mode"] != "prod" {
		t.Errorf("mode = %v, want prod", out["mode"])
	}
	if out["ca_authorized"] != false {
		t.Errorf("ca_authorized = %v, want false (session expired)", out["ca_authorized"])
	}
}
