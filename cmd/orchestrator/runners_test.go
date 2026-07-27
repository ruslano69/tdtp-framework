package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeRunnersFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "runners.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write runners file: %v", err)
	}
	return path
}

func TestLoadRunners_ParsesFile(t *testing.T) {
	path := writeRunnersFile(t, `
runners:
  tdtpcli:
    binary: ./tdtpcli
    args: ["--pipeline", "{{.tmpfile}}"]
  python-etl:
    binary: python
    args: ["etl_runner.py", "--config", "{{.tmpfile}}"]
`)
	runners, err := LoadRunners(path)
	if err != nil {
		t.Fatalf("LoadRunners: %v", err)
	}
	if len(runners) != 2 {
		t.Fatalf("len = %d, want 2", len(runners))
	}
	tdtp, ok := runners["tdtpcli"]
	if !ok {
		t.Fatal("missing tdtpcli runner")
	}
	if tdtp.Binary != "./tdtpcli" || len(tdtp.Args) != 2 || tdtp.Args[1] != "{{.tmpfile}}" {
		t.Errorf("tdtpcli spec = %+v", tdtp)
	}
	py, ok := runners["python-etl"]
	if !ok || py.Binary != "python" || len(py.Args) != 3 {
		t.Errorf("python-etl spec = %+v", py)
	}
}

func TestLoadRunners_EmptyFile_Errors(t *testing.T) {
	path := writeRunnersFile(t, "runners: {}\n")
	if _, err := LoadRunners(path); err == nil {
		t.Error("expected error for a runners file defining no runners")
	}
}

func TestLoadRunners_MissingBinary_Errors(t *testing.T) {
	path := writeRunnersFile(t, "runners:\n  broken:\n    args: [\"x\"]\n")
	if _, err := LoadRunners(path); err == nil {
		t.Error("expected error for a runner with no binary")
	}
}

func TestLoadRunners_FileNotFound_Errors(t *testing.T) {
	if _, err := LoadRunners(filepath.Join(t.TempDir(), "nope.yaml")); err == nil {
		t.Error("expected error for a missing runners file")
	}
}

func TestResolveRunnerName_DeclaredOverridesDefault(t *testing.T) {
	s := &Scenario{Orchestrator: OrchestratorBlock{Name: "x", Runner: "python-etl"}}
	if got := resolveRunnerName(s, "tdtpcli"); got != "python-etl" {
		t.Errorf("got %q, want python-etl", got)
	}
}

func TestResolveRunnerName_EmptyFallsBackToDefault(t *testing.T) {
	s := &Scenario{Orchestrator: OrchestratorBlock{Name: "x"}}
	if got := resolveRunnerName(s, "tdtpcli"); got != "tdtpcli" {
		t.Errorf("got %q, want tdtpcli", got)
	}
}

