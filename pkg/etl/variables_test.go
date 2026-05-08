package etl

import (
	"testing"
)

// ─── ParsePipelineVars ────────────────────────────────────────────────────────

func TestParsePipelineVars_Basic(t *testing.T) {
	vars, other := ParsePipelineVars([]string{"@dept=97-256", "@year=2025"})
	if vars["dept"] != "97-256" {
		t.Errorf("dept: got %q", vars["dept"])
	}
	if vars["year"] != "2025" {
		t.Errorf("year: got %q", vars["year"])
	}
	if len(other) != 0 {
		t.Errorf("unexpected other: %v", other)
	}
}

func TestParsePipelineVars_QuotedValue(t *testing.T) {
	vars, _ := ParsePipelineVars([]string{`@dept="97-256"`})
	if vars["dept"] != "97-256" {
		t.Errorf("got %q, want 97-256", vars["dept"])
	}
}

func TestParsePipelineVars_NonVarPassthrough(t *testing.T) {
	vars, other := ParsePipelineVars([]string{"positional", "@dept=10", "@"})
	if vars["dept"] != "10" {
		t.Errorf("dept: got %q", vars["dept"])
	}
	if len(other) != 2 || other[0] != "positional" || other[1] != "@" {
		t.Errorf("other: got %v", other)
	}
}

func TestParsePipelineVars_Empty(t *testing.T) {
	vars, other := ParsePipelineVars(nil)
	if len(vars) != 0 || len(other) != 0 {
		t.Errorf("expected empty, got vars=%v other=%v", vars, other)
	}
}

// ─── substituteSQL ────────────────────────────────────────────────────────────

func TestSubstituteSQL_StringLiteral(t *testing.T) {
	vars := map[string]string{"dept": "97-256"}
	got := substituteSQL("WHERE col = '@dept'", vars)
	if got != "WHERE col = '97-256'" {
		t.Errorf("got %q", got)
	}
}

func TestSubstituteSQL_StringLiteralEscapesQuotes(t *testing.T) {
	vars := map[string]string{"name": "O'Brien"}
	got := substituteSQL("WHERE name = '@name'", vars)
	if got != "WHERE name = 'O''Brien'" {
		t.Errorf("got %q", got)
	}
}

func TestSubstituteSQL_BareNumeric(t *testing.T) {
	vars := map[string]string{"year": "2025"}
	got := substituteSQL("WHERE year = @year", vars)
	if got != "WHERE year = 2025" {
		t.Errorf("got %q", got)
	}
}

