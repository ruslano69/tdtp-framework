package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/license"
)

func proLicense(features []string, pipelines int) *license.License {
	return license.New("Test", "", "", license.TierProfessional,
		[]string{"postgres", "mssql"}, features,
		license.Limits{RowsPerExport: 0, Pipelines: pipelines})
}

func scenarioNeeding(perms ...string) *Scenario {
	return &Scenario{
		Orchestrator: OrchestratorBlock{
			Name:        "s",
			Permissions: perms,
		},
	}
}

func TestGateScenario_LicenseCovers(t *testing.T) {
	g := &TrustGate{License: proLicense([]string{"etl", "enc"}, 0)}
	if err := g.GateScenario(scenarioNeeding("etl")); err != nil {
		t.Errorf("etl scenario should pass with etl license: %v", err)
	}
	if err := g.GateScenario(scenarioNeeding("etl", "enc")); err != nil {
		t.Errorf("etl+enc scenario should pass: %v", err)
	}
}

func TestGateScenario_LicenseMissingFeature(t *testing.T) {
	g := &TrustGate{License: proLicense([]string{"etl"}, 0)}
	if err := g.GateScenario(scenarioNeeding("etl", "s3")); err == nil {
		t.Error("scenario needing s3 should fail when license lacks s3")
	}
}

func TestGateScenario_MercuryPermissionsAlsoChecked(t *testing.T) {
	// License grants etl+enc, but Mercury env only has etl → enc scenario blocked.
	g := &TrustGate{
		License:       proLicense([]string{"etl", "enc"}, 0),
		MercuryStatus: &MercuryStatus{Mode: "prod", Permissions: []string{"etl"}},
	}
	if err := g.GateScenario(scenarioNeeding("etl")); err != nil {
		t.Errorf("etl should pass (in both license and mercury): %v", err)
	}
	if err := g.GateScenario(scenarioNeeding("enc")); err == nil {
		t.Error("enc should fail: licensed but not in Mercury env permissions")
	}
}

func TestCheckPipelineLimit(t *testing.T) {
	g := &TrustGate{License: proLicense(nil, 2)}
	if err := g.CheckPipelineLimit(0); err != nil {
		t.Errorf("0 active < limit 2 should pass: %v", err)
	}
	if err := g.CheckPipelineLimit(1); err != nil {
		t.Errorf("1 active < limit 2 should pass: %v", err)
	}
	if err := g.CheckPipelineLimit(2); err == nil {
		t.Error("2 active == limit 2 should fail")
	}
}

func TestCheckPipelineLimit_Unlimited(t *testing.T) {
	g := &TrustGate{License: proLicense(nil, 0)} // 0 = unlimited
	if err := g.CheckPipelineLimit(1000); err != nil {
		t.Errorf("unlimited license should never hit limit: %v", err)
	}
}

func TestNewTrustGate_RequireProdRefusesDev(t *testing.T) {
	// Mercury reports dev mode.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"mode":"dev","dev":true,"ca_authorized":false,"permissions":[]}`))
	}))
	defer srv.Close()

	// Community license verifies trivially (no signature). require_prod must refuse.
	_, err := NewTrustGate(context.Background(), "", srv.URL, true)
	if err == nil {
		t.Fatal("NewTrustGate with require_prod must refuse a dev Mercury")
	}
}

func TestNewTrustGate_DevAllowedWithoutRequireProd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"mode":"dev","dev":true,"ca_authorized":false,"permissions":[]}`))
	}))
	defer srv.Close()

	g, err := NewTrustGate(context.Background(), "", srv.URL, false)
	if err != nil {
		t.Fatalf("dev Mercury should be allowed without require_prod: %v", err)
	}
	if g.MercuryStatus == nil || g.MercuryStatus.Mode != "dev" {
		t.Error("expected captured dev Mercury status")
	}
}

func TestNewTrustGate_NoMercurySkipsOnline(t *testing.T) {
	g, err := NewTrustGate(context.Background(), "", "", true)
	if err != nil {
		t.Fatalf("no mercury URL should skip online checks: %v", err)
	}
	if g.MercuryStatus != nil {
		t.Error("MercuryStatus should be nil when no URL given")
	}
	if !g.License.IsCommunity() {
		t.Error("expected community floor with no license file")
	}
}
