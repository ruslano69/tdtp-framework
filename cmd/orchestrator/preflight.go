package main

// preflight.go — trust-chain integration for the orchestrator.
//
// The orchestrator sits above tdtpcli and xZMercury. Before running scenarios
// it verifies two things:
//
//  1. OFFLINE: its own tdtp.lic license — gates which scenarios may run
//     (by scenario.permissions ⊆ license features) and how many may run at once
//     (license pipeline limit).
//
//  2. ONLINE: the xZMercury it will use — that Mercury is in prod mode (not dev)
//     and CA-authorized, and that Mercury's licensed permissions cover the
//     scenarios. Queried via Mercury's /status endpoint.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/ruslano69/tdtp-framework/pkg/license"
)

// MercuryStatus mirrors xZMercury's GET /status response.
type MercuryStatus struct {
	Mode         string   `json:"mode"` // "dev" | "prod"
	Dev          bool     `json:"dev"`
	CAAuthorized bool     `json:"ca_authorized"`
	Permissions  []string `json:"permissions"`
}

// FetchMercuryStatus queries Mercury's /status endpoint.
func FetchMercuryStatus(ctx context.Context, mercuryURL string) (*MercuryStatus, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, mercuryURL+"/status", nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mercury status: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mercury status: HTTP %d", resp.StatusCode)
	}
	var st MercuryStatus
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		return nil, fmt.Errorf("mercury status decode: %w", err)
	}
	return &st, nil
}

// TrustGate holds the orchestrator's verified trust state.
type TrustGate struct {
	License       *license.License
	MercuryStatus *MercuryStatus // nil if Mercury not configured
	requireProd   bool
}

// NewTrustGate resolves the license and (optionally) preflights Mercury.
//
// licensePath: path to tdtp.lic ("" → env/./tdtp.lic → community floor).
// mercuryURL:  Mercury base URL ("" → skip online checks).
// requireProd: if true, refuse to start against a dev-mode Mercury.
func NewTrustGate(ctx context.Context, licensePath, mercuryURL string, requireProd bool) (*TrustGate, error) {
	// 1. OFFLINE: resolve + verify own license.
	path := licensePath
	if path == "" {
		path = resolveDefaultLicensePath()
	}
	lic, err := license.Load(path)
	if err != nil {
		return nil, fmt.Errorf("license load: %w", err)
	}
	if err := lic.Verify(); err != nil {
		return nil, fmt.Errorf("license verification failed: %w", err)
	}

	g := &TrustGate{License: lic, requireProd: requireProd}

	// 2. ONLINE: preflight Mercury if configured.
	if mercuryURL != "" {
		st, err := FetchMercuryStatus(ctx, mercuryURL)
		if err != nil {
			return nil, fmt.Errorf("mercury preflight: %w", err)
		}
		g.MercuryStatus = st

		if requireProd && st.Dev {
			return nil, fmt.Errorf(
				"mercury is in DEV mode but require_prod is set — refusing to start")
		}
		if requireProd && !st.CAAuthorized {
			return nil, fmt.Errorf(
				"mercury is not CA-authorized but require_prod is set — refusing to start")
		}
	}

	return g, nil
}

// GateScenario checks that a scenario's required permissions are covered by both
// the orchestrator license and (if configured) the Mercury environment.
// Returns nil when the scenario may run.
func (g *TrustGate) GateScenario(s *Scenario) error {
	for _, perm := range s.Orchestrator.Permissions {
		if !g.License.AllowsFeature(perm) {
			return fmt.Errorf("scenario %q requires permission %q not granted by license (tier=%s)",
				s.Orchestrator.Name, perm, g.License.GetTier())
		}
		if g.MercuryStatus != nil && !containsPerm(g.MercuryStatus.Permissions, perm) {
			return fmt.Errorf("scenario %q requires permission %q not in Mercury env permissions %v",
				s.Orchestrator.Name, perm, g.MercuryStatus.Permissions)
		}
	}
	return nil
}

// CheckPipelineLimit returns an error if activeJobs has reached the licensed limit.
// A limit of 0 means unlimited.
func (g *TrustGate) CheckPipelineLimit(activeJobs int) error {
	limit := g.License.PipelineLimit()
	if limit == 0 || activeJobs < limit {
		return nil
	}
	return fmt.Errorf("concurrent pipeline limit reached (%d/%d); licensed tier=%s",
		activeJobs, limit, g.License.GetTier())
}

func resolveDefaultLicensePath() string {
	// Mirror tdtpcli precedence: env → ./tdtp.lic → community floor.
	if env := os.Getenv("TDTP_LICENSE"); env != "" {
		return env
	}
	if _, err := os.Stat("tdtp.lic"); err == nil {
		return "tdtp.lic"
	}
	return ""
}

func containsPerm(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