func TestSubstituteSQL_Mixed(t *testing.T) {
	vars := map[string]string{"dept": "97-256", "year": "2025"}
	sql := "WHERE col = '@dept' AND year = @year"
	got := substituteSQL(sql, vars)
	want := "WHERE col = '97-256' AND year = 2025"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSubstituteSQL_UnknownVarLeftAlone(t *testing.T) {
	vars := map[string]string{"dept": "10"}
	got := substituteSQL("WHERE a = '@dept' AND b = @other", vars)
	// @other is not in vars → left as-is
	if got != "WHERE a = '10' AND b = @other" {
		t.Errorf("got %q", got)
	}
}

func TestSubstituteSQL_Empty(t *testing.T) {
	got := substituteSQL("", map[string]string{"x": "y"})
	if got != "" {
		t.Errorf("expected empty string")
	}
}

// ─── substituteYAML ───────────────────────────────────────────────────────────

func TestSubstituteYAML_Basic(t *testing.T) {
	vars := map[string]string{"dept": "97-256"}
	got := substituteYAML("pipelines/out/dept_{{dept}}.tdtp.xml", vars)
	if got != "pipelines/out/dept_97-256.tdtp.xml" {
		t.Errorf("got %q", got)
	}
}

func TestSubstituteYAML_Multiple(t *testing.T) {
	vars := map[string]string{"dept": "10", "date": "2025-01-01"}
	got := substituteYAML("report_{{dept}}_{{date}}.tdtp.xml", vars)
	if got != "report_10_2025-01-01.tdtp.xml" {
		t.Errorf("got %q", got)
	}
}

func TestSubstituteYAML_UnknownLeftAlone(t *testing.T) {
	vars := map[string]string{"dept": "10"}
	got := substituteYAML("{{dept}}_{{other}}", vars)
	if got != "10_{{other}}" {
		t.Errorf("got %q", got)
	}
}

// ─── ApplyVariables ───────────────────────────────────────────────────────────

func makeTestConfig(query, description, destination string) *PipelineConfig {
	cfg := &PipelineConfig{
		Description: description,
		Sources: []SourceConfig{
			{Name: "data", Type: "sqlite", Query: query},
		},
		Transform: TransformConfig{SQL: "SELECT * FROM data"},
		Workspace: WorkspaceConfig{Type: "sqlite", Mode: "memory"},
		Output: OutputConfig{
			Type: "tdtp",
			TDTP: &TDTPOutputConfig{Destination: destination},
		},
	}
	return cfg
}

func TestApplyVariables_FullSubstitution(t *testing.T) {
	cfg := makeTestConfig(
		"SELECT * FROM t WHERE dept = '@dept'",
		"Список відділу {{dept}}",
		"out/dept_{{dept}}.tdtp.xml",
	)
	vars := map[string]string{"dept": "97-256"}
	warnings, err := ApplyVariables(cfg, vars)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	if cfg.Sources[0].Query != "SELECT * FROM t WHERE dept = '97-256'" {
		t.Errorf("query: got %q", cfg.Sources[0].Query)
	}
	if cfg.Description != "Список відділу 97-256" {
		t.Errorf("description: got %q", cfg.Description)
	}
	if cfg.Output.TDTP.Destination != "out/dept_97-256.tdtp.xml" {
		t.Errorf("destination: got %q", cfg.Output.TDTP.Destination)
	}
}

func TestApplyVariables_MissingVar_Error(t *testing.T) {
	cfg := makeTestConfig(
		"SELECT * FROM t WHERE dept = '@dept'",
		"", "",
	)
	_, err := ApplyVariables(cfg, map[string]string{})
	if err == nil {
		t.Fatal("expected error for missing @dept")
	}
}

func TestApplyVariables_UnusedVar_Warning(t *testing.T) {
	cfg := makeTestConfig("SELECT * FROM t", "", "")
	vars := map[string]string{"extra": "val"}
	warnings, err := ApplyVariables(cfg, vars)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(warnings) != 1 {
		t.Errorf("expected 1 warning, got %d: %v", len(warnings), warnings)
	}
}

func TestApplyVariables_NoVars_Noop(t *testing.T) {
	cfg := makeTestConfig("SELECT 1", "desc", "out.tdtp.xml")
	warnings, err := ApplyVariables(cfg, nil)
	if err != nil || len(warnings) != 0 {
		t.Errorf("expected noop: err=%v warnings=%v", err, warnings)
	}
	if cfg.Sources[0].Query != "SELECT 1" {
		t.Error("config was modified")
	}
}

func TestApplyVariables_MultipleVars(t *testing.T) {
	cfg := makeTestConfig(
		"SELECT * FROM t WHERE dept = '@dept' AND year = @year",
		"Відділ {{dept}} за {{year}}",
		"out/{{dept}}_{{year}}.tdtp.xml",
	)
	vars := map[string]string{"dept": "97-256", "year": "2025"}
	_, err := ApplyVariables(cfg, vars)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Sources[0].Query != "SELECT * FROM t WHERE dept = '97-256' AND year = 2025" {
		t.Errorf("query: got %q", cfg.Sources[0].Query)
	}
	if cfg.Description != "Відділ 97-256 за 2025" {
		t.Errorf("description: got %q", cfg.Description)
	}
	if cfg.Output.TDTP.Destination != "out/97-256_2025.tdtp.xml" {
		t.Errorf("destination: got %q", cfg.Output.TDTP.Destination)
	}
}
