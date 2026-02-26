package acl

import (
	"os"
	"path/filepath"
	"testing"
)

// writeTemp создаёт временный YAML-файл и возвращает его путь.
func writeTemp(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "acl.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writeTemp: %v", err)
	}
	return path
}

// --- Load ---

func TestLoad_EmptyPath_ReturnsDefaultACL(t *testing.T) {
	a, err := Load("")
	if err != nil {
		t.Fatalf("Load(\"\") error = %v", err)
	}
	if a.DefaultGroup == "" {
		t.Error("Load(\"\") DefaultGroup is empty")
	}
	if a.DefaultCost <= 0 {
		t.Errorf("Load(\"\") DefaultCost = %d, want > 0", a.DefaultCost)
	}
	if a.Pipelines == nil {
		t.Error("Load(\"\") Pipelines map is nil")
	}
}

func TestLoad_ValidYAML(t *testing.T) {
	yaml := `
default_group: "tdtp-all-users"
default_cost: 2
pipelines:
  salary-report:
    group: "tdtp-finance"
    cost: 5
  audit-log:
    group: "tdtp-admins"
    cost: 10
`
	a, err := Load(writeTemp(t, yaml))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if a.DefaultGroup != "tdtp-all-users" {
		t.Errorf("DefaultGroup = %q, want %q", a.DefaultGroup, "tdtp-all-users")
	}
	if a.DefaultCost != 2 {
		t.Errorf("DefaultCost = %d, want 2", a.DefaultCost)
	}
	if len(a.Pipelines) != 2 {
		t.Errorf("len(Pipelines) = %d, want 2", len(a.Pipelines))
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/path/acl.yaml")
	if err == nil {
		t.Error("Load() expected error for nonexistent file")
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	_, err := Load(writeTemp(t, "{ broken yaml :::"))
	if err == nil {
		t.Error("Load() expected error for invalid YAML")
	}
}

func TestLoad_ZeroDefaultCost_ForcedToOne(t *testing.T) {
	// default_cost <= 0 принудительно заменяется на 1
	yaml := `
default_group: "group"
default_cost: 0
`
	a, err := Load(writeTemp(t, yaml))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if a.DefaultCost != 1 {
		t.Errorf("DefaultCost = %d для нулевого значения, want 1", a.DefaultCost)
	}
}

func TestLoad_NegativeDefaultCost_ForcedToOne(t *testing.T) {
	yaml := `
default_group: "group"
default_cost: -5
`
	a, err := Load(writeTemp(t, yaml))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if a.DefaultCost != 1 {
		t.Errorf("DefaultCost = %d для отрицательного значения, want 1", a.DefaultCost)
	}
}

// --- Lookup ---

func TestLookup_KnownPipeline(t *testing.T) {
	yaml := `
default_group: "tdtp-all"
default_cost: 1
pipelines:
  salary-report:
    group: "tdtp-finance"
    cost: 5
`
	a, _ := Load(writeTemp(t, yaml))
	policy := a.Lookup("salary-report")

	if policy.Group != "tdtp-finance" {
		t.Errorf("Lookup().Group = %q, want %q", policy.Group, "tdtp-finance")
	}
	if policy.Cost != 5 {
		t.Errorf("Lookup().Cost = %d, want 5", policy.Cost)
	}
}

func TestLookup_UnknownPipeline_FallsToDefault(t *testing.T) {
	yaml := `
default_group: "tdtp-all"
default_cost: 3
pipelines: {}
`
	a, _ := Load(writeTemp(t, yaml))
	policy := a.Lookup("unknown-pipeline")

	if policy.Group != "tdtp-all" {
		t.Errorf("Lookup().Group = %q, want %q", policy.Group, "tdtp-all")
	}
	if policy.Cost != 3 {
		t.Errorf("Lookup().Cost = %d, want 3", policy.Cost)
	}
}

func TestLookup_ZeroCostInEntry_FallsToDefault(t *testing.T) {
	// cost=0 в записи пайплайна → используется default_cost
	yaml := `
default_group: "tdtp-all"
default_cost: 2
pipelines:
  lazy-pipeline:
    group: "tdtp-users"
    cost: 0
`
	a, _ := Load(writeTemp(t, yaml))
	policy := a.Lookup("lazy-pipeline")

	if policy.Cost != 2 {
		t.Errorf("Lookup().Cost = %d для нулевого cost в записи, want default 2", policy.Cost)
	}
}

func TestLookup_MultiplePipelines_IndependentPolicies(t *testing.T) {
	yaml := `
default_group: "tdtp-all"
default_cost: 1
pipelines:
  hr-report:
    group: "tdtp-hr"
    cost: 3
  finance-report:
    group: "tdtp-finance"
    cost: 10
`
	a, _ := Load(writeTemp(t, yaml))

	hr := a.Lookup("hr-report")
	fin := a.Lookup("finance-report")

	if hr.Group == fin.Group {
		t.Error("разные пайплайны вернули одинаковую группу")
	}
	if hr.Cost == fin.Cost {
		t.Error("разные пайплайны вернули одинаковую стоимость")
	}
}
