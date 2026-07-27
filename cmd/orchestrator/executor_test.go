package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newTestExecutor returns an executor with an injected runner and a done channel.
// captured receives the rendered pipeline content the runner saw.
func newTestExecutor(t *testing.T, run runnerFunc) (*Executor, *OrchestratorDB) {
	t.Helper()
	db, err := OpenOrchestratorDB(t.TempDir() + "/orch.db")
	if err != nil {
		t.Fatalf("OpenOrchestratorDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	e := &Executor{
		runners: map[string]RunnerSpec{
			defaultRunnerName: {Binary: "tdtpcli-stub", Args: []string{"--pipeline", "{{.tmpfile}}"}},
		},
		defaultRunner: defaultRunnerName,
		tmpDir:        t.TempDir(),
		db:            db,
		run:           run,
		done:          make(chan string, 1),
		registry:      make(map[string]*runningJob),
	}
	return e, db
}

func waitJob(t *testing.T, e *Executor) string {
	t.Helper()
	select {
	case id := <-e.done:
		return id
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for job completion")
		return ""
	}
}

func scenarioFromYAML(name, yaml string) *Scenario {
	return &Scenario{
		Orchestrator: OrchestratorBlock{Name: name},
		RawYAML:      []byte(yaml),
	}
}

func TestExecutor_RendersParamsAndPersists(t *testing.T) {
	var sawPipeline string
	run := func(ctx context.Context, bin string, args ...string) ([]byte, error) {
		// args = ["--pipeline", path]; read the rendered file to verify substitution.
		if len(args) == 2 {
			data, _ := os.ReadFile(args[1])
			sawPipeline = string(data)
		}
		return []byte("pipeline ok"), nil
	}
	e, db := newTestExecutor(t, run)

	s := scenarioFromYAML("export", `
orchestrator:
  name: export
sources:
  - query: "SELECT * FROM Payroll WHERE Period = '{{.period}}'"
`)

	job, err := e.Submit(s, map[string]string{"period": "2026-06"}, "", "")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	jobID := waitJob(t, e)
	if jobID != job.ID {
		t.Errorf("done channel job = %s, want %s", jobID, job.ID)
	}

	// Template substitution happened before the runner saw the file.
	if !strings.Contains(sawPipeline, "Period = '2026-06'") {
		t.Errorf("param not substituted; runner saw: %s", sawPipeline)
	}
	if strings.Contains(sawPipeline, "{{.period}}") {
		t.Error("unsubstituted {{.period}} reached the runner")
	}

	// Job persisted as done with captured log.
	got, err := db.GetJob(job.ID)
	if err != nil || got == nil {
		t.Fatalf("GetJob: %v", err)
	}
	if got.Status != JobDone {
		t.Errorf("status = %s, want done", got.Status)
	}
	if !strings.Contains(got.Log, "pipeline ok") {
		t.Errorf("log = %q, want to contain 'pipeline ok'", got.Log)
	}
	if got.FinishedAt == nil {
		t.Error("FinishedAt not set on completed job")
	}
}

func TestExecutor_FailedRunRecordsError(t *testing.T) {
	run := func(ctx context.Context, bin string, args ...string) ([]byte, error) {
		return []byte("boom output"), &exitError{msg: "exit status 1"}
	}
	e, db := newTestExecutor(t, run)

	s := scenarioFromYAML("failing", "orchestrator:\n  name: failing\nsources: []\n")
	job, err := e.Submit(s, nil, "", "")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	waitJob(t, e)

	got, _ := db.GetJob(job.ID)
	if got.Status != JobFailed {
		t.Errorf("status = %s, want failed", got.Status)
	}
	if got.Error == "" {
		t.Error("Error not recorded on failed job")
	}
	if !strings.Contains(got.Log, "boom output") {
		t.Errorf("failed job log should still capture output, got %q", got.Log)
	}
}

func TestExecutor_MissingParamFailsBeforeRun(t *testing.T) {
	called := false
	run := func(ctx context.Context, bin string, args ...string) ([]byte, error) {
		called = true
		return nil, nil
	}
	e, _ := newTestExecutor(t, run)

	// Scenario references {{.period}} but we pass no params → render error.
	s := scenarioFromYAML("x", "query: '{{.period}}'\n")
	_, err := e.Submit(s, map[string]string{}, "", "")
	if err == nil {
		t.Fatal("Submit should fail when a referenced param is missing")
	}
	if called {
		t.Error("runner must not be called when rendering fails")
	}
}

func TestExecutor_ScheduledRunCarriesScheduleID(t *testing.T) {
	run := func(ctx context.Context, bin string, args ...string) ([]byte, error) {
		return []byte("ok"), nil
	}
	e, db := newTestExecutor(t, run)

	s := scenarioFromYAML("nightly", "orchestrator:\n  name: nightly\nsources: []\n")
	job, err := e.Submit(s, nil, "sched-123", "")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	waitJob(t, e)

	got, _ := db.GetJob(job.ID)
	if got.ScheduleID != "sched-123" {
		t.Errorf("ScheduleID = %q, want sched-123", got.ScheduleID)
	}
}

func TestExecutor_CountActiveJobs(t *testing.T) {
	// A blocking runner keeps one job in 'running' until released.
	release := make(chan struct{})
	run := func(ctx context.Context, bin string, args ...string) ([]byte, error) {
		<-release
		return []byte("ok"), nil
	}
	e, db := newTestExecutor(t, run)

	s := scenarioFromYAML("slow", "orchestrator:\n  name: slow\nsources: []\n")
	_, err := e.Submit(s, nil, "", "")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	// Give the goroutine a moment to flip status to running.
	deadline := time.Now().Add(2 * time.Second)
	var active int
	for time.Now().Before(deadline) {
		active, _ = db.CountActiveJobs()
		if active >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if active < 1 {
		t.Error("expected at least 1 active job while runner blocks")
	}

	close(release)
	waitJob(t, e)

	active, _ = db.CountActiveJobs()
	if active != 0 {
		t.Errorf("active jobs after completion = %d, want 0", active)
	}
}

func TestJobArtifact(t *testing.T) {
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "output.tdtp.xml")
	fileContent := []byte("artifact content for testing 12345")

	run := func(ctx context.Context, bin string, args ...string) ([]byte, error) {
		// Write a small output file to simulate tdtpcli producing an artifact.
		if err := os.WriteFile(outputPath, fileContent, 0o600); err != nil {
			return nil, err
		}
		return []byte("export done"), nil
	}
	e, db := newTestExecutor(t, run)

	// Build a scenario YAML that references the output path.
	yamlSrc := "orchestrator:\n  name: artifact-test\noutput: " + outputPath + "\n"
	s := scenarioFromYAML("artifact-test", yamlSrc)

	job, err := e.Submit(s, nil, "", "")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	waitJob(t, e)

	got, err := db.GetJob(job.ID)
	if err != nil || got == nil {
		t.Fatalf("GetJob: %v", err)
	}
	if got.Status != JobDone {
		t.Errorf("status = %s, want done", got.Status)
	}
	if got.ArtifactSHA256 == "" {
		t.Fatal("ArtifactSHA256 is empty — artifact not recorded")
	}
	if got.ArtifactSize <= 0 {
		t.Errorf("ArtifactSize = %d, want > 0", got.ArtifactSize)
	}
	if got.ArtifactPath != outputPath {
		t.Errorf("ArtifactPath = %q, want %q", got.ArtifactPath, outputPath)
	}

	// Verify SHA-256 matches the file content.
	h := sha256.Sum256(fileContent)
	wantSHA256 := hex.EncodeToString(h[:])
	if got.ArtifactSHA256 != wantSHA256 {
		t.Errorf("ArtifactSHA256 = %q, want %q", got.ArtifactSHA256, wantSHA256)
	}
}

// exitError is a stand-in for a non-zero exit from the runner.
type exitError struct{ msg string }

func (e *exitError) Error() string { return e.msg }