func TestValidateScenarioRunners_AllKnown_Passes(t *testing.T) {
	scenes := map[string]*Scenario{
		"a": {Orchestrator: OrchestratorBlock{Name: "a"}},                   // falls back to default
		"b": {Orchestrator: OrchestratorBlock{Name: "b", Runner: "custom"}}, // explicit, known
	}
	runners := map[string]RunnerSpec{
		"tdtpcli": {Binary: "tdtpcli"},
		"custom":  {Binary: "custom-bin"},
	}
	if err := ValidateScenarioRunners(scenes, runners, "tdtpcli"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateScenarioRunners_UnknownRunner_NamesScenarioAndRunner(t *testing.T) {
	scenes := map[string]*Scenario{
		"broken": {Orchestrator: OrchestratorBlock{Name: "broken", Runner: "typo-runner"}},
	}
	runners := map[string]RunnerSpec{"tdtpcli": {Binary: "tdtpcli"}}

	err := ValidateScenarioRunners(scenes, runners, "tdtpcli")
	if err == nil {
		t.Fatal("expected error for unknown runner")
	}
	if !strings.Contains(err.Error(), "broken") || !strings.Contains(err.Error(), "typo-runner") {
		t.Errorf("error should name both scenario and runner: %v", err)
	}
}

func TestRenderArgs_SubstitutesTmpfileAndParams(t *testing.T) {
	args, err := renderArgs(
		[]string{"--config", "{{.tmpfile}}", "--period", "{{.period}}"},
		"/tmp/pipeline-123.yaml",
		map[string]string{"period": "2026-06"},
	)
	if err != nil {
		t.Fatalf("renderArgs: %v", err)
	}
	want := []string{"--config", "/tmp/pipeline-123.yaml", "--period", "2026-06"}
	for i, w := range want {
		if args[i] != w {
			t.Errorf("args[%d] = %q, want %q", i, args[i], w)
		}
	}
}

func TestRenderArgs_UnreferencedParamsDoNotError(t *testing.T) {
	// Unlike the scenario body template, an arg that references none of the
	// params must not fail just because other params exist and go unused.
	args, err := renderArgs([]string{"--pipeline", "{{.tmpfile}}"}, "/tmp/x.yaml",
		map[string]string{"period": "2026-06", "department": "50003"})
	if err != nil {
		t.Fatalf("renderArgs: %v", err)
	}
	if args[1] != "/tmp/x.yaml" {
		t.Errorf("args[1] = %q", args[1])
	}
}

func TestRenderArgs_MissingKeyReferencedByArg_Errors(t *testing.T) {
	_, err := renderArgs([]string{"--period", "{{.period}}"}, "/tmp/x.yaml", map[string]string{})
	if err == nil {
		t.Error("expected error when an arg references a param that was never provided")
	}
}

// ─── Executor integration: a scenario's declared runner is actually used ──

func TestExecutor_Submit_UsesScenarioDeclaredRunner(t *testing.T) {
	var sawBin string
	var sawArgs []string
	run := func(ctx context.Context, bin string, args ...string) ([]byte, error) {
		sawBin, sawArgs = bin, args
		return []byte("ok"), nil
	}
	e, db := newTestExecutor(t, run)
	// Register a second runner beyond the default the harness already set up.
	e.runners["python-etl"] = RunnerSpec{Binary: "python", Args: []string{"etl.py", "--period", "{{.period}}"}}

	s := &Scenario{
		Orchestrator: OrchestratorBlock{Name: "custom-run", Runner: "python-etl"},
		RawYAML:      []byte("orchestrator:\n  name: custom-run\n  runner: python-etl\nsources: []\n"),
	}
	job, err := e.Submit(s, map[string]string{"period": "2026-06"}, "", "alice")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	waitJob(t, e)

	if sawBin != "python" {
		t.Errorf("bin = %q, want python", sawBin)
	}
	if len(sawArgs) != 3 || sawArgs[0] != "etl.py" || sawArgs[1] != "--period" || sawArgs[2] != "2026-06" {
		t.Errorf("args = %v", sawArgs)
	}

	got, _ := db.GetJob(job.ID)
	if got.Runner != "python-etl" {
		t.Errorf("persisted Runner = %q, want python-etl", got.Runner)
	}
}

func TestExecutor_Submit_DefaultsToConfiguredDefaultRunner(t *testing.T) {
	var sawBin string
	run := func(ctx context.Context, bin string, args ...string) ([]byte, error) {
		sawBin = bin
		return []byte("ok"), nil
	}
	e, db := newTestExecutor(t, run)

	s := scenarioFromYAML("no-runner-declared", "orchestrator:\n  name: no-runner-declared\nsources: []\n")
	job, err := e.Submit(s, nil, "", "alice")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	waitJob(t, e)

	if sawBin != "tdtpcli-stub" {
		t.Errorf("bin = %q, want tdtpcli-stub (the harness's default runner)", sawBin)
	}
	got, _ := db.GetJob(job.ID)
	if got.Runner != defaultRunnerName {
		t.Errorf("persisted Runner = %q, want %q", got.Runner, defaultRunnerName)
	}
}

func TestExecutor_Submit_UnknownRunner_ErrorsBeforePersisting(t *testing.T) {
	called := false
	run := func(context.Context, string, ...string) ([]byte, error) {
		called = true
		return nil, nil
	}
	e, db := newTestExecutor(t, run)

	s := &Scenario{
		Orchestrator: OrchestratorBlock{Name: "bad-runner", Runner: "does-not-exist"},
		RawYAML:      []byte("orchestrator:\n  name: bad-runner\n  runner: does-not-exist\nsources: []\n"),
	}
	_, err := e.Submit(s, nil, "", "alice")
	if err == nil {
		t.Fatal("expected error for unknown runner")
	}
	if called {
		t.Error("runner must not be invoked when the declared runner is unknown")
	}

	jobs, listErr := db.ListJobs(100)
	if listErr != nil {
		t.Fatalf("ListJobs: %v", listErr)
	}
	if len(jobs) != 0 {
		t.Errorf("no job row should be persisted on unknown-runner failure, found %d", len(jobs))
	}
}
