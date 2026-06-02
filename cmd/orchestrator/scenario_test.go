package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeScenario(t *testing.T, dir, file, content string) string {
	t.Helper()
	path := filepath.Join(dir, file)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write scenario: %v", err)
	}
	return path
}

func TestLoadScenario_WithOrchestratorBlock(t *testing.T) {
	dir := t.TempDir()
	path := writeScenario(t, dir, "export.yaml", `
orchestrator:
  name: export-payroll
  description: "Payroll export"
  permissions: [etl, enc]
  params:
    - name: period
      required: true
      pattern: '^\d{4}-\d{2}$'
sources:
  - query: "SELECT 1"
`)
	s, err := LoadScenario(path)
	if err != nil {
		t.Fatalf("LoadScenario: %v", err)
	}
	if s.Orchestrator.Name != "export-payroll" {
		t.Errorf("name = %q", s.Orchestrator.Name)
	}
	if len(s.Orchestrator.Permissions) != 2 {
		t.Errorf("permissions = %v", s.Orchestrator.Permissions)
	}
	if len(s.Orchestrator.Params) != 1 {
		t.Errorf("params = %v", s.Orchestrator.Params)
	}
}

func TestLoadScenario_NoBlockUsesFilename(t *testing.T) {
	dir := t.TempDir()
	path := writeScenario(t, dir, "plain-export.yaml", "sources:\n  - query: \"SELECT 1\"\n")
	s, err := LoadScenario(path)
	if err != nil {
		t.Fatalf("LoadScenario: %v", err)
	}
	// No orchestrator: block → name derived from filename.
	if s.Orchestrator.Name != "plain-export" {
		t.Errorf("name = %q, want plain-export", s.Orchestrator.Name)
	}
}

func TestValidateParams_RequiredAndPattern(t *testing.T) {
	s := &Scenario{Orchestrator: OrchestratorBlock{
		Params: []ParamDef{
			{Name: "period", Required: true, Pattern: `^\d{4}-\d{2}$`},
			{Name: "dept", Required: false, Default: "ALL"},
		},
	}}

	// Valid: period matches, dept defaulted.
	got, err := s.ValidateParams(map[string]string{"period": "2026-06"})
	if err != nil {
		t.Fatalf("valid params rejected: %v", err)
	}
	if got["period"] != "2026-06" || got["dept"] != "ALL" {
		t.Errorf("resolved = %v", got)
	}

	// Missing required.
	if _, err := s.ValidateParams(map[string]string{}); err == nil {
		t.Error("missing required period should fail")
	}

	// Pattern mismatch.
	if _, err := s.ValidateParams(map[string]string{"period": "June"}); err == nil {
		t.Error("period 'June' should fail the YYYY-MM pattern")
	}
}

func TestLoadScenariosDir(t *testing.T) {
	dir := t.TempDir()
	writeScenario(t, dir, "a.yaml", "orchestrator:\n  name: alpha\nsources: []\n")
	writeScenario(t, dir, "b.yaml", "orchestrator:\n  name: beta\nsources: []\n")

	scenes, err := LoadScenariosDir(dir)
	if err != nil {
		t.Fatalf("LoadScenariosDir: %v", err)
	}
	if len(scenes) != 2 {
		t.Fatalf("loaded %d scenarios, want 2", len(scenes))
	}
	if _, ok := scenes["alpha"]; !ok {
		t.Error("alpha not loaded")
	}
	if _, ok := scenes["beta"]; !ok {
		t.Error("beta not loaded")
	}
}

func TestResolveMagicParams(t *testing.T) {
	out := resolveMagicParams(map[string]string{
		"period": "{{current_month}}",
		"static": "fixed",
	}, "")
	if out["static"] != "fixed" {
		t.Errorf("static param mutated: %q", out["static"])
	}
	// current_month → YYYY-MM (7 chars).
	if len(out["period"]) != 7 || out["period"][4] != '-' {
		t.Errorf("current_month not resolved: %q", out["period"])
	}
}

func TestResolveMagicParamsTimezone(t *testing.T) {
	// Valid timezone: Europe/Moscow — function must not panic and must return YYYY-MM.
	out := resolveMagicParams(map[string]string{"m": "{{current_month}}"}, "Europe/Moscow")
	if len(out["m"]) != 7 || out["m"][4] != '-' {
		t.Errorf("unexpected current_month with tz: %q", out["m"])
	}

	// Invalid timezone falls back to UTC silently.
	out2 := resolveMagicParams(map[string]string{"m": "{{current_month}}"}, "Invalid/Zone")
	if len(out2["m"]) != 7 {
		t.Errorf("invalid tz should fall back to UTC, got: %q", out2["m"])
	}

	// Empty timezone = UTC.
	out3 := resolveMagicParams(map[string]string{"d": "{{current_date}}"}, "")
	if len(out3["d"]) != 10 {
		t.Errorf("empty tz should give YYYY-MM-DD, got: %q", out3["d"])
	}
}
